package notificationdelivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) RecoverExpiredLeases(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, salon_id::text, delivery_claim_token::text,
		       delivery_dispatch_started_at IS NOT NULL
		FROM owner_notifications
		WHERE delivery_status = 'delivering'
		  AND delivery_lease_expires_at <= now()
		ORDER BY delivery_lease_expires_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	type expired struct {
		id, salonID, token string
		dispatched         bool
	}
	items := make([]expired, 0, limit)
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.id, &item.salonID, &item.token, &item.dispatched); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, item := range items {
		if item.dispatched {
			if _, err := tx.ExecContext(ctx, `
				UPDATE owner_notifications
				SET delivery_status='dead_letter', dead_lettered_at=now(),
				    last_delivery_error_code='DELIVERY_OUTCOME_UNKNOWN',
				    last_delivery_error='Delivery outcome requires manual review.',
				    delivery_claim_token=NULL, delivery_claimed_at=NULL,
				    delivery_lease_expires_at=NULL, delivery_dispatch_started_at=NULL
				WHERE id=$1 AND delivery_claim_token=$2::uuid
			`, item.id, item.token); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE owner_notification_delivery_attempts
				SET outcome='outcome_unknown', error_code='DELIVERY_OUTCOME_UNKNOWN', finished_at=now()
				WHERE claim_token=$1::uuid AND outcome='leased'
			`, item.token); err != nil {
				return 0, err
			}
			if err := insertEvent(ctx, tx, item.salonID, item.id, "expired-dispatch:"+item.token, "dead_lettered", StatusDeadLetter, "", "DELIVERY_OUTCOME_UNKNOWN"); err != nil {
				return 0, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE owner_notifications
				SET delivery_status='failed', next_delivery_at=now(),
				    last_delivery_error_code='DELIVERY_LEASE_EXPIRED',
				    last_delivery_error='Delivery lease expired before dispatch.',
				    delivery_claim_token=NULL, delivery_claimed_at=NULL,
				    delivery_lease_expires_at=NULL, delivery_dispatch_started_at=NULL
				WHERE id=$1 AND delivery_claim_token=$2::uuid
			`, item.id, item.token); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE owner_notification_delivery_attempts
				SET outcome='safe_retry', error_code='DELIVERY_LEASE_EXPIRED', finished_at=now()
				WHERE claim_token=$1::uuid AND outcome='leased'
			`, item.token); err != nil {
				return 0, err
			}
			if err := insertEvent(ctx, tx, item.salonID, item.id, "expired-before-dispatch:"+item.token, "safe_retry_scheduled", StatusFailed, "", "DELIVERY_LEASE_EXPIRED"); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (r *Repository) ClaimBatch(ctx context.Context, limit int, lease time.Duration) ([]ClaimedNotification, error) {
	if limit <= 0 || lease <= 0 {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		WITH ranked AS (
			SELECT notification.id,notification.salon_id,notification.next_delivery_at,
			       notification.created_at,
			       row_number() OVER (
			           PARTITION BY notification.salon_id
			           ORDER BY notification.next_delivery_at,notification.created_at,notification.id
			       ) AS tenant_rank,
			       COALESCE(limits.worker_claims_per_batch,2) AS tenant_limit
			FROM owner_notifications notification
			LEFT JOIN tenant_runtime_limits limits ON limits.salon_id=notification.salon_id
			WHERE notification.delivery_status IN ('queued','failed')
			  AND notification.next_delivery_at <= now()
			  AND notification.delivery_attempts::bigint < (notification.requeue_count::bigint + 1) * $1
		), candidates AS (
			SELECT notification.id
			FROM owner_notifications notification
			JOIN ranked ON ranked.id=notification.id
			WHERE ranked.tenant_rank<=ranked.tenant_limit
			ORDER BY ranked.next_delivery_at,ranked.created_at,notification.id
			FOR UPDATE OF notification SKIP LOCKED
			LIMIT $2
		), claimed AS (
			UPDATE owner_notifications notification
			SET delivery_status='delivering', delivery_provider='twilio',
			    delivery_claim_token=gen_random_uuid(), delivery_claimed_at=now(),
			    delivery_lease_expires_at=now()+($3 * interval '1 millisecond'),
			    delivery_dispatch_started_at=NULL,
			    delivery_attempts=notification.delivery_attempts+1,
			    dead_lettered_at=NULL, last_delivery_error=NULL,
			    last_delivery_error_code=NULL
			FROM candidates
			WHERE notification.id=candidates.id
			RETURNING notification.id, notification.salon_id, notification.type,
			          notification.message, notification.delivery_claim_token,
			          notification.delivery_attempts, notification.requeue_count,
			          notification.created_at
		)
		SELECT id::text, salon_id::text, type, message, delivery_claim_token::text,
		       delivery_attempts, requeue_count, created_at
		FROM claimed
	`, MaxSafeDeliveryAttempts, limit, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	items := make([]ClaimedNotification, 0, limit)
	for rows.Next() {
		var item ClaimedNotification
		if err := rows.Scan(&item.ID, &item.SalonID, &item.Type, &item.Message, &item.ClaimToken, &item.AttemptNumber, &item.RequeueCount, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO owner_notification_delivery_attempts (
				salon_id, owner_notification_id, attempt_number, claim_token, provider, outcome
			) VALUES ($1,$2,$3,$4::uuid,'twilio','leased')
		`, item.SalonID, item.ID, item.AttemptNumber, item.ClaimToken); err != nil {
			return nil, err
		}
		if err := insertEvent(ctx, tx, item.SalonID, item.ID, "claim:"+item.ClaimToken, "claimed", StatusDelivering, "", ""); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) MarkDispatchStarted(ctx context.Context, item ClaimedNotification, destinationMasked string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE owner_notifications
		SET delivery_dispatch_started_at=now(), delivery_destination_masked=$3
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering'
		  AND delivery_claim_token=$4::uuid AND delivery_lease_expires_at > now()
	`, item.ID, item.SalonID, destinationMasked, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_notification_delivery_attempts SET dispatch_started_at=now()
		WHERE claim_token=$1::uuid AND outcome='leased'
	`, item.ClaimToken); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, item.SalonID, item.ID, "dispatch:"+item.ClaimToken, "dispatch_started", StatusDelivering, "", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RecordDisabled(ctx context.Context, item ClaimedNotification) error {
	return r.completeWithoutProvider(ctx, item, StatusDisabled, "disabled", "OWNER_SMS_DISABLED", time.Time{})
}

func (r *Repository) RecordSafeFailure(ctx context.Context, item ClaimedNotification, code string, next time.Time) error {
	status, outcome := StatusFailed, "safe_retry"
	if DeliveryAttemptInCycle(item) >= MaxSafeDeliveryAttempts {
		status, outcome = StatusDeadLetter, "dead_letter"
	}
	return r.completeWithoutProvider(ctx, item, status, outcome, code, next)
}

func (r *Repository) RecordOutcomeUnknown(ctx context.Context, item ClaimedNotification, code string) error {
	return r.completeWithoutProvider(ctx, item, StatusDeadLetter, "outcome_unknown", code, time.Time{})
}

func (r *Repository) RecordDefinitiveFailure(ctx context.Context, item ClaimedNotification, code string) error {
	return r.completeWithoutProvider(ctx, item, StatusDeadLetter, "dead_letter", code, time.Time{})
}

func (r *Repository) completeWithoutProvider(ctx context.Context, item ClaimedNotification, status, outcome, code string, next time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	deadLetter := status == StatusDeadLetter
	result, err := tx.ExecContext(ctx, `
		UPDATE owner_notifications
		SET delivery_status=$3,
		    next_delivery_at=CASE WHEN $4::timestamptz IS NULL THEN next_delivery_at ELSE $4 END,
		    dead_lettered_at=CASE WHEN $5 THEN now() ELSE NULL END,
		    last_delivery_error_code=NULLIF($6,''),
		    last_delivery_error=CASE WHEN $6='' THEN NULL ELSE 'Delivery is not complete.' END,
		    delivery_claim_token=NULL, delivery_claimed_at=NULL,
		    delivery_lease_expires_at=NULL, delivery_dispatch_started_at=NULL
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering' AND delivery_claim_token=$7::uuid
	`, item.ID, item.SalonID, status, nullTime(next), deadLetter, code, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_notification_delivery_attempts
		SET outcome=$2, error_code=NULLIF($3,''), finished_at=now()
		WHERE claim_token=$1::uuid AND outcome='leased'
	`, item.ClaimToken, outcome, code); err != nil {
		return err
	}
	eventType := "safe_retry_scheduled"
	if deadLetter {
		eventType = "dead_lettered"
	}
	if status == StatusDisabled {
		eventType = "delivery_disabled"
	}
	if err := insertEvent(ctx, tx, item.SalonID, item.ID, outcome+":"+item.ClaimToken, eventType, status, "", code); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RecordProviderResult(ctx context.Context, item ClaimedNotification, result SendResult) error {
	if strings.TrimSpace(result.ProviderMessageID) == "" || result.StatusRank <= 0 {
		return ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	delivered := result.DeliveryStatus == StatusDelivered
	deadLetter := result.DeliveryStatus == StatusDeadLetter
	resultSQL, err := tx.ExecContext(ctx, `
		UPDATE owner_notifications
		SET delivery_status=$3, delivery_provider='twilio', provider_message_id=$4,
		    provider_status=$5, provider_status_rank=$6, last_provider_event_at=now(),
		    delivered_at=CASE WHEN $7 THEN now() ELSE delivered_at END,
		    dead_lettered_at=CASE WHEN $8 THEN now() ELSE NULL END,
		    last_delivery_error=CASE WHEN $8 THEN 'Provider reported terminal delivery failure.' ELSE NULL END,
		    last_delivery_error_code=CASE WHEN $8 THEN 'TWILIO_DELIVERY_FAILED' ELSE NULL END,
		    delivery_claim_token=NULL, delivery_claimed_at=NULL,
		    delivery_lease_expires_at=NULL, delivery_dispatch_started_at=NULL
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering' AND delivery_claim_token=$9::uuid
	`, item.ID, item.SalonID, result.DeliveryStatus, result.ProviderMessageID, result.ProviderStatus, result.StatusRank, delivered, deadLetter, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := resultSQL.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	outcome := "provider_accepted"
	if result.DeliveryStatus == StatusSent {
		outcome = "sent"
	}
	if delivered {
		outcome = "delivered"
	}
	if deadLetter {
		outcome = "provider_failed"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_notification_delivery_attempts
		SET outcome=$2, provider_status=$3, provider_message_id=$4, finished_at=now()
		WHERE claim_token=$1::uuid AND outcome='leased'
	`, item.ClaimToken, outcome, result.ProviderStatus, result.ProviderMessageID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, item.SalonID, item.ID, "provider-response:"+item.ClaimToken, "provider_response", result.DeliveryStatus, result.ProviderStatus, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ApplyProviderCallback(ctx context.Context, callback ProviderCallback) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, salonID, currentStatus string
	var currentRank int
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, delivery_status, provider_status_rank
		FROM owner_notifications
		WHERE delivery_provider=$1 AND provider_message_id=$2
		FOR UPDATE
	`, callback.Provider, callback.ProviderMessageID).Scan(&id, &salonID, &currentStatus, &currentRank); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT event_fingerprint FROM owner_notification_delivery_events
		WHERE salon_id=$1 AND event_key=$2
	`, salonID, callback.EventKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != callback.EventFingerprint {
			return ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	resultStatus := currentStatus
	if callback.StatusRank > currentRank {
		resultStatus = callback.DeliveryStatus
		deadLetter := resultStatus == StatusDeadLetter
		delivered := resultStatus == StatusDelivered
		if _, err := tx.ExecContext(ctx, `
			UPDATE owner_notifications
			SET delivery_status=$2, provider_status=$3, provider_status_rank=$4,
			    last_provider_event_at=$5,
			    delivered_at=CASE WHEN $6 THEN COALESCE(delivered_at,$5) ELSE delivered_at END,
			    dead_lettered_at=CASE WHEN $7 THEN COALESCE(dead_lettered_at,$5) ELSE NULL END,
			    last_delivery_error_code=CASE WHEN $7 THEN NULLIF($8,'') ELSE NULL END,
			    last_delivery_error=CASE WHEN $7 THEN 'Provider reported terminal delivery failure.' ELSE NULL END
			WHERE id=$1
		`, id, resultStatus, callback.ProviderStatus, callback.StatusRank, callback.OccurredAt, delivered, deadLetter, callback.ErrorCode); err != nil {
			return err
		}
		attemptOutcome := "provider_accepted"
		if resultStatus == StatusSent {
			attemptOutcome = "sent"
		}
		if resultStatus == StatusDelivered {
			attemptOutcome = "delivered"
		}
		if resultStatus == StatusDeadLetter {
			attemptOutcome = "provider_failed"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE owner_notification_delivery_attempts
			SET outcome=$2, provider_status=$3, error_code=NULLIF($4,''), finished_at=COALESCE(finished_at,now())
			WHERE owner_notification_id=$1 AND provider_message_id=$5
		`, id, attemptOutcome, callback.ProviderStatus, callback.ErrorCode, callback.ProviderMessageID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notification_delivery_events (
			salon_id, owner_notification_id, event_key, event_fingerprint,
			event_type, delivery_status, provider_status, error_code, created_at
		) VALUES ($1,$2,$3,$4,'status_callback',$5,NULLIF($6,''),NULLIF($7,''),$8)
	`, salonID, id, callback.EventKey, callback.EventFingerprint, resultStatus, callback.ProviderStatus, callback.ErrorCode, callback.OccurredAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error) {
	var salonID string
	err := r.db.QueryRowContext(ctx, `
		SELECT salon_id::text FROM owner_notifications
		WHERE delivery_provider=$1 AND provider_message_id=$2
	`, provider, providerMessageID).Scan(&salonID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return salonID, err
}

func (r *Repository) ListForOwner(ctx context.Context, salonID, ownerUserID, status string, limit, offset int) ([]DeliveryRecord, DeliveryMetrics, error) {
	if err := r.ensureOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, DeliveryMetrics{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, type, status, delivery_status,
		       COALESCE(delivery_provider,''), COALESCE(delivery_destination_masked,''),
		       delivery_attempts, COALESCE(provider_status,''),
		       COALESCE(last_delivery_error_code,''), next_delivery_at,
		       delivered_at, dead_lettered_at, last_provider_event_at,
		       redacted_at, COALESCE(redaction_version, 0), created_at
		FROM owner_notifications
		WHERE salon_id=$1 AND ($2='' OR delivery_status=$2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, salonID, status, limit+1, offset)
	if err != nil {
		return nil, DeliveryMetrics{}, err
	}
	defer rows.Close()
	items := make([]DeliveryRecord, 0, limit+1)
	for rows.Next() {
		var item DeliveryRecord
		if err := scanDelivery(rows, &item); err != nil {
			return nil, DeliveryMetrics{}, err
		}
		setRequeueState(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, DeliveryMetrics{}, err
	}
	metrics, err := r.metrics(ctx, salonID)
	return items, metrics, err
}

func (r *Repository) GetForOwner(ctx context.Context, salonID, ownerUserID, notificationID string) (*DeliveryRecord, error) {
	if err := r.ensureOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, type, status, delivery_status,
		       COALESCE(delivery_provider,''), COALESCE(delivery_destination_masked,''),
		       delivery_attempts, COALESCE(provider_status,''),
		       COALESCE(last_delivery_error_code,''), next_delivery_at,
		       delivered_at, dead_lettered_at, last_provider_event_at,
		       redacted_at, COALESCE(redaction_version, 0), created_at
		FROM owner_notifications WHERE salon_id=$1 AND id=$2
	`, salonID, notificationID)
	var item DeliveryRecord
	if err := scanDelivery(row, &item); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	setRequeueState(&item)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, event_type, delivery_status, COALESCE(provider_status,''),
		       COALESCE(error_code,''), created_at
		FROM owner_notification_delivery_events
		WHERE salon_id=$1 AND owner_notification_id=$2
		ORDER BY created_at ASC, id ASC
	`, salonID, notificationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	item.Events = []DeliveryEvent{}
	for rows.Next() {
		var event DeliveryEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.DeliveryStatus, &event.ProviderStatus, &event.ErrorCode, &event.CreatedAt); err != nil {
			return nil, err
		}
		item.Events = append(item.Events, event)
	}
	return &item, rows.Err()
}

func (r *Repository) RequeueForOwner(ctx context.Context, salonID, ownerUserID, notificationID, actionKey, fingerprint string) (*DeliveryRecord, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var ownerOK bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM salons salon
			WHERE salon.id=$1
			  AND (
			      public.has_active_tenant_membership(salon.id, $2::uuid)
			      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'operations.write')
			  )
		)
	`, salonID, ownerUserID).Scan(&ownerOK); err != nil {
		return nil, false, err
	}
	if !ownerOK {
		return nil, false, ErrNotFound
	}
	var existingFingerprint, existingNotificationID string
	err = tx.QueryRowContext(ctx, `
		SELECT action_fingerprint, owner_notification_id::text
		FROM owner_notification_delivery_actions WHERE salon_id=$1 AND action_key=$2
	`, salonID, actionKey).Scan(&existingFingerprint, &existingNotificationID)
	if err == nil {
		if existingFingerprint != fingerprint || existingNotificationID != notificationID {
			return nil, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		item, getErr := r.GetForOwner(ctx, salonID, ownerUserID, notificationID)
		return item, true, getErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var status, code string
	var redactedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT delivery_status, COALESCE(last_delivery_error_code,''), redacted_at
		FROM owner_notifications WHERE salon_id=$1 AND id=$2 FOR UPDATE
	`, salonID, notificationID).Scan(&status, &code, &redactedAt); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}
	if redactedAt.Valid || status != StatusDeadLetter || code == "DELIVERY_OUTCOME_UNKNOWN" {
		return nil, false, ErrRequeueBlocked
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_notifications
		SET delivery_status='queued', next_delivery_at=now(), dead_lettered_at=NULL,
		    provider_message_id=NULL, provider_status=NULL, provider_status_rank=0,
		    last_provider_event_at=NULL, delivered_at=NULL,
		    last_delivery_error=NULL, last_delivery_error_code=NULL,
		    delivery_claim_token=NULL, delivery_claimed_at=NULL,
		    delivery_lease_expires_at=NULL, delivery_dispatch_started_at=NULL,
		    requeue_count=requeue_count+1
		WHERE salon_id=$1 AND id=$2
	`, salonID, notificationID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notification_delivery_actions (
			salon_id, owner_notification_id, action_key, action_fingerprint,
			action_type, actor_user_id, result_delivery_status
		) VALUES ($1,$2,$3,$4,'requeue',$5,'queued')
	`, salonID, notificationID, actionKey, fingerprint, ownerUserID); err != nil {
		return nil, false, err
	}
	if err := insertEvent(ctx, tx, salonID, notificationID, "owner-requeue:"+actionKey, "owner_requeued", StatusQueued, "", ""); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	item, err := r.GetForOwner(ctx, salonID, ownerUserID, notificationID)
	return item, false, err
}

func (r *Repository) ensureOwner(ctx context.Context, salonID, ownerUserID string) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM salons salon
			WHERE salon.id=$1
			  AND (
			      public.has_active_tenant_membership(salon.id, $2::uuid)
			      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'operations.read')
			  )
		)
	`, salonID, ownerUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) metrics(ctx context.Context, salonID string) (DeliveryMetrics, error) {
	var m DeliveryMetrics
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE delivery_status='queued'),
		       count(*) FILTER (WHERE delivery_status='delivering'),
		       count(*) FILTER (WHERE delivery_status='provider_accepted'),
		       count(*) FILTER (WHERE delivery_status='sent'),
		       count(*) FILTER (WHERE delivery_status='delivered'),
		       count(*) FILTER (WHERE delivery_status='dead_letter'),
		       count(*) FILTER (WHERE delivery_status='disabled')
		FROM owner_notifications WHERE salon_id=$1
	`, salonID).Scan(&m.Queued, &m.Delivering, &m.ProviderAccepted, &m.Sent, &m.Delivered, &m.DeadLetter, &m.Disabled)
	return m, err
}

type scanner interface{ Scan(...any) error }

func scanDelivery(row scanner, item *DeliveryRecord) error {
	var redactedAt sql.NullTime
	err := row.Scan(&item.ID, &item.SalonID, &item.NotificationType, &item.InAppStatus,
		&item.DeliveryStatus, &item.DeliveryProvider, &item.DestinationMasked,
		&item.DeliveryAttempts, &item.ProviderStatus, &item.LastDeliveryErrorCode,
		&item.NextDeliveryAt, &item.DeliveredAt, &item.DeadLetteredAt,
		&item.LastProviderEventAt, &redactedAt, &item.RedactionVersion, &item.CreatedAt)
	if err != nil {
		return err
	}
	if redactedAt.Valid {
		item.Redacted = true
		value := redactedAt.Time
		item.RedactedAt = &value
	}
	return nil
}

func setRequeueState(item *DeliveryRecord) {
	item.CanRequeue = !item.Redacted && item.DeliveryStatus == StatusDeadLetter && item.LastDeliveryErrorCode != "DELIVERY_OUTCOME_UNKNOWN"
	if item.Redacted {
		item.RequeueBlockedReason = "Delivery content was redacted by the retention policy."
		return
	}
	if item.DeliveryStatus == StatusDeadLetter && !item.CanRequeue {
		item.RequeueBlockedReason = "Delivery outcome is unknown and requires manual verification."
	}
}

func insertEvent(ctx context.Context, tx *sql.Tx, salonID, notificationID, eventKey, eventType, status, providerStatus, errorCode string) error {
	fingerprint := fingerprintParts(eventType, status, providerStatus, errorCode)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notification_delivery_events (
			salon_id, owner_notification_id, event_key, event_fingerprint,
			event_type, delivery_status, provider_status, error_code
		) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))
		ON CONFLICT (salon_id,event_key) DO NOTHING
	`, salonID, notificationID, eventKey, fingerprint, eventType, status, providerStatus, errorCode)
	return err
}

func fingerprintParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func RequeueFingerprint(notificationID string) string {
	return fingerprintParts("requeue", strings.TrimSpace(notificationID))
}

func SafeRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > MaxSafeDeliveryAttempts {
		attempt = MaxSafeDeliveryAttempts
	}
	return time.Duration(1<<(attempt-1)) * SafeRetryBaseDelay
}

func DeliveryAttemptInCycle(item ClaimedNotification) int {
	attempt := item.AttemptNumber - item.RequeueCount*MaxSafeDeliveryAttempts
	if attempt < 1 {
		return 1
	}
	if attempt > MaxSafeDeliveryAttempts {
		return MaxSafeDeliveryAttempts
	}
	return attempt
}

func ValidListStatus(value string) bool {
	switch value {
	case "", StatusQueued, StatusDelivering, StatusProviderAccepted, StatusSent, StatusDelivered, StatusFailed, StatusUndelivered, StatusDeadLetter, StatusDisabled:
		return true
	default:
		return false
	}
}

func ValidateListBounds(limit, offset int) error {
	if limit < 1 || limit > 100 || offset < 0 {
		return fmt.Errorf("%w: pagination", ErrValidation)
	}
	return nil
}
