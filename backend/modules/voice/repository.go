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
