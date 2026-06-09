ALTER TABLE appointments
    ADD COLUMN pos_appointment_version INTEGER NOT NULL DEFAULT 0;

ALTER TABLE appointment_services
    ADD COLUMN pos_service_version BIGINT;

ALTER TABLE booking_attempts
    DROP CONSTRAINT IF EXISTS booking_attempts_status_check;

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_status_check
    CHECK (status IN ('started', 'confirmed', 'rescheduled', 'cancelled', 'fallback_pending', 'failed'));
