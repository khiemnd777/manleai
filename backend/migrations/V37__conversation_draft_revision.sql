UPDATE call_sessions
SET dialog_state = jsonb_set(
        jsonb_set(
            jsonb_set(
                jsonb_set(COALESCE(dialog_state, '{}'::jsonb), '{version}', '2'::jsonb, true),
                '{draft_revision}', COALESCE(dialog_state->'draft_revision', '1'::jsonb), true
            ),
            '{reviewed_revision}', COALESCE(dialog_state->'reviewed_revision', '0'::jsonb), true
        ),
        '{authorized_revision}', COALESCE(dialog_state->'authorized_revision', '0'::jsonb), true
    );

ALTER TABLE call_sessions
    ALTER COLUMN dialog_state SET DEFAULT '{"version":2,"phase":"open","review_required":true,"review_accepted":false,"no_progress_count":0,"draft_revision":1,"reviewed_revision":0,"authorized_revision":0}'::jsonb;
