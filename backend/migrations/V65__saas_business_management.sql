-- Phase 3 SaaS Business Management foundation.
--
-- Tenant and Platform surfaces share the same canonical business records and
-- mutation ledger. Provider credentials, provider diagnostics, scheduling
-- authority, and technical readiness are deliberately outside this schema.

CREATE TABLE business_resource_versions (
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL
        CHECK (resource_type IN (
            'salon_profile',
            'public_catalog',
            'business_hours',
            'service',
            'service_category',
            'staff',
            'staff_service_eligibility',
            'customer'
        )),
    resource_id TEXT NOT NULL
        CHECK (resource_id = btrim(resource_id) AND length(resource_id) BETWEEN 1 AND 256),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, resource_type, resource_id)
);

CREATE TABLE business_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    surface TEXT NOT NULL CHECK (surface IN ('tenant', 'platform')),
    action_key TEXT NOT NULL
        CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_type TEXT NOT NULL
        CHECK (action_type = btrim(action_type) AND length(action_type) BETWEEN 1 AND 128),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    previous_version BIGINT NOT NULL CHECK (previous_version >= 0),
    result_version BIGINT NOT NULL CHECK (result_version >= 1),
    response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, actor_user_id, action_key),
    CONSTRAINT business_actions_resource_fk
        FOREIGN KEY (salon_id, resource_type, resource_id)
        REFERENCES business_resource_versions(salon_id, resource_type, resource_id)
        ON DELETE RESTRICT,
    CONSTRAINT business_actions_version_check
        CHECK (result_version > previous_version),
    CONSTRAINT business_actions_response_payload_check
        CHECK (
            jsonb_typeof(response_payload) = 'object'
            AND octet_length(response_payload::text) <= 2048
            AND response_payload - ARRAY[
                'resource_type', 'resource_id', 'version', 'archived', 'management_mode'
            ]::text[] = '{}'::jsonb
        )
);

CREATE TABLE business_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id UUID NOT NULL REFERENCES business_actions(id) ON DELETE RESTRICT,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    surface TEXT NOT NULL CHECK (surface IN ('tenant', 'platform')),
    event_type TEXT NOT NULL
        CHECK (event_type = btrim(event_type) AND length(event_type) BETWEEN 1 AND 128),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    previous_version BIGINT NOT NULL CHECK (previous_version >= 0),
    result_version BIGINT NOT NULL CHECK (result_version >= 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_events_version_check CHECK (result_version > previous_version),
    CONSTRAINT business_events_details_check
        CHECK (
            jsonb_typeof(details) = 'object'
            AND octet_length(details::text) <= 4096
            AND details - ARRAY[
                'changed_fields', 'archived', 'management_mode', 'count'
            ]::text[] = '{}'::jsonb
        )
);

CREATE INDEX idx_business_actions_salon_created
    ON business_actions (salon_id, created_at DESC, id DESC);

CREATE INDEX idx_business_events_salon_created
    ON business_events (salon_id, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION phase3_reject_business_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER business_actions_immutable
BEFORE UPDATE OR DELETE ON business_actions
FOR EACH ROW EXECUTE FUNCTION phase3_reject_business_ledger_mutation();

CREATE TRIGGER business_events_immutable
BEFORE UPDATE OR DELETE ON business_events
FOR EACH ROW EXECUTE FUNCTION phase3_reject_business_ledger_mutation();

-- Canonical rows are also created by provider sync and by the previous
-- compatible application image during an expand/contract rollout. Install the
-- version owner at the database boundary so those rows cannot disappear from
-- the shared Business projections merely because they did not enter through a
-- Phase 3 HTTP route.
CREATE OR REPLACE FUNCTION phase3_ensure_business_resource_versions()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row_value JSONB := to_jsonb(NEW);
    owner_salon_id UUID;
    business_resource_type TEXT;
BEGIN
    owner_salon_id := COALESCE(
        NULLIF(row_value ->> 'salon_id', '')::UUID,
        NULLIF(row_value ->> 'id', '')::UUID
    );
    FOREACH business_resource_type IN ARRAY TG_ARGV LOOP
        INSERT INTO business_resource_versions (
            salon_id, resource_type, resource_id, version
        ) VALUES (
            owner_salon_id,
            business_resource_type,
            CASE
                WHEN TG_TABLE_NAME = 'salons' THEN owner_salon_id::TEXT
                ELSE NEW.id::TEXT
            END,
            1
        )
        ON CONFLICT DO NOTHING;
    END LOOP;
    RETURN NEW;
END
$$;

CREATE TRIGGER salons_ensure_business_resource_versions
AFTER INSERT ON salons
FOR EACH ROW EXECUTE FUNCTION phase3_ensure_business_resource_versions(
    'salon_profile', 'public_catalog', 'business_hours', 'staff_service_eligibility'
);

CREATE TRIGGER services_ensure_business_resource_version
AFTER INSERT ON services
FOR EACH ROW EXECUTE FUNCTION phase3_ensure_business_resource_versions('service');

CREATE TRIGGER service_categories_ensure_business_resource_version
AFTER INSERT ON service_categories
FOR EACH ROW EXECUTE FUNCTION phase3_ensure_business_resource_versions('service_category');

CREATE TRIGGER staff_ensure_business_resource_version
AFTER INSERT ON staff
FOR EACH ROW EXECUTE FUNCTION phase3_ensure_business_resource_versions('staff');

CREATE TRIGGER customers_ensure_business_resource_version
AFTER INSERT ON customers
FOR EACH ROW EXECUTE FUNCTION phase3_ensure_business_resource_versions('customer');

-- Eligibility is a canonical salon business relationship. It must be
-- manageable before a ManleAI Calendar config root exists; the existing V48
-- version trigger still invalidates a config when one is present.
ALTER TABLE manleai_calendar_service_staff
    DROP CONSTRAINT manleai_calendar_service_staff_salon_id_fkey;

ALTER TABLE manleai_calendar_service_staff
    ADD CONSTRAINT manleai_calendar_service_staff_salon_id_fkey
    FOREIGN KEY (salon_id) REFERENCES salons(id) ON DELETE CASCADE;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'salon_profile', id::text, 1 FROM salons
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'public_catalog', id::text, 1 FROM salons
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'business_hours', id::text, 1 FROM salons
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon_id, 'service', id::text, 1 FROM services
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon_id, 'service_category', id::text, 1 FROM service_categories
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon_id, 'staff', id::text, 1 FROM staff
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'staff_service_eligibility', id::text, 1 FROM salons
ON CONFLICT DO NOTHING;

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon_id, 'customer', id::text, 1 FROM customers
ON CONFLICT DO NOTHING;
