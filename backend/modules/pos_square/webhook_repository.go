package pos_square

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
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
		SELECT salon_id::text
		FROM public.app_provider_square_webhook_targets($1,$2,$3,$4::text[])
	`, pos.ProviderSquare, merchantID, locationID, pq.Array(squareWebhookTargetConnectionStatuses()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]SquareWebhookTarget, 0, 2)
	for rows.Next() {
		var target SquareWebhookTarget
		if err := rows.Scan(&target.SalonID); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	target, err := uniqueWebhookTarget(targets)
	if err != nil {
		return nil, err
	}
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, target.SalonID)
	if err := r.db.QueryRowContext(boundCtx, `SELECT owner_user_id::text FROM salons WHERE id=$1`, target.SalonID).Scan(&target.OwnerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookTargetNotFound
		}
		return nil, err
	}
	return target, nil
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
	discoveryCtx := databasecontext.WithScope(ctx, databasecontext.ScopeWorker)
	rows, err := r.db.QueryContext(discoveryCtx, `
		SELECT webhook_id::text, salon_id::text, event_id, event_type,
		       merchant_id, location_id, pos_booking_id, pos_booking_version,
		       booking_status, booking_start_at, delivered_at, processing_attempts,
		       processing_token, owner_user_id::text
		FROM public.app_worker_claim_square_booking_webhooks($1, $2)
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
	discoveryCtx := databasecontext.WithScope(ctx, databasecontext.ScopeWorker)
	rows, err := r.db.QueryContext(discoveryCtx, `
		SELECT salon_id::text, owner_user_id::text, lease_token
		FROM public.app_worker_claim_square_calendar_repairs($1, $2)
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
