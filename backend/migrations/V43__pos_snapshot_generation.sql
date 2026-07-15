ALTER TABLE pos_connections
    ADD COLUMN snapshot_generation BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT pos_connections_snapshot_generation_check
    CHECK (snapshot_generation >= 0);

COMMENT ON COLUMN pos_connections.snapshot_generation IS
    'Monotonic fence for full provider snapshots; location changes and newer syncs invalidate older in-flight imports.';

-- Existing Square catalog rows predate the location/generation fence and
-- cannot prove which selected location produced their provider identifiers.
-- Preserve credentials and explicit terminal states, but require one complete
-- post-deploy snapshot before those identifiers can be used for booking.
UPDATE pos_connections
SET snapshot_generation = snapshot_generation + 1,
    status = 'connected',
    last_sync_at = NULL,
    error_message = NULL,
    updated_at = now()
WHERE provider = 'square'
  AND status IN ('connected', 'active', 'syncing', 'error');
