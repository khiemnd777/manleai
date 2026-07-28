-- Platform-only, reviewed configuration transfer.
--
-- The transfer ledger stores fingerprints, version fences, safe summaries, and
-- the actual Platform actor. It never stores the configuration payload,
-- provider secrets, tokens, recordings, transcripts, or operational data.

ALTER TABLE business_resource_versions
    DROP CONSTRAINT IF EXISTS business_resource_versions_resource_type_check;

ALTER TABLE business_resource_versions
    ADD CONSTRAINT business_resource_versions_resource_type_check
        CHECK (resource_type IN (
            'salon_profile',
            'public_catalog',
            'business_hours',
            'service',
            'service_category',
            'staff',
            'staff_service_eligibility',
            'customer',
            'service_aliases',
            'consultation_profiles',
            'knowledge_base',
            'service_categories'
        ));

INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon.id, resource.resource_type, resource.resource_id, 1
FROM salons salon
CROSS JOIN (VALUES
    ('service_aliases', 'collection'),
    ('consultation_profiles', 'collection'),
    ('knowledge_base', 'collection'),
    ('service_categories', 'collection')
) AS resource(resource_type, resource_id)
ON CONFLICT DO NOTHING;

ALTER TABLE technical_resource_versions
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_type_check,
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_id_check,
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_identity_check;

ALTER TABLE technical_resource_versions
    ADD CONSTRAINT technical_resource_versions_resource_type_check
        CHECK (resource_type IN ('integration_config', 'ai_runtime', 'ai_receptionist')),
    ADD CONSTRAINT technical_resource_versions_resource_identity_check
        CHECK (
            (resource_type = 'integration_config' AND resource_id IN ('square', 'twilio', 'openai'))
            OR (resource_type = 'ai_runtime' AND resource_id = 'ai_booking')
            OR (resource_type = 'ai_receptionist' AND resource_id = 'policy')
        );

INSERT INTO technical_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'ai_receptionist', 'policy', 1
FROM salons
ON CONFLICT DO NOTHING;

INSERT INTO technical_resource_versions (salon_id, resource_type, resource_id, version)
SELECT salon.id, 'integration_config', provider.name, 0
FROM salons salon
CROSS JOIN (VALUES ('square'),('twilio'),('openai')) AS provider(name)
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION phase12_seed_transfer_resource_versions()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
    VALUES
        (NEW.id, 'service_aliases', 'collection', 1),
        (NEW.id, 'consultation_profiles', 'collection', 1),
        (NEW.id, 'knowledge_base', 'collection', 1),
        (NEW.id, 'service_categories', 'collection', 1)
    ON CONFLICT DO NOTHING;

    INSERT INTO technical_resource_versions (salon_id, resource_type, resource_id, version)
    VALUES
        (NEW.id, 'ai_receptionist', 'policy', 1),
        (NEW.id, 'integration_config', 'square', 0),
        (NEW.id, 'integration_config', 'twilio', 0),
        (NEW.id, 'integration_config', 'openai', 0)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END
$$;

CREATE TRIGGER salons_seed_transfer_resource_versions
AFTER INSERT ON salons
FOR EACH ROW EXECUTE FUNCTION phase12_seed_transfer_resource_versions();

CREATE OR REPLACE FUNCTION phase12_bump_transfer_collection_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_salon_id UUID;
    target_resource_type TEXT := TG_ARGV[0];
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_salon_id := OLD.salon_id;
    ELSE
        target_salon_id := NEW.salon_id;
    END IF;
    IF current_setting('app.configuration_transfer', true) = 'on' THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
    END IF;
    UPDATE business_resource_versions
    SET version = version + 1, updated_at = now()
    WHERE salon_id = target_salon_id
      AND resource_type = target_resource_type
      AND resource_id = 'collection';
    IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END
$$;

CREATE TRIGGER service_aliases_bump_transfer_version
AFTER INSERT OR UPDATE OR DELETE ON service_aliases
FOR EACH ROW EXECUTE FUNCTION phase12_bump_transfer_collection_version('service_aliases');

CREATE TRIGGER consultation_profiles_bump_transfer_version
AFTER INSERT OR UPDATE OR DELETE ON service_consultation_profiles
FOR EACH ROW EXECUTE FUNCTION phase12_bump_transfer_collection_version('consultation_profiles');

