package operationshealth

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrValidation   = errors.New("operations health validation failed")
	ErrNotFound     = errors.New("salon not found")
	ErrJobLeaseHeld = errors.New("worker job lease is held")
	ErrRunFenced    = errors.New("worker job run is fenced")
)

var (
	jobNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	errorClassPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	errorCodePattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db, now: time.Now}
}

func (r *Repository) StartRun(ctx context.Context, input StartRunInput) (string, error) {
	if !validStart(input) || r == nil || r.db == nil {
		return "", ErrValidation
	}
	now := r.now().UTC()
	runID := uuid.NewString()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var activeRunID sql.NullString
	var leaseExpiresAt sql.NullTime
	var lastStartedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT active_run_id::text, lease_expires_at, last_started_at
		FROM worker_job_heartbeats
		WHERE job_name = $1
		FOR UPDATE
	`, input.JobName).Scan(&activeRunID, &leaseExpiresAt, &lastStartedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO worker_job_heartbeats (
				job_name, current_worker_instance_id, active_run_id, last_status,
				interval_seconds, stale_after_seconds, last_started_at,
				heartbeat_at, lease_expires_at, created_at, updated_at
			) VALUES ($1,$2,$3,'running',$4,$5,$6,$6,$7,$6,$6)
		`, input.JobName, input.WorkerInstanceID, runID, durationSeconds(input.Interval),
			durationSeconds(input.StaleAfter), now, now.Add(input.LeaseDuration))
		if err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	default:
		if activeRunID.Valid && leaseExpiresAt.Valid && leaseExpiresAt.Time.After(now) {
			return "", ErrJobLeaseHeld
		}
		if !activeRunID.Valid && lastStartedAt.Add(input.Interval).After(now) {
			return "", ErrJobLeaseHeld
		}
		if activeRunID.Valid {
			duration := now.Sub(now)
			var startedAt time.Time
			if scanErr := tx.QueryRowContext(ctx, `SELECT started_at FROM worker_job_runs WHERE id=$1 AND job_name=$2`, activeRunID.String, input.JobName).Scan(&startedAt); scanErr == nil {
				duration = now.Sub(startedAt)
			}
			if duration < 0 {
				duration = 0
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE worker_job_runs
				SET status='abandoned', heartbeat_at=$3, completed_at=$3,
				    duration_ms=$4, processed_count=0,
				    error_class='worker', error_code='JOB_LEASE_EXPIRED'
				WHERE id=$1 AND job_name=$2 AND status='running'
			`, activeRunID.String, input.JobName, now, boundedDurationMillis(duration))
			if err != nil {
				return "", err
			}
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE worker_job_heartbeats
			SET current_worker_instance_id=$2, active_run_id=$3, last_status='running',
			    interval_seconds=$4, stale_after_seconds=$5, last_started_at=$6,
			    last_error_class=NULL, last_error_code=NULL,
			    heartbeat_at=$6, lease_expires_at=$7, updated_at=$6
			WHERE job_name=$1
		`, input.JobName, input.WorkerInstanceID, runID, durationSeconds(input.Interval),
			durationSeconds(input.StaleAfter), now, now.Add(input.LeaseDuration))
		if err != nil {
			return "", err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO worker_job_runs (
			id, job_name, worker_instance_id, status, started_at, heartbeat_at, created_at
		) VALUES ($1,$2,$3,'running',$4,$4,$4)
	`, runID, input.JobName, input.WorkerInstanceID, now)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return runID, nil
}

func (r *Repository) HeartbeatRun(ctx context.Context, jobName, runID, workerInstanceID string, leaseDuration time.Duration) error {
	if !validRunIdentity(jobName, runID, workerInstanceID) || leaseDuration < time.Second || leaseDuration > 30*24*time.Hour {
		return ErrValidation
	}
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE worker_job_runs
		SET heartbeat_at=$4
		WHERE id=$1 AND job_name=$2 AND worker_instance_id=$3 AND status='running'
	`, runID, jobName, workerInstanceID, now)
	if err != nil {
		return err
	}
	if !oneRow(result) {
		return ErrRunFenced
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE worker_job_heartbeats
		SET heartbeat_at=$4, lease_expires_at=$5, updated_at=$4
		WHERE job_name=$1 AND active_run_id=$2 AND current_worker_instance_id=$3 AND last_status='running'
	`, jobName, runID, workerInstanceID, now, now.Add(leaseDuration))
	if err != nil {
		return err
	}
	if !oneRow(result) {
		return ErrRunFenced
	}
	return tx.Commit()
}

func (r *Repository) FinishRun(ctx context.Context, input FinishRunInput) error {
	if !validFinish(input) || r == nil || r.db == nil {
		return ErrValidation
	}
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var startedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT started_at FROM worker_job_runs
		WHERE id=$1 AND job_name=$2 AND worker_instance_id=$3 AND status='running'
		FOR UPDATE
	`, input.RunID, input.JobName, input.WorkerInstanceID).Scan(&startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRunFenced
	}
	if err != nil {
		return err
	}
	durationMS := boundedDurationMillis(now.Sub(startedAt))
	result, err := tx.ExecContext(ctx, `
		UPDATE worker_job_runs
		SET status=$4, heartbeat_at=$5, completed_at=$5, duration_ms=$6,
		    processed_count=$7, error_class=NULLIF($8,''), error_code=NULLIF($9,'')
		WHERE id=$1 AND job_name=$2 AND worker_instance_id=$3 AND status='running'
	`, input.RunID, input.JobName, input.WorkerInstanceID, input.Status, now,
		durationMS, input.ProcessedCount, input.ErrorClass, input.ErrorCode)
	if err != nil {
		return err
	}
	if !oneRow(result) {
		return ErrRunFenced
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE worker_job_heartbeats
		SET active_run_id=NULL, last_status=$4, last_completed_at=$5,
		    last_success_at=CASE WHEN $4='succeeded' THEN $5 ELSE last_success_at END,
		    last_duration_ms=$6, last_processed_count=$7,
		    last_error_class=NULLIF($8,''), last_error_code=NULLIF($9,''),
		    heartbeat_at=$5, lease_expires_at=NULL, updated_at=$5
		WHERE job_name=$1 AND active_run_id=$2 AND current_worker_instance_id=$3 AND last_status='running'
	`, input.JobName, input.RunID, input.WorkerInstanceID, input.Status, now,
		durationMS, input.ProcessedCount, input.ErrorClass, input.ErrorCode)
	if err != nil {
		return err
	}
	if !oneRow(result) {
		return ErrRunFenced
	}
	return tx.Commit()
}

func (r *Repository) LoadStatus(ctx context.Context, salonID, ownerUserID string) ([]jobRecord, []queueRecord, error) {
	if !validUUID(strings.TrimSpace(salonID)) || !validUUID(strings.TrimSpace(ownerUserID)) || r == nil || r.db == nil {
		return nil, nil, ErrValidation
	}
	var owned bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1 AND owner_user_id=$2)`, salonID, ownerUserID).Scan(&owned); err != nil {
		return nil, nil, err
	}
	if !owned {
		return nil, nil, ErrNotFound
	}

	jobs, err := r.loadJobs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return jobs, r.loadQueues(ctx, salonID), nil
}

