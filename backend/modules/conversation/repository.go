package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error) {
	var cfg RuntimeConfig
	err := r.db.QueryRowContext(ctx, `
		SELECT s.name, s.timezone, COALESCE(s.handoff_phone, ''), s.ai_enabled,
		       COALESCE(ss.handoff_enabled, true), COALESCE(ss.ai_greeting, '')
		FROM salons s
		LEFT JOIN salon_settings ss ON ss.salon_id = s.id
		WHERE s.id = $1
		  AND s.owner_user_id = $2
	`, salonID, ownerUserID).Scan(
		&cfg.SalonName,
		&cfg.Timezone,
		&cfg.HandoffPhone,
		&cfg.AIEnabled,
		&cfg.HandoffEnabled,
		&cfg.AIGreeting,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *Repository) CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sessionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO call_sessions (
			salon_id, channel, provider, provider_call_id, inbound_phone, outbound_phone,
			status, intent, outcome, customer_name, customer_phone, customer_email
		)
		VALUES (
			$1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
			$7, $8, $9, NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, '')
		)
		RETURNING id::text
	`, record.SalonID, record.Channel, record.Provider, record.ProviderCallID, record.InboundPhone, record.OutboundPhone, StatusActive, IntentUnknown, OutcomeCollecting, record.CustomerName, record.CustomerPhone, record.CustomerEmail).Scan(&sessionID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, 1)
	`, sessionID, record.SalonID, SpeakerAI, record.InitialReply); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSessionForOwner(ctx, record.SalonID, record.OwnerUserID, sessionID)
}

