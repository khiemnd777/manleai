CREATE TABLE call_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    channel TEXT NOT NULL DEFAULT 'simulator' CHECK (channel IN ('simulator')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'handoff', 'failed')),
    intent TEXT NOT NULL DEFAULT 'unknown',
    outcome TEXT NOT NULL DEFAULT 'collecting',
    customer_name TEXT,
    customer_phone TEXT,
    customer_email TEXT,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    requested_start_time TIMESTAMPTZ,
    booking_attempt_id UUID REFERENCES booking_attempts(id) ON DELETE SET NULL,
    appointment_id UUID REFERENCES appointments(id) ON DELETE SET NULL,
    summary TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_call_sessions_salon_updated ON call_sessions(salon_id, updated_at DESC);
CREATE INDEX idx_call_sessions_status ON call_sessions(status);
CREATE INDEX idx_call_sessions_booking_attempt ON call_sessions(booking_attempt_id);

CREATE TABLE call_transcript_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    speaker TEXT NOT NULL CHECK (speaker IN ('ai', 'customer', 'tool')),
    body TEXT NOT NULL,
    metadata JSONB,
    sequence INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, sequence)
);

CREATE INDEX idx_call_transcript_messages_session_sequence ON call_transcript_messages(session_id, sequence);
CREATE INDEX idx_call_transcript_messages_salon_created ON call_transcript_messages(salon_id, created_at DESC);

CREATE TABLE handoff_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    call_session_id UUID NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'resolved', 'dismissed')),
    reason TEXT NOT NULL,
    customer_name TEXT,
    customer_phone TEXT,
    summary TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_handoff_requests_salon_status ON handoff_requests(salon_id, status, created_at DESC);
CREATE INDEX idx_handoff_requests_call_session ON handoff_requests(call_session_id);
