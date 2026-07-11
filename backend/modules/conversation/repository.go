package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	redactedTranscriptBody = "[redacted]"
	redactedSummaryBody    = "Redacted by call session retention policy."
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
			       COALESCE(ss.handoff_enabled, true), COALESCE(ss.ai_greeting, ''), COALESCE(ss.ai_tone, 'professional_warm'),
			       COALESCE(ss.recording_enabled, true), COALESCE(ss.recording_consent_message, '')
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
		&cfg.AITone,
		&cfg.RecordingEnabled,
		&cfg.RecordingConsentMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cfg.AIEnabled = bookingSafetyEnabled(cfg.AIEnabled)
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

func (r *Repository) GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*Session, bool, error) {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" {
		return nil, false, nil
	}
	session, err := r.getSession(ctx, `
		WHERE cs.id = $1
		  AND cs.salon_id = $2
		  AND salon.owner_user_id = $3
		  AND EXISTS (
		    SELECT 1
		    FROM call_transcript_messages ctm
		    WHERE ctm.session_id = cs.id
		      AND ctm.speaker = $4
		      AND ctm.metadata->>'event_key' = $5
		  )
	`, sessionID, salonID, ownerUserID, SpeakerCustomer, eventKey)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := r.loadSessionDetails(ctx, session); err != nil {
		return nil, false, err
	}
	return session, true, nil
}

func (r *Repository) ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int, offset int) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx, sessionSelect()+`
		WHERE cs.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND cs.lifecycle_status = $3
		ORDER BY cs.updated_at DESC, cs.id DESC
		LIMIT $4
		OFFSET $5
	`, salonID, ownerUserID, lifecycleStatus, limit, offset)
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

func (r *Repository) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]WebhookEventLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id::text, v.provider, COALESCE(v.provider_call_id, ''), v.event_type, v.payload, v.created_at
		FROM call_sessions cs
		JOIN salons salon ON salon.id = cs.salon_id
		JOIN voice_webhook_events v ON (
			v.call_session_id = cs.id
			OR (
				v.call_session_id IS NULL
				AND v.provider = cs.provider
				AND v.provider_call_id = cs.provider_call_id
			)
		)
		WHERE cs.id = $1
		  AND cs.salon_id = $2
		  AND salon.owner_user_id = $3
		  AND v.event_type IN ('realtime_connected', 'realtime_failed', 'realtime_stopped')
		ORDER BY v.created_at ASC
		LIMIT $4
	`, sessionID, salonID, ownerUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]WebhookEventLog, 0)
	found := false
	for rows.Next() {
		item, err := scanWebhookEvent(rows)
		if err != nil {
			return nil, err
		}
		found = true
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if found {
		return items, nil
	}

	// Distinguish "no events yet" from "the session is not owned by this user".
	if _, err := r.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID); err != nil {
		return nil, err
	}
	return []WebhookEventLog{}, nil
}

func (r *Repository) ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE call_sessions cs
		SET lifecycle_status = $1,
		    archived_at = COALESCE(cs.archived_at, now()),
		    updated_at = now()
		FROM salons salon
		WHERE cs.id = $2
		  AND cs.salon_id = $3
		  AND salon.id = cs.salon_id
		  AND salon.owner_user_id = $4
		  AND cs.lifecycle_status <> $5
	`, LifecycleArchived, sessionID, salonID, ownerUserID, LifecycleRedacted)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		session, getErr := r.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
		if getErr != nil {
			return nil, getErr
		}
		if session.LifecycleStatus == LifecycleRedacted {
			return nil, ErrLifecycle
		}
		return session, nil
	}
	return r.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
}

