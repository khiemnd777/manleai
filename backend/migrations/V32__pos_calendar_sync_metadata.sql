ALTER TABLE appointments
    ADD COLUMN pos_sync_status TEXT NOT NULL DEFAULT 'synced';

ALTER TABLE appointments
    ADD CONSTRAINT appointments_pos_sync_status_check
    CHECK (pos_sync_status IN ('synced', 'not_synced', 'sync_failed', 'pending'));

ALTER TABLE appointments
    ADD COLUMN last_pos_synced_at TIMESTAMPTZ;

ALTER TABLE appointments
    ADD COLUMN pos_sync_error TEXT;

CREATE INDEX idx_appointments_salon_range
    ON appointments(salon_id, start_time ASC, end_time ASC);

CREATE INDEX idx_booking_attempts_salon_requested_range
    ON booking_attempts(salon_id, requested_start_time ASC, requested_end_time ASC)
    WHERE status = 'fallback_pending';
