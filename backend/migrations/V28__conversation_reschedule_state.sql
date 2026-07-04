ALTER TABLE call_sessions
    ADD COLUMN booking_action TEXT NOT NULL DEFAULT 'book';

ALTER TABLE call_sessions
    ADD COLUMN target_appointment_id UUID REFERENCES appointments(id) ON DELETE SET NULL;

ALTER TABLE call_sessions
    ADD COLUMN reschedule_candidates JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_booking_action_check
    CHECK (booking_action IN ('book', 'reschedule'));

CREATE INDEX idx_call_sessions_target_appointment
    ON call_sessions(target_appointment_id)
    WHERE target_appointment_id IS NOT NULL;