func (r *Repository) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	session, err := r.getSession(ctx, `
		WHERE cs.id = $1
		  AND cs.salon_id = $2
		  AND salon.owner_user_id = $3
	`, sessionID, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if err := r.loadSessionDetails(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (r *Repository) ListSessions(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx, sessionSelect()+`
		WHERE cs.salon_id = $1
		  AND salon.owner_user_id = $2
		ORDER BY cs.updated_at DESC
		LIMIT $3
	`, salonID, ownerUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Session, 0)
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, name, duration_minutes, COALESCE(price_from, 0), COALESCE(price_display, '')
		FROM services
		WHERE salon_id = $1
		  AND active = true
		  AND ai_bookable = true
		ORDER BY name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceOption, 0)
	for rows.Next() {
		var item ServiceOption
		if err := rows.Scan(&item.ID, &item.Name, &item.DurationMinutes, &item.PriceFrom, &item.PriceDisplay); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, name
		FROM staff
		WHERE salon_id = $1
		  AND active = true
		  AND ai_bookable = true
		ORDER BY name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffOption, 0)
	for rows.Next() {
		var item StaffOption
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT title, category, body
		FROM knowledge_items
		WHERE salon_id = $1
		  AND status = 'active'
		ORDER BY updated_at DESC
		LIMIT 8
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KnowledgeSnippet, 0)
	for rows.Next() {
		var item KnowledgeSnippet
		if err := rows.Scan(&item.Title, &item.Category, &item.Body); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var lockedID string
	if err := tx.QueryRowContext(ctx, `
		SELECT cs.id::text
		FROM call_sessions cs
		JOIN salons s ON s.id = cs.salon_id
		WHERE cs.id = $1
		  AND cs.salon_id = $2
		  AND s.owner_user_id = $3
		FOR UPDATE
	`, record.Session.ID, record.SalonID, record.OwnerUserID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var sequence int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(max(sequence), 0)
		FROM call_transcript_messages
		WHERE session_id = $1
	`, record.Session.ID).Scan(&sequence); err != nil {
		return nil, err
	}
	sequence++
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
	`, record.Session.ID, record.SalonID, SpeakerCustomer, record.CustomerMessage, sequence); err != nil {
		return nil, err
	}
	if record.ToolMessage != "" {
		sequence++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
			VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
		`, record.Session.ID, record.SalonID, SpeakerTool, record.ToolMessage, sequence); err != nil {
			return nil, err
		}
	}
	sequence++
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
	`, record.Session.ID, record.SalonID, SpeakerAI, record.AIMessage, sequence); err != nil {
		return nil, err
	}

	if record.Handoff != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO handoff_requests (
				salon_id, call_session_id, status, reason, customer_name, customer_phone, summary
			)
			VALUES ($1, $2, 'pending', $3, NULLIF($4, ''), NULLIF($5, ''), $6)
		`, record.SalonID, record.Session.ID, record.Handoff.Reason, record.Handoff.CustomerName, record.Handoff.CustomerPhone, record.Handoff.Summary); err != nil {
			return nil, err
		}
	}

	offeredSlots := record.Update.OfferedSlots
	if offeredSlots == nil {
		offeredSlots = []OfferedSlot{}
	}
	offeredSlotsJSON, err := json.Marshal(offeredSlots)
	if err != nil {
		return nil, err
	}
	bookingSegments := record.Update.BookingSegments
	if bookingSegments == nil {
		bookingSegments = []booking.BookingSegmentRequest{}
	}
	bookingSegmentsJSON, err := json.Marshal(bookingSegments)
	if err != nil {
		return nil, err
	}
	staffSelectionMode := strings.TrimSpace(record.Update.StaffSelectionMode)
	if staffSelectionMode == "" {
		staffSelectionMode = booking.StaffSelectionSpecific
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE call_sessions
		SET status = $1,
		    intent = $2,
		    outcome = $3,
		    customer_name = NULLIF($4, ''),
		    customer_phone = NULLIF($5, ''),
		    customer_email = NULLIF($6, ''),
		    service_id = NULLIF($7, '')::uuid,
		    staff_id = NULLIF($8, '')::uuid,
		    staff_selection_mode = $9,
		    requested_start_time = $10,
		    offered_slots = $11::jsonb,
		    booking_segments = $12::jsonb,
		    booking_attempt_id = NULLIF($13, '')::uuid,
		    appointment_id = NULLIF($14, '')::uuid,
		    summary = NULLIF($15, ''),
		    ended_at = CASE WHEN $16 THEN now() ELSE ended_at END,
		    updated_at = now()
		WHERE id = $17
		  AND salon_id = $18
	`, record.Update.Status, record.Update.Intent, record.Update.Outcome, record.Update.CustomerName, record.Update.CustomerPhone, record.Update.CustomerEmail, record.Update.ServiceID, record.Update.StaffID, staffSelectionMode, record.Update.RequestedStartTime, string(offeredSlotsJSON), string(bookingSegmentsJSON), record.Update.BookingAttemptID, record.Update.AppointmentID, record.Update.Summary, record.Update.EndSession, record.Session.ID, record.SalonID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSessionForOwner(ctx, record.SalonID, record.OwnerUserID, record.Session.ID)
}

func (r *Repository) getSession(ctx context.Context, where string, args ...any) (*Session, error) {
	session, err := scanSession(r.db.QueryRowContext(ctx, sessionSelect()+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r *Repository) loadSessionDetails(ctx context.Context, session *Session) error {
	messages, err := r.listTranscript(ctx, session.ID)
	if err != nil {
		return err
	}
	session.Transcript = messages

	handoff, err := r.latestHandoff(ctx, session.ID)
	if err != nil {
		return err
	}
	session.Handoff = handoff
	return nil
}

func (r *Repository) listTranscript(ctx context.Context, sessionID string) ([]TranscriptMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, session_id::text, salon_id::text, speaker, body, sequence, created_at
		FROM call_transcript_messages
		WHERE session_id = $1
		ORDER BY sequence ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TranscriptMessage, 0)
	for rows.Next() {
		var item TranscriptMessage
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SalonID, &item.Speaker, &item.Body, &item.Sequence, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) latestHandoff(ctx context.Context, sessionID string) (*HandoffRequest, error) {
	var item HandoffRequest
	var resolvedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, call_session_id::text, status, reason,
		       COALESCE(customer_name, ''), COALESCE(customer_phone, ''), summary, created_at, resolved_at
		FROM handoff_requests
		WHERE call_session_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID).Scan(
		&item.ID,
		&item.SalonID,
		&item.CallSessionID,
		&item.Status,
		&item.Reason,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.Summary,
		&item.CreatedAt,
		&resolvedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		item.ResolvedAt = &resolvedAt.Time
	}
	return &item, nil
}

func sessionSelect() string {
	return `
		SELECT cs.id::text, cs.salon_id::text, cs.channel,
		       COALESCE(cs.provider, ''), COALESCE(cs.provider_call_id, ''),
		       COALESCE(cs.inbound_phone, ''), COALESCE(cs.outbound_phone, ''),
		       cs.status, cs.intent, cs.outcome,
		       COALESCE(cs.customer_name, ''), COALESCE(cs.customer_phone, ''), COALESCE(cs.customer_email, ''),
		       COALESCE(cs.service_id::text, ''), COALESCE(svc.name, ''),
		       COALESCE(cs.staff_id::text, ''), COALESCE(st.name, ''),
		       COALESCE(cs.staff_selection_mode, 'specific'),
		       cs.requested_start_time, COALESCE(cs.offered_slots, '[]'::jsonb),
		       COALESCE(cs.booking_segments, '[]'::jsonb),
		       COALESCE(cs.booking_attempt_id::text, ''),
		       COALESCE(cs.appointment_id::text, ''), COALESCE(cs.summary, ''),
		       cs.started_at, cs.ended_at, cs.created_at, cs.updated_at
		FROM call_sessions cs
		JOIN salons salon ON salon.id = cs.salon_id
		LEFT JOIN services svc ON svc.id = cs.service_id
		LEFT JOIN staff st ON st.id = cs.staff_id
	`
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner sessionScanner) (*Session, error) {
	var item Session
	var requestedStartAt sql.NullTime
	var endedAt sql.NullTime
	var offeredSlots []byte
	var bookingSegments []byte
	if err := scanner.Scan(
		&item.ID,
		&item.SalonID,
		&item.Channel,
		&item.Provider,
		&item.ProviderCallID,
		&item.InboundPhone,
		&item.OutboundPhone,
		&item.Status,
		&item.Intent,
		&item.Outcome,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.ServiceID,
		&item.ServiceName,
		&item.StaffID,
		&item.StaffName,
		&item.StaffSelectionMode,
		&requestedStartAt,
		&offeredSlots,
		&bookingSegments,
		&item.BookingAttemptID,
		&item.AppointmentID,
		&item.Summary,
		&item.StartedAt,
		&endedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if requestedStartAt.Valid {
		item.RequestedStartTime = &requestedStartAt.Time
	}
	if len(offeredSlots) > 0 {
		if err := json.Unmarshal(offeredSlots, &item.OfferedSlots); err != nil {
			return nil, err
		}
	}
	if len(bookingSegments) > 0 {
		if err := json.Unmarshal(bookingSegments, &item.BookingSegments); err != nil {
			return nil, err
		}
	}
	if item.StaffSelectionMode == "" {
		item.StaffSelectionMode = booking.StaffSelectionSpecific
	}
	if endedAt.Valid {
		item.EndedAt = &endedAt.Time
	}
	return &item, nil
}
