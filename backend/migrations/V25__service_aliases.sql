CREATE TABLE service_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'owner' CHECK (source IN ('owner', 'correction', 'import')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    confidence NUMERIC(4,3) NOT NULL DEFAULT 0.940 CHECK (confidence >= 0 AND confidence <= 1),
    correction_id UUID REFERENCES owner_corrections(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, normalized_alias)
);

CREATE INDEX idx_service_aliases_salon_status_updated
    ON service_aliases(salon_id, status, updated_at DESC);

CREATE INDEX idx_service_aliases_service
    ON service_aliases(service_id, status);

ALTER TABLE owner_corrections
    ADD COLUMN applied_service_alias_id UUID REFERENCES service_aliases(id) ON DELETE SET NULL;
