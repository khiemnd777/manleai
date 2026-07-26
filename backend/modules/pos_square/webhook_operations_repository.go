package pos_square

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

func (r *WebhookRepository) ListBookingWebhooksForOwner(ctx context.Context, salonID, ownerUserID, status string, limit, offset int) ([]WebhookEventRecord, WebhookMetrics, CalendarRepairHealth, error) {
	if err := r.ensureWebhookOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, WebhookMetrics{}, CalendarRepairHealth{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, event_type, processing_status, processing_attempts,
		       requeue_count, COALESCE(last_error_class,''), COALESCE(last_error_code,''),
		       next_attempt_at, delivered_at, processed_at, dead_lettered_at,
		       created_at, updated_at
		FROM square_booking_webhook_events
		WHERE salon_id=$1 AND ($2='' OR processing_status=$2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, salonID, status, limit+1, offset)
	if err != nil {
		return nil, WebhookMetrics{}, CalendarRepairHealth{}, err
	}
	defer rows.Close()
	events := make([]WebhookEventRecord, 0, limit+1)
	for rows.Next() {
		var event WebhookEventRecord
		if err := scanWebhookEvent(rows, &event); err != nil {
			return nil, WebhookMetrics{}, CalendarRepairHealth{}, err
		}
		setWebhookRequeueState(&event)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, WebhookMetrics{}, CalendarRepairHealth{}, err
	}
	metrics, err := r.webhookMetrics(ctx, salonID)
	if err != nil {
		return nil, WebhookMetrics{}, CalendarRepairHealth{}, err
	}
	repair, err := r.calendarRepairHealth(ctx, salonID)
	return events, metrics, repair, err
}

func (r *WebhookRepository) GetBookingWebhookForOwner(ctx context.Context, salonID, ownerUserID, eventID string) (*WebhookEventRecord, error) {
	if err := r.ensureWebhookOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, event_type, processing_status, processing_attempts,
		       requeue_count, COALESCE(last_error_class,''), COALESCE(last_error_code,''),
		       next_attempt_at, delivered_at, processed_at, dead_lettered_at,
		       created_at, updated_at
		FROM square_booking_webhook_events
		WHERE salon_id=$1 AND id=$2
	`, salonID, eventID)
	var event WebhookEventRecord
	if err := scanWebhookEvent(row, &event); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWebhookEventNotFound
	} else if err != nil {
		return nil, err
	}
	setWebhookRequeueState(&event)
	return &event, nil
}

func (r *WebhookRepository) RequeueBookingWebhookForOwner(ctx context.Context, salonID, ownerUserID, eventID, actionKey, fingerprint string) (*WebhookEventRecord, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := ensureWebhookOwnerTx(ctx, tx, salonID, ownerUserID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended('square-webhook-requeue:' || $1 || ':' || $2, 0))
	`, salonID, actionKey); err != nil {
		return nil, false, err
	}

	var existingFingerprint, existingEventID string
	err = tx.QueryRowContext(ctx, `
		SELECT action_fingerprint, webhook_event_id::text
		FROM square_booking_webhook_actions
		WHERE salon_id=$1 AND action_key=$2
	`, salonID, actionKey).Scan(&existingFingerprint, &existingEventID)
	if err == nil {
		if existingFingerprint != fingerprint || existingEventID != eventID {
			return nil, false, ErrWebhookActionConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		event, getErr := r.GetBookingWebhookForOwner(ctx, salonID, ownerUserID, eventID)
		return event, true, getErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	var processingStatus, lastErrorCode string
	var processingAttempts, requeueCount int
	err = tx.QueryRowContext(ctx, `
		SELECT processing_status, processing_attempts, requeue_count,
		       COALESCE(last_error_code,'')
		FROM square_booking_webhook_events
		WHERE salon_id=$1 AND id=$2
		FOR UPDATE
	`, salonID, eventID).Scan(&processingStatus, &processingAttempts, &requeueCount, &lastErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrWebhookEventNotFound
	}
	if err != nil {
		return nil, false, err
	}
	terminalFailed := processingStatus == WebhookStatusFailed && processingAttempts >= (requeueCount+1)*MaxWebhookAttemptsPerCycle
	if (processingStatus != WebhookStatusDeadLetter && !terminalFailed) || lastErrorCode == "" || requeueCount >= MaxWebhookRequeues {
		return nil, false, ErrWebhookRequeueBlocked
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE square_booking_webhook_events
		SET processing_status='pending', next_attempt_at=now(),
		    processing_token=NULL, processing_lease_expires_at=NULL,
		    processed_at=NULL, dead_lettered_at=NULL,
		    last_error=NULL, last_error_class=NULL, last_error_code=NULL,
		    requeue_count=requeue_count+1, updated_at=now()
		WHERE salon_id=$1 AND id=$2
	`, salonID, eventID)
	if err != nil {
		return nil, false, err
	}
	if !oneWebhookRow(result) {
		return nil, false, ErrWebhookEventNotFound
	}
	newRequeueCount := requeueCount + 1
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO square_booking_webhook_actions (
			salon_id, webhook_event_id, action_key, action_fingerprint,
			action_type, actor_user_id, result_processing_status,
			result_processing_attempts, result_requeue_count
		) VALUES ($1,$2,$3,$4,'requeue',$5,'pending',$6,$7)
	`, salonID, eventID, actionKey, fingerprint, ownerUserID, processingAttempts, newRequeueCount); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	event, err := r.GetBookingWebhookForOwner(ctx, salonID, ownerUserID, eventID)
	return event, false, err
}

func (r *WebhookRepository) ensureWebhookOwner(ctx context.Context, salonID, ownerUserID string) error {
	if r == nil || r.db == nil {
		return ErrWebhookOperationsValidation
	}
	var owned bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1 AND owner_user_id=$2)`, salonID, ownerUserID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return pos.ErrNotFound
	}
	return nil
}

func ensureWebhookOwnerTx(ctx context.Context, tx *sql.Tx, salonID, ownerUserID string) error {
	var owned bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1 AND owner_user_id=$2)`, salonID, ownerUserID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return pos.ErrNotFound
	}
	return nil
}

