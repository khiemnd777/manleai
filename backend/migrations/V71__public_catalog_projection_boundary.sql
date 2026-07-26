-- SaaS Phase 10 public projection boundary.
--
-- RLS is a row boundary, not a column masker. Public requests must therefore
-- never receive direct SELECT visibility on base tables that also contain
-- owner IDs, staff contact data, provider IDs, or encrypted provider tokens.
-- These SECURITY DEFINER readers return only the established public Catalog
-- contract after re-evaluating current scheduling-authority readiness.

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
           COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square') AS provider,
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
                 AND connection.provider=COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square')
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

CREATE OR REPLACE FUNCTION public.read_first_public_catalog()
RETURNS JSONB
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT candidate.catalog
    FROM (
        SELECT public.read_public_catalog(salon.public_slug) AS catalog,
               salon.created_at,
               salon.id
        FROM public.salons salon
        WHERE salon.public_catalog_enabled=true
          AND NULLIF(salon.public_slug, '') IS NOT NULL
    ) candidate
    WHERE candidate.catalog IS NOT NULL
    ORDER BY candidate.created_at, candidate.id
    LIMIT 1
$$;

REVOKE ALL ON FUNCTION public.read_public_catalog(TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.read_first_public_catalog() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.read_public_catalog(TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.read_first_public_catalog() TO PUBLIC;

CREATE OR REPLACE FUNCTION public.app_rls_salon_select_allowed(
    target_salon_id UUID,
    required_pii_scope TEXT,
    public_catalog_table BOOLEAN
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT CASE public.app_database_scope()
        WHEN 'worker' THEN true
        WHEN 'provider' THEN true
        WHEN 'public' THEN false
        ELSE public.app_rls_actor_salon_access(target_salon_id, required_pii_scope)
    END
$$;

COMMENT ON FUNCTION public.read_public_catalog(TEXT) IS
'The only database-owned public salon projection. It returns no tenant actor, staff contact, provider identifier, provider token, or diagnostic fields.';
COMMENT ON FUNCTION public.app_rls_salon_select_allowed(UUID, TEXT, BOOLEAN) IS
'Base-table tenant rows are never directly visible to public scope; public catalog reads must use read_public_catalog.';
