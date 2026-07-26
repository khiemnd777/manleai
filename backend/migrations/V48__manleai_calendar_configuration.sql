CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE salon_settings
    ADD COLUMN scheduling_authority_version BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT salon_settings_scheduling_authority_version_check
        CHECK (scheduling_authority_version >= 1);

CREATE FUNCTION enforce_scheduling_authority_version()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority THEN
        NEW.scheduling_authority_version := OLD.scheduling_authority_version + 1;
        RETURN NEW;
    END IF;

    IF OLD.scheduling_authority_version IS DISTINCT FROM NEW.scheduling_authority_version THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority version is database-managed',
            CONSTRAINT = 'salon_settings_scheduling_authority_version_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER salon_settings_scheduling_authority_version_guard
BEFORE UPDATE OF scheduling_authority, scheduling_authority_version ON salon_settings
FOR EACH ROW
EXECUTE FUNCTION enforce_scheduling_authority_version();

ALTER TABLE salon_business_hour_periods
    DROP CONSTRAINT salon_business_hour_periods_check,
    ADD COLUMN end_at_midnight BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT salon_business_hour_periods_range_check
        CHECK (
            (
                end_at_midnight = false
                AND end_local_time > start_local_time
            )
            OR (
                end_at_midnight = true
                AND end_local_time = TIME '00:00:00'
            )
        ),
    ADD CONSTRAINT salon_business_hour_periods_index_check
        CHECK (provider_period_index >= 0),
    ADD CONSTRAINT salon_business_hour_periods_local_override_shape_check
        CHECK (
            source <> 'local_override'
            OR (
                provider = ''
                AND provider_location_id = ''
                AND provider_period_index >= 1
                AND last_synced_at IS NULL
                AND extract(second FROM start_local_time) = 0
                AND extract(second FROM end_local_time) = 0
            )
        );

ALTER TABLE salon_business_hour_periods
    ADD CONSTRAINT salon_business_hour_periods_local_override_no_overlap
    EXCLUDE USING gist (
        salon_id WITH =,
        day_of_week WITH =,
        (int4range(
            (extract(epoch FROM start_local_time) / 60)::INTEGER,
            CASE
                WHEN end_at_midnight THEN 1440
                ELSE (extract(epoch FROM end_local_time) / 60)::INTEGER
            END,
            '[)'
        )) WITH &&
    )
    WHERE (source = 'local_override')
    DEFERRABLE INITIALLY IMMEDIATE;

CREATE TABLE manleai_calendar_configs (
    salon_id UUID PRIMARY KEY REFERENCES salons(id) ON DELETE CASCADE,
    version BIGINT NOT NULL DEFAULT 1,
    slot_step_minutes SMALLINT NOT NULL,
    minimum_booking_notice_minutes INTEGER NOT NULL,
    booking_horizon_days SMALLINT NOT NULL,
    reschedule_cutoff_minutes INTEGER,
    cancellation_cutoff_minutes INTEGER,
    max_party_size SMALLINT NOT NULL,
    default_buffer_before_minutes SMALLINT NOT NULL DEFAULT 0,
    default_buffer_after_minutes SMALLINT NOT NULL DEFAULT 0,
    activated_at TIMESTAMPTZ,
    activated_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    activated_version BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_configs_version_check
        CHECK (version >= 1),
    CONSTRAINT manleai_calendar_configs_slot_step_check
        CHECK (
            slot_step_minutes BETWEEN 1 AND 1440
            AND mod(1440, slot_step_minutes) = 0
        ),
    CONSTRAINT manleai_calendar_configs_minimum_notice_check
        CHECK (minimum_booking_notice_minutes BETWEEN 0 AND 525600),
    CONSTRAINT manleai_calendar_configs_horizon_check
        CHECK (booking_horizon_days BETWEEN 1 AND 366),
    CONSTRAINT manleai_calendar_configs_reschedule_cutoff_check
        CHECK (
            reschedule_cutoff_minutes IS NULL
            OR reschedule_cutoff_minutes BETWEEN 0 AND 525600
        ),
    CONSTRAINT manleai_calendar_configs_cancellation_cutoff_check
        CHECK (
            cancellation_cutoff_minutes IS NULL
            OR cancellation_cutoff_minutes BETWEEN 0 AND 525600
        ),
    CONSTRAINT manleai_calendar_configs_party_size_check
        CHECK (max_party_size BETWEEN 1 AND 100),
    CONSTRAINT manleai_calendar_configs_default_buffer_before_check
        CHECK (default_buffer_before_minutes BETWEEN 0 AND 1440),
    CONSTRAINT manleai_calendar_configs_default_buffer_after_check
        CHECK (default_buffer_after_minutes BETWEEN 0 AND 1440),
    CONSTRAINT manleai_calendar_configs_activation_triple_check
        CHECK (
            (
                activated_at IS NULL
                AND activated_by_user_id IS NULL
                AND activated_version IS NULL
            )
            OR (
                activated_at IS NOT NULL
                AND activated_by_user_id IS NOT NULL
                AND activated_version >= 1
            )
        ),
    CONSTRAINT manleai_calendar_configs_activation_version_check
        CHECK (activated_version IS NULL OR activated_version <= version)
);