CREATE TRIGGER knowledge_items_bump_transfer_version
AFTER INSERT OR UPDATE OR DELETE ON knowledge_items
FOR EACH ROW EXECUTE FUNCTION phase12_bump_transfer_collection_version('knowledge_base');

CREATE TRIGGER service_categories_bump_transfer_collection_version
AFTER INSERT OR UPDATE OR DELETE ON service_categories
FOR EACH ROW EXECUTE FUNCTION phase12_bump_transfer_collection_version('service_categories');

CREATE TRIGGER service_category_aliases_bump_transfer_collection_version
AFTER INSERT OR UPDATE OR DELETE ON service_category_aliases
FOR EACH ROW EXECUTE FUNCTION phase12_bump_transfer_collection_version('service_categories');

CREATE OR REPLACE FUNCTION phase12_bump_business_row_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF current_setting('app.configuration_transfer', true) = 'on' THEN
        RETURN NEW;
    END IF;
    UPDATE business_resource_versions
    SET version = version + 1, updated_at = now()
    WHERE salon_id = NEW.salon_id
      AND resource_type = TG_ARGV[0]
      AND resource_id = NEW.id::text;
    RETURN NEW;
END
$$;

CREATE TRIGGER services_bump_business_resource_version
AFTER UPDATE ON services
FOR EACH ROW EXECUTE FUNCTION phase12_bump_business_row_version('service');

CREATE OR REPLACE FUNCTION phase12_bump_salon_versions()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF current_setting('app.configuration_transfer', true) = 'on' THEN
        RETURN NEW;
    END IF;
    IF (OLD.name,OLD.phone,OLD.address,OLD.city,OLD.state,OLD.zip_code,OLD.timezone,OLD.primary_language,OLD.secondary_language,OLD.handoff_phone)
       IS DISTINCT FROM
       (NEW.name,NEW.phone,NEW.address,NEW.city,NEW.state,NEW.zip_code,NEW.timezone,NEW.primary_language,NEW.secondary_language,NEW.handoff_phone) THEN
        UPDATE business_resource_versions SET version=version+1,updated_at=now()
        WHERE salon_id=NEW.id AND resource_type='salon_profile' AND resource_id=NEW.id::text;
    END IF;
    IF (OLD.public_slug,OLD.public_catalog_enabled) IS DISTINCT FROM (NEW.public_slug,NEW.public_catalog_enabled) THEN
        UPDATE business_resource_versions SET version=version+1,updated_at=now()
        WHERE salon_id=NEW.id AND resource_type='public_catalog' AND resource_id=NEW.id::text;
    END IF;
    IF OLD.ai_enabled IS DISTINCT FROM NEW.ai_enabled THEN
        UPDATE technical_resource_versions SET version=version+1,updated_at=now()
        WHERE salon_id=NEW.id AND resource_type='ai_runtime' AND resource_id='ai_booking';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER salons_bump_transfer_fences
AFTER UPDATE ON salons
FOR EACH ROW EXECUTE FUNCTION phase12_bump_salon_versions();

CREATE OR REPLACE FUNCTION phase12_bump_ai_receptionist_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF current_setting('app.configuration_transfer', true) = 'on' THEN
        RETURN NEW;
    END IF;
    IF (OLD.ai_greeting,OLD.ai_voice,OLD.ai_tone,OLD.booking_mode,OLD.recording_enabled,OLD.recording_consent_message,OLD.sms_confirmation_enabled,OLD.sms_reminder_enabled,OLD.reminder_hours_before,OLD.handoff_enabled,OLD.consultation_enabled)
       IS DISTINCT FROM
       (NEW.ai_greeting,NEW.ai_voice,NEW.ai_tone,NEW.booking_mode,NEW.recording_enabled,NEW.recording_consent_message,NEW.sms_confirmation_enabled,NEW.sms_reminder_enabled,NEW.reminder_hours_before,NEW.handoff_enabled,NEW.consultation_enabled) THEN
        UPDATE technical_resource_versions SET version=version+1,updated_at=now()
        WHERE salon_id=NEW.salon_id AND resource_type='ai_receptionist' AND resource_id='policy';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER salon_settings_bump_ai_receptionist_version
AFTER UPDATE ON salon_settings
FOR EACH ROW EXECUTE FUNCTION phase12_bump_ai_receptionist_version();

