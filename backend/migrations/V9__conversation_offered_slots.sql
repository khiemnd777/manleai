ALTER TABLE call_sessions
    ADD COLUMN offered_slots JSONB NOT NULL DEFAULT '[]'::jsonb;
