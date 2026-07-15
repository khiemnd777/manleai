ALTER TABLE salon_settings
    ALTER COLUMN consultation_enabled SET DEFAULT false;

UPDATE service_consultation_profiles
SET status = 'draft',
    revision = revision + 1,
    updated_at = now()
WHERE status = 'ready'
  AND (
      jsonb_array_length(recommended_outcomes) = 0
      OR jsonb_array_length(compatible_current_systems) = 0
  );

ALTER TABLE service_consultation_profiles
    ADD CONSTRAINT chk_service_consultation_profile_ready_complete
    CHECK (
        status <> 'ready'
        OR (
            jsonb_array_length(recommended_outcomes) > 0
            AND jsonb_array_length(compatible_current_systems) > 0
        )
    );

UPDATE salon_settings settings
SET consultation_enabled = false,
    updated_at = now()
WHERE settings.consultation_enabled = true
  AND NOT EXISTS (
      SELECT 1
      FROM salons salon
      JOIN services svc
        ON svc.salon_id = salon.id
       AND svc.pos_provider = salon.active_pos_provider
       AND svc.active = true
       AND svc.ai_bookable = true
       AND svc.archived_at IS NULL
       AND svc.sync_status = 'synced'
       AND svc.duration_minutes > 0
      JOIN pos_entity_links link
        ON link.salon_id = svc.salon_id
       AND link.entity_type = 'service'
       AND link.entity_id = svc.id
       AND link.provider = salon.active_pos_provider
       AND link.sync_status = 'synced'
       AND COALESCE(link.provider_entity_id, '') <> ''
       AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
      JOIN service_consultation_profiles profile
        ON profile.salon_id = svc.salon_id
       AND profile.service_id = svc.id
       AND profile.status = 'ready'
       AND jsonb_array_length(profile.recommended_outcomes) > 0
       AND jsonb_array_length(profile.compatible_current_systems) > 0
      WHERE salon.id = settings.salon_id
  );
