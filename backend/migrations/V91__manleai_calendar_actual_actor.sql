-- Align ManleAI Calendar database audit guards with the already-authorized
-- Tenant and Platform technical-write surfaces. Every row keeps the actual
-- actor; no Platform actor is rewritten as the salon owner.

CREATE OR REPLACE FUNCTION public.app_manleai_calendar_write_access(
    target_salon_id UUID,
    target_actor_id UUID
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = public, pg_temp
AS $$
    SELECT target_salon_id IS NOT NULL
       AND target_actor_id IS NOT NULL
       AND (
            public.has_active_tenant_membership(target_salon_id, target_actor_id)
            OR public.has_platform_salon_capability(
                target_salon_id,
                target_actor_id,
                'technical.write'
            )
       )
$$;

REVOKE ALL ON FUNCTION public.app_manleai_calendar_write_access(UUID, UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_manleai_calendar_write_access(UUID, UUID) TO PUBLIC;

COMMENT ON FUNCTION public.app_manleai_calendar_write_access(UUID, UUID) IS
'Authorization predicate for ManleAI Calendar mutations. It preserves the actual Tenant or Platform actor and matches the repository technical-write boundary.';

CREATE OR REPLACE FUNCTION enforce_manleai_calendar_config_write()
RETURNS TRIGGER AS $$
DECLARE
    policy_fields_changed BOOLEAN;
    activation_requested BOOLEAN;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.version <> 1 THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'new ManleAI calendar configurations start at version one',
                CONSTRAINT = 'manleai_calendar_configs_version_guard';
        END IF;

        IF NEW.activated_at IS NOT NULL
           OR NEW.activated_by_user_id IS NOT NULL
           OR NEW.activated_version IS NOT NULL THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI calendar configuration activation requires an explicit update',
                CONSTRAINT = 'manleai_calendar_configs_activation_version_guard';
        END IF;
    ELSE
        IF OLD.salon_id IS DISTINCT FROM NEW.salon_id THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI calendar configuration salon is immutable',
                CONSTRAINT = 'manleai_calendar_configs_salon_guard';
        END IF;

        activation_requested := OLD.activated_at IS DISTINCT FROM NEW.activated_at
            OR OLD.activated_by_user_id IS DISTINCT FROM NEW.activated_by_user_id;

        IF NOT activation_requested
           AND OLD.activated_version IS DISTINCT FROM NEW.activated_version THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI calendar activation version is database-managed',
                CONSTRAINT = 'manleai_calendar_configs_activation_version_guard';
        END IF;

        policy_fields_changed := ROW(
            OLD.slot_step_minutes,
            OLD.minimum_booking_notice_minutes,
            OLD.booking_horizon_days,
            OLD.reschedule_cutoff_minutes,
            OLD.cancellation_cutoff_minutes,
            OLD.max_party_size,
            OLD.default_buffer_before_minutes,
            OLD.default_buffer_after_minutes
        ) IS DISTINCT FROM ROW(
            NEW.slot_step_minutes,
            NEW.minimum_booking_notice_minutes,
            NEW.booking_horizon_days,
            NEW.reschedule_cutoff_minutes,
            NEW.cancellation_cutoff_minutes,
            NEW.max_party_size,
            NEW.default_buffer_before_minutes,
            NEW.default_buffer_after_minutes
        );

        IF activation_requested THEN
            IF NEW.activated_at IS NULL OR NEW.activated_by_user_id IS NULL THEN
                RAISE EXCEPTION USING
                    ERRCODE = '23514',
                    MESSAGE = 'ManleAI calendar activation evidence cannot be cleared',
                    CONSTRAINT = 'manleai_calendar_configs_activation_version_guard';
            END IF;

            NEW.version := OLD.version + 1;
            NEW.activated_version := NEW.version;
            NEW.updated_at := now();
        ELSIF policy_fields_changed THEN
            NEW.version := OLD.version + 1;
            NEW.updated_at := now();
        ELSIF OLD.version IS DISTINCT FROM NEW.version THEN
            IF pg_trigger_depth() <= 1 OR NEW.version <> OLD.version + 1 THEN
                RAISE EXCEPTION USING
                    ERRCODE = '23514',
                    MESSAGE = 'ManleAI calendar configuration version is database-managed',
                    CONSTRAINT = 'manleai_calendar_configs_version_guard';
            END IF;
        ELSIF OLD.updated_at IS DISTINCT FROM NEW.updated_at THEN
            NEW.version := OLD.version + 1;
            NEW.updated_at := now();
        END IF;
    END IF;

    IF NEW.activated_by_user_id IS NOT NULL
       AND NOT public.app_manleai_calendar_write_access(
           NEW.salon_id,
           NEW.activated_by_user_id
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar activation actor is not authorized',
            CONSTRAINT = 'manleai_calendar_configs_activation_actor_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_manleai_calendar_exception_write()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF pg_trigger_depth() > 1 THEN
            RETURN OLD;
        END IF;

        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar exceptions must be cancelled instead of deleted',
            CONSTRAINT = 'manleai_calendar_exceptions_delete_guard';
    END IF;

    IF TG_OP = 'INSERT' THEN
        IF NOT public.app_manleai_calendar_write_access(
            NEW.salon_id,
            NEW.created_by_user_id
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI calendar exception creator is not authorized',
                CONSTRAINT = 'manleai_calendar_exceptions_creator_actor_guard';
        END IF;
        RETURN NEW;
    END IF;

    IF ROW(
        OLD.id,
        OLD.salon_id,
        OLD.scope_type,
        OLD.staff_id,
        OLD.resource_pool_id,
        OLD.effect,
        OLD.starts_at,
        OLD.ends_at,
        OLD.capacity_override,
        OLD.reason,
        OLD.created_by_user_id,
        OLD.created_at
    ) IS DISTINCT FROM ROW(
        NEW.id,
        NEW.salon_id,
        NEW.scope_type,
        NEW.staff_id,
        NEW.resource_pool_id,
        NEW.effect,
        NEW.starts_at,
        NEW.ends_at,
        NEW.capacity_override,
        NEW.reason,
        NEW.created_by_user_id,
        NEW.created_at
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar exception core is immutable',
            CONSTRAINT = 'manleai_calendar_exceptions_immutable_guard';
    END IF;

    IF OLD.cancelled_at IS NOT NULL AND (
        OLD.cancelled_at IS DISTINCT FROM NEW.cancelled_at
        OR OLD.cancelled_by_user_id IS DISTINCT FROM NEW.cancelled_by_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar exception cancellation is immutable',
            CONSTRAINT = 'manleai_calendar_exceptions_cancellation_immutable_guard';
    END IF;

    IF NEW.cancelled_by_user_id IS NOT NULL
       AND NOT public.app_manleai_calendar_write_access(
           NEW.salon_id,
           NEW.cancelled_by_user_id
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar exception cancellation actor is not authorized',
            CONSTRAINT = 'manleai_calendar_exceptions_cancellation_actor_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_manleai_calendar_config_event_actor()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT public.app_manleai_calendar_write_access(
        NEW.salon_id,
        NEW.actor_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar configuration event actor is not authorized',
            CONSTRAINT = 'manleai_calendar_config_events_actor_guard';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM manleai_calendar_configs config
        WHERE config.salon_id = NEW.salon_id
          AND config.version = NEW.result_version
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar configuration event result version must equal the current configuration version',
            CONSTRAINT = 'manleai_calendar_config_events_result_version_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS manleai_calendar_config_events_actor_owner_guard
    ON manleai_calendar_config_events;
DROP TRIGGER IF EXISTS manleai_calendar_config_events_actor_guard
    ON manleai_calendar_config_events;
CREATE TRIGGER manleai_calendar_config_events_actor_guard
BEFORE INSERT ON manleai_calendar_config_events
FOR EACH ROW
EXECUTE FUNCTION enforce_manleai_calendar_config_event_actor();
