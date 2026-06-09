ALTER TABLE call_sessions
    DROP CONSTRAINT IF EXISTS call_sessions_channel_check;

ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_channel_check CHECK (channel IN ('simulator', 'phone')),
    ADD COLUMN provider TEXT,
    ADD COLUMN provider_call_id TEXT,
    ADD COLUMN inbound_phone TEXT,
    ADD COLUMN outbound_phone TEXT;

CREATE UNIQUE INDEX idx_call_sessions_provider_call
    ON call_sessions(provider, provider_call_id)
    WHERE provider IS NOT NULL AND provider_call_id IS NOT NULL;

CREATE TABLE voice_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID REFERENCES salons(id) ON DELETE SET NULL,
    call_session_id UUID REFERENCES call_sessions(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    provider_call_id TEXT,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_voice_webhook_events_salon_created
    ON voice_webhook_events(salon_id, created_at DESC);

CREATE INDEX idx_voice_webhook_events_provider_call
    ON voice_webhook_events(provider, provider_call_id, created_at DESC);