CREATE TABLE manleai_calendar_service_policies (
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    service_id UUID NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    capacity_mode TEXT,
    buffer_before_minutes SMALLINT,
    buffer_after_minutes SMALLINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, service_id),
    CONSTRAINT manleai_calendar_service_policies_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_service_policies_capacity_mode_check
        CHECK (capacity_mode IS NULL OR capacity_mode IN ('staff_only', 'pooled')),
    CONSTRAINT manleai_calendar_service_policies_enabled_mode_check
        CHECK (enabled = false OR capacity_mode IS NOT NULL),
    CONSTRAINT manleai_calendar_service_policies_buffer_before_check
        CHECK (buffer_before_minutes IS NULL OR buffer_before_minutes BETWEEN 0 AND 1440),
    CONSTRAINT manleai_calendar_service_policies_buffer_after_check
        CHECK (buffer_after_minutes IS NULL OR buffer_after_minutes BETWEEN 0 AND 1440)
);

CREATE TABLE manleai_calendar_service_staff (
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    service_id UUID NOT NULL,
    staff_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, service_id, staff_id),
    CONSTRAINT manleai_calendar_service_staff_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_service_staff_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idx_manleai_calendar_service_staff_staff
    ON manleai_calendar_service_staff(salon_id, staff_id, service_id);

CREATE TABLE manleai_calendar_staff_weekly_periods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    staff_id UUID NOT NULL,
    day_of_week SMALLINT NOT NULL,
    start_minute SMALLINT NOT NULL,
    end_minute SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_staff_weekly_periods_salon_id_id_key
        UNIQUE (salon_id, id),
    CONSTRAINT manleai_calendar_staff_weekly_periods_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_staff_weekly_periods_day_check
        CHECK (day_of_week BETWEEN 0 AND 6),
    CONSTRAINT manleai_calendar_staff_weekly_periods_range_check
        CHECK (
            start_minute BETWEEN 0 AND 1439
            AND end_minute BETWEEN 1 AND 1440
            AND end_minute > start_minute
        ),
    CONSTRAINT manleai_calendar_staff_weekly_periods_no_overlap
        EXCLUDE USING gist (
            salon_id WITH =,
            staff_id WITH =,
            day_of_week WITH =,
            (int4range(start_minute, end_minute, '[)')) WITH &&
        )
        DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX idx_manleai_calendar_staff_weekly_periods_lookup
    ON manleai_calendar_staff_weekly_periods(salon_id, staff_id, day_of_week, start_minute);

