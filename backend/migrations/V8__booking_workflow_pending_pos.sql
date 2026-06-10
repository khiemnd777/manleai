ALTER TABLE booking_attempts
    ADD COLUMN pos_idempotency_key TEXT;

CREATE UNIQUE INDEX idx_booking_attempts_pos_idempotency_key
    ON booking_attempts(pos_idempotency_key)
    WHERE pos_idempotency_key IS NOT NULL;

ALTER TABLE booking_attempts
    DROP CONSTRAINT IF EXISTS booking_attempts_status_check;

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_status_check
    CHECK (status IN ('started', 'pos_pending', 'confirmed', 'rescheduled', 'cancelled', 'fallback_pending', 'failed'));
