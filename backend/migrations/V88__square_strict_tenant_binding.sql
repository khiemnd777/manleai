-- Strict tenant ownership for Square/POS runtime identity.
--
-- Release A keeps the historical database default for active_pos_provider so
-- existing provisioning remains rollback-safe. Runtime code no longer treats
-- a blank provider as Square, and this migration permits an explicit blank
-- state for the reviewed initial-activation workflow.

DO $$
DECLARE
    duplicate_identity RECORD;
BEGIN
    SELECT lower(btrim(provider)) AS provider,
           btrim(merchant_id) AS merchant_id,
           btrim(location_id) AS location_id,
           array_agg(salon_id ORDER BY salon_id) AS salon_ids
    INTO duplicate_identity
    FROM pos_connections
    WHERE NULLIF(btrim(provider), '') IS NOT NULL
      AND NULLIF(btrim(merchant_id), '') IS NOT NULL
      AND NULLIF(btrim(location_id), '') IS NOT NULL
    GROUP BY lower(btrim(provider)), btrim(merchant_id), btrim(location_id)
    HAVING count(*) > 1
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION
            'V88 duplicate POS tenant identity: provider=%, merchant_id=%, location_id=%, salon_ids=%',
            duplicate_identity.provider,
            duplicate_identity.merchant_id,
            duplicate_identity.location_id,
            duplicate_identity.salon_ids;
    END IF;
END
$$;

UPDATE pos_connections
SET provider = lower(btrim(provider)),
    merchant_id = NULLIF(btrim(merchant_id), ''),
    location_id = NULLIF(btrim(location_id), ''),
    updated_at = now()
WHERE provider IS DISTINCT FROM lower(btrim(provider))
   OR merchant_id IS DISTINCT FROM NULLIF(btrim(merchant_id), '')
   OR location_id IS DISTINCT FROM NULLIF(btrim(location_id), '');

ALTER TABLE pos_connections
    DROP CONSTRAINT IF EXISTS pos_connections_tenant_identity_normalized_check,
    ADD CONSTRAINT pos_connections_tenant_identity_normalized_check
        CHECK (
            provider = lower(btrim(provider))
            AND length(provider) BETWEEN 1 AND 100
            AND (merchant_id IS NULL OR (merchant_id = btrim(merchant_id) AND length(merchant_id) BETWEEN 1 AND 255))
            AND (location_id IS NULL OR (location_id = btrim(location_id) AND length(location_id) BETWEEN 1 AND 255))
        );

CREATE UNIQUE INDEX pos_connections_provider_merchant_location_tenant_unique
    ON pos_connections(lower(btrim(provider)), btrim(merchant_id), btrim(location_id))
    WHERE NULLIF(btrim(merchant_id), '') IS NOT NULL
      AND NULLIF(btrim(location_id), '') IS NOT NULL;

UPDATE salons
SET active_pos_provider = lower(btrim(active_pos_provider)),
    updated_at = now()
WHERE active_pos_provider IS DISTINCT FROM lower(btrim(active_pos_provider));

ALTER TABLE salons
    DROP CONSTRAINT IF EXISTS salons_active_pos_provider_check,
    ADD CONSTRAINT salons_active_pos_provider_check
        CHECK (
            active_pos_provider = lower(btrim(active_pos_provider))
            AND length(active_pos_provider) <= 100
        );

ALTER TABLE technical_resource_versions
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_type_check,
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_id_check,
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_identity_check;

ALTER TABLE technical_resource_versions
    ADD CONSTRAINT technical_resource_versions_resource_type_check
        CHECK (resource_type IN ('integration_config', 'ai_runtime', 'ai_receptionist', 'pos_adapter')),
    ADD CONSTRAINT technical_resource_versions_resource_identity_check
        CHECK (
            (resource_type = 'integration_config' AND resource_id IN ('square', 'twilio', 'openai'))
            OR (resource_type = 'ai_runtime' AND resource_id = 'ai_booking')
            OR (resource_type = 'ai_receptionist' AND resource_id = 'policy')
            OR (resource_type = 'pos_adapter' AND resource_id = 'active_provider')
        );

INSERT INTO technical_resource_versions (salon_id, resource_type, resource_id, version)
SELECT id, 'pos_adapter', 'active_provider', 0
FROM salons
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION phase12_seed_transfer_resource_versions()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
    VALUES
        (NEW.id, 'service', 'collection', 1),
        (NEW.id, 'staff', 'collection', 1),
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
        (NEW.id, 'integration_config', 'openai', 0),
        (NEW.id, 'pos_adapter', 'active_provider', 0)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION public.read_public_catalog(target_slug TEXT)
RETURNS JSONB
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    source RECORD;
    service_rows JSONB := '[]'::jsonb;
    staff_rows JSONB := '[]'::jsonb;
    hour_rows JSONB := '[]'::jsonb;
