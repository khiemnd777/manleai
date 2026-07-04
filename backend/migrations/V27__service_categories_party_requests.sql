CREATE TABLE service_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'system', 'imported')),
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, slug)
);

CREATE INDEX idx_service_categories_salon_status
    ON service_categories(salon_id, status, sort_order, name);

CREATE TABLE service_category_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES service_categories(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'owner' CHECK (source IN ('owner', 'system', 'imported')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    confidence NUMERIC(4,3) NOT NULL DEFAULT 0.940,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, normalized_alias)
);

CREATE INDEX idx_service_category_aliases_category_status
    ON service_category_aliases(category_id, status);

ALTER TABLE services
    ADD COLUMN service_category_id UUID REFERENCES service_categories(id) ON DELETE SET NULL,
    ADD COLUMN service_category_source TEXT NOT NULL DEFAULT 'unassigned'
        CHECK (service_category_source IN ('unassigned', 'manual', 'suggested', 'imported')),
    ADD COLUMN service_category_confidence NUMERIC(4,3),
    ADD COLUMN service_category_reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN service_category_reviewed_at TIMESTAMPTZ;

CREATE INDEX idx_services_salon_category
    ON services(salon_id, service_category_id)
    WHERE archived_at IS NULL;

CREATE TABLE party_booking_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    call_session_id UUID NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    event_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'contacted', 'resolved', 'dismissed')),
    party_size INTEGER,
    representative_name TEXT,
    representative_phone TEXT,
    requested_date DATE,
    requested_time_window TEXT,
    guest_service_requests JSONB NOT NULL DEFAULT '[]'::jsonb,
    flexibility_notes TEXT,
    summary TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (salon_id, call_session_id, event_key)
);

CREATE INDEX idx_party_booking_requests_salon_status
    ON party_booking_requests(salon_id, status, created_at DESC);

CREATE INDEX idx_party_booking_requests_call_session
    ON party_booking_requests(call_session_id);
