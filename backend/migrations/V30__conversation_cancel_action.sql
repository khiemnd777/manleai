ALTER TABLE call_sessions
    DROP CONSTRAINT IF EXISTS call_sessions_booking_action_check;

ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_booking_action_check
    CHECK (booking_action IN ('book', 'reschedule', 'cancel'));