BEGIN
    SELECT salon.id AS salon_id,
           salon.public_slug,
           salon.name,
           salon.phone,
           COALESCE(salon.address, '') AS address,
           COALESCE(salon.city, '') AS city,
           COALESCE(salon.state, '') AS state,
           COALESCE(salon.zip_code, '') AS zip_code,
           salon.timezone,
           salon.primary_language,
           COALESCE(salon.secondary_language, '') AS secondary_language,
           btrim(salon.active_pos_provider) AS provider,
           settings.scheduling_authority,
           settings.scheduling_authority_version,
           EXISTS (
               SELECT 1 FROM manleai_calendar_configs config
               WHERE config.salon_id=salon.id
                 AND config.activated_at IS NOT NULL
                 AND config.activated_version=config.version
           ) AS internal_activation_current,
           EXISTS (
               SELECT 1 FROM pos_connections connection
               WHERE connection.salon_id=salon.id
                 AND connection.provider=btrim(salon.active_pos_provider)
                 AND connection.status='active'
                 AND NULLIF(connection.location_id, '') IS NOT NULL
                 AND connection.last_sync_at IS NOT NULL
                 AND connection.snapshot_generation > 0
           ) AS external_connection_ready
    INTO source
    FROM salons salon
    JOIN salon_settings settings ON settings.salon_id=salon.id
    WHERE salon.public_catalog_enabled=true
      AND lower(salon.public_slug)=lower(target_slug)
    LIMIT 1;

    IF source.salon_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
        'name', service.name,
        'description', COALESCE(service.description, ''),
        'ai_description', COALESCE(service.ai_description, ''),
        'duration_minutes', service.duration_minutes,
        'price_from', service.price_from,
        'price_display', COALESCE(service.price_display, '')
    )) ORDER BY service.name, service.id), '[]'::jsonb)
    INTO service_rows
    FROM services service
    WHERE service.salon_id=source.salon_id
      AND service.active=true
      AND service.ai_bookable=true
      AND service.archived_at IS NULL
      AND service.duration_minutes > 0
      AND (
          source.scheduling_authority='owner_manual'
          OR (
              source.scheduling_authority='manleai_calendar'
              AND EXISTS (
                  SELECT 1 FROM manleai_calendar_service_policies policy
                  WHERE policy.salon_id=service.salon_id
                    AND policy.service_id=service.id
                    AND policy.enabled=true
              )
          )
          OR (
              source.scheduling_authority='external_provider'
              AND service.pos_provider=source.provider
              AND service.sync_status='synced'
              AND COALESCE(service.pos_service_version, 0) > 0
              AND EXISTS (
                  SELECT 1 FROM pos_entity_links link
                  WHERE link.salon_id=service.salon_id
                    AND link.entity_type='service'
                    AND link.entity_id=service.id
                    AND link.provider=source.provider
                    AND link.sync_status='synced'
                    AND NULLIF(link.provider_entity_id, '') IS NOT NULL
              )
          )
      );

    SELECT COALESCE(jsonb_agg(jsonb_build_object('name', staff.name) ORDER BY staff.name, staff.id), '[]'::jsonb)
    INTO staff_rows
    FROM staff staff
    WHERE staff.salon_id=source.salon_id
      AND staff.active=true
      AND staff.ai_bookable=true
      AND staff.archived_at IS NULL
      AND (
          source.scheduling_authority='owner_manual'
          OR (
              source.scheduling_authority='manleai_calendar'
              AND EXISTS (
                  SELECT 1
                  FROM manleai_calendar_service_staff eligible
                  JOIN manleai_calendar_service_policies policy
                    ON policy.salon_id=eligible.salon_id
                   AND policy.service_id=eligible.service_id
                   AND policy.enabled=true
                  WHERE eligible.salon_id=staff.salon_id
                    AND eligible.staff_id=staff.id
              )
              AND EXISTS (
                  SELECT 1 FROM manleai_calendar_staff_weekly_periods weekly
                  WHERE weekly.salon_id=staff.salon_id AND weekly.staff_id=staff.id
              )
          )
          OR (
              source.scheduling_authority='external_provider'
              AND staff.pos_provider=source.provider
              AND staff.sync_status='synced'
              AND EXISTS (
                  SELECT 1 FROM pos_entity_links link
                  WHERE link.salon_id=staff.salon_id
                    AND link.entity_type='staff'
                    AND link.entity_id=staff.id
                    AND link.provider=source.provider
                    AND link.sync_status='synced'
                    AND NULLIF(link.provider_entity_id, '') IS NOT NULL
              )
          )
      );

    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'day_of_week', period.day_of_week,
        'start_local_time', period.start_local_time::text,
        'end_local_time', period.end_local_time::text,
        'source', period.source
    ) ORDER BY period.day_of_week, period.start_local_time, period.provider_period_index), '[]'::jsonb)
    INTO hour_rows
    FROM salon_business_hour_periods period
    WHERE period.salon_id=source.salon_id
      AND (
          (source.scheduling_authority='owner_manual' AND period.source IN ('local_override','local_migrated'))
          OR (source.scheduling_authority='manleai_calendar' AND period.source='local_override')
          OR (source.scheduling_authority='external_provider' AND period.source='imported' AND period.provider=source.provider)
      );

    IF jsonb_array_length(service_rows)=0
       OR (source.scheduling_authority='manleai_calendar' AND (jsonb_array_length(hour_rows)=0 OR NOT source.internal_activation_current))
       OR (source.scheduling_authority='external_provider' AND (jsonb_array_length(staff_rows)=0 OR NOT source.external_connection_ready))
       OR source.scheduling_authority NOT IN ('owner_manual','manleai_calendar','external_provider') THEN
        RETURN NULL;
    END IF;

    RETURN jsonb_build_object(
        'salon', jsonb_build_object(
            'slug', source.public_slug,
            'name', source.name,
            'phone', source.phone,
            'address', source.address,
            'city', source.city,
            'state', source.state,
            'zip_code', source.zip_code,
            'timezone', source.timezone,
            'primary_language', source.primary_language,
            'secondary_language', source.secondary_language
        ),
        'scheduling_authority', source.scheduling_authority,
        'scheduling_authority_version', source.scheduling_authority_version,
        'services', service_rows,
        'staff', staff_rows,
        'hours', hour_rows,
        'booking_note', 'Call the salon to request an appointment. Availability and confirmation are provided by the salon.'
    );
END
$$;

COMMENT ON FUNCTION public.read_public_catalog(TEXT) IS
'The tenant-bound public salon projection. A blank active provider stays unconfigured and never resolves to Square.';
