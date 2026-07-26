package tenantruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/lib/pq"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) GetProfile(ctx context.Context, salonID, actorUserID string, windowMinutes int) (*RuntimeProfile, error) {
	limits, err := r.getLimits(ctx, r.db, salonID, actorUserID, "operations.read")
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT metric,COALESCE(sum(used_count),0),COALESCE(sum(rejected_count),0)
		FROM tenant_usage_minute_buckets
		WHERE salon_id=$1 AND bucket_start>=date_trunc('minute',now())-($2*interval '1 minute')
		GROUP BY metric ORDER BY metric
	`, salonID, windowMinutes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usageByMetric := map[string]UsageMetric{}
	for rows.Next() {
		var item UsageMetric
		if err := rows.Scan(&item.Metric, &item.Used, &item.Rejected); err != nil {
			return nil, err
		}
		usageByMetric[item.Metric] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	limitsByMetric := map[string]int{
		MetricExpensiveRequest: limits.ExpensiveRequestsPerMinute,
		MetricSchedulingWrite:  limits.SchedulingWritesPerMinute,
		MetricProviderWrite:    limits.ProviderWritesPerMinute,
		MetricVoiceStart:       limits.VoiceStartsPerMinute,
	}
	metrics := make([]string, 0, len(limitsByMetric))
	for metric := range limitsByMetric {
		metrics = append(metrics, metric)
	}
	sort.Strings(metrics)
	usage := make([]UsageMetric, 0, len(metrics))
	for _, metric := range metrics {
		item := usageByMetric[metric]
		item.Metric = metric
		item.Limit = limitsByMetric[metric]
		usage = append(usage, item)
	}
	return &RuntimeProfile{Limits: *limits, WindowMinutes: windowMinutes, Usage: usage, EvaluatedAt: time.Now().UTC()}, nil
}

func (r *Repository) UpdateLimits(ctx context.Context, salonID, actorUserID, fingerprint string, req UpdateLimitsRequest, changedFields []string) (*Limits, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var existingFingerprint string
	var existingSnapshot []byte
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint,result_snapshot
		FROM tenant_runtime_limit_actions
		WHERE salon_id=$1 AND action_key=$2
	`, salonID, req.ActionKey).Scan(&existingFingerprint, &existingSnapshot)
	if err == nil {
		if existingFingerprint != fingerprint {
			return nil, false, ErrActionConflict
		}
		var replay Limits
		if err := json.Unmarshal(existingSnapshot, &replay); err != nil {
			return nil, false, err
		}
		return &replay, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	current, err := r.getLimits(ctx, tx, salonID, actorUserID, "operations.write")
	if err != nil {
		return nil, false, err
	}
	if current.Version != req.ExpectedVersion {
		return nil, false, ErrVersionConflict
	}
	var result Limits
	err = tx.QueryRowContext(ctx, `
		UPDATE tenant_runtime_limits
		SET expensive_requests_per_minute=$3,
		    scheduling_writes_per_minute=$4,
		    provider_writes_per_minute=$5,
		    voice_starts_per_minute=$6,
		    worker_claims_per_batch=$7,
		    version=version+1,updated_by_user_id=$2,updated_at=now()
		WHERE salon_id=$1 AND version=$8
		RETURNING salon_id::text,expensive_requests_per_minute,scheduling_writes_per_minute,
		          provider_writes_per_minute,voice_starts_per_minute,worker_claims_per_batch,
		          version,updated_at
	`, salonID, actorUserID, req.ExpensiveRequestsPerMinute, req.SchedulingWritesPerMinute,
		req.ProviderWritesPerMinute, req.VoiceStartsPerMinute, req.WorkerClaimsPerBatch,
		req.ExpectedVersion).Scan(&result.SalonID, &result.ExpensiveRequestsPerMinute,
		&result.SchedulingWritesPerMinute, &result.ProviderWritesPerMinute,
		&result.VoiceStartsPerMinute, &result.WorkerClaimsPerBatch, &result.Version, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrVersionConflict
	}
	if err != nil {
		return nil, false, err
	}
	snapshot, err := json.Marshal(result)
	if err != nil {
		return nil, false, err
	}
	var actionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO tenant_runtime_limit_actions (
			salon_id,actor_user_id,action_key,request_fingerprint,result_snapshot
		) VALUES ($1,$2,$3,$4,$5::jsonb)
		RETURNING id::text
	`, salonID, actorUserID, req.ActionKey, fingerprint, string(snapshot)).Scan(&actionID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenant_runtime_limit_events (
			salon_id,action_id,actor_user_id,previous_version,result_version,changed_fields
		) VALUES ($1,$2,$3,$4,$5,$6)
	`, salonID, actionID, actorUserID, current.Version, result.Version, pq.Array(changedFields)); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &result, false, nil
}

type limitsQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Repository) getLimits(ctx context.Context, queryer limitsQueryer, salonID, actorUserID, capability string) (*Limits, error) {
	var result Limits
	err := queryer.QueryRowContext(ctx, `
		SELECT limits.salon_id::text,limits.expensive_requests_per_minute,
		       limits.scheduling_writes_per_minute,limits.provider_writes_per_minute,
		       limits.voice_starts_per_minute,limits.worker_claims_per_batch,
		       limits.version,limits.updated_at
		FROM tenant_runtime_limits limits
		WHERE limits.salon_id=$1
		  AND public.has_platform_salon_capability(limits.salon_id,$2::uuid,$3)
	`, salonID, actorUserID, capability).Scan(&result.SalonID, &result.ExpensiveRequestsPerMinute,
		&result.SchedulingWritesPerMinute, &result.ProviderWritesPerMinute,
		&result.VoiceStartsPerMinute, &result.WorkerClaimsPerBatch, &result.Version, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &result, err
}

func (r *Repository) Consume(ctx context.Context, salonID, metric string, units int) (Decision, error) {
	decision := Decision{Metric: metric}
	err := r.db.QueryRowContext(ctx, `
		SELECT allowed,quota_limit,used_count,remaining_count,rejected_count,reset_at
		FROM consume_tenant_runtime_quota($1::uuid,$2,$3)
	`, salonID, metric, units).Scan(&decision.Allowed, &decision.Limit, &decision.Used,
		&decision.Remaining, &decision.Rejected, &decision.ResetAt)
	if err != nil {
		return Decision{}, err
	}
	decision.RetryAfterSec = int(time.Until(decision.ResetAt).Seconds()) + 1
	if decision.RetryAfterSec < 1 {
		decision.RetryAfterSec = 1
	}
	return decision, nil
}

func LimitsFingerprint(req UpdateLimitsRequest) string {
	payload, _ := json.Marshal(struct {
		ExpectedVersion            int64 `json:"expected_version"`
		ExpensiveRequestsPerMinute int   `json:"expensive_requests_per_minute"`
		SchedulingWritesPerMinute  int   `json:"scheduling_writes_per_minute"`
		ProviderWritesPerMinute    int   `json:"provider_writes_per_minute"`
		VoiceStartsPerMinute       int   `json:"voice_starts_per_minute"`
		WorkerClaimsPerBatch       int   `json:"worker_claims_per_batch"`
	}{req.ExpectedVersion, req.ExpensiveRequestsPerMinute, req.SchedulingWritesPerMinute,
		req.ProviderWritesPerMinute, req.VoiceStartsPerMinute, req.WorkerClaimsPerBatch})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