CREATE TABLE manleai_calendar_resource_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    capacity INTEGER NOT NULL,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_resource_pools_salon_id_id_key
        UNIQUE (salon_id, id),
    CONSTRAINT manleai_calendar_resource_pools_name_check
        CHECK (length(trim(name)) BETWEEN 1 AND 200),
    CONSTRAINT manleai_calendar_resource_pools_capacity_check
        CHECK (capacity BETWEEN 1 AND 1000)
);

CREATE UNIQUE INDEX idx_manleai_calendar_resource_pools_active_name
    ON manleai_calendar_resource_pools(salon_id, lower(trim(name)))
    WHERE archived_at IS NULL;

CREATE TABLE manleai_calendar_service_resources (
    salon_id UUID NOT NULL,
    service_id UUID NOT NULL,
    resource_pool_id UUID NOT NULL,
    units_required INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, service_id, resource_pool_id),
    CONSTRAINT manleai_calendar_service_resources_policy_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES manleai_calendar_service_policies(salon_id, service_id)
        ON DELETE CASCADE,
    CONSTRAINT manleai_calendar_service_resources_pool_tenant_fk
        FOREIGN KEY (salon_id, resource_pool_id)
        REFERENCES manleai_calendar_resource_pools(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_service_resources_units_check
        CHECK (units_required BETWEEN 1 AND 1000)
);

CREATE INDEX idx_manleai_calendar_service_resources_pool
    ON manleai_calendar_service_resources(salon_id, resource_pool_id, service_id);

