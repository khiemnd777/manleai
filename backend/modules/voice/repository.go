package voice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error) {
	var status SalonVoiceStatus
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, COALESCE(phone, '')
		FROM salons
		WHERE id = $1
		  AND owner_user_id = $2
	`, salonID, ownerUserID).Scan(&status.SalonID, &status.Phone)
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
	err := r.db.QueryRowContext(ctx, `
		SELECT
			s.ai_enabled,
			s.active_pos_provider,
			EXISTS (
				SELECT 1
				FROM pos_connections pc
				WHERE pc.salon_id = s.id
				  AND pc.provider = s.active_pos_provider
				  AND pc.status NOT IN ('not_connected', 'error', 'expired_token', 'disabled')
				  AND COALESCE(pc.location_id, '') <> ''
			) AS provider_connected,
			EXISTS (
				SELECT 1
				FROM pos_connections pc
				WHERE pc.salon_id = s.id
				  AND pc.provider = s.active_pos_provider
				  AND pc.status NOT IN ('not_connected', 'error', 'expired_token', 'disabled')
				  AND COALESCE(pc.location_id, '') <> ''
				  AND pc.last_sync_at IS NOT NULL
			) AS provider_synced,
			(
				SELECT COUNT(*)
				FROM services svc
				WHERE svc.salon_id = s.id
				  AND svc.pos_provider = s.active_pos_provider
				  AND svc.active = true
				  AND svc.ai_bookable = true
				  AND COALESCE(svc.pos_service_id, '') <> ''
				  AND COALESCE(svc.pos_service_version, 0) > 0
				  AND svc.duration_minutes > 0
			) AS service_count,
			(
				SELECT COUNT(*)
				FROM staff st
				WHERE st.salon_id = s.id
				  AND st.pos_provider = s.active_pos_provider
				  AND st.active = true
				  AND st.ai_bookable = true
				  AND COALESCE(st.pos_staff_id, '') <> ''
			) AS staff_count,
				(
					SELECT COUNT(*)
					FROM salon_business_hours bh
					WHERE bh.salon_id = s.id
					  AND bh.is_closed = false
					  AND bh.open_time IS NOT NULL
					  AND bh.close_time IS NOT NULL
					  AND bh.close_time > bh.open_time
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
				) AS test_booking_cancelled
			FROM salons s
			WHERE s.id = $1
			  AND s.owner_user_id = $2
	`, salonID, ownerUserID).Scan(
		&readiness.AIEnabled,
		&readiness.ActiveProvider,
		&readiness.ProviderConnected,
		&readiness.ProviderSynced,
		&readiness.ServiceCount,
		&readiness.StaffCount,
		&readiness.BusinessHoursCount,
		&readiness.TestBookingCancelled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	readiness.SquareConnected = readiness.ActiveProvider == "square" && readiness.ProviderConnected
	readiness.SquareSynced = readiness.ActiveProvider == "square" && readiness.ProviderSynced
	providerLabel := readiness.ActiveProvider
	if providerLabel == "square" {
		providerLabel = "Square Appointments"
	}

	readiness.Checks = []ReadinessCheck{
		{Key: "connect_active_provider", Label: "Connect active POS provider", Complete: readiness.ProviderConnected, Message: incompleteReadinessMessage(readiness.ProviderConnected, providerLabel+" is not connected with a selected location.")},
		{Key: "sync_active_provider", Label: "Sync active POS provider", Complete: readiness.ProviderSynced, Message: incompleteReadinessMessage(readiness.ProviderSynced, providerLabel+" services and staff have not been synced.")},
		{Key: "bookable_services", Label: "AI-bookable services", Complete: readiness.ServiceCount > 0, Message: incompleteReadinessMessage(readiness.ServiceCount > 0, "No active AI-bookable service is synced from the active POS provider.")},
		{Key: "bookable_staff", Label: "AI-bookable staff", Complete: readiness.StaffCount > 0, Message: incompleteReadinessMessage(readiness.StaffCount > 0, "No active AI-bookable staff member is synced from the active POS provider.")},
		{Key: "business_hours", Label: "Business hours", Complete: readiness.BusinessHoursCount > 0, Message: incompleteReadinessMessage(readiness.BusinessHoursCount > 0, "Salon business hours are not configured.")},
		{Key: "cancel_test_booking", Label: "Cancel POS test booking", Complete: readiness.TestBookingCancelled, Message: incompleteReadinessMessage(readiness.TestBookingCancelled, "The latest active-provider test booking has not been created and cancelled.")},
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

func (r *Repository) FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error) {
	var salon InboundSalon
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, owner_user_id::text, name, COALESCE(phone, '')
		FROM salons
		WHERE regexp_replace(COALESCE(phone, ''), '[^0-9]', '', 'g') = regexp_replace($1, '[^0-9]', '', 'g')
		  AND COALESCE(phone, '') <> ''
		ORDER BY created_at ASC
		LIMIT 1
	`, phone).Scan(&salon.SalonID, &salon.OwnerUserID, &salon.SalonName, &salon.Phone)
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
		RETURNING id::text, content_type, audio_data
	`, record.SalonID, record.CallSessionID, record.Provider, record.ProviderCallID, contentType, record.Audio, expiresAt).Scan(
		&output.ID,
		&output.ContentType,
		&output.Audio,
	)
	if err != nil {
		return nil, err
	}
	return &output, nil
}

func (r *Repository) GetAudioOutput(ctx context.Context, id string) (*AudioOutput, error) {
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
