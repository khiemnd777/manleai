package voice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ResolveSalonOwnerForPlatform(ctx context.Context, salonID string, platformUserID string) (string, error) {
	var ownerUserID string
	err := r.db.QueryRowContext(ctx, `
		SELECT salon.owner_user_id::text
		FROM salons salon
		WHERE salon.id = $1
		  AND public.app_active_support_authorization($2::uuid, salon.id, 'calls.read')
		  AND public.app_active_support_pii_grant($2::uuid, salon.id, 'calls.read', 'calls')
	`, salonID, platformUserID).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return ownerUserID, nil
}

func (r *Repository) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error) {
	var status SalonVoiceStatus
	err := r.db.QueryRowContext(ctx, `
		SELECT salon.id::text, COALESCE(salon.phone, ''), salon.ai_enabled,
		       settings.scheduling_authority, settings.scheduling_authority_version,
		       settings.booking_mode
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id = salon.id
		WHERE salon.id = $1
		  AND public.has_active_tenant_membership(salon.id, $2::uuid)
	`, salonID, ownerUserID).Scan(
		&status.SalonID, &status.Phone, &status.AIEnabled,
		&status.SchedulingAuthority, &status.SchedulingAuthorityVersion, &status.BookingMode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &status, nil
}

func (r *Repository) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*PhoneBookingReadiness, error) {
	readiness := PhoneBookingReadiness{}
	var bookingWriteBlockedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT
			s.ai_enabled,
			COALESCE(settings.consultation_enabled, false),
			s.active_pos_provider,
			EXISTS (
				SELECT 1
				FROM pos_connections pc
				WHERE pc.salon_id = s.id
				  AND pc.provider = s.active_pos_provider
				  AND pc.status = 'active'
				  AND COALESCE(pc.location_id, '') <> ''
			) AS provider_connected,
			EXISTS (
				SELECT 1
				FROM pos_connections pc
				WHERE pc.salon_id = s.id
				  AND pc.provider = s.active_pos_provider
				  AND pc.status = 'active'
				  AND COALESCE(pc.location_id, '') <> ''
				  AND pc.snapshot_generation > 0
				  AND pc.last_sync_at IS NOT NULL
			) AS provider_synced,
			(
				SELECT COUNT(DISTINCT svc.id)
				FROM services svc
				JOIN pos_entity_links link
				  ON link.salon_id = svc.salon_id
				 AND link.entity_type = 'service'
				 AND link.entity_id = svc.id
				 AND link.provider = s.active_pos_provider
				 AND link.sync_status = 'synced'
				 AND NULLIF(BTRIM(link.provider_entity_id), '') IS NOT NULL
				WHERE svc.salon_id = s.id
				  AND svc.pos_provider = s.active_pos_provider
				  AND svc.active = true
				  AND svc.ai_bookable = true
				  AND svc.archived_at IS NULL
				  AND svc.sync_status = 'synced'
				  AND svc.duration_minutes > 0
				  AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
			) AS guidance_service_count,
			(
				SELECT COUNT(DISTINCT svc.id)
				FROM services svc
				JOIN pos_connections pc
				  ON pc.salon_id = svc.salon_id
				 AND pc.provider = s.active_pos_provider
				 AND pc.status = 'active'
				 AND NULLIF(BTRIM(pc.location_id), '') IS NOT NULL
				 AND pc.snapshot_generation > 0
				 AND pc.last_sync_at IS NOT NULL
				JOIN pos_entity_links link
				  ON link.salon_id = svc.salon_id
				 AND link.entity_type = 'service'
				 AND link.entity_id = svc.id
				 AND link.provider = s.active_pos_provider
				 AND link.sync_status = 'synced'
				 AND NULLIF(BTRIM(link.provider_entity_id), '') IS NOT NULL
				WHERE svc.salon_id = s.id
				  AND svc.pos_provider = s.active_pos_provider
				  AND svc.active = true
				  AND svc.ai_bookable = true
				  AND svc.archived_at IS NULL
				  AND svc.sync_status = 'synced'
				  AND svc.duration_minutes > 0
				  AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
			) AS service_count,
			(
				SELECT COUNT(DISTINCT svc.id)
				FROM services svc
				JOIN pos_entity_links link
				  ON link.salon_id = svc.salon_id
				 AND link.entity_type = 'service'
				 AND link.entity_id = svc.id
				 AND link.provider = s.active_pos_provider
				 AND link.sync_status = 'synced'
				 AND NULLIF(BTRIM(link.provider_entity_id), '') IS NOT NULL
				JOIN service_consultation_profiles profile
				  ON profile.salon_id = svc.salon_id
				 AND profile.service_id = svc.id
				 AND profile.status = 'ready'
				 AND jsonb_array_length(profile.recommended_outcomes) > 0
				 AND jsonb_array_length(profile.compatible_current_systems) > 0
				WHERE svc.salon_id = s.id
				  AND svc.pos_provider = s.active_pos_provider
				  AND svc.active = true
				  AND svc.ai_bookable = true
				  AND svc.archived_at IS NULL
				  AND svc.sync_status = 'synced'
				  AND svc.duration_minutes > 0
				  AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
			) AS consultation_ready_service_count,
			(
				SELECT COUNT(DISTINCT st.id)
				FROM staff st
				JOIN pos_connections pc
				  ON pc.salon_id = st.salon_id
				 AND pc.provider = s.active_pos_provider
				 AND pc.status = 'active'
				 AND NULLIF(BTRIM(pc.location_id), '') IS NOT NULL
				 AND pc.snapshot_generation > 0
				 AND pc.last_sync_at IS NOT NULL
				JOIN pos_entity_links link
				  ON link.salon_id = st.salon_id
				 AND link.entity_type = 'staff'
				 AND link.entity_id = st.id
				 AND link.provider = s.active_pos_provider
				 AND link.sync_status = 'synced'
				 AND NULLIF(BTRIM(link.provider_entity_id), '') IS NOT NULL
				WHERE st.salon_id = s.id
				  AND st.pos_provider = s.active_pos_provider
				  AND st.active = true
				  AND st.ai_bookable = true
				  AND st.archived_at IS NULL
				  AND st.sync_status = 'synced'
			) AS staff_count,
				(
					SELECT COUNT(*)
					FROM salon_business_hour_periods bhp
					WHERE bhp.salon_id = s.id
					  AND bhp.source = 'imported'
					  AND bhp.provider = s.active_pos_provider
					  AND bhp.start_local_time IS NOT NULL
					  AND bhp.end_local_time IS NOT NULL
					  AND bhp.end_local_time > bhp.start_local_time
				) AS business_hours_count,
				EXISTS (
					SELECT 1
					FROM (
						SELECT ba.status, ba.pos_provider, COALESCE(ba.pos_booking_id, '') AS pos_booking_id
						FROM booking_attempts ba
						WHERE ba.salon_id = s.id
						  AND ba.source = 'square_test_booking'
						ORDER BY ba.created_at DESC
						LIMIT 1
					) latest
					JOIN appointments appt ON appt.salon_id = s.id
					                       AND appt.pos_provider = latest.pos_provider
					                       AND appt.pos_appointment_id = latest.pos_booking_id
					WHERE latest.status = 'cancelled'
					  AND latest.pos_booking_id <> ''
					  AND appt.status = 'cancelled'
				) AS test_booking_cancelled,
				COALESCE(booking_write_blocker.error_code, '') AS booking_write_blocked_code,
				COALESCE(booking_write_blocker.error_message, '') AS booking_write_blocked_reason,
				booking_write_blocker.created_at AS booking_write_blocked_at
			FROM salons s
			LEFT JOIN LATERAL (
				SELECT pe.error_code, pe.error_message, pe.created_at
				FROM pos_errors pe
				WHERE pe.salon_id = s.id
				  AND pe.provider = s.active_pos_provider
				  AND pe.operation = 'create_booking'
				  AND pe.error_code = 'POS_PERMISSION_DENIED'
				  AND NOT EXISTS (
				    SELECT 1
				    FROM booking_attempts ba
				    WHERE ba.salon_id = s.id
				      AND ba.source = 'square_test_booking'
				      AND COALESCE(ba.pos_booking_id, '') <> ''
				      AND COALESCE(ba.error_code, '') = ''
				      AND ba.created_at > pe.created_at
				  )
				ORDER BY pe.created_at DESC
				LIMIT 1
			) booking_write_blocker ON true
			LEFT JOIN salon_settings settings ON settings.salon_id = s.id
			WHERE s.id = $1
			  AND public.has_active_tenant_membership(s.id, $2::uuid)
	`, salonID, ownerUserID).Scan(
		&readiness.AIEnabled,
		&readiness.ConsultationEnabled,
		&readiness.ActiveProvider,
		&readiness.ProviderConnected,
		&readiness.ProviderSynced,
		&readiness.GuidanceServiceCount,
		&readiness.ServiceCount,
		&readiness.ConsultationReadyServices,
		&readiness.StaffCount,
		&readiness.BusinessHoursCount,
		&readiness.TestBookingCancelled,
		&readiness.BookingWriteBlockedCode,
		&readiness.BookingWriteBlockedReason,
		&bookingWriteBlockedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	readiness.SquareConnected = readiness.ActiveProvider == "square" && readiness.ProviderConnected
	readiness.SquareSynced = readiness.ActiveProvider == "square" && readiness.ProviderSynced
	readiness.BookingWriteBlocked = readiness.BookingWriteBlockedCode != ""
	if bookingWriteBlockedAt.Valid {
		readiness.BookingWriteBlockedAt = &bookingWriteBlockedAt.Time
	}
	readiness.ServiceGuidance = serviceGuidanceReadiness(
		readiness.GuidanceServiceCount, readiness.ConsultationEnabled, readiness.ConsultationReadyServices,
	)
	providerLabel := readiness.ActiveProvider
	if providerLabel == "square" {
		providerLabel = "Square Appointments"
	}
	bookingWritesReady := !readiness.BookingWriteBlocked
	bookingWriteMessage := readiness.BookingWriteBlockedReason
	if bookingWriteMessage == "" {
		bookingWriteMessage = "Square Appointments rejected booking writes."
	}

	readiness.Checks = []ReadinessCheck{
		{Key: "connect_active_provider", Label: "Connect active POS provider", Complete: readiness.ProviderConnected, Message: incompleteReadinessMessage(readiness.ProviderConnected, providerLabel+" is not connected with a selected location.")},
		{Key: "sync_active_provider", Label: "Sync active POS provider", Complete: readiness.ProviderSynced, Message: incompleteReadinessMessage(readiness.ProviderSynced, providerLabel+" records have not been synced.")},
		{Key: "bookable_services", Label: "AI-bookable services", Complete: readiness.ServiceCount > 0, Message: incompleteReadinessMessage(readiness.ServiceCount > 0, "No active AI-bookable service is synced from the active POS provider.")},
		{Key: "bookable_staff", Label: "AI-bookable staff", Complete: readiness.StaffCount > 0, Message: incompleteReadinessMessage(readiness.StaffCount > 0, "No active AI-bookable staff member is synced from the active POS provider.")},
		{Key: "business_hours", Label: "Business hours", Complete: readiness.BusinessHoursCount > 0, Message: incompleteReadinessMessage(readiness.BusinessHoursCount > 0, "Salon business hours are not configured.")},
		{Key: "booking_writes", Label: "Square booking writes", Complete: bookingWritesReady, Message: incompleteReadinessMessage(bookingWritesReady, bookingWriteMessage)},
		{Key: "enable_ai_booking", Label: "Enable AI booking", Complete: readiness.AIEnabled, Message: incompleteReadinessMessage(readiness.AIEnabled, "AI booking is disabled for this salon.")},
	}
	readiness.Ready = true
	for _, check := range readiness.Checks {
		if !check.Complete {
			readiness.Ready = false
			if readiness.BlockedReason == "" {
				readiness.BlockedReason = check.Message
			}
		}
	}
	return &readiness, nil
}

func serviceGuidanceReadiness(serviceCount int, consultationEnabled bool, readyServiceCount int) ServiceGuidanceReadiness {
	result := ServiceGuidanceReadiness{
		CatalogAvailable: serviceCount > 0, ConsultationEnabled: consultationEnabled,
		ReadyServiceCount: readyServiceCount,
	}
	switch {
	case serviceCount == 0:
		result.Status = conversation.ServiceGuidanceCapabilityCatalogUnavailable
		result.Message = "The runtime service catalog is unavailable; sync at least one active provider-linked AI-bookable service."
	case !consultationEnabled:
		result.Status = conversation.ServiceGuidanceCapabilityDisabled
		result.Message = "The service catalog is available, but personalized consultation is disabled."
	case readyServiceCount == 0:
		result.Status = conversation.ServiceGuidanceCapabilityCatalogOnly
		result.Message = "The service catalog is available, but no owner-approved consultation profile is ready."
	default:
		result.Status = conversation.ServiceGuidanceCapabilityRecommendationReady
		result.RecommendationReady = true
	}
	return result
}

func (r *Repository) FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error) {
	var salon InboundSalon
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id::text, s.owner_user_id::text, s.name, COALESCE(s.phone, ''),
		       COALESCE(ss.recording_enabled, true), COALESCE(ss.recording_consent_message, '')
		FROM salons s
		LEFT JOIN salon_settings ss ON ss.salon_id = s.id
		WHERE regexp_replace(COALESCE(s.phone, ''), '[^0-9]', '', 'g') = regexp_replace($1, '[^0-9]', '', 'g')
		  AND COALESCE(s.phone, '') <> ''
		ORDER BY s.created_at ASC
		LIMIT 1
	`, phone).Scan(&salon.SalonID, &salon.OwnerUserID, &salon.SalonName, &salon.Phone, &salon.RecordingEnabled, &salon.RecordingConsentMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &salon, nil
}

