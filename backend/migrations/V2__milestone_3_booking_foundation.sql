ALTER TABLE services
    ADD COLUMN pos_service_version BIGINT;

CREATE TABLE pos_oauth_states (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    state_hash TEXT NOT NULL UNIQUE,
    nonce_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pos_oauth_states_salon_provider ON pos_oauth_states(salon_id, provider);
CREATE INDEX idx_pos_oauth_states_expires_at ON pos_oauth_states(expires_at);

CREATE TABLE booking_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'owner_dashboard',
    status TEXT NOT NULL CHECK (status IN ('started', 'confirmed', 'fallback_pending', 'failed')),
    pos_provider TEXT NOT NULL DEFAULT 'square',
    pos_booking_id TEXT,
    customer_name TEXT NOT NULL,
    customer_phone TEXT NOT NULL,
    customer_email TEXT,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    requested_start_time TIMESTAMPTZ NOT NULL,
    requested_end_time TIMESTAMPTZ NOT NULL,
    notes TEXT,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_booking_attempts_salon_created ON booking_attempts(salon_id, created_at DESC);
CREATE INDEX idx_booking_attempts_status ON booking_attempts(status);

CREATE TABLE appointments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    booking_attempt_id UUID NOT NULL REFERENCES booking_attempts(id) ON DELETE RESTRICT,
    pos_provider TEXT NOT NULL DEFAULT 'square',
    pos_appointment_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('confirmed', 'rescheduled', 'cancelled')),
    customer_name TEXT NOT NULL,
    customer_phone TEXT NOT NULL,
    customer_email TEXT,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, pos_provider, pos_appointment_id)
);

CREATE INDEX idx_appointments_salon_start ON appointments(salon_id, start_time DESC);
CREATE INDEX idx_appointments_booking_attempt ON appointments(booking_attempt_id);

CREATE TABLE appointment_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    appointment_id UUID NOT NULL REFERENCES appointments(id) ON DELETE CASCADE,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    pos_service_id TEXT NOT NULL,
    name TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL DEFAULT 0,
    price_from NUMERIC(10,2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_appointment_services_appointment ON appointment_services(appointment_id);

CREATE TABLE owner_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    booking_attempt_id UUID REFERENCES booking_attempts(id) ON DELETE CASCADE,
    appointment_id UUID REFERENCES appointments(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'read', 'dismissed')),
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ
);

CREATE INDEX idx_owner_notifications_salon_status ON owner_notifications(salon_id, status, created_at DESC);
