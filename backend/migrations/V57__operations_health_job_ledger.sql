-- Phase 6B: persisted worker-job health and run history. This ledger contains
-- technical metadata only. Provider payloads, provider entity identifiers,
-- customer data, and raw error messages are deliberately excluded.

CREATE TABLE worker_job_heartbeats (
    job_name TEXT PRIMARY KEY,
    current_worker_instance_id UUID NOT NULL,
    active_run_id UUID,
    last_status TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL,
    stale_after_seconds INTEGER NOT NULL,
    last_started_at TIMESTAMPTZ NOT NULL,
    last_completed_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_duration_ms BIGINT,
    last_processed_count INTEGER,
    last_error_class TEXT,
    last_error_code TEXT,
    heartbeat_at TIMESTAMPTZ NOT NULL,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT worker_job_heartbeats_name_check
        CHECK (job_name ~ '^[a-z][a-z0-9_]{0,63}$'),
    CONSTRAINT worker_job_heartbeats_status_check
        CHECK (last_status IN ('running', 'succeeded', 'failed', 'cancelled', 'panicked', 'abandoned')),
    CONSTRAINT worker_job_heartbeats_interval_check
        CHECK (interval_seconds BETWEEN 1 AND 604800),
    CONSTRAINT worker_job_heartbeats_stale_check
        CHECK (stale_after_seconds BETWEEN interval_seconds AND 2592000),
    CONSTRAINT worker_job_heartbeats_duration_check
        CHECK (last_duration_ms IS NULL OR last_duration_ms BETWEEN 0 AND 604800000),
    CONSTRAINT worker_job_heartbeats_processed_check
        CHECK (last_processed_count IS NULL OR last_processed_count BETWEEN 0 AND 1000000),
    CONSTRAINT worker_job_heartbeats_error_class_check
        CHECK (last_error_class IS NULL OR last_error_class ~ '^[a-z][a-z0-9_]{0,31}$'),
    CONSTRAINT worker_job_heartbeats_error_code_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    CONSTRAINT worker_job_heartbeats_time_check
        CHECK (
            heartbeat_at >= last_started_at
            AND (last_completed_at IS NULL OR last_completed_at <= updated_at)
            AND (last_success_at IS NULL OR last_success_at <= updated_at)
        ),
    CONSTRAINT worker_job_heartbeats_active_shape_check
        CHECK (
            (last_status = 'running'
                AND active_run_id IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND lease_expires_at > heartbeat_at)
            OR
            (last_status <> 'running'
                AND active_run_id IS NULL
                AND lease_expires_at IS NULL
                AND last_completed_at IS NOT NULL)
        ),
    CONSTRAINT worker_job_heartbeats_error_shape_check
        CHECK (
            (last_status IN ('failed', 'cancelled', 'panicked', 'abandoned')
                AND last_error_class IS NOT NULL
                AND last_error_code IS NOT NULL)
            OR
            (last_status IN ('running', 'succeeded')
                AND last_error_class IS NULL
                AND last_error_code IS NULL)
        )
);

CREATE TABLE worker_job_runs (
    id UUID PRIMARY KEY,
    job_name TEXT NOT NULL REFERENCES worker_job_heartbeats(job_name) ON DELETE RESTRICT,
    worker_instance_id UUID NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    duration_ms BIGINT,
    processed_count INTEGER,
    error_class TEXT,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT worker_job_runs_job_id_key UNIQUE (job_name, id),
    CONSTRAINT worker_job_runs_status_check
        CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled', 'panicked', 'abandoned')),
    CONSTRAINT worker_job_runs_duration_check
        CHECK (duration_ms IS NULL OR duration_ms BETWEEN 0 AND 604800000),
    CONSTRAINT worker_job_runs_processed_check
        CHECK (processed_count IS NULL OR processed_count BETWEEN 0 AND 1000000),
    CONSTRAINT worker_job_runs_error_class_check
        CHECK (error_class IS NULL OR error_class ~ '^[a-z][a-z0-9_]{0,31}$'),
    CONSTRAINT worker_job_runs_error_code_check
        CHECK (error_code IS NULL OR error_code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    CONSTRAINT worker_job_runs_time_check
        CHECK (
            heartbeat_at >= started_at
            AND (completed_at IS NULL OR completed_at >= started_at)
        ),
    CONSTRAINT worker_job_runs_lifecycle_check
        CHECK (
            (status = 'running'
                AND completed_at IS NULL
                AND duration_ms IS NULL
                AND processed_count IS NULL
                AND error_class IS NULL
                AND error_code IS NULL)
            OR
            (status = 'succeeded'
                AND completed_at IS NOT NULL
                AND duration_ms IS NOT NULL
                AND processed_count IS NOT NULL
                AND error_class IS NULL
                AND error_code IS NULL)
            OR
            (status IN ('failed', 'cancelled', 'panicked', 'abandoned')
                AND completed_at IS NOT NULL
                AND duration_ms IS NOT NULL
                AND processed_count IS NOT NULL
                AND error_class IS NOT NULL
                AND error_code IS NOT NULL)
        )
);

ALTER TABLE worker_job_heartbeats
    ADD CONSTRAINT worker_job_heartbeats_active_run_fk
    FOREIGN KEY (job_name, active_run_id)
    REFERENCES worker_job_runs(job_name, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_worker_job_runs_job_started
    ON worker_job_runs(job_name, started_at DESC, id DESC);

CREATE INDEX idx_worker_job_heartbeats_stale
    ON worker_job_heartbeats(heartbeat_at, job_name);

CREATE FUNCTION protect_terminal_worker_job_run()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status <> 'running' THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'terminal worker job runs are immutable',
            CONSTRAINT = 'worker_job_runs_terminal_immutable_guard';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.job_name IS DISTINCT FROM NEW.job_name
       OR OLD.worker_instance_id IS DISTINCT FROM NEW.worker_instance_id
       OR OLD.started_at IS DISTINCT FROM NEW.started_at
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'worker job run identity is immutable',
            CONSTRAINT = 'worker_job_runs_identity_immutable_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER worker_job_runs_terminal_immutable_guard
BEFORE UPDATE ON worker_job_runs
FOR EACH ROW EXECUTE FUNCTION protect_terminal_worker_job_run();
