-- Fail closed rather than attaching a historical party request to a call
-- session owned by another salon. V47 already provides the referenced parent
-- key on call_sessions(salon_id, id).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM party_booking_requests request
        JOIN call_sessions session ON session.id = request.call_session_id
        WHERE session.salon_id <> request.salon_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'party booking request tenant preflight failed',
            CONSTRAINT = 'party_booking_requests_salon_call_session_preflight';
    END IF;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE party_booking_requests
    DROP CONSTRAINT party_booking_requests_call_session_id_fkey,
    ADD CONSTRAINT party_booking_requests_salon_call_session_fkey
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES call_sessions(salon_id, id)
        ON DELETE CASCADE;