func (r *Repository) LoadStatusForSalon(ctx context.Context, salonID string) ([]jobRecord, []queueRecord, string, error) {
	if !validUUID(strings.TrimSpace(salonID)) || r == nil || r.db == nil {
		return nil, nil, "", ErrValidation
	}
	var ownerUserID string
	if err := r.db.QueryRowContext(ctx, `SELECT owner_user_id::text FROM salons WHERE id=$1`, salonID).Scan(&ownerUserID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, "", ErrNotFound
	} else if err != nil {
		return nil, nil, "", err
	}
	jobs, err := r.loadJobs(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	return jobs, r.loadQueues(ctx, salonID), ownerUserID, nil
}

func (r *Repository) loadJobs(ctx context.Context) ([]jobRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT job_name, last_status, stale_after_seconds, last_started_at,
		       last_completed_at, last_success_at, last_duration_ms,
		       last_processed_count, COALESCE(last_error_class,''),
		       COALESCE(last_error_code,''), heartbeat_at, lease_expires_at
		FROM worker_job_heartbeats
		ORDER BY job_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]jobRecord, 0)
	for rows.Next() {
		var item jobRecord
		var completed, success, lease sql.NullTime
		var duration sql.NullInt64
		var processed sql.NullInt64
		if err := rows.Scan(&item.Name, &item.Status, &item.StaleAfterSeconds, &item.LastStartedAt,
			&completed, &success, &duration, &processed, &item.ErrorClass, &item.ErrorCode,
			&item.HeartbeatAt, &lease); err != nil {
			return nil, err
		}
		item.LastCompletedAt = nullableTime(completed)
		item.LastSuccessAt = nullableTime(success)
		item.LeaseExpiresAt = nullableTime(lease)
		if duration.Valid {
			value := duration.Int64
			item.LastDurationMS = &value
		}
		if processed.Valid {
			value := int(processed.Int64)
			item.ProcessedCount = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) loadQueues(ctx context.Context, salonID string) []queueRecord {
	items := []queueRecord{
		r.notificationQueue(ctx, salonID),
		r.customerNotificationQueue(ctx, salonID),
		r.metric(ctx, salonID, "scheduling_requests", `
			SELECT count(*), min(created_at), 0
			FROM scheduling_requests WHERE salon_id=$1 AND status IN ('pending','contacted')`),
		r.metric(ctx, salonID, "openai_runtime_verification", `
			SELECT count(*), min(created_at), 0
			FROM openai_runtime_verification_runs
			WHERE salon_id=$1 AND status IN ('queued','claimed')`),
		r.metric(ctx, salonID, "availability_quote_cleanup", `
			SELECT count(*), min(COALESCE(consumed_at, expires_at)), 0
			FROM availability_quotes quote
			WHERE quote.salon_id=$1 AND quote.consumed_by_attempt_id IS NULL
			  AND NOT EXISTS (SELECT 1 FROM booking_attempts attempt WHERE attempt.availability_quote_id=quote.id)
			  AND ((quote.consumed_at IS NULL AND quote.expires_at <= now()-interval '24 hours')
			    OR (quote.consumed_at IS NOT NULL AND quote.consumed_at <= now()-interval '30 days'))`),
		r.metric(ctx, salonID, "external_slot_claims_pre_dispatch", `
			SELECT count(*), min(created_at), 0
			FROM external_slot_claims
			WHERE salon_id=$1 AND released_at IS NULL AND state='claimed_pre_dispatch'`),
		r.metric(ctx, salonID, "external_slot_claims_unknown", `
			SELECT count(*), min(COALESCE(dispatch_started_at,created_at)), 0
			FROM external_slot_claims
			WHERE salon_id=$1 AND released_at IS NULL
			  AND state IN ('dispatched_unknown','reconciliation_required')`),
		r.metric(ctx, salonID, "conversation_retention", `
			SELECT count(*), min(retention_expires_at), 0
			FROM call_sessions WHERE salon_id=$1 AND lifecycle_status<>'redacted' AND retention_expires_at<=now()`),
		r.metric(ctx, salonID, "scheduling_pii_retention", `
			WITH due AS (
				SELECT retention_expires_at AS due_at
				FROM scheduling_requests request
				WHERE request.salon_id=$1 AND request.redacted_at IS NULL
				  AND request.status IN ('resolved','dismissed')
				  AND request.retention_expires_at<=now()
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
				UNION ALL
				SELECT retention_expires_at FROM owner_notifications
				WHERE salon_id=$1 AND redacted_at IS NULL AND retention_expires_at<=now()
				UNION ALL
				SELECT retention_expires_at FROM customer_notification_deliveries
				WHERE salon_id=$1 AND redacted_at IS NULL AND retention_expires_at<=now()
				UNION ALL
				SELECT expires_at FROM voice_audio_outputs
				WHERE salon_id=$1 AND redacted_at IS NULL AND expires_at<=now()
			)
			SELECT count(*), min(due_at), 0 FROM due`),
	}
	return items
}

func (r *Repository) notificationQueue(ctx context.Context, salonID string) queueRecord {
	var available bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema=current_schema() AND table_name='owner_notifications' AND column_name='dead_lettered_at'
		)
	`).Scan(&available)
	if err != nil || !available {
		return queueRecord{Key: "notification_delivery", ErrorCode: "NOTIFICATION_METRICS_UNAVAILABLE"}
	}
	return r.metric(ctx, salonID, "notification_delivery", `
		SELECT count(*) FILTER (WHERE (delivery_status IN ('queued','failed') AND next_delivery_at<=now())
		                              OR (delivery_status='delivering' AND delivery_lease_expires_at<=now())),
		       min(created_at) FILTER (WHERE (delivery_status IN ('queued','failed') AND next_delivery_at<=now())
		                                 OR (delivery_status='delivering' AND delivery_lease_expires_at<=now())),
		       count(*) FILTER (WHERE delivery_status='dead_letter')
		FROM owner_notifications WHERE salon_id=$1`)
}

func (r *Repository) customerNotificationQueue(ctx context.Context, salonID string) queueRecord {
	var available bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema=current_schema()
			  AND table_name='customer_notification_deliveries'
			  AND column_name='dead_lettered_at'
		)
	`).Scan(&available)
	if err != nil || !available {
		return queueRecord{Key: "customer_notifications", ErrorCode: "CUSTOMER_NOTIFICATION_METRICS_UNAVAILABLE"}
	}
	return r.metric(ctx, salonID, "customer_notifications", `
		SELECT count(*) FILTER (
		         WHERE (delivery_status IN ('queued','quiet_hours','failed') AND next_delivery_at<=now())
		            OR (delivery_status='delivering' AND delivery_lease_expires_at<=now())
		       ),
		       min(created_at) FILTER (
		         WHERE (delivery_status IN ('queued','quiet_hours','failed') AND next_delivery_at<=now())
		            OR (delivery_status='delivering' AND delivery_lease_expires_at<=now())
		       ),
		       count(*) FILTER (WHERE delivery_status='dead_letter')
		FROM customer_notification_deliveries WHERE salon_id=$1`)
}

