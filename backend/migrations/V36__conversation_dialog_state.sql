ALTER TABLE call_sessions
    ADD COLUMN IF NOT EXISTS dialog_state JSONB NOT NULL
    DEFAULT '{"version":1,"phase":"open","review_required":true,"review_accepted":false,"no_progress_count":0}'::jsonb;

UPDATE call_sessions
SET dialog_state = '{"version":1,"phase":"open","review_required":true,"review_accepted":false,"no_progress_count":0}'::jsonb
WHERE dialog_state IS NULL OR dialog_state = '{}'::jsonb;
