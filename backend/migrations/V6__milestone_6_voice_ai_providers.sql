CREATE TABLE voice_audio_outputs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID REFERENCES salons(id) ON DELETE CASCADE,
    call_session_id UUID REFERENCES call_sessions(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_call_id TEXT,
    content_type TEXT NOT NULL,
    audio_data BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_voice_audio_outputs_expires
    ON voice_audio_outputs(expires_at);

CREATE INDEX idx_voice_audio_outputs_call_session
    ON voice_audio_outputs(call_session_id, created_at DESC);
