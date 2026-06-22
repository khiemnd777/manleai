CREATE TABLE pos_entity_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('service', 'staff', 'customer')),
    entity_id UUID NOT NULL,
    provider TEXT NOT NULL,
    provider_entity_id TEXT,
    provider_version BIGINT,
    sync_status TEXT NOT NULL DEFAULT 'local_only' CHECK (sync_status IN ('local_only', 'syncing', 'synced', 'sync_failed', 'unmapped', 'archived')),
    last_synced_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, entity_type, entity_id, provider)
);

CREATE INDEX idx_pos_entity_links_salon_entity
    ON pos_entity_links(salon_id, entity_type, entity_id);

CREATE INDEX idx_pos_entity_links_salon_provider_status
    ON pos_entity_links(salon_id, provider, sync_status);

CREATE UNIQUE INDEX idx_pos_entity_links_provider_entity
    ON pos_entity_links(salon_id, entity_type, provider, provider_entity_id)
    WHERE provider_entity_id IS NOT NULL;

ALTER TABLE services
    ADD COLUMN sync_status TEXT NOT NULL DEFAULT 'local_only' CHECK (sync_status IN ('local_only', 'syncing', 'synced', 'sync_failed', 'unmapped', 'archived')),
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN last_synced_at TIMESTAMPTZ,
    ADD COLUMN sync_error TEXT,
    ADD COLUMN source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'imported'));

CREATE INDEX idx_services_salon_sync_status
    ON services(salon_id, sync_status);

CREATE INDEX idx_services_salon_source
    ON services(salon_id, source);

ALTER TABLE staff
    ADD COLUMN sync_status TEXT NOT NULL DEFAULT 'local_only' CHECK (sync_status IN ('local_only', 'syncing', 'synced', 'sync_failed', 'unmapped', 'archived')),
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN last_synced_at TIMESTAMPTZ,
    ADD COLUMN sync_error TEXT,
    ADD COLUMN source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'imported'));

CREATE INDEX idx_staff_salon_sync_status
    ON staff(salon_id, sync_status);

CREATE INDEX idx_staff_salon_source
    ON staff(salon_id, source);

INSERT INTO pos_entity_links (
    salon_id,
    entity_type,
    entity_id,
    provider,
    provider_entity_id,
    provider_version,
    sync_status,
    last_synced_at,
    created_at,
    updated_at
)
SELECT
    salon_id,
    'service',
    id,
    pos_provider,
    pos_service_id,
    pos_service_version,
    'synced',
    updated_at,
    created_at,
    updated_at
FROM services
WHERE pos_service_id <> ''
ON CONFLICT (salon_id, entity_type, entity_id, provider) DO NOTHING;

INSERT INTO pos_entity_links (
    salon_id,
    entity_type,
    entity_id,
    provider,
    provider_entity_id,
    sync_status,
    last_synced_at,
    created_at,
    updated_at
)
SELECT
    salon_id,
    'staff',
    id,
    pos_provider,
    pos_staff_id,
    'synced',
    updated_at,
    created_at,
    updated_at
FROM staff
WHERE pos_staff_id <> ''
ON CONFLICT (salon_id, entity_type, entity_id, provider) DO NOTHING;

UPDATE services
SET sync_status = 'synced',
    source = 'imported',
    last_synced_at = updated_at,
    sync_error = NULL
WHERE pos_service_id <> '';

UPDATE staff
SET sync_status = 'synced',
    source = 'imported',
    last_synced_at = updated_at,
    sync_error = NULL
WHERE pos_staff_id <> '';