func (r *WebhookRepository) webhookMetrics(ctx context.Context, salonID string) (WebhookMetrics, error) {
	metrics := WebhookMetrics{RecentWindowHours: int(WebhookRecentSuccessWindow / time.Hour)}
	var lastDeliveredAt, lastSucceededAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE processing_status='pending'),
		       count(*) FILTER (WHERE processing_status='processing'),
		       count(*) FILTER (WHERE processing_status='failed'),
		       count(*) FILTER (WHERE processing_status='dead_letter'),
		       count(*) FILTER (WHERE processing_status='succeeded' AND processed_at >= now()-($2 * interval '1 second')),
		       max(delivered_at),
		       max(processed_at) FILTER (WHERE processing_status='succeeded')
		FROM square_booking_webhook_events
		WHERE salon_id=$1
	`, salonID, int(WebhookRecentSuccessWindow/time.Second)).Scan(
		&metrics.Pending, &metrics.Processing, &metrics.Failed, &metrics.DeadLetter,
		&metrics.SucceededRecent, &lastDeliveredAt, &lastSucceededAt,
	)
	if err != nil {
		return WebhookMetrics{}, err
	}
	metrics.LastDeliveredAt = webhookNullableTime(lastDeliveredAt)
	metrics.LastSucceededAt = webhookNullableTime(lastSucceededAt)
	return metrics, nil
}

func (r *WebhookRepository) calendarRepairHealth(ctx context.Context, salonID string) (CalendarRepairHealth, error) {
	var relevant bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM pos_connections WHERE salon_id=$1 AND provider='square')
		    OR EXISTS(SELECT 1 FROM square_booking_webhook_events WHERE salon_id=$1)
		    OR EXISTS(SELECT 1 FROM square_calendar_repair_state WHERE salon_id=$1)
	`, salonID).Scan(&relevant); err != nil {
		return CalendarRepairHealth{}, err
	}
	health := CalendarRepairHealth{Relevant: relevant, Status: "unknown"}
	if !relevant {
		return health, nil
	}
	var nextRepairAt, leaseExpiresAt, lastRepairedAt sql.NullTime
	var updatedAt time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT next_repair_at, lease_expires_at, repair_attempts, last_repaired_at,
		       COALESCE(last_error_class,''), COALESCE(last_error_code,''), updated_at
		FROM square_calendar_repair_state
		WHERE salon_id=$1
	`, salonID).Scan(&nextRepairAt, &leaseExpiresAt, &health.RepairAttempts, &lastRepairedAt,
		&health.LastErrorClass, &health.LastErrorCode, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return health, nil
	}
	if err != nil {
		return CalendarRepairHealth{}, err
	}
	health.NextRepairAt = webhookNullableTime(nextRepairAt)
	health.LeaseExpiresAt = webhookNullableTime(leaseExpiresAt)
	health.LastRepairedAt = webhookNullableTime(lastRepairedAt)
	health.UpdatedAt = webhookTimePointer(updatedAt)
	now := time.Now().UTC()
	switch {
	case health.LeaseExpiresAt != nil && health.LeaseExpiresAt.After(now):
		health.Status = "running"
	case health.LastErrorCode != "":
		health.Status = "degraded"
	case health.NextRepairAt != nil && !health.NextRepairAt.After(now):
		health.Status = "degraded"
	case health.LastRepairedAt != nil:
		health.Status = "healthy"
	}
	return health, nil
}

type webhookScanner interface{ Scan(...any) error }

func scanWebhookEvent(scanner webhookScanner, event *WebhookEventRecord) error {
	return scanner.Scan(
		&event.ID, &event.EventType, &event.ProcessingStatus, &event.ProcessingAttempts,
		&event.RequeueCount, &event.LastErrorClass, &event.LastErrorCode,
		&event.NextAttemptAt, &event.DeliveredAt, &event.ProcessedAt, &event.DeadLetteredAt,
		&event.CreatedAt, &event.UpdatedAt,
	)
}

func oneWebhookRow(result sql.Result) bool {
	if result == nil {
		return false
	}
	count, err := result.RowsAffected()
	return err == nil && count == 1
}

func webhookNullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return webhookTimePointer(value.Time)
}

func webhookTimePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
