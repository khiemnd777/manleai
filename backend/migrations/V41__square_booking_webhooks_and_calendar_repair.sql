CREATE TABLE square_booking_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    merchant_id TEXT NOT NULL,
    location_id TEXT NOT NULL,
    pos_booking_id TEXT NOT NULL,
    pos_booking_version INTEGER,
    booking_status TEXT,
    booking_start_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    processing_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (processing_status IN ('pending', 'processing', 'succeeded', 'failed', 'ignored')),
    processing_attempts INTEGER NOT NULL DEFAULT 0 CHECK (processing_attempts >= 0),
    processing_token TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processing_lease_expires_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, event_id)
);

CREATE INDEX idx_square_booking_webhook_events_ready
    ON square_booking_webhook_events(next_attempt_at, created_at)
    WHERE processing_status IN ('pending', 'failed', 'processing');

CREATE INDEX idx_square_booking_webhook_events_booking
    ON square_booking_webhook_events(salon_id, pos_booking_id, created_at DESC);

CREATE INDEX idx_pos_connections_square_webhook_target
    ON pos_connections(provider, merchant_id, location_id)
    WHERE provider = 'square'
      AND merchant_id IS NOT NULL AND merchant_id <> ''
      AND location_id IS NOT NULL AND location_id <> '';

CREATE TABLE square_calendar_repair_state (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	salon_id UUID NOT NULL UNIQUE REFERENCES salons(id) ON DELETE CASCADE,
    next_repair_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_expires_at TIMESTAMPTZ,
    lease_token TEXT,
    repair_attempts INTEGER NOT NULL DEFAULT 0 CHECK (repair_attempts >= 0),
    last_repaired_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_square_calendar_repair_state_ready
    ON square_calendar_repair_state(next_repair_at, salon_id);
