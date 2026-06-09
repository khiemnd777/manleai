CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    phone TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'invited')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

INSERT INTO roles (name, display_name)
VALUES
    ('super_admin', 'Super Admin'),
    ('salon_owner', 'Salon Owner'),
    ('salon_manager', 'Salon Manager'),
    ('staff', 'Staff')
ON CONFLICT (name) DO NOTHING;

INSERT INTO permissions (name, display_name)
VALUES
    ('salons:read', 'Read salons'),
    ('salons:write', 'Manage salons'),
    ('pos:read', 'Read POS status'),
    ('pos:write', 'Manage POS integration')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name IN ('super_admin', 'salon_owner')
ON CONFLICT DO NOTHING;

CREATE TABLE salons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    phone TEXT NOT NULL,
    address TEXT,
    city TEXT,
    state TEXT,
    zip_code TEXT,
    timezone TEXT NOT NULL DEFAULT 'America/Chicago',
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    primary_language TEXT NOT NULL DEFAULT 'en',
    secondary_language TEXT NOT NULL DEFAULT 'vi',
    handoff_phone TEXT,
    ai_enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_salons_owner_user_id ON salons(owner_user_id);

CREATE TABLE salon_business_hours (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    open_time TIME,
    close_time TIME,
    is_closed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, day_of_week)
);

CREATE TABLE salon_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL UNIQUE REFERENCES salons(id) ON DELETE CASCADE,
    ai_greeting TEXT NOT NULL DEFAULT 'Thank you for calling. This call may be recorded to help us manage appointments and improve service.',
    ai_voice TEXT NOT NULL DEFAULT 'professional_female',
    booking_mode TEXT NOT NULL DEFAULT 'pending_approval' CHECK (booking_mode IN ('confirmed_booking', 'pending_approval', 'disabled')),
    recording_enabled BOOLEAN NOT NULL DEFAULT true,
    recording_consent_message TEXT NOT NULL DEFAULT 'Thank you for calling. This call may be recorded to help us manage appointments and improve service.',
    sms_confirmation_enabled BOOLEAN NOT NULL DEFAULT true,
    sms_reminder_enabled BOOLEAN NOT NULL DEFAULT true,
    reminder_hours_before INTEGER NOT NULL DEFAULT 24,
    handoff_enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    pos_provider TEXT NOT NULL DEFAULT 'square',
    pos_service_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    ai_description TEXT,
    duration_minutes INTEGER NOT NULL DEFAULT 0,
    price_from NUMERIC(10,2),
    price_display TEXT,
    ai_bookable BOOLEAN NOT NULL DEFAULT true,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, pos_provider, pos_service_id)
);

CREATE INDEX idx_services_salon_id ON services(salon_id);

CREATE TABLE staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    pos_provider TEXT NOT NULL DEFAULT 'square',
    pos_staff_id TEXT NOT NULL,
    name TEXT NOT NULL,
    phone TEXT,
    email TEXT,
    ai_bookable BOOLEAN NOT NULL DEFAULT true,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, pos_provider, pos_staff_id)
);

CREATE INDEX idx_staff_salon_id ON staff(salon_id);

CREATE TABLE pos_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'not_connected' CHECK (status IN ('not_connected', 'connected', 'syncing', 'active', 'error', 'expired_token', 'disabled')),
    access_token_encrypted TEXT,
    refresh_token_encrypted TEXT,
    merchant_id TEXT,
    location_id TEXT,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    last_sync_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, provider)
);

CREATE INDEX idx_pos_connections_salon_provider ON pos_connections(salon_id, provider);

CREATE TABLE pos_sync_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    sync_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('started', 'succeeded', 'failed')),
    message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_pos_sync_logs_salon_provider ON pos_sync_logs(salon_id, provider);

CREATE TABLE pos_errors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    operation TEXT NOT NULL,
    error_code TEXT NOT NULL,
    error_message TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pos_errors_salon_provider_created ON pos_errors(salon_id, provider, created_at DESC);

