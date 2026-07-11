ALTER TABLE booking_attempts
    ADD COLUMN operation_key TEXT,
    ADD COLUMN request_fingerprint TEXT,
    ADD COLUMN operation_type TEXT NOT NULL DEFAULT 'book',
    ADD COLUMN target_appointment_id UUID REFERENCES appointments(id) ON DELETE SET NULL,
    ADD COLUMN processing_token TEXT,
    ADD COLUMN processing_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN provider_outcome TEXT NOT NULL DEFAULT 'not_started',
    ADD COLUMN retry_policy TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN reconciliation_status TEXT NOT NULL DEFAULT 'not_required';

UPDATE booking_attempts
SET provider_outcome = 'succeeded', retry_policy = 'none', reconciliation_status = 'not_required'
WHERE status IN ('confirmed', 'rescheduled', 'cancelled');

UPDATE booking_attempts
SET provider_outcome = 'failed', retry_policy = 'safe', reconciliation_status = 'not_required'
WHERE status = 'fallback_pending';

UPDATE booking_attempts
SET provider_outcome = 'unknown', retry_policy = 'blocked', reconciliation_status = 'required'
WHERE status = 'pos_pending'
   OR (status = 'fallback_pending' AND error_code = 'POS_TIMEOUT');

UPDATE booking_attempts ba
SET operation_type = 'reschedule', target_appointment_id = notification.appointment_id
FROM owner_notifications notification
WHERE notification.booking_attempt_id = ba.id
  AND notification.type = 'reschedule_fallback_pending';

UPDATE booking_attempts ba
SET operation_type = 'cancel', target_appointment_id = notification.appointment_id
FROM owner_notifications notification
WHERE notification.booking_attempt_id = ba.id
  AND notification.type = 'cancel_fallback_pending';

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_operation_type_check
    CHECK (operation_type IN ('book', 'reschedule', 'cancel')),
    ADD CONSTRAINT booking_attempts_provider_outcome_check
    CHECK (provider_outcome IN ('not_started', 'in_flight', 'succeeded', 'failed', 'unknown')),
    ADD CONSTRAINT booking_attempts_retry_policy_check
    CHECK (retry_policy IN ('none', 'safe', 'blocked')),
    ADD CONSTRAINT booking_attempts_reconciliation_status_check
    CHECK (reconciliation_status IN ('not_required', 'required', 'resolved'));

CREATE UNIQUE INDEX idx_booking_attempts_salon_operation_key
    ON booking_attempts(salon_id, operation_key)
    WHERE operation_key IS NOT NULL;

CREATE INDEX idx_booking_attempts_reconciliation_required
    ON booking_attempts(salon_id, updated_at DESC)
    WHERE reconciliation_status = 'required';

CREATE INDEX idx_booking_attempts_processing_lease
    ON booking_attempts(processing_lease_expires_at)
    WHERE status = 'pos_pending';