func incompleteReadinessMessage(complete bool, message string) string {
	if complete {
		return ""
	}
	return message
}

func (r *Repository) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*CallRoute, error) {
	var route CallRoute
	err := r.db.QueryRowContext(ctx, `
		SELECT cs.salon_id::text, s.owner_user_id::text, cs.id::text
		FROM call_sessions cs
		JOIN salons s ON s.id = cs.salon_id
		WHERE cs.provider = $1
		  AND cs.provider_call_id = $2
		LIMIT 1
	`, provider, providerCallID).Scan(&route.SalonID, &route.OwnerUserID, &route.SessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &route, nil
}

func (r *Repository) RecordWebhookEvent(ctx context.Context, event WebhookEvent) error {
	payload := event.Payload
	if payload == nil {
		payload = map[string]string{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO voice_webhook_events (
			salon_id, call_session_id, provider, provider_call_id, event_type, payload
		)
		VALUES (
			NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, NULLIF($4, ''), $5, $6::jsonb
		)
	`, event.SalonID, event.CallSessionID, event.Provider, event.ProviderCallID, event.EventType, string(raw))
	return err
}

func (r *Repository) HasTerminalRealtimeFailure(ctx context.Context, provider string, providerCallID string, sessionID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM voice_webhook_events
			WHERE provider = $1
			  AND provider_call_id = $2
			  AND event_type = 'realtime_failed'
			  AND (
			       COALESCE($3, '') = ''
			       OR call_session_id::text = $3
			       OR call_session_id IS NULL
			  )
			  AND (
			       payload->>'terminal' = 'true'
			       OR lower(COALESCE(payload->>'StreamEvent', payload->>'stream_event', '')) = 'stream-error'
			  )
			LIMIT 1
		)
	`, provider, providerCallID, sessionID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) SaveAudioOutput(ctx context.Context, record AudioOutputRecord) (*AudioOutput, error) {
	contentType := record.ContentType
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	var output AudioOutput
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO voice_audio_outputs (
			salon_id, call_session_id, provider, provider_call_id, content_type, audio_data, expires_at
		)
		VALUES (
			NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, NULLIF($4, ''), $5, $6, $7
		)
		RETURNING id::text, COALESCE(salon_id::text, ''), COALESCE(call_session_id::text, ''),
		          provider, COALESCE(provider_call_id, ''), content_type, audio_data, expires_at
	`, record.SalonID, record.CallSessionID, record.Provider, record.ProviderCallID, contentType, record.Audio, expiresAt).Scan(
		&output.ID,
		&output.SalonID,
		&output.CallSessionID,
		&output.Provider,
		&output.ProviderCallID,
		&output.ContentType,
		&output.Audio,
		&output.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return &output, nil
}

func (r *Repository) GetAudioOutputMetadata(ctx context.Context, id string) (*AudioOutput, error) {
	var output AudioOutput
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, COALESCE(salon_id::text, ''), COALESCE(call_session_id::text, ''),
		       provider, COALESCE(provider_call_id, ''), content_type, expires_at
		FROM voice_audio_outputs
		WHERE id = $1
	`, id).Scan(
		&output.ID,
		&output.SalonID,
		&output.CallSessionID,
		&output.Provider,
		&output.ProviderCallID,
		&output.ContentType,
		&output.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &output, nil
}

func (r *Repository) GetAudioOutputContent(ctx context.Context, id string) (*AudioOutput, error) {
	var output AudioOutput
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, content_type, audio_data
		FROM voice_audio_outputs
		WHERE id = $1
		  AND expires_at > now()
	`, id).Scan(&output.ID, &output.ContentType, &output.Audio)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &output, nil
}
