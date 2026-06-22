CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    phone TEXT,
    normalized_phone TEXT,
    email TEXT,
    normalized_email TEXT,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT true,
    sync_status TEXT NOT NULL DEFAULT 'local_only' CHECK (sync_status IN ('local_only', 'syncing', 'synced', 'sync_failed', 'unmapped', 'archived')),
    archived_at TIMESTAMPTZ,
    last_synced_at TIMESTAMPTZ,
    sync_error TEXT,
    source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'imported')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customers_salon_active
    ON customers(salon_id, active);

CREATE INDEX idx_customers_salon_sync_status
    ON customers(salon_id, sync_status);

CREATE UNIQUE INDEX idx_customers_salon_normalized_phone_active
    ON customers(salon_id, normalized_phone)
    WHERE normalized_phone IS NOT NULL AND archived_at IS NULL;

CREATE UNIQUE INDEX idx_customers_salon_normalized_email_active
    ON customers(salon_id, normalized_email)
    WHERE normalized_email IS NOT NULL AND archived_at IS NULL;