func (r *Repository) metric(ctx context.Context, salonID, key, query string) queueRecord {
	item := queueRecord{Key: key}
	var oldest sql.NullTime
	if err := r.db.QueryRowContext(ctx, query, salonID).Scan(&item.BacklogCount, &oldest, &item.DeadLetterCount); err != nil {
		item.ErrorCode = "QUEUE_METRICS_UNAVAILABLE"
		return item
	}
	item.Available = true
	item.OldestAt = nullableTime(oldest)
	return item
}

func validStart(input StartRunInput) bool {
	return jobNamePattern.MatchString(input.JobName) && validUUID(input.WorkerInstanceID) &&
		input.Interval >= time.Second && input.Interval <= 7*24*time.Hour &&
		input.StaleAfter >= input.Interval && input.StaleAfter <= 30*24*time.Hour &&
		input.LeaseDuration >= time.Second && input.LeaseDuration <= 30*24*time.Hour
}

func validFinish(input FinishRunInput) bool {
	if !validRunIdentity(input.JobName, input.RunID, input.WorkerInstanceID) || input.ProcessedCount < 0 || input.ProcessedCount > MaxProcessedCount {
		return false
	}
	switch input.Status {
	case RunStatusSucceeded:
		return input.ErrorClass == "" && input.ErrorCode == ""
	case RunStatusFailed, RunStatusCancelled, RunStatusPanicked, RunStatusAbandoned:
		return errorClassPattern.MatchString(input.ErrorClass) && errorCodePattern.MatchString(input.ErrorCode)
	default:
		return false
	}
}

func validRunIdentity(jobName, runID, workerInstanceID string) bool {
	return jobNamePattern.MatchString(jobName) && validUUID(runID) && validUUID(workerInstanceID)
}

func validUUID(value string) bool             { _, err := uuid.Parse(value); return err == nil }
func durationSeconds(value time.Duration) int { return int(value / time.Second) }
func boundedDurationMillis(value time.Duration) int64 {
	if value < 0 {
		return 0
	}
	const max = 7 * 24 * time.Hour
	if value > max {
		value = max
	}
	return value.Milliseconds()
}
func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	item := value.Time.UTC()
	return &item
}
func oneRow(result sql.Result) bool {
	count, err := result.RowsAffected()
	return err == nil && count == 1
}
