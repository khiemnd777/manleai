ALTER TABLE call_sessions
    ADD COLUMN booking_segments JSONB NOT NULL DEFAULT '[]'::jsonb;
