package pos_square

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

var (
	ErrWebhookTargetNotFound  = errors.New("square webhook target not found")
	ErrWebhookTargetAmbiguous = errors.New("square webhook target is ambiguous")
	ErrWebhookClaimLost       = errors.New("square webhook processing claim was lost")
)

type WebhookRepository struct {
	db *sql.DB
}

type SquareWebhookTarget struct {
	SalonID     string
	OwnerUserID string
}

type SquareBookingWebhookEvent struct {
	ID                 string
	SalonID            string
	OwnerUserID        string
	EventID            string
	EventType          string
	MerchantID         string
	LocationID         string
	POSBookingID       string
	POSBookingVersion  int
	BookingStatus      string
	BookingStartAt     *time.Time
	DeliveredAt        *time.Time
	ProcessingAttempts int
	ProcessingToken    string
}

type SquareCalendarRepairTarget struct {
	SalonID     string
	OwnerUserID string
	LeaseToken  string
}

func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

func squareWebhookTargetConnectionStatuses() []string {
	return []string{
		pos.StatusConnected,
		pos.StatusSyncing,
		pos.StatusActive,
		pos.StatusError,
		pos.StatusExpiredToken,
	}
}

func (r *WebhookRepository) FindWebhookTarget(ctx context.Context, merchantID string, locationID string) (*SquareWebhookTarget, error) {
	merchantID = strings.TrimSpace(merchantID)
	locationID = strings.TrimSpace(locationID)
	if merchantID == "" || locationID == "" {
		return nil, ErrWebhookTargetNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT connection.salon_id::text, salon.owner_user_id::text
		FROM pos_connections connection
		JOIN salons salon ON salon.id = connection.salon_id
		WHERE connection.provider = $1
		  AND connection.merchant_id = $2
		  AND connection.location_id = $3
		  AND salon.active_pos_provider = $1
		  AND connection.status = ANY($4::text[])
		ORDER BY connection.salon_id
		LIMIT 2
	`, pos.ProviderSquare, merchantID, locationID, pq.Array(squareWebhookTargetConnectionStatuses()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]SquareWebhookTarget, 0, 2)
	for rows.Next() {
		var target SquareWebhookTarget
		if err := rows.Scan(&target.SalonID, &target.OwnerUserID); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uniqueWebhookTarget(targets)
}

func (r *WebhookRepository) EnqueueBookingWebhook(ctx context.Context, event SquareBookingWebhookEvent) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO square_booking_webhook_events (
			salon_id, event_id, event_type, merchant_id, location_id, pos_booking_id,
			pos_booking_version, booking_status, booking_start_at, delivered_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0), NULLIF($8, ''), $9, $10)
		ON CONFLICT (salon_id, event_id) DO NOTHING
	`, event.SalonID, event.EventID, event.EventType, event.MerchantID, event.LocationID,
		event.POSBookingID, event.POSBookingVersion, event.BookingStatus, event.BookingStartAt, event.DeliveredAt)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (r *WebhookRepository) ClaimBookingWebhooks(ctx context.Context, limit int) ([]SquareBookingWebhookEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH exhausted AS (
			UPDATE square_booking_webhook_events event
			SET processing_status = 'dead_letter',
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    dead_lettered_at = COALESCE(event.dead_lettered_at, now()),
			    last_error = NULL,
			    last_error_class = 'processing',
			    last_error_code = 'SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED',
			    updated_at = now()
			WHERE event.processing_attempts >= (event.requeue_count + 1) * $2
			  AND (
			      event.processing_status IN ('pending', 'failed')
			      OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
			  )
			RETURNING event.id
		), ranked AS (
			SELECT event.id,event.next_attempt_at,event.created_at,
			       row_number() OVER (
			           PARTITION BY event.salon_id
			           ORDER BY event.next_attempt_at,event.created_at,event.id
			       ) AS tenant_rank,
			       COALESCE(limits.worker_claims_per_batch,2) AS tenant_limit
			FROM square_booking_webhook_events event
			LEFT JOIN tenant_runtime_limits limits ON limits.salon_id=event.salon_id
			WHERE event.processing_attempts < (event.requeue_count + 1) * $2
			  AND (
			      (event.processing_status IN ('pending', 'failed') AND event.next_attempt_at <= now())
			      OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
			  )
		), candidates AS (
			SELECT event.id
			FROM square_booking_webhook_events event
			JOIN ranked ON ranked.id=event.id
			WHERE ranked.tenant_rank<=ranked.tenant_limit
			ORDER BY ranked.next_attempt_at,ranked.created_at,event.id
			FOR UPDATE OF event SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE square_booking_webhook_events event
			SET processing_status = 'processing',
			    processing_attempts = event.processing_attempts + 1,
			    processing_token = gen_random_uuid()::text,
			    processing_lease_expires_at = now() + interval '4 minutes',
			    dead_lettered_at = NULL,
			    last_error = NULL,
			    last_error_class = NULL,
			    last_error_code = NULL,
			    updated_at = now()
			FROM candidates
			WHERE event.id = candidates.id
			RETURNING event.id::text, event.salon_id::text, event.event_id, event.event_type,
			          event.merchant_id, event.location_id, event.pos_booking_id,
			          COALESCE(event.pos_booking_version, 0), COALESCE(event.booking_status, ''),
			          event.booking_start_at, event.delivered_at, event.processing_attempts,
			          event.processing_token
		)
		SELECT claimed.*, salon.owner_user_id::text
		FROM claimed
		JOIN salons salon ON salon.id::text = claimed.salon_id
		ORDER BY claimed.event_id
	`, limit, MaxWebhookAttemptsPerCycle)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SquareBookingWebhookEvent, 0)
	for rows.Next() {
		var item SquareBookingWebhookEvent
		if err := rows.Scan(
			&item.ID, &item.SalonID, &item.EventID, &item.EventType,
			&item.MerchantID, &item.LocationID, &item.POSBookingID,
			&item.POSBookingVersion, &item.BookingStatus, &item.BookingStartAt,
			&item.DeliveredAt, &item.ProcessingAttempts, &item.ProcessingToken, &item.OwnerUserID,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WebhookRepository) CompleteBookingWebhook(ctx context.Context, id string, processingToken string, processingAttempts int, processingErr error) error {
	id = strings.TrimSpace(id)
	processingToken = strings.TrimSpace(processingToken)
	if id == "" || processingToken == "" {
		return ErrWebhookClaimLost
	}
	var (
		result sql.Result
		err    error
	)
	if processingErr == nil {
		result, err = r.db.ExecContext(ctx, `
			UPDATE square_booking_webhook_events
			SET processing_status = 'succeeded', processed_at = now(),
			    processing_token = NULL, processing_lease_expires_at = NULL,
			    dead_lettered_at = NULL,
			    last_error = NULL, last_error_class = NULL, last_error_code = NULL,
			    updated_at = now()
			WHERE id = $1
			  AND processing_status = 'processing'
			  AND processing_token = $2
		`, id, processingToken)
	} else {
		delay := webhookRetryDelay(webhookAttemptInCycle(processingAttempts))
		errorClass, errorCode := webhookOperationalError(processingErr, "webhook")
		result, err = r.db.ExecContext(ctx, `
			UPDATE square_booking_webhook_events
			SET processing_status = CASE
			        WHEN processing_attempts >= (requeue_count + 1) * $6 THEN 'dead_letter'
			        ELSE 'failed'
			    END,
			    next_attempt_at = now() + $3::interval,
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    dead_lettered_at = CASE
			        WHEN processing_attempts >= (requeue_count + 1) * $6 THEN COALESCE(dead_lettered_at, now())
			        ELSE NULL
			    END,
			    last_error = NULL,
			    last_error_class = CASE
			        WHEN processing_attempts >= (requeue_count + 1) * $6 THEN 'processing'
			        ELSE $4
			    END,
			    last_error_code = CASE
			        WHEN processing_attempts >= (requeue_count + 1) * $6 THEN 'SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED'
			        ELSE $5
			    END,
			    updated_at = now()
			WHERE id = $1
			  AND processing_status = 'processing'
			  AND processing_token = $2
		`, id, processingToken, pqInterval(delay), errorClass, errorCode, MaxWebhookAttemptsPerCycle)
	}
	return webhookClaimUpdateResult(result, err)
}

func uniqueWebhookTarget(targets []SquareWebhookTarget) (*SquareWebhookTarget, error) {
	if len(targets) == 0 {
		return nil, ErrWebhookTargetNotFound
	}
	if len(targets) > 1 {
		return nil, ErrWebhookTargetAmbiguous
	}
	target := targets[0]
	return &target, nil
}

func webhookClaimUpdateResult(result sql.Result, updateErr error) error {
	if updateErr != nil {
		return updateErr
	}
	if result == nil {
		return ErrWebhookClaimLost
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrWebhookClaimLost
	}
	return nil
}

func webhookRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 15 * time.Second
	for index := 1; index < attempt && delay < 15*time.Minute; index++ {
		delay *= 2
	}
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func webhookAttemptInCycle(attempt int) int {
	if attempt < 1 {
		return 1
	}
	return ((attempt - 1) % MaxWebhookAttemptsPerCycle) + 1
}

func pqInterval(value time.Duration) string {
	return value.String()
}

func (r *WebhookRepository) ClaimCalendarRepairTargets(ctx context.Context, limit int) ([]SquareCalendarRepairTarget, error) {
	if limit <= 0 || limit > 20 {
		limit = 2
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO square_calendar_repair_state (salon_id)
		SELECT connection.salon_id
		FROM pos_connections connection
		JOIN salons salon ON salon.id = connection.salon_id
		WHERE connection.provider = $1
		  AND salon.active_pos_provider = $1
		  AND connection.status = 'active'
		  AND COALESCE(connection.merchant_id, '') <> ''
		  AND COALESCE(connection.location_id, '') <> ''
		ON CONFLICT (salon_id) DO NOTHING
	`, pos.ProviderSquare); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		WITH candidates AS (
			SELECT state.salon_id
			FROM square_calendar_repair_state state
			JOIN pos_connections connection
			  ON connection.salon_id = state.salon_id AND connection.provider = $1
			JOIN salons salon ON salon.id = connection.salon_id AND salon.active_pos_provider = $1
			WHERE state.next_repair_at <= now()
			  AND (state.lease_expires_at IS NULL OR state.lease_expires_at < now())
			  AND connection.status = 'active'
			ORDER BY state.next_repair_at, state.salon_id
			FOR UPDATE OF state SKIP LOCKED
			LIMIT $2
		), claimed AS (
			UPDATE square_calendar_repair_state state
			SET lease_expires_at = now() + interval '5 minutes',
			    lease_token = gen_random_uuid()::text,
			    next_repair_at = now() + interval '5 minutes',
			    repair_attempts = state.repair_attempts + 1,
			    updated_at = now()
			FROM candidates
			WHERE state.salon_id = candidates.salon_id
			RETURNING state.salon_id, state.lease_token
		)
		SELECT claimed.salon_id::text, salon.owner_user_id::text, claimed.lease_token
		FROM claimed
		JOIN salons salon ON salon.id = claimed.salon_id
		ORDER BY claimed.salon_id
	`, pos.ProviderSquare, limit)
	if err != nil {
		return nil, err
	}
	items := make([]SquareCalendarRepairTarget, 0)
	for rows.Next() {
		var item SquareCalendarRepairTarget
		if err := rows.Scan(&item.SalonID, &item.OwnerUserID, &item.LeaseToken); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *WebhookRepository) CompleteCalendarRepair(ctx context.Context, salonID string, leaseToken string, repairErr error) error {
	salonID = strings.TrimSpace(salonID)
	leaseToken = strings.TrimSpace(leaseToken)
	if salonID == "" || leaseToken == "" {
		return ErrWebhookClaimLost
	}
	var (
		result sql.Result
		err    error
	)
	if repairErr == nil {
		result, err = r.db.ExecContext(ctx, `
			UPDATE square_calendar_repair_state
			SET next_repair_at = now() + interval '6 hours',
			    lease_expires_at = NULL,
			    lease_token = NULL,
			    last_repaired_at = now(),
			    last_error = NULL, last_error_class = NULL, last_error_code = NULL,
			    updated_at = now()
			WHERE salon_id = $1
			  AND lease_token = $2
		`, salonID, leaseToken)
	} else {
		errorClass, errorCode := webhookOperationalError(repairErr, "calendar_repair")
		result, err = r.db.ExecContext(ctx, `
			UPDATE square_calendar_repair_state
			SET next_repair_at = now() + interval '15 minutes',
			    lease_expires_at = NULL,
			    lease_token = NULL,
			    last_error = NULL,
			    last_error_class = $3,
			    last_error_code = $4,
			    updated_at = now()
			WHERE salon_id = $1
			  AND lease_token = $2
		`, salonID, leaseToken, errorClass, errorCode)
	}
	return webhookClaimUpdateResult(result, err)
}