CREATE OR REPLACE FUNCTION phase12_bump_local_hours_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_salon_id UUID;
    target_source TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN target_salon_id := OLD.salon_id; target_source := OLD.source;
    ELSE target_salon_id := NEW.salon_id; target_source := NEW.source;
    END IF;
    IF current_setting('app.configuration_transfer', true) IS DISTINCT FROM 'on' AND target_source = 'local_override' THEN
        UPDATE business_resource_versions SET version=version+1,updated_at=now()
        WHERE salon_id=target_salon_id AND resource_type='business_hours' AND resource_id=target_salon_id::text;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END
$$;

CREATE TRIGGER local_hours_bump_business_resource_version
AFTER INSERT OR UPDATE OR DELETE ON salon_business_hour_periods
FOR EACH ROW EXECUTE FUNCTION phase12_bump_local_hours_version();

CREATE OR REPLACE FUNCTION phase12_bump_integration_config_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF current_setting('app.configuration_transfer', true) = 'on' THEN
        RETURN NEW;
    END IF;
    INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version)
    VALUES(NEW.salon_id,'integration_config',NEW.provider,1)
    ON CONFLICT(salon_id,resource_type,resource_id)
    DO UPDATE SET version=technical_resource_versions.version+1,updated_at=now();
    RETURN NEW;
END
$$;

CREATE TRIGGER integration_configs_bump_technical_resource_version
AFTER INSERT OR UPDATE ON salon_integration_configs
FOR EACH ROW EXECUTE FUNCTION phase12_bump_integration_config_version();

CREATE TABLE configuration_transfer_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    source_type TEXT NOT NULL CHECK (source_type IN ('tenant', 'json_upload')),
    source_salon_id UUID REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    schema_version TEXT NOT NULL
        CHECK (schema_version IN ('manleai.salon_configuration.v8', 'manleai.salon_configuration.v9')),
    included_sections TEXT[] NOT NULL CHECK (
        cardinality(included_sections) BETWEEN 1 AND 9
        AND included_sections <@ ARRAY[
            'salon_profile','ai_receptionist','public_booking_page','integrations','knowledge_base',
            'service_categories','service_aliases','service_consultation_profiles','local_business_hours'
        ]::TEXT[]
    ),
    source_fingerprint TEXT NOT NULL CHECK (source_fingerprint ~ '^[0-9a-f]{64}$'),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    target_fences JSONB NOT NULL DEFAULT '{}'::jsonb,
    target_scheduling_authority TEXT NOT NULL,
    target_scheduling_authority_version BIGINT NOT NULL CHECK (target_scheduling_authority_version >= 1),
    source_active_pos_provider TEXT NOT NULL DEFAULT '' CHECK (source_active_pos_provider=btrim(source_active_pos_provider) AND length(source_active_pos_provider) <= 64),
    target_active_pos_provider TEXT NOT NULL DEFAULT '' CHECK (target_active_pos_provider=btrim(target_active_pos_provider) AND length(target_active_pos_provider) <= 64),
    requires_secret_reentry TEXT[] NOT NULL DEFAULT '{}'::TEXT[] CHECK (
        cardinality(requires_secret_reentry) <= 3
        AND requires_secret_reentry <@ ARRAY['square','twilio','openai']::TEXT[]
    ),
    status TEXT NOT NULL CHECK (status IN ('previewed', 'applied')),
    action_key TEXT CHECK (action_key IS NULL OR (action_key=btrim(action_key) AND length(action_key) BETWEEN 1 AND 120)),
    summary JSONB NOT NULL DEFAULT '[]'::jsonb,
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    conflicts JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at TIMESTAMPTZ,
    CONSTRAINT configuration_transfer_runs_source_check CHECK (
        (source_type = 'tenant' AND source_salon_id IS NOT NULL AND source_salon_id <> salon_id)
        OR (source_type = 'json_upload' AND source_salon_id IS NULL)
    ),
    CONSTRAINT configuration_transfer_runs_action_check CHECK (
        (status = 'previewed' AND action_key IS NULL AND applied_at IS NULL)
        OR (status = 'applied' AND action_key IS NOT NULL AND applied_at IS NOT NULL)
    ),
    CONSTRAINT configuration_transfer_runs_safe_json_check CHECK (
        jsonb_typeof(target_fences) = 'object'
        AND jsonb_typeof(summary) = 'array'
        AND jsonb_typeof(warnings) = 'array'
        AND jsonb_typeof(conflicts) = 'array'
        AND octet_length(target_fences::text) <= 262144
        AND octet_length(summary::text) <= 16384
        AND octet_length(warnings::text) <= 32768
        AND octet_length(conflicts::text) <= 32768
    )
);