CREATE TABLE manleai_calendar_exceptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL,
    staff_id UUID,
    resource_pool_id UUID,
    effect TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    capacity_override INTEGER,
    reason TEXT,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    cancelled_at TIMESTAMPTZ,
    cancelled_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_exceptions_salon_id_id_key
        UNIQUE (salon_id, id),
    CONSTRAINT manleai_calendar_exceptions_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_exceptions_resource_pool_tenant_fk
        FOREIGN KEY (salon_id, resource_pool_id)
        REFERENCES manleai_calendar_resource_pools(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_exceptions_range_check
        CHECK (ends_at > starts_at),
    CONSTRAINT manleai_calendar_exceptions_scope_effect_check
        CHECK (
            (
                scope_type = 'salon'
                AND staff_id IS NULL
                AND resource_pool_id IS NULL
                AND effect IN ('available', 'unavailable')
                AND capacity_override IS NULL
            )
            OR (
                scope_type = 'staff'
                AND staff_id IS NOT NULL
                AND resource_pool_id IS NULL
                AND effect IN ('available', 'unavailable')
                AND capacity_override IS NULL
            )
            OR (
                scope_type = 'resource'
                AND staff_id IS NULL
                AND resource_pool_id IS NOT NULL
                AND effect = 'capacity_override'
                AND capacity_override BETWEEN 0 AND 1000
            )
        ),
    CONSTRAINT manleai_calendar_exceptions_reason_check
        CHECK (reason IS NULL OR length(trim(reason)) BETWEEN 1 AND 2000),
    CONSTRAINT manleai_calendar_exceptions_cancellation_pair_check
        CHECK (
            (cancelled_at IS NULL AND cancelled_by_user_id IS NULL)
            OR (
                cancelled_at IS NOT NULL
                AND cancelled_by_user_id IS NOT NULL
                AND cancelled_at >= created_at
            )
        ),
    CONSTRAINT manleai_calendar_exceptions_no_overlap
        EXCLUDE USING gist (
            salon_id WITH =,
            scope_type WITH =,
            (COALESCE(staff_id, resource_pool_id, salon_id)) WITH =,
            (tstzrange(starts_at, ends_at, '[)')) WITH &&
        )
        WHERE (cancelled_at IS NULL)
        DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX idx_manleai_calendar_exceptions_active_range
    ON manleai_calendar_exceptions(salon_id, starts_at, ends_at)
    WHERE cancelled_at IS NULL;

CREATE TABLE manleai_calendar_config_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES manleai_calendar_configs(salon_id) ON DELETE CASCADE,
    action_key TEXT NOT NULL,
    action_fingerprint TEXT NOT NULL,
    event_type TEXT NOT NULL,
    previous_version BIGINT NOT NULL,
    result_version BIGINT NOT NULL,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_config_events_action_key
        UNIQUE (salon_id, action_key),
    CONSTRAINT manleai_calendar_config_events_action_key_check
        CHECK (length(trim(action_key)) BETWEEN 1 AND 256),
    CONSTRAINT manleai_calendar_config_events_fingerprint_check
        CHECK (action_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT manleai_calendar_config_events_type_check
        CHECK (event_type IN (
            'config_created',
            'config_updated',
            'config_activated',
            'salon_hours_replaced',
            'staff_schedule_replaced',
            'service_policy_updated',
            'resource_pool_created',
            'resource_pool_updated',
            'resource_pool_archived',
            'exception_created',
            'exception_cancelled'
        )),
    CONSTRAINT manleai_calendar_config_events_version_check
        CHECK (
            previous_version >= 0
            AND result_version >= 1
            AND result_version > previous_version
        ),
    CONSTRAINT manleai_calendar_config_events_payload_check
        CHECK (
            jsonb_typeof(payload) = 'object'
            AND octet_length(payload::text) <= 16384
        )
);

CREATE INDEX idx_manleai_calendar_config_events_salon_created
    ON manleai_calendar_config_events(salon_id, created_at ASC, id ASC);

CREATE FUNCTION enforce_manleai_calendar_config_write()
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

    IF NEW.activated_by_user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM salons salon
        WHERE salon.id = NEW.salon_id
          AND salon.owner_user_id = NEW.activated_by_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar activation actor must own the salon',
            CONSTRAINT = 'manleai_calendar_configs_activation_actor_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manleai_calendar_configs_write_guard
BEFORE INSERT OR UPDATE ON manleai_calendar_configs
FOR EACH ROW
EXECUTE FUNCTION enforce_manleai_calendar_config_write();

CREATE FUNCTION bump_manleai_calendar_config_version()
RETURNS TRIGGER AS $$
DECLARE
    old_salon_id UUID;
    new_salon_id UUID;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_salon_id := OLD.salon_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        new_salon_id := NEW.salon_id;
    END IF;

    IF old_salon_id IS NOT NULL THEN
        UPDATE manleai_calendar_configs
        SET version = version + 1,
            updated_at = now()
        WHERE salon_id = old_salon_id;
    END IF;

    IF new_salon_id IS NOT NULL AND new_salon_id IS DISTINCT FROM old_salon_id THEN
        UPDATE manleai_calendar_configs
        SET version = version + 1,
            updated_at = now()
        WHERE salon_id = new_salon_id;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION bump_manleai_calendar_config_for_local_hours()
RETURNS TRIGGER AS $$
DECLARE
    old_salon_id UUID;
    new_salon_id UUID;
BEGIN
    IF TG_OP <> 'INSERT' AND OLD.source = 'local_override' THEN
        old_salon_id := OLD.salon_id;
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.source = 'local_override' THEN
        new_salon_id := NEW.salon_id;
    END IF;

    IF old_salon_id IS NOT NULL THEN
        UPDATE manleai_calendar_configs
        SET version = version + 1,
            updated_at = now()
        WHERE salon_id = old_salon_id;
    END IF;

    IF new_salon_id IS NOT NULL AND new_salon_id IS DISTINCT FROM old_salon_id THEN
        UPDATE manleai_calendar_configs
        SET version = version + 1,
            updated_at = now()
        WHERE salon_id = new_salon_id;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION bump_manleai_calendar_config_for_timezone()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE manleai_calendar_configs
    SET version = version + 1,
        updated_at = now()
    WHERE salon_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER salon_business_hour_periods_manleai_calendar_version
AFTER INSERT OR UPDATE OR DELETE ON salon_business_hour_periods
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_for_local_hours();

CREATE TRIGGER manleai_calendar_service_policies_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_service_policies
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER manleai_calendar_service_staff_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_service_staff
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER manleai_calendar_staff_weekly_periods_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_staff_weekly_periods
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER manleai_calendar_resource_pools_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_resource_pools
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER manleai_calendar_service_resources_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_service_resources
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER manleai_calendar_exceptions_version
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_exceptions
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER services_manleai_calendar_version_insert_delete
AFTER INSERT OR DELETE ON services
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER services_manleai_calendar_version_update
AFTER UPDATE OF duration_minutes, active, ai_bookable, archived_at ON services
FOR EACH ROW
WHEN (
    OLD.duration_minutes IS DISTINCT FROM NEW.duration_minutes
    OR OLD.active IS DISTINCT FROM NEW.active
    OR OLD.ai_bookable IS DISTINCT FROM NEW.ai_bookable
    OR OLD.archived_at IS DISTINCT FROM NEW.archived_at
)
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER staff_manleai_calendar_version_insert_delete
AFTER INSERT OR DELETE ON staff
FOR EACH ROW
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER staff_manleai_calendar_version_update
AFTER UPDATE OF active, ai_bookable, archived_at ON staff
FOR EACH ROW
WHEN (
    OLD.active IS DISTINCT FROM NEW.active
    OR OLD.ai_bookable IS DISTINCT FROM NEW.ai_bookable
    OR OLD.archived_at IS DISTINCT FROM NEW.archived_at
)
EXECUTE FUNCTION bump_manleai_calendar_config_version();

CREATE TRIGGER salons_manleai_calendar_timezone_version
AFTER UPDATE OF timezone ON salons
FOR EACH ROW
WHEN (OLD.timezone IS DISTINCT FROM NEW.timezone)
EXECUTE FUNCTION bump_manleai_calendar_config_for_timezone();

CREATE FUNCTION enforce_manleai_calendar_exception_write()
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
        IF NOT EXISTS (
            SELECT 1
            FROM salons salon
            WHERE salon.id = NEW.salon_id
              AND salon.owner_user_id = NEW.created_by_user_id
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI calendar exception creator must own the salon',
                CONSTRAINT = 'manleai_calendar_exceptions_creator_owner_guard';
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

    IF NEW.cancelled_by_user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM salons salon
        WHERE salon.id = NEW.salon_id
          AND salon.owner_user_id = NEW.cancelled_by_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar exception cancellation actor must own the salon',
            CONSTRAINT = 'manleai_calendar_exceptions_cancellation_actor_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manleai_calendar_exceptions_write_guard
BEFORE INSERT OR UPDATE OR DELETE ON manleai_calendar_exceptions
FOR EACH ROW
EXECUTE FUNCTION enforce_manleai_calendar_exception_write();

CREATE FUNCTION enforce_manleai_calendar_config_event_actor()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM salons salon
        WHERE salon.id = NEW.salon_id
          AND salon.owner_user_id = NEW.actor_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI calendar configuration event actor must own the salon',
            CONSTRAINT = 'manleai_calendar_config_events_actor_owner_guard';
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

CREATE TRIGGER manleai_calendar_config_events_actor_owner_guard
BEFORE INSERT ON manleai_calendar_config_events
FOR EACH ROW
EXECUTE FUNCTION enforce_manleai_calendar_config_event_actor();

CREATE FUNCTION reject_manleai_calendar_config_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'ManleAI calendar configuration events are immutable',
        CONSTRAINT = 'manleai_calendar_config_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manleai_calendar_config_events_immutable_guard
BEFORE UPDATE OR DELETE ON manleai_calendar_config_events
FOR EACH ROW
EXECUTE FUNCTION reject_manleai_calendar_config_event_mutation();
