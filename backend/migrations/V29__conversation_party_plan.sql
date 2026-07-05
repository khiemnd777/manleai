ALTER TABLE call_sessions
    ADD COLUMN IF NOT EXISTS party_plan JSONB NOT NULL DEFAULT '{}'::jsonb;
