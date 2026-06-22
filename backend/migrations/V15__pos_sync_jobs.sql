CREATE TABLE pos_sync_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('service', 'staff', 'customer')),
    entity_id UUID NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('upsert_service', 'archive_service', 'upsert_staff', 'archive_staff', 'upsert_customer')),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    attempt_count INT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts INT NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pos_sync_jobs_claim
    ON pos_sync_jobs(status, next_attempt_at, created_at);

CREATE INDEX idx_pos_sync_jobs_salon_entity
    ON pos_sync_jobs(salon_id, entity_type, entity_id, created_at DESC);

CREATE INDEX idx_pos_sync_jobs_provider_status
    ON pos_sync_jobs(provider, status, next_attempt_at);

CREATE UNIQUE INDEX idx_pos_sync_jobs_open_entity_operation
    ON pos_sync_jobs(salon_id, provider, entity_type, entity_id, operation)
    WHERE status IN ('queued', 'running');
