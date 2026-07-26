package pos_square

import (
	"context"
	"database/sql"
	"time"

	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
)

// LoadTenantQueueMetrics is the provider-owned boundary used by generic
// operations health. Only bounded salon aggregates cross the boundary; Square
// identifiers, payloads, tokens, raw errors, and scheduling internals do not.
func (r *WebhookRepository) LoadTenantQueueMetrics(ctx context.Context, salonID, ownerUserID string) (operationshealth.TenantMetricSnapshot, error) {
	if r == nil || r.db == nil {
		return operationshealth.TenantMetricSnapshot{}, operationshealth.ErrValidation
	}
	var owned bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1 AND owner_user_id=$2)`, salonID, ownerUserID).Scan(&owned); err != nil {
		return operationshealth.TenantMetricSnapshot{}, err
	}
	if !owned {
		return operationshealth.TenantMetricSnapshot{}, operationshealth.ErrNotFound
	}
	var relevant bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM pos_connections WHERE salon_id=$1 AND provider='square')
		    OR EXISTS(SELECT 1 FROM square_booking_webhook_events WHERE salon_id=$1)
		    OR EXISTS(SELECT 1 FROM square_calendar_repair_state WHERE salon_id=$1)
	`, salonID).Scan(&relevant); err != nil {
		return operationshealth.TenantMetricSnapshot{}, err
	}
	snapshot := operationshealth.TenantMetricSnapshot{Relevant: relevant, Queues: []operationshealth.TenantQueueMetric{}}
	if !relevant {
		return snapshot, nil
	}
	snapshot.Queues = append(snapshot.Queues,
		r.safeTenantQueueMetric(ctx, salonID, "pos_sync_jobs", `
			SELECT count(*) FILTER (WHERE status IN ('queued','failed') AND next_attempt_at<=now()),
			       min(next_attempt_at) FILTER (WHERE status IN ('queued','failed') AND next_attempt_at<=now()),
			       count(*) FILTER (WHERE status='failed' AND attempt_count>=max_attempts)
			FROM pos_sync_jobs WHERE salon_id=$1 AND provider='square'`),
		r.safeTenantQueueMetric(ctx, salonID, "booking_lease_recovery", `
			SELECT count(*), min(processing_lease_expires_at), 0
			FROM booking_attempts
			WHERE salon_id=$1 AND scheduling_authority='external_provider' AND status='pos_pending'
			  AND provider_outcome IN ('not_started','in_flight') AND superseded_at IS NULL
			  AND processing_lease_expires_at IS NOT NULL AND processing_lease_expires_at<=now()`),
		r.safeTenantQueueMetric(ctx, salonID, "square_booking_webhooks", `
			SELECT count(*) FILTER (WHERE (processing_status IN ('pending','failed') AND next_attempt_at<=now())
			                              OR (processing_status='processing' AND processing_lease_expires_at<=now())),
			       min(CASE WHEN processing_status='processing' THEN processing_lease_expires_at ELSE next_attempt_at END)
			           FILTER (WHERE (processing_status IN ('pending','failed') AND next_attempt_at<=now())
			                    OR (processing_status='processing' AND processing_lease_expires_at<=now())),
			       count(*) FILTER (WHERE processing_status='dead_letter')
			FROM square_booking_webhook_events WHERE salon_id=$1`),
		r.safeTenantQueueMetric(ctx, salonID, "square_calendar_repair", `
			SELECT count(*) FILTER (WHERE next_repair_at<=now()
			                              OR (lease_expires_at IS NOT NULL AND lease_expires_at<=now())),
			       min(CASE WHEN lease_expires_at IS NOT NULL AND lease_expires_at<=now()
			                THEN lease_expires_at ELSE next_repair_at END)
			           FILTER (WHERE next_repair_at<=now()
			                    OR (lease_expires_at IS NOT NULL AND lease_expires_at<=now())),
			       0
			FROM square_calendar_repair_state WHERE salon_id=$1`),
	)
	return snapshot, nil
}

func (r *WebhookRepository) safeTenantQueueMetric(ctx context.Context, salonID, key, query string) operationshealth.TenantQueueMetric {
	metric := operationshealth.TenantQueueMetric{Key: key}
	var oldest sql.NullTime
	if err := r.db.QueryRowContext(ctx, query, salonID).Scan(&metric.BacklogCount, &oldest, &metric.DeadLetterCount); err != nil {
		metric.ErrorCode = "QUEUE_METRICS_UNAVAILABLE"
		return metric
	}
	metric.Available = true
	metric.OldestAt = squareMetricNullableTime(oldest)
	return metric
}

func squareMetricNullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	item := value.Time.UTC()
	return &item
}

var _ operationshealth.TenantMetricSource = (*WebhookRepository)(nil)
