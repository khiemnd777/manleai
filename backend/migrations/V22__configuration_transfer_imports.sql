ALTER TABLE knowledge_items
    ADD COLUMN import_key TEXT;

CREATE UNIQUE INDEX idx_knowledge_items_salon_import_key
    ON knowledge_items(salon_id, import_key)
    WHERE import_key IS NOT NULL;

CREATE TABLE configuration_import_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    request_id TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    payload_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('previewed', 'applied', 'failed')),
    summary JSONB NOT NULL DEFAULT '[]'::jsonb,
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    conflicts JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, request_id)
);

CREATE INDEX idx_configuration_import_runs_salon_created
    ON configuration_import_runs(salon_id, created_at DESC);
