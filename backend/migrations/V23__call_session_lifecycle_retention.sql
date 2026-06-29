ALTER TABLE call_sessions
    ADD COLUMN lifecycle_status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN retention_expires_at TIMESTAMPTZ DEFAULT (now() + INTERVAL '90 days');

UPDATE call_sessions
SET retention_expires_at = created_at + INTERVAL '90 days'
WHERE retention_expires_at IS NULL;

ALTER TABLE call_sessions
    ALTER COLUMN retention_expires_at SET NOT NULL;

ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_lifecycle_status_check
    CHECK (lifecycle_status IN ('active', 'archived', 'redacted'));

CREATE INDEX idx_call_sessions_salon_lifecycle_updated
    ON call_sessions(salon_id, lifecycle_status, updated_at DESC);

CREATE INDEX idx_call_sessions_retention_expires
    ON call_sessions(retention_expires_at)
    WHERE lifecycle_status <> 'redacted';