func (r *Repository) RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := scanSession(tx.QueryRowContext(ctx, sessionSelect()+`
		WHERE cs.id = $1
		  AND cs.salon_id = $2
		  AND salon.owner_user_id = $3
		FOR UPDATE OF cs
	`, sessionID, salonID, ownerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if session.LifecycleStatus == LifecycleRedacted {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return r.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	}
	if session.Status == StatusActive {
		return nil, ErrLifecycle
	}
	if err := redactSessionInTx(ctx, tx, sessionID, salonID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
}

func (r *Repository) RedactExpiredSessions(ctx context.Context, limit int) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, salon_id::text
		FROM call_sessions
		WHERE lifecycle_status <> $1
		  AND retention_expires_at <= now()
		ORDER BY retention_expires_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, LifecycleRedacted, clampRetentionLimit(limit))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type expiredSession struct {
		id      string
		salonID string
	}
	items := make([]expiredSession, 0)
	for rows.Next() {
		var item expiredSession
		if err := rows.Scan(&item.id, &item.salonID); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, item := range items {
		if err := redactSessionInTx(ctx, tx, item.id, item.salonID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (r *Repository) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT svc.id::text, svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''),
		       svc.duration_minutes, COALESCE(svc.price_from, 0), COALESCE(svc.price_display, ''),
		       COALESCE(cat.id::text, ''), COALESCE(cat.name, ''), COALESCE(cat.slug, '')
		FROM services svc
		JOIN salons salon ON salon.id = svc.salon_id
		JOIN pos_entity_links link
		  ON link.salon_id = svc.salon_id
		 AND link.entity_type = 'service'
		 AND link.entity_id = svc.id
		 AND link.provider = salon.active_pos_provider
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		LEFT JOIN service_categories cat ON cat.id = svc.service_category_id
		                                AND cat.salon_id = svc.salon_id
		                                AND cat.status = 'active'
		WHERE svc.salon_id = $1
		  AND svc.pos_provider = salon.active_pos_provider
		  AND svc.active = true
		  AND svc.ai_bookable = true
		  AND svc.archived_at IS NULL
		  AND svc.sync_status = 'synced'
		  AND svc.duration_minutes > 0
		  AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
		ORDER BY COALESCE(cat.sort_order, 9999), COALESCE(cat.name, ''), svc.name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceOption, 0)
	for rows.Next() {
		var item ServiceOption
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.AIDescription, &item.DurationMinutes, &item.PriceFrom, &item.PriceDisplay, &item.CategoryID, &item.CategoryName, &item.CategorySlug); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.id::text, st.name, st.ai_bookable
		FROM staff st
		JOIN salons salon ON salon.id = st.salon_id
		JOIN pos_entity_links link
		  ON link.salon_id = st.salon_id
		 AND link.entity_type = 'staff'
		 AND link.entity_id = st.id
		 AND link.provider = salon.active_pos_provider
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		WHERE st.salon_id = $1
		  AND st.pos_provider = salon.active_pos_provider
		  AND st.active = true
		  AND st.ai_bookable = true
		  AND st.archived_at IS NULL
		  AND st.sync_status = 'synced'
		ORDER BY st.name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffOption, 0)
	for rows.Next() {
		var item StaffOption
		if err := rows.Scan(&item.ID, &item.Name, &item.AIBookable); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.id::text, st.name, st.ai_bookable
		FROM staff st
		JOIN salons salon ON salon.id = st.salon_id
		JOIN pos_entity_links link
		  ON link.salon_id = st.salon_id
		 AND link.entity_type = 'staff'
		 AND link.entity_id = st.id
		 AND link.provider = salon.active_pos_provider
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		WHERE st.salon_id = $1
		  AND st.pos_provider = salon.active_pos_provider
		  AND st.active = true
		  AND st.archived_at IS NULL
		  AND st.sync_status = 'synced'
		ORDER BY st.ai_bookable DESC, st.name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffOption, 0)
	for rows.Next() {
		var item StaffOption
		if err := rows.Scan(&item.ID, &item.Name, &item.AIBookable); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListStaffAssignmentStats(ctx context.Context, salonID string, staffIDs []string, from time.Time, to time.Time) (map[string]StaffAssignmentStat, error) {
	ids := make([]string, 0, len(staffIDs))
	seen := map[string]bool{}
	for _, staffID := range staffIDs {
		staffID = strings.TrimSpace(staffID)
		if staffID == "" || seen[staffID] {
			continue
		}
		seen[staffID] = true
		ids = append(ids, staffID)
	}
	out := make(map[string]StaffAssignmentStat, len(ids))
	for _, staffID := range ids {
		out[staffID] = StaffAssignmentStat{StaffID: staffID}
	}
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH requested_staff AS (
			SELECT unnest($2::uuid[]) AS staff_id
		),
		assignment_events AS (
			SELECT a.id AS appointment_id, a.staff_id, a.created_at
			FROM appointments a
			WHERE a.salon_id = $1
			  AND a.staff_id = ANY($2::uuid[])
			  AND a.status IN ('confirmed', 'rescheduled')
			  AND a.start_time >= $3
			  AND a.start_time < $4
			UNION
			SELECT a.id AS appointment_id, aps.staff_id, a.created_at
			FROM appointments a
			JOIN appointment_services aps ON aps.appointment_id = a.id
			WHERE a.salon_id = $1
			  AND aps.staff_id = ANY($2::uuid[])
			  AND a.status IN ('confirmed', 'rescheduled')
			  AND a.start_time >= $3
			  AND a.start_time < $4
		)
		SELECT rs.staff_id::text, COALESCE(COUNT(DISTINCT ae.appointment_id), 0)::int, MAX(ae.created_at)
		FROM requested_staff rs
		LEFT JOIN assignment_events ae ON ae.staff_id = rs.staff_id
		GROUP BY rs.staff_id
	`, salonID, pq.Array(ids), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var stat StaffAssignmentStat
		var lastAssignedAt sql.NullTime
		if err := rows.Scan(&stat.StaffID, &stat.AssignedCount, &lastAssignedAt); err != nil {
			return nil, err
		}
		if lastAssignedAt.Valid {
			stat.LastAssignedAt = &lastAssignedAt.Time
		}
		out[stat.StaffID] = stat
	}
	return out, rows.Err()
}

func (r *Repository) ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT sa.id::text, sa.service_id::text, svc.name, sa.alias, sa.normalized_alias,
		       sa.source, sa.confidence
		FROM service_aliases sa
		JOIN services svc ON svc.id = sa.service_id
		JOIN salons salon ON salon.id = svc.salon_id
		JOIN pos_entity_links link
		  ON link.salon_id = svc.salon_id
		 AND link.entity_type = 'service'
		 AND link.entity_id = svc.id
		 AND link.provider = salon.active_pos_provider
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		WHERE sa.salon_id = $1
		  AND sa.status = 'active'
		  AND svc.salon_id = sa.salon_id
		  AND svc.pos_provider = salon.active_pos_provider
		  AND svc.active = true
		  AND svc.ai_bookable = true
		  AND svc.archived_at IS NULL
		  AND svc.sync_status = 'synced'
		  AND svc.duration_minutes > 0
		  AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
		ORDER BY sa.updated_at DESC
		LIMIT 200
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceAlias, 0)
	for rows.Next() {
		var item ServiceAlias
		if err := rows.Scan(&item.ID, &item.ServiceID, &item.ServiceName, &item.Alias, &item.NormalizedAlias, &item.Source, &item.Confidence); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT alias.id::text, alias.category_id::text, cat.name, alias.alias,
		       alias.normalized_alias, alias.source, alias.confidence
		FROM service_category_aliases alias
		JOIN service_categories cat ON cat.id = alias.category_id
		                           AND cat.salon_id = alias.salon_id
		                           AND cat.status = 'active'
		WHERE alias.salon_id = $1
		  AND alias.status = 'active'
		ORDER BY alias.updated_at DESC
		LIMIT 200
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceCategoryAlias, 0)
	for rows.Next() {
		var item ServiceCategoryAlias
		if err := rows.Scan(&item.ID, &item.CategoryID, &item.CategoryName, &item.Alias, &item.NormalizedAlias, &item.Source, &item.Confidence); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, title, category, body
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
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.Body); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT bhp.id::text, bhp.day_of_week, bhp.start_local_time::text,
		       bhp.end_local_time::text, bhp.source, bhp.provider
		FROM salon_business_hour_periods bhp
		JOIN salons salon ON salon.id = bhp.salon_id
		WHERE bhp.salon_id = $1
		  AND bhp.source = 'imported'
		  AND bhp.provider = salon.active_pos_provider
		ORDER BY bhp.day_of_week ASC, bhp.start_local_time ASC, bhp.provider_period_index ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BusinessHourPeriod, 0)
	for rows.Next() {
		var item BusinessHourPeriod
		if err := rows.Scan(&item.ID, &item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime, &item.Source, &item.Provider); err != nil {
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
		  AND cs.lifecycle_status <> $4
		FOR UPDATE
		`, record.Session.ID, record.SalonID, record.OwnerUserID, LifecycleRedacted).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	eventKey := strings.TrimSpace(record.EventKey)
	if eventKey != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM call_transcript_messages
				WHERE session_id = $1
				  AND speaker = $2
				  AND metadata->>'event_key' = $3
			)
		`, record.Session.ID, SpeakerCustomer, eventKey).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			_ = tx.Rollback()
			return r.GetSessionForOwner(ctx, record.SalonID, record.OwnerUserID, record.Session.ID)
		}
	}

	var sequence int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(max(sequence), 0)
		FROM call_transcript_messages
		WHERE session_id = $1
	`, record.Session.ID).Scan(&sequence); err != nil {
		return nil, err
	}
	customerMetadata, err := metadataJSON(record.CustomerMetadata)
	if err != nil {
		return nil, err
	}
	toolMetadata, err := metadataJSON(record.ToolMetadata)
	if err != nil {
		return nil, err
	}
	aiMetadata, err := metadataJSON(record.AIMetadata)
	if err != nil {
		return nil, err
	}
	sequence++
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, record.Session.ID, record.SalonID, SpeakerCustomer, record.CustomerMessage, customerMetadata, sequence); err != nil {
		return nil, err
	}
	if record.ToolMessage != "" {
		sequence++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		`, record.Session.ID, record.SalonID, SpeakerTool, record.ToolMessage, toolMetadata, sequence); err != nil {
			return nil, err
		}
	}
	sequence++
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_transcript_messages (session_id, salon_id, speaker, body, metadata, sequence)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, record.Session.ID, record.SalonID, SpeakerAI, record.AIMessage, aiMetadata, sequence); err != nil {
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

	if record.PartyRequest != nil {
		guestRequests := record.PartyRequest.GuestServiceRequests
		if guestRequests == nil {
			guestRequests = []PartyGuestService{}
		}
		guestRequestsJSON, err := json.Marshal(guestRequests)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO party_booking_requests (
				salon_id, call_session_id, event_key, status, party_size, representative_name,
				representative_phone, requested_date, requested_time_window, guest_service_requests,
				flexibility_notes, summary
			)
			VALUES (
				$1, $2, $3, 'pending', NULLIF($4, 0), NULLIF($5, ''), NULLIF($6, ''),
				NULLIF($7, '')::date, NULLIF($8, ''), $9::jsonb, NULLIF($10, ''), $11
			)
			ON CONFLICT (salon_id, call_session_id, event_key)
			DO UPDATE SET party_size = COALESCE(EXCLUDED.party_size, party_booking_requests.party_size),
			              representative_name = COALESCE(EXCLUDED.representative_name, party_booking_requests.representative_name),
			              representative_phone = COALESCE(EXCLUDED.representative_phone, party_booking_requests.representative_phone),
			              requested_date = COALESCE(EXCLUDED.requested_date, party_booking_requests.requested_date),
			              requested_time_window = COALESCE(EXCLUDED.requested_time_window, party_booking_requests.requested_time_window),
			              guest_service_requests = CASE
			                  WHEN jsonb_array_length(EXCLUDED.guest_service_requests) > 0 THEN EXCLUDED.guest_service_requests
			                  ELSE party_booking_requests.guest_service_requests
			              END,
			              flexibility_notes = COALESCE(EXCLUDED.flexibility_notes, party_booking_requests.flexibility_notes),
			              summary = EXCLUDED.summary,
			              updated_at = now()
		`, record.SalonID, record.Session.ID, record.PartyRequest.EventKey, record.PartyRequest.PartySize, record.PartyRequest.RepresentativeName, record.PartyRequest.RepresentativePhone, record.PartyRequest.RequestedDate, record.PartyRequest.RequestedTimeWindow, string(guestRequestsJSON), record.PartyRequest.FlexibilityNotes, record.PartyRequest.Summary); err != nil {
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
	partyPlanJSON := "{}"
	if record.Update.PartyPlan != nil {
		partyPlanBytes, err := json.Marshal(record.Update.PartyPlan)
		if err != nil {
			return nil, err
		}
		partyPlanJSON = string(partyPlanBytes)
	}
	dialogStateJSON, err := json.Marshal(normalizedDialogState(record.Update.DialogState))
	if err != nil {
		return nil, err
	}
	rescheduleCandidates := record.Update.RescheduleCandidates
	if rescheduleCandidates == nil {
		rescheduleCandidates = []RescheduleCandidate{}
	}
	rescheduleCandidatesJSON, err := json.Marshal(rescheduleCandidates)
	if err != nil {
		return nil, err
	}
	staffSelectionMode := strings.TrimSpace(record.Update.StaffSelectionMode)
	if staffSelectionMode == "" {
		staffSelectionMode = booking.StaffSelectionSpecific
	}
	bookingAction := strings.TrimSpace(record.Update.BookingAction)
	if bookingAction == "" {
		bookingAction = BookingActionBook
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE call_sessions
		SET status = $1,
		    intent = $2,
		    outcome = $3,
		    booking_action = $4,
		    target_appointment_id = NULLIF($5, '')::uuid,
		    reschedule_candidates = $6::jsonb,
		    customer_name = NULLIF($7, ''),
		    customer_phone = NULLIF($8, ''),
		    customer_email = NULLIF($9, ''),
		    service_id = NULLIF($10, '')::uuid,
			    staff_id = NULLIF($11, '')::uuid,
			    staff_selection_mode = $12,
			    requested_date = NULLIF($13, '')::date,
			    requested_start_time = $14,
			    offered_slots = $15::jsonb,
			    booking_segments = $16::jsonb,
			    party_plan = $17::jsonb,
			    dialog_state = $18::jsonb,
			    booking_attempt_id = NULLIF($19, '')::uuid,
			    appointment_id = NULLIF($20, '')::uuid,
			    summary = NULLIF($21, ''),
			    ended_at = CASE WHEN $22 THEN now() ELSE ended_at END,
			    updated_at = now()
			WHERE id = $23
			  AND salon_id = $24
		`, record.Update.Status, record.Update.Intent, record.Update.Outcome, bookingAction, record.Update.TargetAppointmentID, string(rescheduleCandidatesJSON), record.Update.CustomerName, record.Update.CustomerPhone, record.Update.CustomerEmail, record.Update.ServiceID, record.Update.StaffID, staffSelectionMode, record.Update.RequestedDate, record.Update.RequestedStartTime, string(offeredSlotsJSON), string(bookingSegmentsJSON), partyPlanJSON, string(dialogStateJSON), record.Update.BookingAttemptID, record.Update.AppointmentID, record.Update.Summary, record.Update.EndSession, record.Session.ID, record.SalonID); err != nil {
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
	partyRequest, err := r.latestPartyRequest(ctx, session.ID)
	if err != nil {
		return err
	}
	session.PartyRequest = partyRequest
	return nil
}

func (r *Repository) listTranscript(ctx context.Context, sessionID string) ([]TranscriptMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, session_id::text, salon_id::text, speaker, body, COALESCE(metadata, '{}'::jsonb), sequence, created_at
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
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SalonID, &item.Speaker, &item.Body, &metadata, &item.Sequence, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
				return nil, err
			}
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

func (r *Repository) latestPartyRequest(ctx context.Context, sessionID string) (*PartyBookingRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, call_session_id::text, event_key, status,
		       COALESCE(party_size, 0), COALESCE(representative_name, ''), COALESCE(representative_phone, ''),
		       COALESCE(requested_date::text, ''), COALESCE(requested_time_window, ''),
		       guest_service_requests, COALESCE(flexibility_notes, ''), summary,
		       created_at, updated_at, resolved_at, COALESCE(resolved_by::text, '')
		FROM party_booking_requests
		WHERE call_session_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID)
	item, err := scanPartyBookingRequest(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return item, err
}

func (r *Repository) ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]PartyBookingRequest, error) {
	if limit <= 0 {
		limit = defaultSessionListLimit
	}
	if limit > maxSessionListLimit+1 {
		limit = maxSessionListLimit + 1
	}
	if offset < 0 {
		offset = 0
	}
	status = strings.TrimSpace(status)
	statusFilter := ""
	args := []any{salonID, ownerUserID, limit, offset}
	if status != "" && status != "all" {
		statusFilter = "AND req.status = $5"
		args = append(args, status)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT req.id::text, req.salon_id::text, req.call_session_id::text, req.event_key, req.status,
		       COALESCE(req.party_size, 0), COALESCE(req.representative_name, ''), COALESCE(req.representative_phone, ''),
		       COALESCE(req.requested_date::text, ''), COALESCE(req.requested_time_window, ''),
		       req.guest_service_requests, COALESCE(req.flexibility_notes, ''), req.summary,
		       req.created_at, req.updated_at, req.resolved_at, COALESCE(req.resolved_by::text, '')
		FROM party_booking_requests req
		JOIN salons salon ON salon.id = req.salon_id
		WHERE req.salon_id = $1
		  AND salon.owner_user_id = $2
			`+statusFilter+`
			ORDER BY req.created_at DESC
			LIMIT $3
			OFFSET $4
		`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PartyBookingRequest, 0)
	for rows.Next() {
		item, err := scanPartyBookingRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) UpdatePartyBookingRequestStatus(ctx context.Context, salonID string, ownerUserID string, requestID string, status string) (*PartyBookingRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE party_booking_requests req
		SET status = $1,
		    resolved_at = CASE WHEN $1 IN ('resolved', 'dismissed') THEN COALESCE(req.resolved_at, now()) ELSE req.resolved_at END,
		    resolved_by = CASE WHEN $1 IN ('resolved', 'dismissed') THEN $2::uuid ELSE req.resolved_by END,
		    updated_at = now()
		FROM salons salon
		WHERE req.id = $3
		  AND req.salon_id = $4
		  AND salon.id = req.salon_id
		  AND salon.owner_user_id = $2
		RETURNING req.id::text, req.salon_id::text, req.call_session_id::text, req.event_key, req.status,
		          COALESCE(req.party_size, 0), COALESCE(req.representative_name, ''), COALESCE(req.representative_phone, ''),
		          COALESCE(req.requested_date::text, ''), COALESCE(req.requested_time_window, ''),
		          req.guest_service_requests, COALESCE(req.flexibility_notes, ''), req.summary,
		          req.created_at, req.updated_at, req.resolved_at, COALESCE(req.resolved_by::text, '')
	`, status, ownerUserID, requestID, salonID)
	return scanPartyBookingRequest(row)
}

func metadataJSON(metadata map[string]any) (string, error) {
	if len(metadata) == 0 {
		return "{}", nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func redactSessionInTx(ctx context.Context, execer sqlExecer, sessionID string, salonID string) error {
	if _, err := execer.ExecContext(ctx, `
		UPDATE call_sessions
		SET lifecycle_status = $1,
		    redacted_at = COALESCE(redacted_at, now()),
		    customer_name = NULL,
		    customer_phone = NULL,
		    customer_email = NULL,
		    inbound_phone = NULL,
		    outbound_phone = NULL,
		    summary = CASE WHEN summary IS NULL THEN NULL ELSE $2 END,
		    offered_slots = '[]'::jsonb,
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
	`, LifecycleRedacted, redactedSummaryBody, sessionID, salonID); err != nil {
		return err
	}
	if _, err := execer.ExecContext(ctx, `
		UPDATE call_transcript_messages
		SET body = $1,
		    metadata = jsonb_build_object('redacted', true)
		WHERE session_id = $2
		  AND salon_id = $3
	`, redactedTranscriptBody, sessionID, salonID); err != nil {
		return err
	}
	if _, err := execer.ExecContext(ctx, `
		UPDATE handoff_requests
		SET customer_name = NULL,
		    customer_phone = NULL,
		    summary = $1
		WHERE call_session_id = $2
		  AND salon_id = $3
	`, redactedSummaryBody, sessionID, salonID); err != nil {
		return err
	}
	if _, err := execer.ExecContext(ctx, `
		UPDATE party_booking_requests
		SET representative_name = NULL,
		    representative_phone = NULL,
		    guest_service_requests = '[]'::jsonb,
		    flexibility_notes = NULL,
		    summary = $1,
		    updated_at = now()
		WHERE call_session_id = $2
		  AND salon_id = $3
	`, redactedSummaryBody, sessionID, salonID); err != nil {
		return err
	}
	if _, err := execer.ExecContext(ctx, `
		UPDATE voice_webhook_events
		SET payload = jsonb_build_object('redacted', true)
		WHERE call_session_id = $1
	`, sessionID); err != nil {
		return err
	}
	if _, err := execer.ExecContext(ctx, `
		DELETE FROM voice_audio_outputs
		WHERE call_session_id = $1
	`, sessionID); err != nil {
		return err
	}
	return nil
}

type webhookEventScanner interface {
	Scan(dest ...any) error
}

func scanWebhookEvent(scanner webhookEventScanner) (WebhookEventLog, error) {
	var item WebhookEventLog
	var payload []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Provider,
		&item.ProviderCallID,
		&item.EventType,
		&payload,
		&item.CreatedAt,
	); err != nil {
		return item, err
	}
	applyWebhookPayload(&item, payload)
	return item, nil
}

func scanPartyBookingRequest(scanner interface{ Scan(dest ...any) error }) (*PartyBookingRequest, error) {
	var item PartyBookingRequest
	var guestRequests []byte
	var resolvedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.SalonID,
		&item.CallSessionID,
		&item.EventKey,
		&item.Status,
		&item.PartySize,
		&item.RepresentativeName,
		&item.RepresentativePhone,
		&item.RequestedDate,
		&item.RequestedTimeWindow,
		&guestRequests,
		&item.FlexibilityNotes,
		&item.Summary,
		&item.CreatedAt,
		&item.UpdatedAt,
		&resolvedAt,
		&item.ResolvedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(guestRequests) > 0 {
		if err := json.Unmarshal(guestRequests, &item.GuestServiceRequests); err != nil {
			return nil, err
		}
	}
	if resolvedAt.Valid {
		item.ResolvedAt = &resolvedAt.Time
	}
	return &item, nil
}

func applyWebhookPayload(item *WebhookEventLog, raw []byte) {
	if item == nil || len(raw) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if boolPayload(payload, "redacted") {
		item.Redacted = true
		return
	}
	item.Stage = stringPayload(payload, "stage", "Stage")
	item.StreamSID = stringPayload(payload, "stream_sid", "StreamSid")
	item.StreamEvent = stringPayload(payload, "StreamEvent", "stream_event")
	item.StreamError = stringPayload(payload, "StreamError", "stream_error")
	item.Error = stringPayload(payload, "error", "Error")
}

func stringPayload(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func boolPayload(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	if flag, ok := value.(bool); ok {
		return flag
	}
	if text, ok := value.(string); ok {
		return strings.EqualFold(strings.TrimSpace(text), "true")
	}
	return false
}

func sessionSelect() string {
	return `
		SELECT cs.id::text, cs.salon_id::text, cs.channel,
		       COALESCE(cs.provider, ''), COALESCE(cs.provider_call_id, ''),
		       COALESCE(cs.inbound_phone, ''), COALESCE(cs.outbound_phone, ''),
		       cs.status, cs.intent, cs.outcome,
		       COALESCE(cs.booking_action, 'book'), COALESCE(cs.target_appointment_id::text, ''),
		       COALESCE(cs.reschedule_candidates, '[]'::jsonb),
		       COALESCE(cs.customer_name, ''), COALESCE(cs.customer_phone, ''), COALESCE(cs.customer_email, ''),
		       COALESCE(cs.service_id::text, ''), COALESCE(svc.name, ''),
		       COALESCE(cs.staff_id::text, ''), COALESCE(st.name, ''),
		       COALESCE(cs.staff_selection_mode, 'specific'),
		       COALESCE(cs.requested_date::text, ''),
		       cs.requested_start_time, COALESCE(cs.offered_slots, '[]'::jsonb),
		       COALESCE(cs.booking_segments, '[]'::jsonb),
		       COALESCE(cs.party_plan, '{}'::jsonb),
		       COALESCE(cs.dialog_state, '{"version":2,"phase":"open","review_required":true,"review_accepted":false,"no_progress_count":0,"draft_revision":1,"reviewed_revision":0,"authorized_revision":0}'::jsonb),
		       COALESCE(cs.booking_attempt_id::text, ''),
		       COALESCE(cs.appointment_id::text, ''), COALESCE(cs.summary, ''),
		       cs.lifecycle_status, cs.archived_at, cs.redacted_at, cs.retention_expires_at,
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
	var archivedAt sql.NullTime
	var redactedAt sql.NullTime
	var offeredSlots []byte
	var bookingSegments []byte
	var partyPlan []byte
	var dialogState []byte
	var rescheduleCandidates []byte
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
		&item.BookingAction,
		&item.TargetAppointmentID,
		&rescheduleCandidates,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.ServiceID,
		&item.ServiceName,
		&item.StaffID,
		&item.StaffName,
		&item.StaffSelectionMode,
		&item.RequestedDate,
		&requestedStartAt,
		&offeredSlots,
		&bookingSegments,
		&partyPlan,
		&dialogState,
		&item.BookingAttemptID,
		&item.AppointmentID,
		&item.Summary,
		&item.LifecycleStatus,
		&archivedAt,
		&redactedAt,
		&item.RetentionExpiresAt,
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
	if item.BookingAction == "" {
		item.BookingAction = BookingActionBook
	}
	if len(rescheduleCandidates) > 0 {
		if err := json.Unmarshal(rescheduleCandidates, &item.RescheduleCandidates); err != nil {
			return nil, err
		}
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
	if len(partyPlan) > 0 && strings.TrimSpace(string(partyPlan)) != "{}" {
		if err := json.Unmarshal(partyPlan, &item.PartyPlan); err != nil {
			return nil, err
		}
	}
	if len(dialogState) > 0 {
		if err := json.Unmarshal(dialogState, &item.DialogState); err != nil {
			return nil, err
		}
	}
	item.DialogState = normalizedDialogState(item.DialogState)
	if item.StaffSelectionMode == "" {
		item.StaffSelectionMode = booking.StaffSelectionSpecific
	}
	if item.LifecycleStatus == "" {
		item.LifecycleStatus = LifecycleActive
	}
	if archivedAt.Valid {
		item.ArchivedAt = &archivedAt.Time
	}
	if redactedAt.Valid {
		item.RedactedAt = &redactedAt.Time
	}
	if endedAt.Valid {
		item.EndedAt = &endedAt.Time
	}
	return &item, nil
}
