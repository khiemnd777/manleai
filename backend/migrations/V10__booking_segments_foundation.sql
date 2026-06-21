ALTER TABLE booking_attempts
    ADD COLUMN staff_selection_mode TEXT NOT NULL DEFAULT 'specific';

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_staff_selection_mode_check
    CHECK (staff_selection_mode IN ('specific', 'anyone'));

ALTER TABLE appointments
    ADD COLUMN staff_selection_mode TEXT NOT NULL DEFAULT 'specific';

ALTER TABLE appointments
    ADD CONSTRAINT appointments_staff_selection_mode_check
    CHECK (staff_selection_mode IN ('specific', 'anyone'));

ALTER TABLE call_sessions
    ADD COLUMN staff_selection_mode TEXT NOT NULL DEFAULT 'specific';

ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_staff_selection_mode_check
    CHECK (staff_selection_mode IN ('specific', 'anyone'));

ALTER TABLE appointment_services
    ADD COLUMN staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    ADD COLUMN staff_selection_mode TEXT NOT NULL DEFAULT 'specific',
    ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 1;

ALTER TABLE appointment_services
    ADD CONSTRAINT appointment_services_staff_selection_mode_check
    CHECK (staff_selection_mode IN ('specific', 'anyone'));

CREATE INDEX idx_appointment_services_staff ON appointment_services(staff_id);

CREATE TABLE booking_attempt_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_attempt_id UUID NOT NULL REFERENCES booking_attempts(id) ON DELETE CASCADE,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    staff_selection_mode TEXT NOT NULL DEFAULT 'specific' CHECK (staff_selection_mode IN ('specific', 'anyone')),
    pos_service_id TEXT NOT NULL,
    pos_service_version BIGINT,
    name TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL DEFAULT 0,
    price_from NUMERIC(10,2),
    sort_order INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (booking_attempt_id, sort_order)
);

CREATE INDEX idx_booking_attempt_segments_attempt ON booking_attempt_segments(booking_attempt_id);
CREATE INDEX idx_booking_attempt_segments_service ON booking_attempt_segments(service_id);
CREATE INDEX idx_booking_attempt_segments_staff ON booking_attempt_segments(staff_id);
