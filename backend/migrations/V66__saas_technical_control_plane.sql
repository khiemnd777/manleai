-- SaaS Phase 5 technical-control-plane mutation ownership.
--
-- Provider secrets remain in salon_integration_configs. This ledger stores
-- only mutation identity, version fences, and safe metadata; it never copies
-- credentials or provider tokens into audit rows.

CREATE TABLE technical_resource_versions (
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL
        CHECK (resource_type IN ('integration_config')),
    resource_id TEXT NOT NULL
        CHECK (resource_id IN ('square', 'twilio', 'openai')),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, resource_type, resource_id)
);

CREATE TABLE technical_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action_key TEXT NOT NULL
        CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_type TEXT NOT NULL
        CHECK (action_type = btrim(action_type) AND length(action_type) BETWEEN 1 AND 128),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    previous_version BIGINT NOT NULL CHECK (previous_version >= 0),
    result_version BIGINT NOT NULL CHECK (result_version = previous_version + 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, actor_user_id, action_key),
    CONSTRAINT technical_actions_resource_fk
        FOREIGN KEY (salon_id, resource_type, resource_id)
        REFERENCES technical_resource_versions(salon_id, resource_type, resource_id)
        ON DELETE RESTRICT,
    CONSTRAINT technical_actions_details_safe CHECK (
        jsonb_typeof(details) = 'object'
        AND octet_length(details::text) <= 2048
        AND details - ARRAY['provider', 'changed_fields']::text[] = '{}'::jsonb
    )
);

CREATE TABLE technical_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id UUID NOT NULL REFERENCES technical_actions(id) ON DELETE RESTRICT,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL
        CHECK (event_type = btrim(event_type) AND length(event_type) BETWEEN 1 AND 128),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    previous_version BIGINT NOT NULL CHECK (previous_version >= 0),
    result_version BIGINT NOT NULL CHECK (result_version = previous_version + 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT technical_events_details_safe CHECK (
        jsonb_typeof(details) = 'object'
        AND octet_length(details::text) <= 2048
        AND details - ARRAY['provider', 'changed_fields']::text[] = '{}'::jsonb
    )
);

CREATE INDEX idx_technical_actions_salon_created
    ON technical_actions (salon_id, created_at DESC, id DESC);

CREATE INDEX idx_technical_events_salon_created
    ON technical_events (salon_id, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION phase5_reject_technical_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER technical_actions_immutable
BEFORE UPDATE OR DELETE ON technical_actions
FOR EACH ROW EXECUTE FUNCTION phase5_reject_technical_ledger_mutation();

CREATE TRIGGER technical_events_immutable
BEFORE UPDATE OR DELETE ON technical_events
FOR EACH ROW EXECUTE FUNCTION phase5_reject_technical_ledger_mutation();

INSERT INTO technical_resource_versions (
    salon_id, resource_type, resource_id, version
)
SELECT salon_id, 'integration_config', provider, 1
FROM salon_integration_configs
WHERE provider IN ('square', 'twilio', 'openai')
ON CONFLICT DO NOTHING;
