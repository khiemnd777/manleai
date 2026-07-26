-- Phase 5A persists owner-reviewed scheduling-authority switch previews only.
-- It intentionally does not update salon_settings or any operational scheduling row.

CREATE TABLE scheduling_authority_switch_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    source_scheduling_authority TEXT NOT NULL,
    target_scheduling_authority TEXT NOT NULL,
    expected_source_authority_version BIGINT NOT NULL,
    operation_key TEXT NOT NULL,
    payload_fingerprint TEXT NOT NULL,
    readiness_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    blockers JSONB NOT NULL DEFAULT '[]'::jsonb,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    previewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    blocked_at TIMESTAMPTZ,
    committed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    rollback_of_switch_run_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scheduling_authority_switch_runs_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT scheduling_authority_switch_runs_source_authority_check
        CHECK (source_scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    CONSTRAINT scheduling_authority_switch_runs_target_authority_check
        CHECK (target_scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    CONSTRAINT scheduling_authority_switch_runs_authorities_differ_check
        CHECK (source_scheduling_authority <> target_scheduling_authority),
    CONSTRAINT scheduling_authority_switch_runs_expected_source_version_check
        CHECK (expected_source_authority_version >= 1),
    CONSTRAINT scheduling_authority_switch_runs_operation_key_nonempty_check
        CHECK (length(trim(operation_key)) BETWEEN 1 AND 256),
    CONSTRAINT scheduling_authority_switch_runs_payload_fingerprint_check
        CHECK (length(payload_fingerprint) = 64),
    CONSTRAINT scheduling_authority_switch_runs_readiness_snapshot_shape_check
        CHECK (jsonb_typeof(readiness_snapshot) = 'object'),
    CONSTRAINT scheduling_authority_switch_runs_blockers_shape_check
        CHECK (jsonb_typeof(blockers) = 'array'),
    CONSTRAINT scheduling_authority_switch_runs_status_check
        CHECK (status IN ('preview_ready', 'preview_blocked', 'committed', 'failed')),
    CONSTRAINT scheduling_authority_switch_runs_status_timestamps_check
        CHECK (
            (
                status = 'preview_ready'
                AND previewed_at IS NOT NULL
                AND blocked_at IS NULL
                AND committed_at IS NULL
                AND failed_at IS NULL
                AND jsonb_array_length(blockers) = 0
            )
            OR (
                status = 'preview_blocked'
                AND previewed_at IS NOT NULL
                AND blocked_at IS NOT NULL
                AND blocked_at >= previewed_at
                AND committed_at IS NULL
                AND failed_at IS NULL
                AND jsonb_array_length(blockers) > 0
            )
            OR (
                status = 'committed'
                AND previewed_at IS NOT NULL
                AND blocked_at IS NULL
                AND committed_at IS NOT NULL
                AND committed_at >= previewed_at
                AND failed_at IS NULL
                AND jsonb_array_length(blockers) = 0
            )
            OR (
                status = 'failed'
                AND previewed_at IS NOT NULL
                AND committed_at IS NULL
                AND failed_at IS NOT NULL
                AND failed_at >= previewed_at
                AND (blocked_at IS NULL OR failed_at >= blocked_at)
            )
        ),
    CONSTRAINT scheduling_authority_switch_runs_rollback_not_self_check
        CHECK (rollback_of_switch_run_id IS NULL OR rollback_of_switch_run_id <> id),
    CONSTRAINT scheduling_authority_switch_runs_rollback_tenant_fk
        FOREIGN KEY (salon_id, rollback_of_switch_run_id)
        REFERENCES scheduling_authority_switch_runs(salon_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT scheduling_authority_switch_runs_salon_operation_key
        UNIQUE (salon_id, operation_key)
);

CREATE INDEX idx_scheduling_authority_switch_runs_salon_created
    ON scheduling_authority_switch_runs(salon_id, created_at DESC, id DESC);

CREATE INDEX idx_scheduling_authority_switch_runs_salon_status
    ON scheduling_authority_switch_runs(salon_id, status, created_at DESC, id DESC);

CREATE FUNCTION enforce_scheduling_authority_switch_run_owner_actor()
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
            MESSAGE = 'scheduling authority switch run actor must own the salon',
            CONSTRAINT = 'scheduling_authority_switch_runs_actor_owner_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_scheduling_authority_switch_run_state()
RETURNS TRIGGER AS $$
DECLARE
    rollback_item RECORD;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status NOT IN ('preview_ready', 'preview_blocked') THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'scheduling authority switch runs must begin as a preview',
                CONSTRAINT = 'scheduling_authority_switch_runs_initial_preview_guard';
        END IF;
    ELSIF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.salon_id IS DISTINCT FROM NEW.salon_id
       OR OLD.source_scheduling_authority IS DISTINCT FROM NEW.source_scheduling_authority
       OR OLD.target_scheduling_authority IS DISTINCT FROM NEW.target_scheduling_authority
       OR OLD.expected_source_authority_version IS DISTINCT FROM NEW.expected_source_authority_version
       OR OLD.operation_key IS DISTINCT FROM NEW.operation_key
       OR OLD.payload_fingerprint IS DISTINCT FROM NEW.payload_fingerprint
       OR OLD.readiness_snapshot IS DISTINCT FROM NEW.readiness_snapshot
       OR OLD.blockers IS DISTINCT FROM NEW.blockers
       OR OLD.actor_user_id IS DISTINCT FROM NEW.actor_user_id
       OR OLD.previewed_at IS DISTINCT FROM NEW.previewed_at
       OR OLD.rollback_of_switch_run_id IS DISTINCT FROM NEW.rollback_of_switch_run_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch run core fields are immutable',
            CONSTRAINT = 'scheduling_authority_switch_runs_immutable_core_guard';
    ELSIF OLD.status IS NOT DISTINCT FROM NEW.status THEN
        RETURN NEW;
    ELSIF (OLD.status = 'preview_ready' AND NEW.status IN ('committed', 'failed'))
       OR (OLD.status = 'preview_blocked' AND NEW.status = 'failed') THEN
        RETURN NEW;
    ELSE
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch run status transition is not allowed',
            CONSTRAINT = 'scheduling_authority_switch_runs_status_transition_guard';
    END IF;

    IF NEW.rollback_of_switch_run_id IS NOT NULL THEN
        SELECT prior_run.source_scheduling_authority,
               prior_run.target_scheduling_authority,
               prior_run.status
        INTO rollback_item
        FROM scheduling_authority_switch_runs prior_run
        WHERE prior_run.salon_id = NEW.salon_id
          AND prior_run.id = NEW.rollback_of_switch_run_id;

        IF FOUND AND (
            rollback_item.status <> 'committed'
            OR rollback_item.source_scheduling_authority <> NEW.target_scheduling_authority
            OR rollback_item.target_scheduling_authority <> NEW.source_scheduling_authority
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'scheduling authority rollback must reference a committed exact inverse run',
                CONSTRAINT = 'scheduling_authority_switch_runs_rollback_state_guard';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION reject_scheduling_authority_switch_run_deletion()
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling authority switch runs are immutable audit records',
        CONSTRAINT = 'scheduling_authority_switch_runs_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_authority_switch_runs_owner_actor_guard
BEFORE INSERT OR UPDATE OF salon_id, actor_user_id ON scheduling_authority_switch_runs
FOR EACH ROW EXECUTE FUNCTION enforce_scheduling_authority_switch_run_owner_actor();

CREATE TRIGGER scheduling_authority_switch_runs_state_guard
BEFORE INSERT OR UPDATE ON scheduling_authority_switch_runs
FOR EACH ROW EXECUTE FUNCTION enforce_scheduling_authority_switch_run_state();

CREATE TRIGGER scheduling_authority_switch_runs_immutable_guard
BEFORE DELETE ON scheduling_authority_switch_runs
FOR EACH ROW EXECUTE FUNCTION reject_scheduling_authority_switch_run_deletion();

CREATE TABLE scheduling_authority_switch_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    switch_run_id UUID NOT NULL,
    action_key TEXT NOT NULL,
    action_fingerprint TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scheduling_authority_switch_events_run_tenant_fk
        FOREIGN KEY (salon_id, switch_run_id)
        REFERENCES scheduling_authority_switch_runs(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT scheduling_authority_switch_events_action_key_nonempty_check
        CHECK (length(trim(action_key)) BETWEEN 1 AND 256),
    CONSTRAINT scheduling_authority_switch_events_action_fingerprint_check
        CHECK (length(action_fingerprint) = 64),
    CONSTRAINT scheduling_authority_switch_events_event_type_check
        CHECK (event_type IN ('preview', 'commit', 'fail')),
    CONSTRAINT scheduling_authority_switch_events_payload_shape_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT scheduling_authority_switch_events_action_key
        UNIQUE (switch_run_id, action_key)
);

CREATE INDEX idx_scheduling_authority_switch_events_run
    ON scheduling_authority_switch_events(switch_run_id, created_at ASC, id ASC);

CREATE FUNCTION enforce_scheduling_authority_switch_event_owner_actor()
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
            MESSAGE = 'scheduling authority switch event actor must own the salon',
            CONSTRAINT = 'scheduling_authority_switch_events_actor_owner_guard';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM scheduling_authority_switch_runs switch_run
        WHERE switch_run.id = NEW.switch_run_id
          AND switch_run.salon_id = NEW.salon_id
          AND (
              (NEW.event_type = 'preview' AND switch_run.status IN ('preview_ready', 'preview_blocked'))
              OR (NEW.event_type = 'commit' AND switch_run.status = 'committed')
              OR (NEW.event_type = 'fail' AND switch_run.status = 'failed')
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch event must match the durable run state',
            CONSTRAINT = 'scheduling_authority_switch_events_state_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_authority_switch_events_owner_actor_guard
BEFORE INSERT ON scheduling_authority_switch_events
FOR EACH ROW EXECUTE FUNCTION enforce_scheduling_authority_switch_event_owner_actor();

CREATE FUNCTION reject_scheduling_authority_switch_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling authority switch events are immutable',
        CONSTRAINT = 'scheduling_authority_switch_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_authority_switch_events_immutable_guard
BEFORE UPDATE OR DELETE ON scheduling_authority_switch_events
FOR EACH ROW EXECUTE FUNCTION reject_scheduling_authority_switch_event_mutation();
