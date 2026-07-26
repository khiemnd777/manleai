package schedulingretention

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ProcessNext(ctx context.Context, kind string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("scheduling retention repository is unavailable")
	}
	switch kind {
	case KindOwnerRetentionExpiry:
		return r.prepareNextRetentionExpiry(ctx, prepareOwnerNotificationRetentionSQL, "owner_notifications")
	case KindCustomerRetentionExpiry:
		return r.prepareNextRetentionExpiry(ctx, prepareCustomerNotificationRetentionSQL, "customer_notification_deliveries")
	case KindSchedulingRequest:
		return r.redactNextSchedulingRequest(ctx)
	case KindOwnerNotification:
		return r.redactNextOwnerNotification(ctx)
	case KindCustomerNotification:
		return r.redactNextCustomerNotification(ctx)
	case KindVoiceAudio:
		return r.redactNextVoiceAudio(ctx)
	default:
		return false, errors.New("scheduling retention kind is invalid")
	}
}

func (r *Repository) RedactNext(ctx context.Context, kind string) (bool, error) {
	if kind == KindOwnerRetentionExpiry || kind == KindCustomerRetentionExpiry {
		return false, errors.New("scheduling retention kind is not a redaction owner")
	}
	return r.ProcessNext(ctx, kind)
}

func (r *Repository) prepareNextRetentionExpiry(ctx context.Context, candidateSQL, table string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	var terminalAt time.Time
	if err := tx.QueryRowContext(ctx, candidateSQL).Scan(&id, &terminalAt); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var updateSQL string
	switch table {
	case "owner_notifications":
		updateSQL = `UPDATE owner_notifications SET retention_expires_at=$2::timestamptz + interval '90 days' WHERE id=$1 AND retention_expires_at IS NULL`
	case "customer_notification_deliveries":
		updateSQL = `UPDATE customer_notification_deliveries SET retention_expires_at=$2::timestamptz + interval '90 days' WHERE id=$1 AND retention_expires_at IS NULL`
	default:
		return false, errors.New("scheduling retention expiry owner is invalid")
	}
	result, err := tx.ExecContext(ctx, updateSQL, id, terminalAt)
	if err != nil {
		return false, err
	}
	if !oneRow(result) {
		return false, errors.New("scheduling retention expiry claim was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) redactNextSchedulingRequest(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var requestID, salonID string
	err = tx.QueryRowContext(ctx, `
		SELECT request.id::text, request.salon_id::text
		FROM scheduling_requests request
		WHERE request.status IN ('resolved','dismissed')
		  AND request.redacted_at IS NULL
		  AND request.retention_expires_at <= now()
		  AND NOT EXISTS (
		      SELECT 1 FROM owner_notifications notification
		      WHERE notification.salon_id=request.salon_id
		        AND notification.scheduling_request_id=request.id
		        AND (notification.delivery_status NOT IN ('delivered','undelivered','dead_letter','disabled')
		             OR notification.delivery_claim_token IS NOT NULL
		             OR notification.delivery_lease_expires_at IS NOT NULL)
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM customer_notification_deliveries delivery
		      WHERE delivery.salon_id=request.salon_id
		        AND delivery.scheduling_request_id=request.id
		        AND (delivery.delivery_status NOT IN ('delivered','undelivered','dead_letter','suppressed')
		             OR delivery.delivery_claim_token IS NOT NULL
		             OR delivery.delivery_lease_expires_at IS NOT NULL)
		  )
		ORDER BY request.retention_expires_at, request.id
		LIMIT 1
		FOR UPDATE OF request SKIP LOCKED
	`).Scan(&requestID, &salonID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var redactedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT now()`).Scan(&redactedAt); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_request_segments
		SET guest_reference=NULL, redacted_at=$3, redaction_version=$4
		WHERE scheduling_request_id=$1 AND salon_id=$2 AND redacted_at IS NULL
	`, requestID, salonID, redactedAt.Time, PolicyVersion); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_request_events
		SET payload=retention_safe_audit_payload(payload), redacted_at=$3, redaction_version=$4
		WHERE scheduling_request_id=$1 AND salon_id=$2 AND redacted_at IS NULL
	`, requestID, salonID, redactedAt.Time, PolicyVersion); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE scheduling_requests
		SET customer_name='[redacted]', customer_phone='[redacted]', customer_email=NULL,
		    notes=NULL, resolution_reason=NULL,
		    target_description=CASE
		        WHEN operation_type IN ('reschedule','cancel') AND target_appointment_id IS NULL
		            THEN '[redacted]'
		        ELSE NULL
		    END,
		    redacted_at=$3, redaction_version=$4
		WHERE id=$1 AND salon_id=$2 AND redacted_at IS NULL
	`, requestID, salonID, redactedAt.Time, PolicyVersion)
	if err != nil {
		return false, err
	}
	if !oneRow(result) {
		return false, errors.New("scheduling retention request claim was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) redactNextOwnerNotification(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var notificationID string
	err = tx.QueryRowContext(ctx, ownerNotificationCandidateSQL).Scan(&notificationID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE owner_notifications
		SET message='[redacted]', payload=retention_safe_audit_payload(payload),
		    delivery_destination_masked=NULL, last_delivery_error=NULL,
		    redacted_at=now(), redaction_version=$2
		WHERE id=$1 AND redacted_at IS NULL
	`, notificationID, PolicyVersion)
	if err != nil {
		return false, err
	}
	if !oneRow(result) {
		return false, errors.New("scheduling retention owner notification claim was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) redactNextCustomerNotification(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var deliveryID string
	err = tx.QueryRowContext(ctx, customerNotificationCandidateSQL).Scan(&deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_deliveries
		SET message_body='[redacted]', destination_e164=NULL, destination_masked=NULL, destination_hash=NULL,
		    redacted_at=now(), redaction_version=$2, updated_at=now()
		WHERE id=$1 AND redacted_at IS NULL
	`, deliveryID, PolicyVersion)
	if err != nil {
		return false, err
	}
	if !oneRow(result) {
		return false, errors.New("scheduling retention customer notification claim was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) redactNextVoiceAudio(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var audioID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM voice_audio_outputs
		WHERE redacted_at IS NULL AND expires_at <= now()
		ORDER BY expires_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&audioID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE voice_audio_outputs
		SET audio_data=''::bytea, redacted_at=now(), redaction_version=$2
		WHERE id=$1 AND redacted_at IS NULL
	`, audioID, PolicyVersion)
	if err != nil {
		return false, err
	}
	if !oneRow(result) {
		return false, errors.New("scheduling retention audio claim was lost")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func oneRow(result sql.Result) bool {
	count, err := result.RowsAffected()
	return err == nil && count == 1
}

const prepareOwnerNotificationRetentionSQL = `
    SELECT notification.id::text,
           GREATEST(
               CASE
                   WHEN request.id IS NOT NULL THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                   WHEN attempt.id IS NOT NULL THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
               END,
               COALESCE(notification.delivered_at, notification.dead_lettered_at,
                        notification.last_provider_event_at, notification.created_at)
           ) AS terminal_at
    FROM owner_notifications notification
    LEFT JOIN scheduling_requests request
      ON request.salon_id=notification.salon_id AND request.id=notification.scheduling_request_id
    LEFT JOIN booking_attempts attempt
      ON attempt.salon_id=notification.salon_id AND attempt.id=notification.booking_attempt_id
    LEFT JOIN booking_reconciliation_tasks task
      ON task.salon_id=notification.salon_id AND task.booking_attempt_id=attempt.id
    WHERE notification.retention_expires_at IS NULL
      AND notification.redacted_at IS NULL
      AND notification.delivery_status IN ('delivered','undelivered','dead_letter','disabled')
      AND notification.delivery_claim_token IS NULL
      AND notification.delivery_lease_expires_at IS NULL
      AND (
          (request.id IS NOT NULL AND request.status IN ('resolved','dismissed'))
          OR
          (attempt.id IS NOT NULL
              AND attempt.status IN ('confirmed','rescheduled','cancelled','declined','no_show','failed')
              AND attempt.processing_token IS NULL AND attempt.processing_lease_expires_at IS NULL
              AND attempt.reconciliation_status IN ('not_required','resolved')
              AND (task.id IS NULL OR task.status='resolved'))
      )
    ORDER BY terminal_at, notification.id
    LIMIT 1
    FOR UPDATE OF notification SKIP LOCKED`

const prepareCustomerNotificationRetentionSQL = `
    SELECT delivery.id::text,
           GREATEST(
               CASE
                   WHEN request.id IS NOT NULL THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                   WHEN attempt.id IS NOT NULL THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
               END,
               COALESCE(delivery.delivered_at, delivery.dead_lettered_at,
                        delivery.suppressed_at, delivery.last_provider_event_at, delivery.updated_at)
           ) AS terminal_at
    FROM customer_notification_deliveries delivery
    LEFT JOIN scheduling_requests request
      ON request.salon_id=delivery.salon_id AND request.id=delivery.scheduling_request_id
    LEFT JOIN booking_attempts attempt
      ON attempt.salon_id=delivery.salon_id AND attempt.id=delivery.booking_attempt_id
    LEFT JOIN booking_reconciliation_tasks task
      ON task.salon_id=delivery.salon_id AND task.booking_attempt_id=attempt.id
    WHERE delivery.retention_expires_at IS NULL
      AND delivery.redacted_at IS NULL
      AND delivery.delivery_status IN ('delivered','undelivered','dead_letter','suppressed')
      AND delivery.delivery_claim_token IS NULL
      AND delivery.delivery_lease_expires_at IS NULL
      AND (
          (request.id IS NOT NULL AND request.status IN ('resolved','dismissed'))
          OR
          (attempt.id IS NOT NULL
              AND attempt.status IN ('confirmed','rescheduled','cancelled','declined','no_show','failed')
              AND attempt.processing_token IS NULL AND attempt.processing_lease_expires_at IS NULL
              AND attempt.reconciliation_status IN ('not_required','resolved')
              AND (task.id IS NULL OR task.status='resolved'))
      )
    ORDER BY terminal_at, delivery.id
    LIMIT 1
    FOR UPDATE OF delivery SKIP LOCKED`

const ownerNotificationCandidateSQL = `
SELECT notification.id::text
FROM owner_notifications notification
LEFT JOIN scheduling_requests request
  ON request.salon_id=notification.salon_id AND request.id=notification.scheduling_request_id
LEFT JOIN booking_attempts attempt
  ON attempt.salon_id=notification.salon_id AND attempt.id=notification.booking_attempt_id
LEFT JOIN booking_reconciliation_tasks task
  ON task.salon_id=notification.salon_id AND task.booking_attempt_id=attempt.id
WHERE notification.redacted_at IS NULL
  AND notification.retention_expires_at <= now()
  AND notification.delivery_status IN ('delivered','undelivered','dead_letter','disabled')
  AND notification.delivery_claim_token IS NULL AND notification.delivery_lease_expires_at IS NULL
  AND (
      (request.id IS NOT NULL AND request.status IN ('resolved','dismissed'))
      OR
      (attempt.id IS NOT NULL
          AND attempt.status IN ('confirmed','rescheduled','cancelled','declined','no_show','failed')
          AND attempt.processing_token IS NULL AND attempt.processing_lease_expires_at IS NULL
          AND attempt.reconciliation_status IN ('not_required','resolved')
          AND (task.id IS NULL OR task.status='resolved'))
  )
ORDER BY notification.retention_expires_at, notification.id
LIMIT 1
FOR UPDATE OF notification SKIP LOCKED`

const customerNotificationCandidateSQL = `
SELECT delivery.id::text
FROM customer_notification_deliveries delivery
LEFT JOIN scheduling_requests request
  ON request.salon_id=delivery.salon_id AND request.id=delivery.scheduling_request_id
LEFT JOIN booking_attempts attempt
  ON attempt.salon_id=delivery.salon_id AND attempt.id=delivery.booking_attempt_id
LEFT JOIN booking_reconciliation_tasks task
  ON task.salon_id=delivery.salon_id AND task.booking_attempt_id=attempt.id
WHERE delivery.redacted_at IS NULL
  AND delivery.retention_expires_at <= now()
  AND delivery.delivery_status IN ('delivered','undelivered','dead_letter','suppressed')
  AND delivery.delivery_claim_token IS NULL AND delivery.delivery_lease_expires_at IS NULL
  AND (
      (request.id IS NOT NULL AND request.status IN ('resolved','dismissed'))
      OR
      (attempt.id IS NOT NULL
          AND attempt.status IN ('confirmed','rescheduled','cancelled','declined','no_show','failed')
          AND attempt.processing_token IS NULL AND attempt.processing_lease_expires_at IS NULL
          AND attempt.reconciliation_status IN ('not_required','resolved')
          AND (task.id IS NULL OR task.status='resolved'))
  )
ORDER BY delivery.retention_expires_at, delivery.id
LIMIT 1
FOR UPDATE OF delivery SKIP LOCKED`