CREATE UNIQUE INDEX uq_configuration_transfer_runs_action
    ON configuration_transfer_runs (salon_id, actor_user_id, action_key)
    WHERE action_key IS NOT NULL;

CREATE INDEX idx_configuration_transfer_runs_target_created
    ON configuration_transfer_runs (salon_id, created_at DESC, id DESC);

CREATE TABLE configuration_transfer_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES configuration_transfer_runs(id) ON DELETE RESTRICT,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL CHECK (event_type IN ('configuration_transfer.previewed', 'configuration_transfer.applied')),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT configuration_transfer_events_safe_details_check CHECK (
        jsonb_typeof(details) = 'object'
        AND octet_length(details::text) <= 4096
        AND details - ARRAY['source_type', 'source_salon_id', 'included_sections', 'schema_version']::text[] = '{}'::jsonb
    )
);

CREATE INDEX idx_configuration_transfer_events_target_created
    ON configuration_transfer_events (salon_id, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION phase12_reject_transfer_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER configuration_transfer_events_immutable
BEFORE UPDATE OR DELETE ON configuration_transfer_events
FOR EACH ROW EXECUTE FUNCTION phase12_reject_transfer_event_mutation();

CREATE OR REPLACE FUNCTION phase12_guard_transfer_run_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status <> 'previewed' OR NEW.status <> 'applied'
       OR OLD.id IS DISTINCT FROM NEW.id
       OR OLD.salon_id IS DISTINCT FROM NEW.salon_id
       OR OLD.source_type IS DISTINCT FROM NEW.source_type
       OR OLD.source_salon_id IS DISTINCT FROM NEW.source_salon_id
       OR OLD.actor_user_id IS DISTINCT FROM NEW.actor_user_id
       OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
       OR OLD.included_sections IS DISTINCT FROM NEW.included_sections
       OR OLD.source_fingerprint IS DISTINCT FROM NEW.source_fingerprint
       OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
       OR OLD.target_fences IS DISTINCT FROM NEW.target_fences
       OR OLD.target_scheduling_authority IS DISTINCT FROM NEW.target_scheduling_authority
       OR OLD.target_scheduling_authority_version IS DISTINCT FROM NEW.target_scheduling_authority_version
       OR OLD.source_active_pos_provider IS DISTINCT FROM NEW.source_active_pos_provider
       OR OLD.target_active_pos_provider IS DISTINCT FROM NEW.target_active_pos_provider
       OR OLD.requires_secret_reentry IS DISTINCT FROM NEW.requires_secret_reentry
       OR OLD.summary IS DISTINCT FROM NEW.summary
       OR OLD.warnings IS DISTINCT FROM NEW.warnings
       OR OLD.conflicts IS DISTINCT FROM NEW.conflicts
       OR OLD.created_at IS DISTINCT FROM NEW.created_at
       OR NEW.action_key IS NULL OR NEW.applied_at IS NULL THEN
        RAISE EXCEPTION 'configuration_transfer_runs identity and reviewed evidence are immutable';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER configuration_transfer_runs_guard_update
BEFORE UPDATE OR DELETE ON configuration_transfer_runs
FOR EACH ROW EXECUTE FUNCTION phase12_guard_transfer_run_update();

ALTER TABLE configuration_transfer_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE configuration_transfer_events ENABLE ROW LEVEL SECURITY;

CREATE POLICY configuration_transfer_runs_select ON configuration_transfer_runs
    FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id, NULL, false));
CREATE POLICY configuration_transfer_runs_insert ON configuration_transfer_runs
    FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id, NULL));
CREATE POLICY configuration_transfer_runs_update ON configuration_transfer_runs
    FOR UPDATE USING (public.app_rls_salon_write_allowed(salon_id, NULL))
    WITH CHECK (public.app_rls_salon_write_allowed(salon_id, NULL));

CREATE POLICY configuration_transfer_events_select ON configuration_transfer_events
    FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id, NULL, false));
CREATE POLICY configuration_transfer_events_insert ON configuration_transfer_events
    FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id, NULL));

COMMENT ON TABLE configuration_transfer_runs IS
'Platform-only reviewed transfer runs. Contains safe fingerprints, fences, summaries, and actual actor identity; never raw configuration or secrets.';
COMMENT ON TABLE configuration_transfer_events IS
'Immutable safe audit events for Platform configuration preview and apply.';
