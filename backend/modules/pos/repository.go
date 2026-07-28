package pos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

var (
	ErrNotFound                 = errors.New("pos record not found")
	ErrStaleProviderSnapshot    = errors.New("provider snapshot is stale")
	ErrStaleProviderFence       = errors.New("provider catalog fence is stale")
	ErrTechnicalVersionConflict = errors.New("technical resource version conflict")
	ErrTechnicalActionConflict  = errors.New("technical action conflict")
)

const (
	bookingCalendarReconciliationLockPrefix = fence.AdvisoryKeyPrefix
	serviceAliasOwnershipConstraint         = "service_alias_cross_table_active_unique"
	schedulingAuthorityOwnerManual          = "owner_manual"
	schedulingAuthorityManleAICalendar      = "manleai_calendar"
	schedulingAuthorityExternalProvider     = "external_provider"
)

type Repository struct {
	db *sql.DB
}

type serviceCategorySuggestionCandidate struct {
	ServiceID         string
	ServiceName       string
	CurrentCategoryID string
	Source            string
}

type serviceTaxonomyAliasRecord struct {
	Alias      string
	Normalized string
	Confidence float64
}

type serviceTaxonomyCategoryRecord struct {
	Name        string
	Slug        string
	Description string
	SortOrder   int
	Confidence  float64
	Aliases     []serviceTaxonomyAliasRecord
}

type serviceTaxonomyConceptRecord struct {
	CategorySlug   string
	CanonicalName  string
	NormalizedName string
	Confidence     float64
	Aliases        []serviceTaxonomyAliasRecord
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func lockSchedulingMutationFenceTx(ctx context.Context, tx *sql.Tx, salonID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID))
	return err
}

func lockServiceAliasOwnershipTx(ctx context.Context, tx *sql.Tx, salonID string, normalizedAlias string) error {
	_, err := tx.ExecContext(ctx, `SELECT public.lock_service_alias_ownership($1::uuid, $2)`, salonID, normalizedAlias)
	return err
}

func isServiceAliasOwnershipConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Constraint == serviceAliasOwnershipConstraint
}

// WithSchedulingFenceTx gives a provider-owned readiness evaluator a coherent
// database snapshot under the same salon fence used by authority-switch commit.
func (r *Repository) WithSchedulingFenceTx(ctx context.Context, salonID string, evaluate func(*sql.Tx) error) error {
	if strings.TrimSpace(salonID) == "" || evaluate == nil {
		return ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return err
	}
	if err := evaluate(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM salons salon
			WHERE salon.id = $1
			  AND (
			      public.has_active_tenant_membership(salon.id, $2::uuid)
			      OR public.app_active_support_authorization($2::uuid, salon.id, 'services.read')
			      OR public.app_active_support_authorization($2::uuid, salon.id, 'training.read')
			      OR public.app_active_support_authorization($2::uuid, salon.id, 'calls.read')
			  )
		)
	`, salonID, ownerUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) EnsureSalonExists(ctx context.Context, salonID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1)`, salonID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SalonOwnerUserID(ctx context.Context, salonID string) (string, error) {
	var ownerUserID string
	err := r.db.QueryRowContext(ctx, `SELECT owner_user_id::text FROM salons WHERE id=$1`, salonID).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return ownerUserID, err
}

func (r *Repository) GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return "", err
	}
	var provider string
	err := r.db.QueryRowContext(ctx, `
		SELECT active_pos_provider
		FROM salons
		WHERE id = $1
	`, salonID).Scan(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(provider) == "" {
		return ProviderSquare, nil
	}
	return provider, nil
}

func (r *Repository) GetSchedulingAuthorityVersion(ctx context.Context, salonID string, ownerUserID string) (int64, error) {
	var version int64
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority_version
		FROM salon_settings settings
		JOIN salons salon ON salon.id = settings.salon_id
		WHERE settings.salon_id = $1
		  AND salon.owner_user_id = $2
	`, salonID, ownerUserID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return version, err
}

func (r *Repository) GetSalonAIEnabled(ctx context.Context, salonID string, ownerUserID string) (bool, error) {
	var enabled bool
	err := r.db.QueryRowContext(ctx, `
		SELECT ai_enabled
		FROM salons
		WHERE id = $1 AND owner_user_id = $2
	`, salonID, ownerUserID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return enabled, err
}

// GetSalonAIRuntimeForPlatform is intentionally owner-independent. It is used
// only after the fixed Platform route has authorized the actual actor; the
// request-scoped database actor and RLS remain a second boundary.
func (r *Repository) GetSalonAIRuntimeForPlatform(ctx context.Context, salonID string) (AIRuntimeState, error) {
	var state AIRuntimeState
	err := r.db.QueryRowContext(ctx, `
		SELECT salon.ai_enabled, COALESCE(version.version, 0)
		FROM salons salon
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=salon.id
		 AND version.resource_type='ai_runtime'
		 AND version.resource_id='ai_booking'
		WHERE salon.id=$1
	`, salonID).Scan(&state.Enabled, &state.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return AIRuntimeState{}, ErrNotFound
	}
	return state, err
}

// GetSchedulingAuthorityForPlatform reads the current technical selection
// without resolving or substituting the salon owner's user ID.
func (r *Repository) GetSchedulingAuthorityForPlatform(ctx context.Context, salonID string) (string, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority
		FROM salon_settings settings
		WHERE settings.salon_id=$1
	`, salonID).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return authority, err
}

func (r *Repository) SetSalonAIRuntimeForPlatform(ctx context.Context, input AIRuntimeMutation) (AIRuntimeState, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIRuntimeState{}, false, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_resource_versions (salon_id,resource_type,resource_id,version)
		SELECT id,'ai_runtime','ai_booking',0 FROM salons WHERE id=$1
		ON CONFLICT DO NOTHING
	`, input.SalonID); err != nil {
		return AIRuntimeState{}, false, err
	}

	var existingFingerprint, existingActionType, existingResourceType, existingResourceID string
	var existingVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint,action_type,resource_type,resource_id,result_version
		FROM technical_actions
		WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3
	`, input.SalonID, input.ActorUserID, input.ActionKey).Scan(
		&existingFingerprint, &existingActionType, &existingResourceType, &existingResourceID, &existingVersion,
	)
	if err == nil {
		if existingFingerprint != input.RequestFingerprint || existingResourceType != "ai_runtime" || existingResourceID != "ai_booking" {
			return AIRuntimeState{}, false, ErrTechnicalActionConflict
		}
		state := AIRuntimeState{Enabled: existingActionType == "ai_booking.enable", Version: existingVersion}
		if err := tx.Commit(); err != nil {
			return AIRuntimeState{}, false, err
		}
		return state, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AIRuntimeState{}, false, err
	}

	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT version
		FROM technical_resource_versions
		WHERE salon_id=$1 AND resource_type='ai_runtime' AND resource_id='ai_booking'
		FOR UPDATE
	`, input.SalonID).Scan(&currentVersion); errors.Is(err, sql.ErrNoRows) {
		return AIRuntimeState{}, false, ErrNotFound
	} else if err != nil {
		return AIRuntimeState{}, false, err
	}
	if currentVersion != input.ExpectedVersion {
		return AIRuntimeState{}, false, ErrTechnicalVersionConflict
	}

	result, err := tx.ExecContext(ctx, `UPDATE salons SET ai_enabled=$1,updated_at=now() WHERE id=$2`, input.Enabled, input.SalonID)
	if err != nil {
		return AIRuntimeState{}, false, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return AIRuntimeState{}, false, err
	} else if affected != 1 {
		return AIRuntimeState{}, false, ErrNotFound
	}
	resultVersion := currentVersion + 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE technical_resource_versions
		SET version=$2,updated_by_user_id=$3,updated_at=now()
		WHERE salon_id=$1 AND resource_type='ai_runtime' AND resource_id='ai_booking'
	`, input.SalonID, resultVersion, input.ActorUserID); err != nil {
		return AIRuntimeState{}, false, err
	}
	actionType := "ai_booking.disable"
	if input.Enabled {
		actionType = "ai_booking.enable"
	}
	details := `{"changed_fields":["ai_enabled"]}`
	var actionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO technical_actions (
			salon_id,actor_user_id,action_key,action_type,request_fingerprint,
			resource_type,resource_id,previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,$5,'ai_runtime','ai_booking',$6,$7,$8::jsonb)
		RETURNING id::text
	`, input.SalonID, input.ActorUserID, input.ActionKey, actionType, input.RequestFingerprint,
		currentVersion, resultVersion, details).Scan(&actionID); err != nil {
		return AIRuntimeState{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_events (
			action_id,salon_id,actor_user_id,event_type,resource_type,resource_id,
			previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,'ai_runtime','ai_booking',$5,$6,$7::jsonb)
	`, actionID, input.SalonID, input.ActorUserID, actionType, currentVersion, resultVersion, details); err != nil {
		return AIRuntimeState{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIRuntimeState{}, false, err
	}
	return AIRuntimeState{Enabled: input.Enabled, Version: resultVersion}, false, nil
}

func (r *Repository) SetSalonAIEnabled(ctx context.Context, salonID string, ownerUserID string, enabled bool) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE salons
		SET ai_enabled = $1,
		    updated_at = now()
		WHERE id = $2 AND owner_user_id = $3
	`, enabled, salonID, ownerUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetConnection(ctx context.Context, salonID string, provider string) (*Connection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, provider, status, COALESCE(access_token_encrypted, ''),
		       COALESCE(refresh_token_encrypted, ''), COALESCE(merchant_id, ''), COALESCE(location_id, ''),
		       snapshot_generation, scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
		FROM pos_connections
		WHERE salon_id = $1 AND provider = $2
	`, salonID, provider)
	return scanConnection(row)
}

func (r *Repository) UpsertConnection(ctx context.Context, connection Connection) (*Connection, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, connection.SalonID); err != nil {
		return nil, err
	}
	item, err := scanConnection(tx.QueryRowContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, access_token_encrypted, refresh_token_encrypted, merchant_id, location_id, scopes, error_message
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''))
		ON CONFLICT (salon_id, provider)
		DO UPDATE SET status = EXCLUDED.status,
		              access_token_encrypted = EXCLUDED.access_token_encrypted,
		              refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
		              merchant_id = EXCLUDED.merchant_id,
		              scopes = EXCLUDED.scopes,
		              error_message = EXCLUDED.error_message,
		              updated_at = now()
		RETURNING id::text, salon_id::text, provider, status, COALESCE(access_token_encrypted, ''),
		          COALESCE(refresh_token_encrypted, ''), COALESCE(merchant_id, ''), COALESCE(location_id, ''),
		          snapshot_generation, scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
	`, connection.SalonID, connection.Provider, connection.Status, connection.AccessTokenEncrypted, connection.RefreshTokenEncrypted, connection.MerchantID, connection.LocationID, pq.Array(connection.Scopes), connection.ErrorMessage))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) UpdateLocation(ctx context.Context, salonID string, provider string, locationID string) (*Connection, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	item, err := scanConnection(tx.QueryRowContext(ctx, `
		UPDATE pos_connections
		SET location_id = $1,
		    snapshot_generation = CASE
		        WHEN COALESCE(location_id, '') IS DISTINCT FROM $1 THEN snapshot_generation + 1
		        ELSE snapshot_generation
		    END,
		    status = CASE
		        WHEN COALESCE(location_id, '') IS DISTINCT FROM $1
		         AND status IN ('connected', 'active', 'syncing', 'error') THEN 'connected'
		        ELSE status
		    END,
		    last_sync_at = CASE
		        WHEN COALESCE(location_id, '') IS DISTINCT FROM $1 THEN NULL
		        ELSE last_sync_at
		    END,
		    error_message = CASE
		        WHEN COALESCE(location_id, '') IS DISTINCT FROM $1
		         AND status IN ('connected', 'active', 'syncing', 'error') THEN NULL
		        ELSE error_message
		    END,
		    updated_at = now()
		WHERE salon_id = $2 AND provider = $3
		RETURNING id::text, salon_id::text, provider, status, COALESCE(access_token_encrypted, ''),
		          COALESCE(refresh_token_encrypted, ''), COALESCE(merchant_id, ''), COALESCE(location_id, ''),
		          snapshot_generation, scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
	`, locationID, salonID, provider))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) BeginProviderSnapshot(ctx context.Context, salonID string, provider string, locationID string) (int64, error) {
	salonID = strings.TrimSpace(salonID)
	provider = strings.TrimSpace(provider)
	locationID = strings.TrimSpace(locationID)
	if salonID == "" || provider == "" || locationID == "" {
		return 0, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return 0, err
	}
	var generation int64
	err = tx.QueryRowContext(ctx, `
		UPDATE pos_connections
		SET snapshot_generation = snapshot_generation + 1,
		    status = 'syncing',
		    last_sync_at = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE salon_id = $1
		  AND provider = $2
		  AND COALESCE(location_id, '') = $3
		RETURNING snapshot_generation
	`, salonID, provider, locationID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrStaleProviderSnapshot
	}
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return generation, nil
}

func (r *Repository) MarkSyncing(ctx context.Context, salonID string, provider string) error {
	return r.withSchedulingFenceMutation(ctx, salonID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = 'syncing', last_sync_at = NULL, updated_at = now()
		WHERE salon_id = $1 AND provider = $2
		`, salonID, provider)
		return err
	})
}

func (r *Repository) MarkSyncComplete(ctx context.Context, salonID string, provider string, status string, message string) error {
	return r.withSchedulingFenceMutation(ctx, salonID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = $3,
		    last_sync_at = CASE WHEN $3 = 'active' THEN now() ELSE NULL END,
		    error_message = NULLIF($4, ''),
		    updated_at = now()
		WHERE salon_id = $1 AND provider = $2
		`, salonID, provider, status, message)
		return err
	})
}

func (r *Repository) MarkSyncCompleteForGeneration(ctx context.Context, salonID string, provider string, generation int64, status string, message string) error {
	if generation <= 0 {
		return ErrValidation
	}
	return r.withSchedulingFenceMutation(ctx, salonID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = $4,
		    last_sync_at = CASE WHEN $4 = 'active' THEN now() ELSE NULL END,
		    error_message = NULLIF($5, ''),
		    updated_at = now()
		WHERE salon_id = $1
		  AND provider = $2
		  AND snapshot_generation = $3
		`, salonID, provider, generation, status, message)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrStaleProviderSnapshot
		}
		return nil
	})
}

func (r *Repository) withSchedulingFenceMutation(ctx context.Context, salonID string, mutate func(*sql.Tx) error) error {
	return r.WithSchedulingFenceTx(ctx, salonID, mutate)
}

func (r *Repository) CreateSyncLog(ctx context.Context, salonID string, provider string, syncType string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO pos_sync_logs (salon_id, provider, sync_type, status)
		VALUES ($1, $2, $3, 'started')
		RETURNING id::text
	`, salonID, provider, syncType).Scan(&id)
	return id, err
}

func (r *Repository) CompleteSyncLog(ctx context.Context, id string, status string, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pos_sync_logs
		SET status = $2, message = NULLIF($3, ''), completed_at = now()
		WHERE id = $1
	`, id, status, message)
	return err
}

func (r *Repository) RecentSyncLogs(ctx context.Context, salonID string, provider string, limit int) ([]SyncLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, provider, sync_type, status, COALESCE(message, ''), started_at, completed_at
		FROM pos_sync_logs
		WHERE salon_id = $1 AND provider = $2
		ORDER BY started_at DESC
		LIMIT $3
	`, salonID, provider, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]SyncLog, 0)
	for rows.Next() {
		var item SyncLog
		if err := rows.Scan(&item.ID, &item.SalonID, &item.Provider, &item.SyncType, &item.Status, &item.Message, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func (r *Repository) EnqueuePOSSyncJob(ctx context.Context, input SyncJobMutation) (*SyncJob, error) {
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, input.SalonID); err != nil {
		return nil, err
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO pos_sync_jobs (
			salon_id, provider, entity_type, entity_id, operation, status, max_attempts, next_attempt_at
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, now())
		ON CONFLICT (salon_id, provider, entity_type, entity_id, operation)
		WHERE status IN ('queued', 'running')
		DO UPDATE SET next_attempt_at = CASE
			              WHEN pos_sync_jobs.status = 'queued' THEN now()
			              ELSE pos_sync_jobs.next_attempt_at
			          END,
			          max_attempts = GREATEST(pos_sync_jobs.max_attempts, EXCLUDED.max_attempts),
			          last_error = CASE
			              WHEN pos_sync_jobs.status = 'queued' THEN NULL
			              ELSE pos_sync_jobs.last_error
			          END,
			          updated_at = now()
		RETURNING id::text, salon_id::text, provider, entity_type, entity_id::text, operation, status,
		          attempt_count, max_attempts, next_attempt_at, locked_at, completed_at,
		          COALESCE(last_error, ''), created_at, updated_at
	`, input.SalonID, input.Provider, input.EntityType, input.EntityID, input.Operation, maxAttempts)
	job, err := scanSyncJob(row)
	if err != nil {
		return nil, err
	}
	if err := markEntitySyncing(ctx, tx, *job); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *Repository) ClaimPOSSyncJobs(ctx context.Context, limit int) ([]SyncJob, error) {
	if limit <= 0 {
		limit = 10
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		WITH ranked AS (
			SELECT job.id,job.next_attempt_at,job.created_at,
			       row_number() OVER (
			           PARTITION BY job.salon_id
			           ORDER BY job.next_attempt_at,job.created_at,job.id
			       ) AS tenant_rank,
			       COALESCE(limits.worker_claims_per_batch,2) AS tenant_limit
			FROM pos_sync_jobs job
			LEFT JOIN tenant_runtime_limits limits ON limits.salon_id=job.salon_id
			WHERE job.status IN ('queued', 'failed')
			  AND job.attempt_count < job.max_attempts
			  AND job.next_attempt_at <= now()
		), candidates AS (
			SELECT job.id
			FROM pos_sync_jobs job
			JOIN ranked ON ranked.id=job.id
			WHERE ranked.tenant_rank<=ranked.tenant_limit
			ORDER BY ranked.next_attempt_at,ranked.created_at,job.id
			FOR UPDATE OF job SKIP LOCKED
			LIMIT $1
		)
		UPDATE pos_sync_jobs job
		SET status = 'running',
		    attempt_count = attempt_count + 1,
		    locked_at = now(),
		    completed_at = NULL,
		    updated_at = now()
		FROM candidates
		WHERE job.id = candidates.id
		RETURNING job.id::text, job.salon_id::text, job.provider, job.entity_type, job.entity_id::text,
		          job.operation, job.status, job.attempt_count, job.max_attempts, job.next_attempt_at,
		          job.locked_at, job.completed_at, COALESCE(job.last_error, ''), job.created_at, job.updated_at
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]SyncJob, 0)
	for rows.Next() {
		job, err := scanSyncJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *Repository) MarkPOSSyncJobSucceeded(ctx context.Context, job SyncJob, result ProviderSyncResult) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, job.SalonID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE pos_sync_jobs
		SET status = 'succeeded',
		    completed_at = now(),
		    last_error = NULL,
		    updated_at = now()
		WHERE id = $1
	`, job.ID); err != nil {
		return err
	}
	if err := markEntitySyncSucceeded(ctx, tx, job, result); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) MarkPOSSyncJobFailed(ctx context.Context, job SyncJob, message string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, job.SalonID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE pos_sync_jobs
		SET status = 'failed',
		    next_attempt_at = CASE
		        WHEN attempt_count < max_attempts THEN now() + (attempt_count * interval '5 minutes')
		        ELSE next_attempt_at
		    END,
		    completed_at = CASE
		        WHEN attempt_count >= max_attempts THEN now()
		        ELSE completed_at
		    END,
		    last_error = NULLIF($2, ''),
		    updated_at = now()
		WHERE id = $1
	`, job.ID, message); err != nil {
		return err
	}
	if err := markEntitySyncFailed(ctx, tx, job, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) LogError(ctx context.Context, item POSError) error {
	item.ErrorMessage = SafeErrorMessage(item.ErrorCode)
	item.Payload = nil
	return r.withSchedulingFenceMutation(ctx, item.SalonID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message, payload)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::jsonb)
		`, item.SalonID, item.Provider, item.Operation, item.ErrorCode, item.ErrorMessage, string(item.Payload))
		return err
	})
}

func (r *Repository) LatestErrorForOperations(ctx context.Context, salonID string, provider string, operations []string) (*POSErrorRecord, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(provider) == "" || len(operations) == 0 {
		return nil, ErrNotFound
	}
	item := POSErrorRecord{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, provider, operation, error_code, error_message, created_at
		FROM pos_errors
		WHERE salon_id = $1
		  AND provider = $2
		  AND operation = ANY($3)
		ORDER BY created_at DESC
		LIMIT 1
	`, salonID, provider, pq.Array(operations)).Scan(&item.ID, &item.SalonID, &item.Provider, &item.Operation, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) CreateOAuthState(ctx context.Context, state OAuthState) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pos_oauth_states (salon_id, provider, state_hash, nonce_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, state.SalonID, state.Provider, state.StateHash, state.NonceHash, state.ExpiresAt)
	return err
}

func (r *Repository) ConsumeOAuthState(ctx context.Context, provider string, salonID string, stateHash string, nonceHash string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
		UPDATE pos_oauth_states
		SET used_at = now()
		WHERE provider = $1
		  AND salon_id = $2
		  AND state_hash = $3
		  AND nonce_hash = $4
		  AND used_at IS NULL
		  AND expires_at > now()
		RETURNING id::text
	`, provider, salonID, stateHash, nonceHash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r *Repository) UpsertServices(ctx context.Context, salonID string, services []Service) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return err
	}
	if err := upsertServicesTx(ctx, tx, salonID, services); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertServicesTx(ctx context.Context, tx *sql.Tx, salonID string, services []Service) error {
	for _, svc := range services {
		provider := svc.POSProvider
		if provider == "" {
			provider = ProviderSquare
		}
		var serviceID string
		var archived bool
		// Provider eligibility is a hard cap: sync may revoke AI booking, but it must not silently re-enable an owner-disabled service.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO services (
				salon_id, pos_provider, pos_service_id, pos_service_version, name, description, ai_description,
				duration_minutes, price_from, price_display, ai_bookable, active, sync_status, last_synced_at, sync_error, source
			)
			VALUES ($1, $2, $3, NULLIF($4::bigint, 0), $5, NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, 0), NULLIF($10, ''), $11, $12, 'synced', now(), NULL, 'imported')
			ON CONFLICT (salon_id, pos_provider, pos_service_id)
			DO UPDATE SET name = EXCLUDED.name,
			              pos_service_version = EXCLUDED.pos_service_version,
			              description = EXCLUDED.description,
			              ai_description = COALESCE(services.ai_description, EXCLUDED.ai_description),
			              duration_minutes = EXCLUDED.duration_minutes,
			              price_from = EXCLUDED.price_from,
			              price_display = EXCLUDED.price_display,
			              active = CASE WHEN services.archived_at IS NULL THEN EXCLUDED.active ELSE false END,
			              ai_bookable = CASE
			                  WHEN services.archived_at IS NULL THEN services.ai_bookable AND EXCLUDED.ai_bookable
			                  ELSE false
			              END,
			              sync_status = CASE WHEN services.archived_at IS NULL THEN 'synced' ELSE 'archived' END,
			              last_synced_at = now(),
			              sync_error = NULL,
			              source = 'imported',
			              updated_at = now()
			RETURNING id::text, archived_at IS NOT NULL
		`, salonID, provider, svc.POSServiceID, svc.POSServiceVersion, svc.Name, svc.Description, svc.AIDescription, svc.DurationMinutes, svc.PriceFrom, svc.PriceDisplay, svc.AIBookable, svc.Active).Scan(&serviceID, &archived); err != nil {
			return err
		}
		syncStatus := SyncStatusSynced
		if archived {
			syncStatus = SyncStatusArchived
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pos_entity_links (
				salon_id, entity_type, entity_id, provider, provider_entity_id, provider_version, sync_status, last_synced_at, last_error
			)
			VALUES ($1, 'service', $2, $3, NULLIF($4, ''), NULLIF($5::bigint, 0), $6, now(), NULL)
			ON CONFLICT (salon_id, entity_type, entity_id, provider)
			DO UPDATE SET provider_entity_id = EXCLUDED.provider_entity_id,
			              provider_version = EXCLUDED.provider_version,
			              sync_status = EXCLUDED.sync_status,
			              last_synced_at = now(),
			              last_error = NULL,
			              updated_at = now()
		`, salonID, serviceID, provider, svc.POSServiceID, svc.POSServiceVersion, syncStatus); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) UpsertStaff(ctx context.Context, salonID string, staff []StaffMember) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return err
	}
	if err := upsertStaffTx(ctx, tx, salonID, staff); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertStaffTx(ctx context.Context, tx *sql.Tx, salonID string, staff []StaffMember) error {
	for _, member := range staff {
		provider := member.POSProvider
		if provider == "" {
			provider = ProviderSquare
		}
		var staffID string
		var archived bool
		// Provider eligibility is a hard cap: sync may revoke AI booking, but it must not silently re-enable an owner-disabled staff member.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO staff (
				salon_id, pos_provider, pos_staff_id, name, phone, email, ai_bookable, active,
				sync_status, last_synced_at, sync_error, source
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, 'synced', now(), NULL, 'imported')
			ON CONFLICT (salon_id, pos_provider, pos_staff_id)
			DO UPDATE SET name = EXCLUDED.name,
			              phone = EXCLUDED.phone,
			              email = EXCLUDED.email,
			              active = CASE WHEN staff.archived_at IS NULL THEN EXCLUDED.active ELSE false END,
			              ai_bookable = CASE
			                  WHEN staff.archived_at IS NULL THEN staff.ai_bookable AND EXCLUDED.ai_bookable
			                  ELSE false
			              END,
			              sync_status = CASE WHEN staff.archived_at IS NULL THEN 'synced' ELSE 'archived' END,
			              last_synced_at = now(),
			              sync_error = NULL,
			              source = 'imported',
			              updated_at = now()
			RETURNING id::text, archived_at IS NOT NULL
		`, salonID, provider, member.POSStaffID, member.Name, member.Phone, member.Email, member.AIBookable, member.Active).Scan(&staffID, &archived); err != nil {
			return err
		}
		syncStatus := SyncStatusSynced
		if archived {
			syncStatus = SyncStatusArchived
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pos_entity_links (
				salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
			)
			VALUES ($1, 'staff', $2, $3, NULLIF($4, ''), $5, now(), NULL)
			ON CONFLICT (salon_id, entity_type, entity_id, provider)
			DO UPDATE SET provider_entity_id = EXCLUDED.provider_entity_id,
			              sync_status = EXCLUDED.sync_status,
			              last_synced_at = now(),
			              last_error = NULL,
			              updated_at = now()
		`, salonID, staffID, provider, member.POSStaffID, syncStatus); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) UpsertBusinessHourPeriods(ctx context.Context, salonID string, provider string, locationID string, periods []BusinessHourPeriod) (int, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = ProviderSquare
	}
	locationID = strings.TrimSpace(locationID)
	if locationID == "" {
		return 0, ErrValidation
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return 0, err
	}
	count, err := upsertBusinessHourPeriodsTx(ctx, tx, salonID, provider, locationID, periods)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func upsertBusinessHourPeriodsTx(ctx context.Context, tx *sql.Tx, salonID string, provider string, locationID string, periods []BusinessHourPeriod) (int, error) {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM salon_business_hour_periods
		WHERE salon_id = $1
		  AND source = 'imported'
		  AND provider = $2
		  AND provider_location_id = $3
	`, salonID, provider, locationID); err != nil {
		return 0, err
	}

	count := 0
	for _, period := range periods {
		startTime, startOK := normalizeLocalClock(period.StartLocalTime)
		endTime, endOK := normalizeLocalClock(period.EndLocalTime)
		if !startOK || !endOK || period.DayOfWeek < 0 || period.DayOfWeek > 6 {
			continue
		}
		startDuration, _ := localClockDuration(startTime)
		endDuration, _ := localClockDuration(endTime)
		if endDuration <= startDuration {
			continue
		}
		periodIndex := period.ProviderPeriodIndex
		if periodIndex < 0 {
			periodIndex = count
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hour_periods (
				salon_id, day_of_week, start_local_time, end_local_time, source,
				provider, provider_location_id, provider_period_index, last_synced_at
			)
			VALUES ($1, $2, $3::time, $4::time, 'imported', $5, $6, $7, now())
			ON CONFLICT (salon_id, provider, provider_location_id, day_of_week, provider_period_index)
			DO UPDATE SET start_local_time = EXCLUDED.start_local_time,
			              end_local_time = EXCLUDED.end_local_time,
			              source = 'imported',
			              last_synced_at = now(),
			              updated_at = now()
		`, salonID, period.DayOfWeek, startTime, endTime, provider, locationID, periodIndex); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func (r *Repository) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, day_of_week, start_local_time::text, end_local_time::text,
		       source, provider, provider_location_id, provider_period_index, last_synced_at, created_at, updated_at
		FROM salon_business_hour_periods
		WHERE salon_id = $1
		ORDER BY day_of_week ASC, start_local_time ASC, provider_period_index ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BusinessHourPeriod, 0)
	for rows.Next() {
		var item BusinessHourPeriod
		var lastSyncedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.DayOfWeek,
			&item.StartLocalTime,
			&item.EndLocalTime,
			&item.Source,
			&item.Provider,
			&item.ProviderLocationID,
			&item.ProviderPeriodIndex,
			&lastSyncedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastSyncedAt.Valid {
			item.LastSyncedAt = &lastSyncedAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertCustomers(ctx context.Context, salonID string, provider string, customers []Customer) (int, int, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = ProviderSquare
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	synced, skipped, err := upsertCustomersTx(ctx, tx, salonID, provider, customers)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return synced, skipped, nil
}

func upsertCustomersTx(ctx context.Context, tx *sql.Tx, salonID string, provider string, customers []Customer) (int, int, error) {
	synced := 0
	skipped := 0
	for _, customer := range customers {
		providerCustomerID := strings.TrimSpace(customer.POSCustomerID)
		if providerCustomerID == "" {
			skipped++
			continue
		}
		name := importedCustomerName(customer)
		phone := validation.NormalizePhone(customer.Phone)
		email := strings.ToLower(strings.TrimSpace(customer.Email))

		customerID, err := findCustomerIDForImport(ctx, tx, salonID, provider, providerCustomerID, phone, email)
		if err != nil {
			return 0, 0, err
		}
		if customerID == "" {
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO customers (
					salon_id, name, phone, normalized_phone, email, normalized_email, active,
					sync_status, last_synced_at, sync_error, source
				)
				VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), true, 'synced', now(), NULL, 'imported')
				RETURNING id::text
			`, salonID, name, phone, phone, email, email).Scan(&customerID); err != nil {
				return 0, 0, err
			}
		} else {
			safePhone := phone
			if safePhone != "" {
				conflicts, err := customerNormalizedValueConflicts(ctx, tx, salonID, customerID, "normalized_phone", safePhone)
				if err != nil {
					return 0, 0, err
				}
				if conflicts {
					safePhone = ""
				}
			}
			safeEmail := email
			if safeEmail != "" {
				conflicts, err := customerNormalizedValueConflicts(ctx, tx, salonID, customerID, "normalized_email", safeEmail)
				if err != nil {
					return 0, 0, err
				}
				if conflicts {
					safeEmail = ""
				}
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE customers
				SET name = CASE
				        WHEN source = 'imported' OR btrim(name) = '' THEN $3
				        ELSE name
				    END,
				    phone = CASE
				        WHEN NULLIF($4, '') IS NOT NULL AND (source = 'imported' OR COALESCE(phone, '') = '') THEN $4
				        ELSE phone
				    END,
				    normalized_phone = CASE
				        WHEN NULLIF($5, '') IS NOT NULL AND (source = 'imported' OR COALESCE(normalized_phone, '') = '') THEN $5
				        ELSE normalized_phone
				    END,
				    email = CASE
				        WHEN NULLIF($6, '') IS NOT NULL AND (source = 'imported' OR COALESCE(email, '') = '') THEN $6
				        ELSE email
				    END,
				    normalized_email = CASE
				        WHEN NULLIF($7, '') IS NOT NULL AND (source = 'imported' OR COALESCE(normalized_email, '') = '') THEN $7
				        ELSE normalized_email
				    END,
				    active = true,
				    sync_status = 'synced',
				    last_synced_at = now(),
				    sync_error = NULL,
				    updated_at = now()
				WHERE salon_id = $1
				  AND id = $2
				  AND archived_at IS NULL
			`, salonID, customerID, name, safePhone, safePhone, safeEmail, safeEmail); err != nil {
				return 0, 0, err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pos_entity_links (
				salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
			)
			VALUES ($1, 'customer', $2, $3, $4, 'synced', now(), NULL)
			ON CONFLICT (salon_id, entity_type, entity_id, provider)
			DO UPDATE SET provider_entity_id = EXCLUDED.provider_entity_id,
			              sync_status = 'synced',
			              last_synced_at = now(),
			              last_error = NULL,
			              updated_at = now()
		`, salonID, customerID, provider, providerCustomerID); err != nil {
			return 0, 0, err
		}
		synced++
	}
	return synced, skipped, nil
}

func (r *Repository) ApplyProviderSnapshot(ctx context.Context, salonID string, snapshot ProviderSnapshot) (*SyncSummary, error) {
	provider := strings.TrimSpace(snapshot.Provider)
	if provider == "" {
		provider = ProviderSquare
	}
	locationID := strings.TrimSpace(snapshot.LocationID)
	if locationID == "" || snapshot.Generation <= 0 {
		return nil, ErrValidation
	}
	for index := range snapshot.Services {
		serviceProvider := strings.TrimSpace(snapshot.Services[index].POSProvider)
		if serviceProvider != "" && serviceProvider != provider {
			return nil, ErrValidation
		}
		snapshot.Services[index].POSProvider = provider
	}
	for index := range snapshot.Staff {
		staffProvider := strings.TrimSpace(snapshot.Staff[index].POSProvider)
		if staffProvider != "" && staffProvider != provider {
			return nil, ErrValidation
		}
		snapshot.Staff[index].POSProvider = provider
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"pos-provider-snapshot:"+salonID+":"+provider,
	); err != nil {
		return nil, err
	}
	var currentLocationID string
	var currentGeneration int64
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(location_id, ''), snapshot_generation
		FROM pos_connections
		WHERE salon_id = $1 AND provider = $2
		FOR UPDATE
	`, salonID, provider).Scan(&currentLocationID, &currentGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrStaleProviderSnapshot
	}
	if err != nil {
		return nil, err
	}
	if currentLocationID != locationID || currentGeneration != snapshot.Generation {
		return nil, ErrStaleProviderSnapshot
	}

	if err := upsertServicesTx(ctx, tx, salonID, snapshot.Services); err != nil {
		return nil, err
	}
	if err := markMissingImportedServicesTx(ctx, tx, salonID, provider, snapshot.Services); err != nil {
		return nil, err
	}
	if err := upsertStaffTx(ctx, tx, salonID, snapshot.Staff); err != nil {
		return nil, err
	}
	if err := markMissingImportedStaffTx(ctx, tx, salonID, provider, snapshot.Staff); err != nil {
		return nil, err
	}
	periodsSynced, err := upsertBusinessHourPeriodsTx(ctx, tx, salonID, provider, locationID, snapshot.BusinessHourPeriods)
	if err != nil {
		return nil, err
	}
	customersSynced, customersSkipped, err := upsertCustomersTx(ctx, tx, salonID, provider, snapshot.Customers)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SyncSummary{
		ServicesSynced:            len(snapshot.Services),
		StaffSynced:               len(snapshot.Staff),
		BusinessHourPeriodsSynced: periodsSynced,
		CustomersSynced:           customersSynced,
		CustomersSkipped:          customersSkipped,
		SnapshotGeneration:        snapshot.Generation,
	}, nil
}

func markMissingImportedServicesTx(ctx context.Context, tx *sql.Tx, salonID string, provider string, services []Service) error {
	providerIDs := make([]string, 0, len(services))
	for _, service := range services {
		if providerID := strings.TrimSpace(service.POSServiceID); providerID != "" {
			providerIDs = append(providerIDs, providerID)
		}
	}
	const reason = "Missing from latest provider snapshot"
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET active = false,
		    ai_bookable = false,
		    sync_status = 'unmapped',
		    last_synced_at = now(),
		    sync_error = $4,
		    updated_at = now()
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND source = 'imported'
		  AND archived_at IS NULL
		  AND NOT (COALESCE(pos_service_id, '') = ANY($3::text[]))
	`, salonID, provider, pq.Array(providerIDs), reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links link
		SET sync_status = 'unmapped',
		    last_synced_at = now(),
		    last_error = $4,
		    updated_at = now()
		WHERE link.salon_id = $1
		  AND link.provider = $2
		  AND link.entity_type = 'service'
		  AND NOT (COALESCE(link.provider_entity_id, '') = ANY($3::text[]))
		  AND EXISTS (
		      SELECT 1
		      FROM services svc
		      WHERE svc.id = link.entity_id
		        AND svc.salon_id = link.salon_id
		        AND svc.source = 'imported'
		        AND svc.archived_at IS NULL
		  )
	`, salonID, provider, pq.Array(providerIDs), reason)
	return err
}

func markMissingImportedStaffTx(ctx context.Context, tx *sql.Tx, salonID string, provider string, staff []StaffMember) error {
	providerIDs := make([]string, 0, len(staff))
	for _, member := range staff {
		if providerID := strings.TrimSpace(member.POSStaffID); providerID != "" {
			providerIDs = append(providerIDs, providerID)
		}
	}
	const reason = "Missing from latest provider snapshot"
	if _, err := tx.ExecContext(ctx, `
		UPDATE staff
		SET active = false,
		    ai_bookable = false,
		    sync_status = 'unmapped',
		    last_synced_at = now(),
		    sync_error = $4,
		    updated_at = now()
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND source = 'imported'
		  AND archived_at IS NULL
		  AND NOT (COALESCE(pos_staff_id, '') = ANY($3::text[]))
	`, salonID, provider, pq.Array(providerIDs), reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links link
		SET sync_status = 'unmapped',
		    last_synced_at = now(),
		    last_error = $4,
		    updated_at = now()
		WHERE link.salon_id = $1
		  AND link.provider = $2
		  AND link.entity_type = 'staff'
		  AND NOT (COALESCE(link.provider_entity_id, '') = ANY($3::text[]))
		  AND EXISTS (
		      SELECT 1
		      FROM staff member
		      WHERE member.id = link.entity_id
		        AND member.salon_id = link.salon_id
		        AND member.source = 'imported'
		        AND member.archived_at IS NULL
		  )
	`, salonID, provider, pq.Array(providerIDs), reason)
	return err
}

func (r *Repository) ListServices(ctx context.Context, salonID string, provider string) ([]Service, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT svc.id::text, svc.salon_id::text, svc.pos_provider, COALESCE(svc.pos_service_id, ''), COALESCE(svc.pos_service_version, 0),
		       svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''), svc.duration_minutes,
		       COALESCE(svc.price_from, 0), COALESCE(svc.price_display, ''), svc.ai_bookable, svc.active,
		       svc.sync_status, svc.archived_at, svc.last_synced_at, COALESCE(svc.sync_error, ''), svc.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = svc.salon_id
		             AND link.entity_type = 'service'
		             AND link.entity_id = svc.id
		             AND link.provider = svc.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked,
		       COALESCE(cat.id::text, ''), COALESCE(cat.name, ''), COALESCE(cat.slug, ''),
		       svc.service_category_source, COALESCE(svc.service_category_confidence, 0), svc.service_category_reviewed_at,
		       COALESCE(profile.id::text, ''), COALESCE(profile.status, 'draft'),
		       COALESCE(profile.recommended_outcomes, '[]'::jsonb), COALESCE(profile.compatible_current_systems, '[]'::jsonb),
		       COALESCE(profile.length_capabilities, '[]'::jsonb), COALESCE(profile.priority_tags, '[]'::jsonb),
		       COALESCE(profile.finish_options, '[]'::jsonb), COALESCE(profile.maintenance_note, ''),
		       COALESCE(profile.owner_approved_summary, ''), COALESCE(profile.revision, 0),
		       COALESCE(profile.updated_by::text, ''), profile.created_at, profile.updated_at
		FROM services svc
		LEFT JOIN service_categories cat ON cat.id = svc.service_category_id
		                                AND cat.salon_id = svc.salon_id
		                                AND cat.status = 'active'
		LEFT JOIN service_consultation_profiles profile ON profile.salon_id = svc.salon_id
		                                                  AND profile.service_id = svc.id
		WHERE svc.salon_id = $1 AND svc.pos_provider = $2
		ORDER BY (svc.archived_at IS NOT NULL) ASC, svc.active DESC, COALESCE(cat.sort_order, 9999), COALESCE(cat.name, ''), svc.name ASC
	`, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Service, 0)
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListServiceCategories(ctx context.Context, salonID string, ownerUserID string) ([]ServiceCategory, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT cat.id::text, cat.salon_id::text, cat.name, cat.slug, COALESCE(cat.description, ''),
		       cat.status, cat.sort_order, cat.source, COUNT(svc.id)::int,
		       cat.reviewed_at, cat.archived_at, cat.created_at, cat.updated_at
		FROM service_categories cat
		LEFT JOIN services svc ON svc.service_category_id = cat.id
		                      AND svc.salon_id = cat.salon_id
		                      AND svc.archived_at IS NULL
		WHERE cat.salon_id = $1
		GROUP BY cat.id
		ORDER BY (cat.status = 'archived') ASC, cat.sort_order ASC, cat.name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceCategory, 0)
	for rows.Next() {
		item, err := scanServiceCategory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	aliases, err := r.listServiceCategoryAliases(ctx, salonID)
	if err != nil {
		return nil, err
	}
	byCategory := map[string][]ServiceCategoryAlias{}
	for _, alias := range aliases {
		byCategory[alias.CategoryID] = append(byCategory[alias.CategoryID], alias)
	}
	for i := range items {
		items[i].Aliases = byCategory[items[i].ID]
	}
	return items, nil
}

func (r *Repository) CreateServiceCategory(ctx context.Context, salonID string, ownerUserID string, input ServiceCategoryMutation) (*ServiceCategory, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO service_categories (
			salon_id, name, slug, description, sort_order, source, reviewed_by, reviewed_at
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, 'manual', $6, now())
		RETURNING id::text, salon_id::text, name, slug, COALESCE(description, ''), status, sort_order,
		          source, 0::int, reviewed_at, archived_at, created_at, updated_at
	`, salonID, input.Name, input.Slug, input.Description, input.SortOrder, ownerUserID)
	return scanServiceCategory(row)
}

func (r *Repository) UpdateServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string, input ServiceCategoryMutation) (*ServiceCategory, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE service_categories
		SET name = $1,
		    slug = $2,
		    description = NULLIF($3, ''),
		    sort_order = $4,
		    reviewed_by = $5,
		    reviewed_at = now(),
		    updated_at = now()
		WHERE id = $6
		  AND salon_id = $7
		RETURNING id::text, salon_id::text, name, slug, COALESCE(description, ''), status, sort_order,
		          source,
		          (SELECT COUNT(*)::int FROM services WHERE service_category_id = service_categories.id AND archived_at IS NULL),
		          reviewed_at, archived_at, created_at, updated_at
	`, input.Name, input.Slug, input.Description, input.SortOrder, ownerUserID, categoryID, salonID)
	return scanServiceCategory(row)
}

func (r *Repository) ArchiveServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		UPDATE service_categories
		SET status = 'archived',
		    archived_at = COALESCE(archived_at, now()),
		    updated_at = now()
		WHERE id = $1
		  AND salon_id = $2
		RETURNING id::text, salon_id::text, name, slug, COALESCE(description, ''), status, sort_order,
		          source, 0::int, reviewed_at, archived_at, created_at, updated_at
	`, categoryID, salonID)
	item, err := scanServiceCategory(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET service_category_id = NULL,
		    service_category_source = 'unassigned',
		    service_category_confidence = NULL,
		    service_category_reviewed_by = NULL,
		    service_category_reviewed_at = NULL,
		    updated_at = now()
		WHERE salon_id = $1
		  AND service_category_id = $2
	`, salonID, categoryID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE service_category_aliases
		SET status = 'archived',
		    updated_at = now()
		WHERE salon_id = $1
		  AND category_id = $2
	`, salonID, categoryID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) RestoreServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE service_categories
		SET status = 'active',
		    archived_at = NULL,
		    reviewed_by = $1,
		    reviewed_at = now(),
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
		RETURNING id::text, salon_id::text, name, slug, COALESCE(description, ''), status, sort_order,
		          source,
		          (SELECT COUNT(*)::int FROM services WHERE service_category_id = service_categories.id AND archived_at IS NULL),
		          reviewed_at, archived_at, created_at, updated_at
	`, ownerUserID, categoryID, salonID)
	return scanServiceCategory(row)
}

func (r *Repository) UpsertServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, input ServiceCategoryAliasMutation) (*ServiceCategoryAlias, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := r.ensureActiveCategory(ctx, salonID, ownerUserID, input.CategoryID); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockServiceAliasOwnershipTx(ctx, tx, salonID, input.NormalizedAlias); err != nil {
		return nil, err
	}
	var conflictingServiceAlias bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_aliases
			WHERE salon_id = $1
			  AND normalized_alias = $2
			  AND status = 'active'
		)
	`, salonID, input.NormalizedAlias).Scan(&conflictingServiceAlias); err != nil {
		return nil, err
	}
	if conflictingServiceAlias {
		return nil, ErrValidation
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO service_category_aliases (
			salon_id, category_id, alias, normalized_alias, source, status, confidence
		)
		VALUES ($1, $2, $3, $4, 'owner', 'active', $5)
		ON CONFLICT (salon_id, normalized_alias)
		DO UPDATE SET category_id = EXCLUDED.category_id,
		              alias = EXCLUDED.alias,
		              source = 'owner',
		              status = 'active',
		              confidence = EXCLUDED.confidence,
		              updated_at = now()
		RETURNING id::text, salon_id::text, category_id::text,
		          (SELECT name FROM service_categories WHERE id = service_category_aliases.category_id),
		          alias, normalized_alias, source, status, confidence, created_at, updated_at
	`, salonID, input.CategoryID, input.Alias, input.NormalizedAlias, input.Confidence)
	alias, err := scanServiceCategoryAlias(row)
	if err != nil {
		if isServiceAliasOwnershipConflict(err) {
			return nil, ErrValidation
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return alias, nil
}

func (r *Repository) ArchiveServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, aliasID string) (*ServiceCategoryAlias, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE service_category_aliases
		SET status = 'archived',
		    updated_at = now()
		WHERE id = $1
		  AND salon_id = $2
		RETURNING id::text, salon_id::text, category_id::text,
		          (SELECT name FROM service_categories WHERE id = service_category_aliases.category_id),
		          alias, normalized_alias, source, status, confidence, created_at, updated_at
	`, aliasID, salonID)
	return scanServiceCategoryAlias(row)
}

func (r *Repository) AssignServiceCategory(ctx context.Context, salonID string, ownerUserID string, serviceID string, categoryID string) (*Service, error) {
	if _, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(categoryID) != "" {
		if err := r.ensureActiveCategory(ctx, salonID, ownerUserID, categoryID); err != nil {
			return nil, err
		}
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE services
		SET service_category_id = NULLIF($1, '')::uuid,
		    service_category_source = CASE WHEN NULLIF($1, '') IS NULL THEN 'unassigned' ELSE 'manual' END,
		    service_category_confidence = CASE WHEN NULLIF($1, '') IS NULL THEN NULL ELSE 1.000 END,
		    service_category_reviewed_by = CASE WHEN NULLIF($1, '') IS NULL THEN NULL ELSE $2::uuid END,
		    service_category_reviewed_at = CASE WHEN NULLIF($1, '') IS NULL THEN NULL ELSE now() END,
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
	`, categoryID, ownerUserID, serviceID, salonID); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) RefreshServiceCategorySuggestions(ctx context.Context, salonID string, ownerUserID string) (*ServiceCategorySuggestionRefresh, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &ServiceCategorySuggestionRefresh{}
	taxonomyCategories, err := activeServiceTaxonomyCategories(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(taxonomyCategories) == 0 {
		return nil, errors.New("active nail service taxonomy is unavailable")
	}
	categoryIDsBySlug := make(map[string]string, len(taxonomyCategories))
	for _, category := range taxonomyCategories {
		categoryID, status, created, restored, err := upsertSystemServiceCategory(ctx, tx, salonID, category)
		if err != nil {
			return nil, err
		}
		if created {
			result.CreatedCategories++
		}
		if restored {
			result.RestoredSystemCategories++
		}
		if status == ServiceCategoryStatusActive {
			categoryIDsBySlug[category.Slug] = categoryID
			for _, alias := range category.Aliases {
				createdAlias, updatedAlias, skippedConflict, err := upsertSystemServiceCategoryAlias(ctx, tx, salonID, categoryID, alias)
				if err != nil {
					return nil, err
				}
				if createdAlias {
					result.CreatedAliases++
				}
				if updatedAlias {
					result.UpdatedSystemAliases++
				}
				if skippedConflict {
					result.SkippedAliasConflicts++
				}
			}
		}
	}

	taxonomyConcepts, err := activeServiceTaxonomyConcepts(ctx, tx)
	if err != nil {
		return nil, err
	}
	conceptsByName := make(map[string][]serviceTaxonomyConceptRecord, len(taxonomyConcepts))
	for _, concept := range taxonomyConcepts {
		conceptsByName[concept.NormalizedName] = append(conceptsByName[concept.NormalizedName], concept)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, name, COALESCE(service_category_id::text, ''), service_category_source
		FROM services
		WHERE salon_id = $1
		  AND archived_at IS NULL
		ORDER BY name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}

	candidates := make([]serviceCategorySuggestionCandidate, 0)
	for rows.Next() {
		var candidate serviceCategorySuggestionCandidate
		if err := rows.Scan(&candidate.ServiceID, &candidate.ServiceName, &candidate.CurrentCategoryID, &candidate.Source); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	type aliasTarget struct {
		Alias      string
		Confidence float64
		ServiceIDs map[string]bool
	}
	aliasTargets := map[string]*aliasTarget{}
	for _, candidate := range candidates {
		if candidate.Source != ServiceCategoryAssignmentUnassigned && candidate.Source != ServiceCategoryAssignmentSuggested {
			result.SkippedReviewedServices++
		}
		matches := conceptsByName[normalizeAliasKey(candidate.ServiceName)]
		if len(matches) == 0 {
			if candidate.Source == ServiceCategoryAssignmentUnassigned || candidate.Source == ServiceCategoryAssignmentSuggested {
				result.UnmatchedUnreviewedServices++
			}
			continue
		}
		if len(matches) != 1 {
			if candidate.Source == ServiceCategoryAssignmentUnassigned || candidate.Source == ServiceCategoryAssignmentSuggested {
				result.SkippedAmbiguousServices++
			}
			continue
		}
		concept := matches[0]
		for _, alias := range concept.Aliases {
			target := aliasTargets[alias.Normalized]
			if target == nil {
				target = &aliasTarget{Alias: alias.Alias, Confidence: alias.Confidence, ServiceIDs: map[string]bool{}}
				aliasTargets[alias.Normalized] = target
			}
			if alias.Confidence > target.Confidence {
				target.Alias = alias.Alias
				target.Confidence = alias.Confidence
			}
			target.ServiceIDs[candidate.ServiceID] = true
		}
		if candidate.Source != ServiceCategoryAssignmentUnassigned && candidate.Source != ServiceCategoryAssignmentSuggested {
			continue
		}
		categoryID := categoryIDsBySlug[concept.CategorySlug]
		if categoryID == "" {
			result.SkippedAmbiguousServices++
			continue
		}
		if candidate.CurrentCategoryID == categoryID && candidate.Source == ServiceCategoryAssignmentSuggested {
			continue
		}
		execResult, err := tx.ExecContext(ctx, `
			UPDATE services
			SET service_category_id = $1::uuid,
			    service_category_source = 'suggested',
			    service_category_confidence = $2,
			    service_category_reviewed_by = NULL,
			    service_category_reviewed_at = NULL,
			    updated_at = now()
			WHERE id = $3
			  AND salon_id = $4
			  AND (service_category_source IN ('unassigned', 'suggested') OR service_category_id IS NULL)
		`, categoryID, concept.Confidence, candidate.ServiceID, salonID)
		if err != nil {
			return nil, err
		}
		if affected, err := execResult.RowsAffected(); err == nil && affected > 0 {
			result.SuggestedServices++
		}
	}
	for normalized, target := range aliasTargets {
		if normalized == "" || len(target.ServiceIDs) != 1 {
			result.SkippedServiceAliasConflicts++
			continue
		}
		serviceID := ""
		for id := range target.ServiceIDs {
			serviceID = id
		}
		created, updated, conflict, err := upsertSystemServiceAlias(ctx, tx, salonID, serviceID, serviceTaxonomyAliasRecord{
			Alias: target.Alias, Normalized: normalized, Confidence: target.Confidence,
		})
		if err != nil {
			return nil, err
		}
		if created {
			result.CreatedServiceAliases++
		}
		if updated {
			result.UpdatedSystemServiceAliases++
		}
		if conflict {
			result.SkippedServiceAliasConflicts++
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) CreateService(ctx context.Context, salonID string, ownerUserID string, provider string, input ServiceMutation) (*Service, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ServiceCategoryID) != "" {
		if err := r.ensureActiveCategory(ctx, salonID, ownerUserID, input.ServiceCategoryID); err != nil {
			return nil, err
		}
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = ProviderSquare
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}

	var serviceID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description, ai_description, duration_minutes,
			price_from, price_display, ai_bookable, active, sync_status, archived_at, last_synced_at, sync_error, source,
			service_category_id, service_category_source, service_category_confidence, service_category_reviewed_by, service_category_reviewed_at
		)
		VALUES (
			$1, $2, NULL, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, NULL, false, $8,
			'local_only', NULL, NULL, NULL, 'local',
			NULLIF($9, '')::uuid,
			CASE WHEN NULLIF($9, '') IS NULL THEN 'unassigned' ELSE 'manual' END,
			CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE 1.000 END,
			CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE $10::uuid END,
			CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE now() END
		)
		RETURNING id::text
	`, salonID, provider, input.Name, input.Description, input.AIDescription, input.DurationMinutes, servicePriceValue(input.PriceFrom), input.Active, input.ServiceCategoryID, ownerUserID).Scan(&serviceID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
		)
		VALUES ($1, 'service', $2, $3, NULL, 'local_only', NULL, NULL)
		ON CONFLICT (salon_id, entity_type, entity_id, provider)
		DO UPDATE SET sync_status = 'local_only',
		              provider_entity_id = NULL,
		              provider_version = NULL,
		              last_synced_at = NULL,
		              last_error = NULL,
		              updated_at = now()
	`, salonID, serviceID, provider); err != nil {
		return nil, err
	}
	if input.ConsultationProfile != nil {
		if err := UpsertServiceConsultationProfileTx(ctx, tx, salonID, serviceID, ownerUserID, *input.ConsultationProfile); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) UpdateService(ctx context.Context, salonID string, ownerUserID string, serviceID string, input ServiceMutation) (*Service, error) {
	current, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return nil, ErrValidation
	}
	if strings.TrimSpace(input.ServiceCategoryID) != "" {
		if err := r.ensureActiveCategory(ctx, salonID, ownerUserID, input.ServiceCategoryID); err != nil {
			return nil, err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET name = $1,
		    description = NULLIF($2, ''),
		    ai_description = NULLIF($3, ''),
		    duration_minutes = $4,
		    price_from = $5,
		    price_display = NULL,
		    active = $6,
		    ai_bookable = CASE WHEN $6 THEN ai_bookable ELSE false END,
		    service_category_id = NULLIF($9, '')::uuid,
		    service_category_source = CASE WHEN NULLIF($9, '') IS NULL THEN 'unassigned' ELSE 'manual' END,
		    service_category_confidence = CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE 1.000 END,
		    service_category_reviewed_by = CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE $10::uuid END,
		    service_category_reviewed_at = CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE now() END,
		    updated_at = now()
		WHERE id = $7
		  AND salon_id = $8
	`, input.Name, input.Description, input.AIDescription, input.DurationMinutes, servicePriceValue(input.PriceFrom), input.Active, serviceID, salonID, input.ServiceCategoryID, ownerUserID); err != nil {
		return nil, err
	}
	if input.ConsultationProfile != nil {
		if err := UpsertServiceConsultationProfileTx(ctx, tx, salonID, serviceID, ownerUserID, *input.ConsultationProfile); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) UpdateServiceOwnerControls(ctx context.Context, salonID string, ownerUserID string, serviceID string, input ServiceOwnerControlsMutation) (*Service, error) {
	current, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return nil, ErrValidation
	}
	if strings.TrimSpace(input.ServiceCategoryID) != "" {
		if err := r.ensureActiveCategory(ctx, salonID, ownerUserID, input.ServiceCategoryID); err != nil {
			return nil, err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE services
		SET ai_description = NULLIF($1, ''),
		    service_category_id = NULLIF($2, '')::uuid,
		    service_category_source = CASE WHEN NULLIF($2, '') IS NULL THEN 'unassigned' ELSE 'manual' END,
		    service_category_confidence = CASE WHEN NULLIF($2, '') IS NULL THEN NULL ELSE 1.000 END,
		    service_category_reviewed_by = CASE WHEN NULLIF($2, '') IS NULL THEN NULL ELSE $5::uuid END,
		    service_category_reviewed_at = CASE WHEN NULLIF($2, '') IS NULL THEN NULL ELSE now() END,
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
		  AND archived_at IS NULL
	`, input.AIDescription, input.ServiceCategoryID, serviceID, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrValidation
	}
	if input.ConsultationProfile != nil {
		if err := UpsertServiceConsultationProfileTx(ctx, tx, salonID, serviceID, ownerUserID, *input.ConsultationProfile); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) ArchiveService(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	current, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return current, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET active = false,
		    ai_bookable = false,
		    sync_status = 'archived',
		    archived_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND salon_id = $2
	`, serviceID, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET sync_status = 'archived',
		    updated_at = now()
		WHERE salon_id = $1
		  AND entity_type = 'service'
		  AND entity_id = $2
	`, salonID, serviceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.id::text, st.salon_id::text, st.pos_provider, COALESCE(st.pos_staff_id, ''), st.name,
		       COALESCE(st.phone, ''), COALESCE(st.email, ''), st.ai_bookable, st.active,
		       st.sync_status, st.archived_at, st.last_synced_at, COALESCE(st.sync_error, ''), st.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = st.salon_id
		             AND link.entity_type = 'staff'
		             AND link.entity_id = st.id
		             AND link.provider = st.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked
		FROM staff st
		WHERE st.salon_id = $1 AND st.pos_provider = $2
		ORDER BY (st.archived_at IS NOT NULL) ASC, st.active DESC, st.name ASC
	`, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffMember, 0)
	for rows.Next() {
		item, err := scanStaffMember(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateStaff(ctx context.Context, salonID string, ownerUserID string, provider string, input StaffMutation) (*StaffMember, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = ProviderSquare
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}

	var staffID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, phone, email, ai_bookable, active,
			sync_status, archived_at, last_synced_at, sync_error, source
		)
		VALUES ($1, $2, NULL, $3, NULLIF($4, ''), NULLIF($5, ''), false, $6, 'local_only', NULL, NULL, NULL, 'local')
		RETURNING id::text
	`, salonID, provider, input.Name, input.Phone, input.Email, input.Active).Scan(&staffID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
		)
		VALUES ($1, 'staff', $2, $3, NULL, 'local_only', NULL, NULL)
		ON CONFLICT (salon_id, entity_type, entity_id, provider)
		DO UPDATE SET sync_status = 'local_only',
		              provider_entity_id = NULL,
		              provider_version = NULL,
		              last_synced_at = NULL,
		              last_error = NULL,
		              updated_at = now()
	`, salonID, staffID, provider); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
}

func (r *Repository) UpdateStaff(ctx context.Context, salonID string, ownerUserID string, staffID string, input StaffMutation) (*StaffMember, error) {
	current, err := r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE staff
		SET name = $1,
		    phone = NULLIF($2, ''),
		    email = NULLIF($3, ''),
		    active = $4,
		    ai_bookable = CASE WHEN $4 THEN ai_bookable ELSE false END,
		    updated_at = now()
		WHERE id = $5
		  AND salon_id = $6
	`, input.Name, input.Phone, input.Email, input.Active, staffID, salonID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
}

func (r *Repository) ArchiveStaff(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error) {
	current, err := r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return current, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, salonID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE staff
		SET active = false,
		    ai_bookable = false,
		    sync_status = 'archived',
		    archived_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND salon_id = $2
	`, staffID, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET sync_status = 'archived',
		    updated_at = now()
		WHERE salon_id = $1
		  AND entity_type = 'staff'
		  AND entity_id = $2
	`, salonID, staffID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
}

func (r *Repository) GetServiceForSync(ctx context.Context, salonID string, serviceID string) (*Service, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT svc.id::text, svc.salon_id::text, svc.pos_provider, COALESCE(svc.pos_service_id, ''), COALESCE(svc.pos_service_version, 0),
		       svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''), svc.duration_minutes,
		       COALESCE(svc.price_from, 0), COALESCE(svc.price_display, ''), svc.ai_bookable, svc.active,
		       svc.sync_status, svc.archived_at, svc.last_synced_at, COALESCE(svc.sync_error, ''), svc.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = svc.salon_id
		             AND link.entity_type = 'service'
		             AND link.entity_id = svc.id
		             AND link.provider = svc.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked,
		       COALESCE(cat.id::text, ''), COALESCE(cat.name, ''), COALESCE(cat.slug, ''),
		       svc.service_category_source, COALESCE(svc.service_category_confidence, 0), svc.service_category_reviewed_at,
		       COALESCE(profile.id::text, ''), COALESCE(profile.status, 'draft'),
		       COALESCE(profile.recommended_outcomes, '[]'::jsonb), COALESCE(profile.compatible_current_systems, '[]'::jsonb),
		       COALESCE(profile.length_capabilities, '[]'::jsonb), COALESCE(profile.priority_tags, '[]'::jsonb),
		       COALESCE(profile.finish_options, '[]'::jsonb), COALESCE(profile.maintenance_note, ''),
		       COALESCE(profile.owner_approved_summary, ''), COALESCE(profile.revision, 0),
		       COALESCE(profile.updated_by::text, ''), profile.created_at, profile.updated_at
		FROM services svc
		LEFT JOIN service_categories cat ON cat.id = svc.service_category_id
		                                AND cat.salon_id = svc.salon_id
		                                AND cat.status = 'active'
		LEFT JOIN service_consultation_profiles profile ON profile.salon_id = svc.salon_id
		                                                  AND profile.service_id = svc.id
		WHERE svc.id = $1
		  AND svc.salon_id = $2
	`, serviceID, salonID)
	return scanService(row)
}

func (r *Repository) GetStaffForSync(ctx context.Context, salonID string, staffID string) (*StaffMember, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT st.id::text, st.salon_id::text, st.pos_provider, COALESCE(st.pos_staff_id, ''), st.name,
		       COALESCE(st.phone, ''), COALESCE(st.email, ''), st.ai_bookable, st.active,
		       st.sync_status, st.archived_at, st.last_synced_at, COALESCE(st.sync_error, ''), st.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = st.salon_id
		             AND link.entity_type = 'staff'
		             AND link.entity_id = st.id
		             AND link.provider = st.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked
		FROM staff st
		WHERE st.id = $1
		  AND st.salon_id = $2
	`, staffID, salonID)
	return scanStaffMember(row)
}

func (r *Repository) UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fence, err := lockAIBookableMutationFenceTx(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	target, err := lockServiceAIBookableTargetTx(ctx, tx, salonID, serviceID)
	if err != nil {
		return nil, err
	}
	if aiBookable {
		canEnable, err := serviceCanEnableAIBookingTx(ctx, tx, salonID, fence, target)
		if err != nil {
			return nil, err
		}
		if !canEnable {
			return nil, ErrValidation
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
	`, aiBookable, serviceID, salonID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fence, err := lockAIBookableMutationFenceTx(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	target, err := lockStaffAIBookableTargetTx(ctx, tx, salonID, staffID)
	if err != nil {
		return nil, err
	}
	if aiBookable {
		canEnable, err := staffCanEnableAIBookingTx(ctx, tx, salonID, fence, target)
		if err != nil {
			return nil, err
		}
		if !canEnable {
			return nil, ErrValidation
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE staff
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
	`, aiBookable, staffID, salonID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
}

func (r *Repository) ProviderMappingSummary(ctx context.Context, salonID string, ownerUserID string, provider string) (*ProviderMappingSummary, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = ProviderSquare
	}
	var summary ProviderMappingSummary
	err := r.db.QueryRowContext(ctx, `
		WITH service_rows AS (
			SELECT svc.id, svc.active, svc.ai_bookable, svc.archived_at, svc.sync_status, svc.duration_minutes,
			       COALESCE(svc.pos_service_version, 0) AS pos_service_version,
			       EXISTS (
			           SELECT 1
			           FROM pos_entity_links link
			           WHERE link.salon_id = svc.salon_id
			             AND link.entity_type = 'service'
			             AND link.entity_id = svc.id
			             AND link.provider = $2
			             AND link.sync_status = 'synced'
			             AND link.provider_entity_id IS NOT NULL
			             AND link.provider_entity_id <> ''
			       ) AS linked
			FROM services svc
			WHERE svc.salon_id = $1
			  AND svc.pos_provider = $2
		),
		staff_rows AS (
			SELECT st.id, st.active, st.ai_bookable, st.archived_at, st.sync_status,
			       EXISTS (
			           SELECT 1
			           FROM pos_entity_links link
			           WHERE link.salon_id = st.salon_id
			             AND link.entity_type = 'staff'
			             AND link.entity_id = st.id
			             AND link.provider = $2
			             AND link.sync_status = 'synced'
			             AND link.provider_entity_id IS NOT NULL
			             AND link.provider_entity_id <> ''
			       ) AS linked
			FROM staff st
			WHERE st.salon_id = $1
			  AND st.pos_provider = $2
		),
		customer_rows AS (
			SELECT c.id, c.active, c.archived_at, c.sync_status,
			       EXISTS (
			           SELECT 1
			           FROM pos_entity_links link
			           WHERE link.salon_id = c.salon_id
			             AND link.entity_type = 'customer'
			             AND link.entity_id = c.id
			             AND link.provider = $2
			             AND link.sync_status = 'synced'
			             AND link.provider_entity_id IS NOT NULL
			             AND link.provider_entity_id <> ''
			       ) AS linked
			FROM customers c
			WHERE c.salon_id = $1
		)
		SELECT
			(SELECT count(*)::int FROM service_rows WHERE archived_at IS NULL),
			(SELECT count(*)::int FROM staff_rows WHERE archived_at IS NULL),
			(SELECT count(*)::int FROM customer_rows WHERE archived_at IS NULL),
			(SELECT count(*)::int FROM service_rows WHERE active = true AND ai_bookable = true AND archived_at IS NULL AND sync_status = 'synced' AND linked = true AND duration_minutes > 0 AND pos_service_version > 0),
			(SELECT count(*)::int FROM staff_rows WHERE active = true AND ai_bookable = true AND archived_at IS NULL AND sync_status = 'synced' AND linked = true),
			(SELECT count(*)::int FROM service_rows WHERE linked = true),
			(SELECT count(*)::int FROM staff_rows WHERE linked = true),
			(SELECT count(*)::int FROM customer_rows WHERE linked = true),
			(SELECT count(*)::int FROM service_rows WHERE active = true AND archived_at IS NULL AND (sync_status <> 'synced' OR linked = false)),
			(SELECT count(*)::int FROM staff_rows WHERE active = true AND archived_at IS NULL AND (sync_status <> 'synced' OR linked = false)),
			(
				(SELECT count(*) FROM service_rows WHERE sync_status = 'sync_failed') +
				(SELECT count(*) FROM staff_rows WHERE sync_status = 'sync_failed') +
				(SELECT count(*) FROM customer_rows WHERE sync_status = 'sync_failed')
			)::int
	`, salonID, provider).Scan(
		&summary.ServiceCount,
		&summary.StaffCount,
		&summary.CustomerCount,
		&summary.BookableServiceCount,
		&summary.BookableStaffCount,
		&summary.LinkedServiceCount,
		&summary.LinkedStaffCount,
		&summary.LinkedCustomerCount,
		&summary.UnmappedServiceCount,
		&summary.UnmappedStaffCount,
		&summary.SyncFailedCount,
	)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (r *Repository) CreateProviderSwitchRun(ctx context.Context, input ProviderSwitchRunMutation) (*ProviderSwitchRun, error) {
	if err := r.EnsureSalonOwner(ctx, input.SalonID, input.OwnerUserID); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = SwitchRunStatusDraft
	}
	createdBy := strings.TrimSpace(input.CreatedByUserID)
	if createdBy == "" {
		createdBy = strings.TrimSpace(input.OwnerUserID)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO pos_provider_switch_runs (
			salon_id, from_provider, to_provider, status, blocked_reason, dry_run_ready, created_by_user_id
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, NULLIF($7, '')::uuid)
		RETURNING id::text, salon_id::text, from_provider, to_provider, status,
		          COALESCE(blocked_reason, ''), dry_run_ready, activated_at, cancelled_at,
		          COALESCE(created_by_user_id::text, ''), created_at, updated_at
	`, input.SalonID, input.FromProvider, input.ToProvider, status, input.BlockedReason, input.DryRunReady, createdBy)
	return scanProviderSwitchRun(row)
}

func (r *Repository) GetProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchRun, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT run.id::text, run.salon_id::text, run.from_provider, run.to_provider, run.status,
		       COALESCE(run.blocked_reason, ''), run.dry_run_ready, run.activated_at, run.cancelled_at,
		       COALESCE(run.created_by_user_id::text, ''), run.created_at, run.updated_at
		FROM pos_provider_switch_runs run
		JOIN salons salon ON salon.id = run.salon_id
		WHERE run.id = $1
		  AND run.salon_id = $2
		  AND salon.owner_user_id = $3
	`, runID, salonID, ownerUserID)
	return scanProviderSwitchRun(row)
}

func (r *Repository) LatestProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string) (*ProviderSwitchRun, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT run.id::text, run.salon_id::text, run.from_provider, run.to_provider, run.status,
		       COALESCE(run.blocked_reason, ''), run.dry_run_ready, run.activated_at, run.cancelled_at,
		       COALESCE(run.created_by_user_id::text, ''), run.created_at, run.updated_at
		FROM pos_provider_switch_runs run
		JOIN salons salon ON salon.id = run.salon_id
		WHERE run.salon_id = $1
		  AND salon.owner_user_id = $2
		ORDER BY run.created_at DESC
		LIMIT 1
	`, salonID, ownerUserID)
	run, err := scanProviderSwitchRun(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return run, err
}

func (r *Repository) ListProviderSwitchMatches(ctx context.Context, salonID string, ownerUserID string, runID string) ([]ProviderSwitchMatch, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT match.id::text, match.run_id::text, match.salon_id::text, match.entity_type,
		       COALESCE(match.canonical_entity_id::text, ''), COALESCE(match.canonical_name, ''),
		       match.provider_entity_id, match.provider_name, COALESCE(match.provider_phone, ''),
		       COALESCE(match.provider_email, ''), COALESCE(match.provider_duration_minutes, 0),
		       match.match_status, match.match_confidence, COALESCE(match.match_reason, ''),
		       match.created_at, match.updated_at
		FROM pos_provider_switch_matches match
		JOIN pos_provider_switch_runs run ON run.id = match.run_id
		JOIN salons salon ON salon.id = run.salon_id
		WHERE match.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND match.run_id = $3
		ORDER BY match.entity_type ASC,
		         CASE match.match_status
		           WHEN 'conflict' THEN 0
		           WHEN 'unmatched' THEN 1
		           WHEN 'suggested' THEN 2
		           WHEN 'confirmed' THEN 3
		           ELSE 4
		         END,
		         match.provider_name ASC
	`, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ProviderSwitchMatch, 0)
	for rows.Next() {
		item, err := scanProviderSwitchMatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) UpdateProviderSwitchMatch(ctx context.Context, input ProviderSwitchMatchUpdateMutation) (*ProviderSwitchMatch, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE pos_provider_switch_matches AS match
		SET canonical_entity_id = NULLIF($5, '')::uuid,
		    canonical_name = NULLIF($6, ''),
		    match_status = $7,
		    match_confidence = $8,
		    match_reason = NULLIF($9, ''),
		    updated_at = now()
		FROM pos_provider_switch_runs run
		JOIN salons salon ON salon.id = run.salon_id
		WHERE match.id = $1
		  AND match.run_id = run.id
		  AND match.run_id = $2
		  AND match.salon_id = $3
		  AND salon.owner_user_id = $4
		RETURNING match.id::text, match.run_id::text, match.salon_id::text, match.entity_type,
		          COALESCE(match.canonical_entity_id::text, ''), COALESCE(match.canonical_name, ''),
		          match.provider_entity_id, match.provider_name, COALESCE(match.provider_phone, ''),
		          COALESCE(match.provider_email, ''), COALESCE(match.provider_duration_minutes, 0),
		          match.match_status, match.match_confidence, COALESCE(match.match_reason, ''),
		          match.created_at, match.updated_at
	`, input.MatchID, input.RunID, input.SalonID, input.OwnerUserID, input.CanonicalEntityID, input.CanonicalName, input.MatchStatus, input.MatchConfidence, input.MatchReason)
	return scanProviderSwitchMatch(row)
}

func (r *Repository) UpdateProviderSwitchRunStatus(ctx context.Context, salonID string, ownerUserID string, runID string, status string, blockedReason string) (*ProviderSwitchRun, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE pos_provider_switch_runs AS run
		SET status = $4,
		    blocked_reason = NULLIF($5, ''),
		    updated_at = now()
		FROM salons salon
		WHERE run.salon_id = salon.id
		  AND run.id = $1
		  AND run.salon_id = $2
		  AND salon.owner_user_id = $3
		RETURNING run.id::text, run.salon_id::text, run.from_provider, run.to_provider, run.status,
		          COALESCE(run.blocked_reason, ''), run.dry_run_ready, run.activated_at, run.cancelled_at,
		          COALESCE(run.created_by_user_id::text, ''), run.created_at, run.updated_at
	`, runID, salonID, ownerUserID, status, blockedReason)
	return scanProviderSwitchRun(row)
}

func (r *Repository) ListProviderSwitchServiceCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	source, err := r.listProviderSwitchServiceCandidates(ctx, salonID, fromProvider, false)
	if err != nil {
		return nil, nil, err
	}
	target, err := r.listProviderSwitchServiceCandidates(ctx, salonID, toProvider, true)
	if err != nil {
		return nil, nil, err
	}
	return source, target, nil
}

func (r *Repository) ListProviderSwitchStaffCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	source, err := r.listProviderSwitchStaffCandidates(ctx, salonID, fromProvider, false)
	if err != nil {
		return nil, nil, err
	}
	target, err := r.listProviderSwitchStaffCandidates(ctx, salonID, toProvider, true)
	if err != nil {
		return nil, nil, err
	}
	return source, target, nil
}

func (r *Repository) ListProviderSwitchCustomerCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	source, err := r.listProviderSwitchCustomerCandidates(ctx, salonID, fromProvider, false)
	if err != nil {
		return nil, nil, err
	}
	target, err := r.listProviderSwitchCustomerCandidates(ctx, salonID, toProvider, true)
	if err != nil {
		return nil, nil, err
	}
	return source, target, nil
}

func (r *Repository) ReplaceProviderSwitchMatches(ctx context.Context, salonID string, runID string, matches []ProviderSwitchMatchMutation) ([]ProviderSwitchMatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pos_provider_switch_runs
			WHERE id = $1
			  AND salon_id = $2
		)
	`, runID, salonID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM pos_provider_switch_matches
		WHERE run_id = $1
		  AND salon_id = $2
	`, runID, salonID); err != nil {
		return nil, err
	}

	inserted := make([]ProviderSwitchMatch, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.EntityType) == "" || strings.TrimSpace(match.ProviderEntityID) == "" || strings.TrimSpace(match.ProviderName) == "" {
			continue
		}
		row := tx.QueryRowContext(ctx, `
			INSERT INTO pos_provider_switch_matches (
				run_id, salon_id, entity_type, canonical_entity_id, canonical_name,
				provider_entity_id, provider_name, provider_phone, provider_email,
				provider_duration_minutes, match_status, match_confidence, match_reason
			)
			VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, ''), $6, $7,
			        NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, 0), $11, $12, NULLIF($13, ''))
			RETURNING id::text, run_id::text, salon_id::text, entity_type,
			          COALESCE(canonical_entity_id::text, ''), COALESCE(canonical_name, ''),
			          provider_entity_id, provider_name, COALESCE(provider_phone, ''),
			          COALESCE(provider_email, ''), COALESCE(provider_duration_minutes, 0),
			          match_status, match_confidence, COALESCE(match_reason, ''),
			          created_at, updated_at
		`, runID, salonID, match.EntityType, match.CanonicalEntityID, match.CanonicalName, match.ProviderEntityID, match.ProviderName, match.ProviderPhone, match.ProviderEmail, match.ProviderDurationMinutes, match.MatchStatus, match.MatchConfidence, match.MatchReason)
		item, err := scanProviderSwitchMatch(row)
		if err != nil {
			return nil, err
		}
		inserted = append(inserted, *item)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inserted, nil
}

func (r *Repository) listProviderSwitchServiceCandidates(ctx context.Context, salonID string, provider string, requireProviderID bool) ([]ProviderSwitchEntityCandidate, error) {
	query := `
		SELECT id::text, COALESCE(pos_service_id, ''), name, '' AS phone, '' AS email, duration_minutes
		FROM services
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND archived_at IS NULL
	`
	if requireProviderID {
		query += " AND COALESCE(pos_service_id, '') <> ''"
	}
	query += " ORDER BY name ASC"
	rows, err := r.db.QueryContext(ctx, query, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProviderSwitchCandidateRows(rows)
}

func (r *Repository) listProviderSwitchStaffCandidates(ctx context.Context, salonID string, provider string, requireProviderID bool) ([]ProviderSwitchEntityCandidate, error) {
	query := `
		SELECT id::text, COALESCE(pos_staff_id, ''), name, COALESCE(phone, ''), COALESCE(email, ''), 0 AS duration_minutes
		FROM staff
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND archived_at IS NULL
	`
	if requireProviderID {
		query += " AND COALESCE(pos_staff_id, '') <> ''"
	}
	query += " ORDER BY name ASC"
	rows, err := r.db.QueryContext(ctx, query, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProviderSwitchCandidateRows(rows)
}

func (r *Repository) listProviderSwitchCustomerCandidates(ctx context.Context, salonID string, provider string, requireProviderID bool) ([]ProviderSwitchEntityCandidate, error) {
	query := `
		SELECT c.id::text, COALESCE(link.provider_entity_id, ''), c.name,
		       COALESCE(c.phone, ''), COALESCE(c.email, ''), 0 AS duration_minutes
		FROM customers c
		LEFT JOIN pos_entity_links link
		  ON link.salon_id = c.salon_id
		 AND link.entity_type = 'customer'
		 AND link.entity_id = c.id
		 AND link.provider = $2
		 AND link.sync_status <> 'archived'
		WHERE c.salon_id = $1
		  AND c.archived_at IS NULL
	`
	if requireProviderID {
		query += " AND COALESCE(link.provider_entity_id, '') <> ''"
	}
	query += " ORDER BY c.name ASC"
	rows, err := r.db.QueryContext(ctx, query, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProviderSwitchCandidateRows(rows)
}

func (r *Repository) GetServiceForOwner(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) getServiceForOwner(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT svc.id::text, svc.salon_id::text, svc.pos_provider, COALESCE(svc.pos_service_id, ''), COALESCE(svc.pos_service_version, 0),
		       svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''), svc.duration_minutes,
		       COALESCE(svc.price_from, 0), COALESCE(svc.price_display, ''), svc.ai_bookable, svc.active,
		       svc.sync_status, svc.archived_at, svc.last_synced_at, COALESCE(svc.sync_error, ''), svc.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = svc.salon_id
		             AND link.entity_type = 'service'
		             AND link.entity_id = svc.id
		             AND link.provider = svc.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked,
		       COALESCE(cat.id::text, ''), COALESCE(cat.name, ''), COALESCE(cat.slug, ''),
		       svc.service_category_source, COALESCE(svc.service_category_confidence, 0), svc.service_category_reviewed_at,
		       COALESCE(profile.id::text, ''), COALESCE(profile.status, 'draft'),
		       COALESCE(profile.recommended_outcomes, '[]'::jsonb), COALESCE(profile.compatible_current_systems, '[]'::jsonb),
		       COALESCE(profile.length_capabilities, '[]'::jsonb), COALESCE(profile.priority_tags, '[]'::jsonb),
		       COALESCE(profile.finish_options, '[]'::jsonb), COALESCE(profile.maintenance_note, ''),
		       COALESCE(profile.owner_approved_summary, ''), COALESCE(profile.revision, 0),
		       COALESCE(profile.updated_by::text, ''), profile.created_at, profile.updated_at
		FROM services svc
		LEFT JOIN service_categories cat ON cat.id = svc.service_category_id
		                                AND cat.salon_id = svc.salon_id
		                                AND cat.status = 'active'
		LEFT JOIN service_consultation_profiles profile ON profile.salon_id = svc.salon_id
		                                                  AND profile.service_id = svc.id
		WHERE svc.id = $1
		  AND svc.salon_id = $2
	`, serviceID, salonID)
	return scanService(row)
}

func (r *Repository) GetStaffForOwner(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error) {
	return r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
}

func (r *Repository) getStaffForOwner(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT st.id::text, st.salon_id::text, st.pos_provider, COALESCE(st.pos_staff_id, ''), st.name,
		       COALESCE(st.phone, ''), COALESCE(st.email, ''), st.ai_bookable, st.active,
		       st.sync_status, st.archived_at, st.last_synced_at, COALESCE(st.sync_error, ''), st.source,
		       EXISTS (
		           SELECT 1
		           FROM pos_entity_links link
		           WHERE link.salon_id = st.salon_id
		             AND link.entity_type = 'staff'
		             AND link.entity_id = st.id
		             AND link.provider = st.pos_provider
		             AND link.provider_entity_id IS NOT NULL
		             AND link.sync_status = 'synced'
		       ) AS pos_linked
		FROM staff st
		JOIN salons salon ON salon.id = st.salon_id
		WHERE st.id = $1
		  AND st.salon_id = $2
		  AND salon.owner_user_id = $3
	`, staffID, salonID, ownerUserID)
	return scanStaffMember(row)
}

func markEntitySyncing(ctx context.Context, tx *sql.Tx, job SyncJob) error {
	switch job.EntityType {
	case EntityTypeService:
		if _, err := tx.ExecContext(ctx, `
			UPDATE services
			SET sync_status = 'syncing',
			    sync_error = NULL,
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider); err != nil {
			return err
		}
	case EntityTypeStaff:
		if _, err := tx.ExecContext(ctx, `
			UPDATE staff
			SET sync_status = 'syncing',
			    sync_error = NULL,
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider); err != nil {
			return err
		}
	default:
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, sync_status, last_error
		)
		VALUES ($1, $2, $3, $4, 'syncing', NULL)
		ON CONFLICT (salon_id, entity_type, entity_id, provider)
		DO UPDATE SET sync_status = 'syncing',
		              last_error = NULL,
		              updated_at = now()
	`, job.SalonID, job.EntityType, job.EntityID, job.Provider)
	return err
}

func markEntitySyncSucceeded(ctx context.Context, tx *sql.Tx, job SyncJob, result ProviderSyncResult) error {
	syncStatus := SyncStatusSynced
	if job.Operation == SyncOperationArchiveService || job.Operation == SyncOperationArchiveStaff {
		syncStatus = SyncStatusArchived
	}
	switch job.EntityType {
	case EntityTypeService:
		if _, err := tx.ExecContext(ctx, `
			UPDATE services
			SET pos_service_id = COALESCE(NULLIF($4, ''), pos_service_id),
			    pos_service_version = CASE WHEN $5 > 0 THEN $5 ELSE pos_service_version END,
			    sync_status = $6,
			    last_synced_at = now(),
			    sync_error = NULL,
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider, result.ProviderEntityID, result.ProviderVersion, syncStatus); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO pos_entity_links (
				salon_id, entity_type, entity_id, provider, provider_entity_id, provider_version, sync_status, last_synced_at, last_error
			)
			VALUES ($1, 'service', $2, $3, NULLIF($4, ''), NULLIF($5::bigint, 0), $6, now(), NULL)
			ON CONFLICT (salon_id, entity_type, entity_id, provider)
			DO UPDATE SET provider_entity_id = COALESCE(EXCLUDED.provider_entity_id, pos_entity_links.provider_entity_id),
			              provider_version = COALESCE(EXCLUDED.provider_version, pos_entity_links.provider_version),
			              sync_status = EXCLUDED.sync_status,
			              last_synced_at = now(),
			              last_error = NULL,
			              updated_at = now()
		`, job.SalonID, job.EntityID, job.Provider, result.ProviderEntityID, result.ProviderVersion, syncStatus)
		return err
	case EntityTypeStaff:
		if _, err := tx.ExecContext(ctx, `
			UPDATE staff
			SET pos_staff_id = COALESCE(NULLIF($4, ''), pos_staff_id),
			    sync_status = $5,
			    last_synced_at = now(),
			    sync_error = NULL,
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider, result.ProviderEntityID, syncStatus); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO pos_entity_links (
				salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
			)
			VALUES ($1, 'staff', $2, $3, NULLIF($4, ''), $5, now(), NULL)
			ON CONFLICT (salon_id, entity_type, entity_id, provider)
			DO UPDATE SET provider_entity_id = COALESCE(EXCLUDED.provider_entity_id, pos_entity_links.provider_entity_id),
			              sync_status = EXCLUDED.sync_status,
			              last_synced_at = now(),
			              last_error = NULL,
			              updated_at = now()
		`, job.SalonID, job.EntityID, job.Provider, result.ProviderEntityID, syncStatus)
		return err
	default:
		return nil
	}
}

func markEntitySyncFailed(ctx context.Context, tx *sql.Tx, job SyncJob, message string) error {
	switch job.EntityType {
	case EntityTypeService:
		if _, err := tx.ExecContext(ctx, `
			UPDATE services
			SET sync_status = 'sync_failed',
			    sync_error = NULLIF($4, ''),
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider, message); err != nil {
			return err
		}
	case EntityTypeStaff:
		if _, err := tx.ExecContext(ctx, `
			UPDATE staff
			SET sync_status = 'sync_failed',
			    sync_error = NULLIF($4, ''),
			    updated_at = now()
			WHERE salon_id = $1
			  AND id = $2
			  AND pos_provider = $3
		`, job.SalonID, job.EntityID, job.Provider, message); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET sync_status = 'sync_failed',
		    last_error = NULLIF($5, ''),
		    updated_at = now()
		WHERE salon_id = $1
		  AND entity_type = $2
		  AND entity_id = $3
		  AND provider = $4
	`, job.SalonID, job.EntityType, job.EntityID, job.Provider, message)
	return err
}

type aiBookableMutationFence struct {
	ActiveProvider             string
	SchedulingAuthority        string
	SchedulingAuthorityVersion int64
	HasSchedulingSettings      bool
}

type serviceAIBookableTarget struct {
	ID              string
	Provider        string
	ProviderVersion int64
	SyncStatus      string
	Active          bool
	Archived        bool
	DurationMinutes int
}

type staffAIBookableTarget struct {
	ID         string
	Provider   string
	SyncStatus string
	Active     bool
	Archived   bool
}

type providerLinkEvidence struct {
	ProviderEntityID   string
	ProviderVersion    int64
	HasProviderVersion bool
	SyncStatus         string
}

func lockAIBookableMutationFenceTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) (aiBookableMutationFence, error) {
	var fence aiBookableMutationFence
	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		bookingCalendarReconciliationLockPrefix+salonID,
	); err != nil {
		return fence, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(BTRIM(active_pos_provider), '')
		FROM salons
		WHERE id = $1
		  AND (
		      public.has_active_tenant_membership(id, $2::uuid)
		      OR public.app_active_support_authorization($2::uuid, id, 'services.write')
		  )
		FOR UPDATE
	`, salonID, ownerUserID).Scan(&fence.ActiveProvider); errors.Is(err, sql.ErrNoRows) {
		return fence, ErrNotFound
	} else if err != nil {
		return fence, err
	}
	err := tx.QueryRowContext(ctx, `
		SELECT BTRIM(scheduling_authority), scheduling_authority_version
		FROM salon_settings
		WHERE salon_id = $1
		FOR UPDATE
	`, salonID).Scan(&fence.SchedulingAuthority, &fence.SchedulingAuthorityVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return fence, nil
	}
	if err != nil {
		return fence, err
	}
	fence.HasSchedulingSettings = true
	return fence, nil
}

func lockServiceAIBookableTargetTx(ctx context.Context, tx *sql.Tx, salonID string, serviceID string) (serviceAIBookableTarget, error) {
	var target serviceAIBookableTarget
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, BTRIM(pos_provider), COALESCE(pos_service_version, 0), sync_status, active,
		       archived_at IS NOT NULL, duration_minutes
		FROM services
		WHERE salon_id = $1 AND id = $2
		FOR UPDATE
	`, salonID, serviceID).Scan(
		&target.ID,
		&target.Provider,
		&target.ProviderVersion,
		&target.SyncStatus,
		&target.Active,
		&target.Archived,
		&target.DurationMinutes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return target, ErrNotFound
	}
	return target, err
}

func lockStaffAIBookableTargetTx(ctx context.Context, tx *sql.Tx, salonID string, staffID string) (staffAIBookableTarget, error) {
	var target staffAIBookableTarget
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, BTRIM(pos_provider), sync_status, active, archived_at IS NOT NULL
		FROM staff
		WHERE salon_id = $1 AND id = $2
		FOR UPDATE
	`, salonID, staffID).Scan(
		&target.ID,
		&target.Provider,
		&target.SyncStatus,
		&target.Active,
		&target.Archived,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return target, ErrNotFound
	}
	return target, err
}

func serviceCanEnableAIBookingTx(ctx context.Context, tx *sql.Tx, salonID string, fence aiBookableMutationFence, target serviceAIBookableTarget) (bool, error) {
	if !fence.HasSchedulingSettings || fence.SchedulingAuthorityVersion < 1 || !target.Active || target.Archived || target.DurationMinutes <= 0 {
		return false, nil
	}
	switch fence.SchedulingAuthority {
	case schedulingAuthorityOwnerManual, schedulingAuthorityManleAICalendar:
		return true, nil
	case schedulingAuthorityExternalProvider:
		if fence.ActiveProvider == "" || target.Provider == "" || target.Provider != fence.ActiveProvider ||
			target.SyncStatus != SyncStatusSynced {
			return false, nil
		}
		link, found, err := lockSyncedProviderLinkTx(ctx, tx, salonID, EntityTypeService, target.ID, fence.ActiveProvider)
		if err != nil || !found {
			return false, err
		}
		providerVersion := target.ProviderVersion
		if link.HasProviderVersion {
			providerVersion = link.ProviderVersion
		}
		return link.SyncStatus == SyncStatusSynced && link.ProviderEntityID != "" && providerVersion > 0, nil
	default:
		return false, nil
	}
}

func staffCanEnableAIBookingTx(ctx context.Context, tx *sql.Tx, salonID string, fence aiBookableMutationFence, target staffAIBookableTarget) (bool, error) {
	if !fence.HasSchedulingSettings || fence.SchedulingAuthorityVersion < 1 || !target.Active || target.Archived {
		return false, nil
	}
	switch fence.SchedulingAuthority {
	case schedulingAuthorityOwnerManual, schedulingAuthorityManleAICalendar:
		return true, nil
	case schedulingAuthorityExternalProvider:
		if fence.ActiveProvider == "" || target.Provider == "" || target.Provider != fence.ActiveProvider ||
			target.SyncStatus != SyncStatusSynced {
			return false, nil
		}
		link, found, err := lockSyncedProviderLinkTx(ctx, tx, salonID, EntityTypeStaff, target.ID, fence.ActiveProvider)
		if err != nil || !found {
			return false, err
		}
		return link.SyncStatus == SyncStatusSynced && link.ProviderEntityID != "", nil
	default:
		return false, nil
	}
}

func lockSyncedProviderLinkTx(ctx context.Context, tx *sql.Tx, salonID string, entityType string, entityID string, provider string) (providerLinkEvidence, bool, error) {
	var evidence providerLinkEvidence
	var providerEntityID sql.NullString
	var providerVersion sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT provider_entity_id, provider_version, sync_status
		FROM pos_entity_links
		WHERE salon_id = $1
		  AND entity_type = $2
		  AND entity_id = $3
		  AND provider = $4
		FOR UPDATE
	`, salonID, entityType, entityID, provider).Scan(&providerEntityID, &providerVersion, &evidence.SyncStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence, false, nil
	}
	if err != nil {
		return evidence, false, err
	}
	if providerEntityID.Valid {
		evidence.ProviderEntityID = strings.TrimSpace(providerEntityID.String)
	}
	if providerVersion.Valid {
		evidence.ProviderVersion = providerVersion.Int64
		evidence.HasProviderVersion = true
	}
	return evidence, true, nil
}

func servicePriceValue(price *float64) any {
	if price == nil {
		return nil
	}
	return *price
}

// UpsertServiceConsultationProfileTx keeps profile writes idempotent inside an
// existing salon-scoped transaction. Identical data leaves the revision intact.
func UpsertServiceConsultationProfileTx(ctx context.Context, tx *sql.Tx, salonID string, serviceID string, ownerUserID string, input ServiceConsultationProfileMutation) error {
	outcomes, err := json.Marshal(input.RecommendedOutcomes)
	if err != nil {
		return err
	}
	systems, err := json.Marshal(input.CompatibleCurrentSystems)
	if err != nil {
		return err
	}
	lengths, err := json.Marshal(input.LengthCapabilities)
	if err != nil {
		return err
	}
	priorities, err := json.Marshal(input.PriorityTags)
	if err != nil {
		return err
	}
	finishes, err := json.Marshal(input.FinishOptions)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO service_consultation_profiles (
			salon_id, service_id, status, recommended_outcomes, compatible_current_systems,
			length_capabilities, priority_tags, finish_options, maintenance_note,
			owner_approved_summary, updated_by
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, NULLIF($9, ''), NULLIF($10, ''), $11)
		ON CONFLICT (salon_id, service_id)
		DO UPDATE SET status = EXCLUDED.status,
		              recommended_outcomes = EXCLUDED.recommended_outcomes,
		              compatible_current_systems = EXCLUDED.compatible_current_systems,
		              length_capabilities = EXCLUDED.length_capabilities,
		              priority_tags = EXCLUDED.priority_tags,
		              finish_options = EXCLUDED.finish_options,
		              maintenance_note = EXCLUDED.maintenance_note,
		              owner_approved_summary = EXCLUDED.owner_approved_summary,
		              revision = service_consultation_profiles.revision + 1,
		              updated_by = EXCLUDED.updated_by,
		              updated_at = now()
		WHERE (service_consultation_profiles.status,
		       service_consultation_profiles.recommended_outcomes,
		       service_consultation_profiles.compatible_current_systems,
		       service_consultation_profiles.length_capabilities,
		       service_consultation_profiles.priority_tags,
		       service_consultation_profiles.finish_options,
		       service_consultation_profiles.maintenance_note,
		       service_consultation_profiles.owner_approved_summary)
		      IS DISTINCT FROM
		      (EXCLUDED.status,
		       EXCLUDED.recommended_outcomes,
		       EXCLUDED.compatible_current_systems,
		       EXCLUDED.length_capabilities,
		       EXCLUDED.priority_tags,
		       EXCLUDED.finish_options,
		       EXCLUDED.maintenance_note,
		       EXCLUDED.owner_approved_summary)
	`, salonID, serviceID, input.Status, string(outcomes), string(systems), string(lengths), string(priorities), string(finishes), input.MaintenanceNote, input.OwnerApprovedSummary, ownerUserID)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(row rowScanner) (*Service, error) {
	var item Service
	var archivedAt sql.NullTime
	var lastSyncedAt sql.NullTime
	var categoryReviewedAt sql.NullTime
	var profileID string
	var profileStatus string
	var profileOutcomes []byte
	var profileSystems []byte
	var profileLengths []byte
	var profilePriorities []byte
	var profileFinishes []byte
	var profileMaintenanceNote string
	var profileOwnerSummary string
	var profileRevision int
	var profileUpdatedBy string
	var profileCreatedAt sql.NullTime
	var profileUpdatedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.POSProvider,
		&item.POSServiceID,
		&item.POSServiceVersion,
		&item.Name,
		&item.Description,
		&item.AIDescription,
		&item.DurationMinutes,
		&item.PriceFrom,
		&item.PriceDisplay,
		&item.AIBookable,
		&item.Active,
		&item.SyncStatus,
		&archivedAt,
		&lastSyncedAt,
		&item.SyncError,
		&item.Source,
		&item.POSLinked,
		&item.ServiceCategoryID,
		&item.CategoryName,
		&item.CategorySlug,
		&item.CategorySource,
		&item.CategoryConfidence,
		&categoryReviewedAt,
		&profileID,
		&profileStatus,
		&profileOutcomes,
		&profileSystems,
		&profileLengths,
		&profilePriorities,
		&profileFinishes,
		&profileMaintenanceNote,
		&profileOwnerSummary,
		&profileRevision,
		&profileUpdatedBy,
		&profileCreatedAt,
		&profileUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archivedAt.Valid {
		item.ArchivedAt = &archivedAt.Time
	}
	if lastSyncedAt.Valid {
		item.LastSyncedAt = &lastSyncedAt.Time
	}
	if categoryReviewedAt.Valid {
		item.CategoryReviewedAt = &categoryReviewedAt.Time
	}
	if profileID != "" {
		profile := &ServiceConsultationProfile{
			ID: profileID, SalonID: item.SalonID, ServiceID: item.ID, Status: profileStatus,
			MaintenanceNote: profileMaintenanceNote, OwnerApprovedSummary: profileOwnerSummary,
			Revision: profileRevision, UpdatedBy: profileUpdatedBy,
		}
		if err := json.Unmarshal(profileOutcomes, &profile.RecommendedOutcomes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profileSystems, &profile.CompatibleCurrentSystems); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profileLengths, &profile.LengthCapabilities); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profilePriorities, &profile.PriorityTags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profileFinishes, &profile.FinishOptions); err != nil {
			return nil, err
		}
		if profileCreatedAt.Valid {
			profile.CreatedAt = &profileCreatedAt.Time
		}
		if profileUpdatedAt.Valid {
			profile.UpdatedAt = &profileUpdatedAt.Time
		}
		item.ConsultationProfile = profile
	}
	return &item, nil
}

func scanServiceCategory(row rowScanner) (*ServiceCategory, error) {
	var item ServiceCategory
	var reviewedAt sql.NullTime
	var archivedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.Name,
		&item.Slug,
		&item.Description,
		&item.Status,
		&item.SortOrder,
		&item.Source,
		&item.ServiceCount,
		&reviewedAt,
		&archivedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if reviewedAt.Valid {
		item.ReviewedAt = &reviewedAt.Time
	}
	if archivedAt.Valid {
		item.ArchivedAt = &archivedAt.Time
	}
	return &item, nil
}

func scanServiceCategoryAlias(row rowScanner) (*ServiceCategoryAlias, error) {
	var item ServiceCategoryAlias
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.CategoryID,
		&item.CategoryName,
		&item.Alias,
		&item.NormalizedAlias,
		&item.Source,
		&item.Status,
		&item.Confidence,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func upsertSystemServiceCategory(ctx context.Context, tx *sql.Tx, salonID string, category serviceTaxonomyCategoryRecord) (string, string, bool, bool, error) {
	slug := strings.TrimSpace(category.Slug)
	var existingID, existingStatus, existingSource string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, status, source
		FROM service_categories
		WHERE salon_id = $1
		  AND slug = $2
	`, salonID, slug).Scan(&existingID, &existingStatus, &existingSource)
	if errors.Is(err, sql.ErrNoRows) {
		var categoryID, status string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO service_categories (
				salon_id, name, slug, description, sort_order, source, status
			)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, 'system', 'active')
			RETURNING id::text, status
		`, salonID, strings.TrimSpace(category.Name), slug, strings.TrimSpace(category.Description), category.SortOrder).Scan(&categoryID, &status)
		return categoryID, status, true, false, err
	}
	if err != nil {
		return "", "", false, false, err
	}
	if existingSource != ServiceCategorySourceSystem {
		return existingID, existingStatus, false, false, nil
	}
	restored := existingStatus == ServiceCategoryStatusArchived
	var status string
	err = tx.QueryRowContext(ctx, `
		UPDATE service_categories
		SET name = $1,
		    description = NULLIF($2, ''),
		    sort_order = $3,
		    status = 'active',
		    archived_at = NULL,
		    updated_at = now()
		WHERE id = $4
		  AND salon_id = $5
		RETURNING status
	`, strings.TrimSpace(category.Name), strings.TrimSpace(category.Description), category.SortOrder, existingID, salonID).Scan(&status)
	return existingID, status, false, restored, err
}

func upsertSystemServiceCategoryAlias(ctx context.Context, tx *sql.Tx, salonID string, categoryID string, record serviceTaxonomyAliasRecord) (bool, bool, bool, error) {
	alias := strings.TrimSpace(record.Alias)
	normalized := strings.TrimSpace(record.Normalized)
	if alias == "" || normalized == "" {
		return false, false, false, nil
	}
	if err := lockServiceAliasOwnershipTx(ctx, tx, salonID, normalized); err != nil {
		return false, false, false, err
	}
	var serviceAliasConflict bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_aliases
			WHERE salon_id = $1
			  AND normalized_alias = $2
			  AND status = 'active'
		)
	`, salonID, normalized).Scan(&serviceAliasConflict); err != nil {
		return false, false, false, err
	}
	if serviceAliasConflict {
		return false, false, true, nil
	}

	var existingID, existingCategoryID, existingAlias, existingSource, existingStatus string
	var existingConfidence float64
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, category_id::text, alias, source, status, confidence
		FROM service_category_aliases
		WHERE salon_id = $1
		  AND normalized_alias = $2
	`, salonID, normalized).Scan(&existingID, &existingCategoryID, &existingAlias, &existingSource, &existingStatus, &existingConfidence)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO service_category_aliases (
				salon_id, category_id, alias, normalized_alias, source, status, confidence
			)
			VALUES ($1, $2, $3, $4, 'system', 'active', $5)
		`, salonID, categoryID, alias, normalized, record.Confidence); err != nil {
			if isServiceAliasOwnershipConflict(err) {
				return false, false, false, ErrValidation
			}
			return false, false, false, err
		}
		return true, false, false, nil
	}
	if err != nil {
		return false, false, false, err
	}
	if existingSource != ServiceCategoryAliasSourceSystem {
		return false, false, existingCategoryID != categoryID || existingStatus != ServiceCategoryStatusActive, nil
	}
	needsUpdate := existingCategoryID != categoryID ||
		existingAlias != alias ||
		existingStatus != ServiceCategoryStatusActive ||
		existingConfidence != record.Confidence
	if !needsUpdate {
		return false, false, false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE service_category_aliases
		SET category_id = $1,
		    alias = $2,
		    status = 'active',
		    confidence = $3,
		    updated_at = now()
		WHERE id = $4
		  AND salon_id = $5
	`, categoryID, alias, record.Confidence, existingID, salonID); err != nil {
		if isServiceAliasOwnershipConflict(err) {
			return false, false, false, ErrValidation
		}
		return false, false, false, err
	}
	return false, true, false, nil
}

func activeServiceTaxonomyCategories(ctx context.Context, tx *sql.Tx) ([]serviceTaxonomyCategoryRecord, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT category.name, category.slug, COALESCE(category.description, ''),
		       category.sort_order, category.confidence,
		       COALESCE(alias.alias, ''), COALESCE(alias.normalized_alias, ''), COALESCE(alias.confidence, 0)
		FROM service_taxonomy_releases release
		JOIN service_taxonomy_categories category
		  ON category.release_id = release.id AND category.status = 'active'
		LEFT JOIN service_taxonomy_category_aliases alias
		  ON alias.category_id = category.id AND alias.status = 'active'
		WHERE release.locale = 'en-US'
		  AND release.status = 'active'
		ORDER BY category.sort_order ASC, category.name ASC, alias.normalized_alias ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := make([]serviceTaxonomyCategoryRecord, 0)
	indexes := map[string]int{}
	for rows.Next() {
		var category serviceTaxonomyCategoryRecord
		var alias serviceTaxonomyAliasRecord
		if err := rows.Scan(
			&category.Name, &category.Slug, &category.Description, &category.SortOrder, &category.Confidence,
			&alias.Alias, &alias.Normalized, &alias.Confidence,
		); err != nil {
			return nil, err
		}
		index, ok := indexes[category.Slug]
		if !ok {
			index = len(categories)
			indexes[category.Slug] = index
			categories = append(categories, category)
		}
		if alias.Alias != "" && alias.Normalized != "" {
			categories[index].Aliases = append(categories[index].Aliases, alias)
		}
	}
	return categories, rows.Err()
}

func activeServiceTaxonomyConcepts(ctx context.Context, tx *sql.Tx) ([]serviceTaxonomyConceptRecord, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT category.slug, concept.canonical_name, concept.normalized_name, concept.confidence,
		       COALESCE(alias.alias, ''), COALESCE(alias.normalized_alias, ''), COALESCE(alias.confidence, 0)
		FROM service_taxonomy_releases release
		JOIN service_taxonomy_service_concepts concept
		  ON concept.release_id = release.id AND concept.status = 'active'
		JOIN service_taxonomy_categories category
		  ON category.id = concept.category_id AND category.status = 'active'
		LEFT JOIN service_taxonomy_service_aliases alias
		  ON alias.concept_id = concept.id AND alias.status = 'active'
		WHERE release.locale = 'en-US'
		  AND release.status = 'active'
		ORDER BY concept.normalized_name ASC, alias.normalized_alias ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	concepts := make([]serviceTaxonomyConceptRecord, 0)
	indexes := map[string]int{}
	for rows.Next() {
		var concept serviceTaxonomyConceptRecord
		var alias serviceTaxonomyAliasRecord
		if err := rows.Scan(
			&concept.CategorySlug, &concept.CanonicalName, &concept.NormalizedName, &concept.Confidence,
			&alias.Alias, &alias.Normalized, &alias.Confidence,
		); err != nil {
			return nil, err
		}
		key := concept.CategorySlug + ":" + concept.NormalizedName
		index, ok := indexes[key]
		if !ok {
			index = len(concepts)
			indexes[key] = index
			concepts = append(concepts, concept)
		}
		if alias.Alias != "" && alias.Normalized != "" {
			concepts[index].Aliases = append(concepts[index].Aliases, alias)
		}
	}
	return concepts, rows.Err()
}

func upsertSystemServiceAlias(ctx context.Context, tx *sql.Tx, salonID string, serviceID string, record serviceTaxonomyAliasRecord) (bool, bool, bool, error) {
	alias := strings.TrimSpace(record.Alias)
	normalized := strings.TrimSpace(record.Normalized)
	if alias == "" || normalized == "" || strings.TrimSpace(serviceID) == "" {
		return false, false, false, nil
	}
	if err := lockServiceAliasOwnershipTx(ctx, tx, salonID, normalized); err != nil {
		return false, false, false, err
	}
	var categoryAliasConflict bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM service_category_aliases
			WHERE salon_id = $1 AND normalized_alias = $2 AND status = 'active'
		)
	`, salonID, normalized).Scan(&categoryAliasConflict); err != nil {
		return false, false, false, err
	}
	if categoryAliasConflict {
		return false, false, true, nil
	}

	var existingID, existingServiceID, existingAlias, existingSource, existingStatus string
	var existingConfidence float64
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, service_id::text, alias, source, status, confidence
		FROM service_aliases
		WHERE salon_id = $1 AND normalized_alias = $2
	`, salonID, normalized).Scan(
		&existingID, &existingServiceID, &existingAlias, &existingSource, &existingStatus, &existingConfidence,
	)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO service_aliases (
				salon_id, service_id, alias, normalized_alias, source, status, confidence
			)
			VALUES ($1, $2, $3, $4, 'system', 'active', $5)
		`, salonID, serviceID, alias, normalized, record.Confidence)
		if isServiceAliasOwnershipConflict(err) {
			return false, false, false, ErrValidation
		}
		return err == nil, false, false, err
	}
	if err != nil {
		return false, false, false, err
	}
	if existingSource != "system" {
		return false, false, existingServiceID != serviceID || existingStatus != "active", nil
	}
	if existingServiceID == serviceID && existingAlias == alias && existingStatus == "active" && existingConfidence == record.Confidence {
		return false, false, false, nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE service_aliases
		SET service_id = $1,
		    alias = $2,
		    status = 'active',
		    confidence = $3,
		    updated_at = now()
		WHERE id = $4 AND salon_id = $5 AND source = 'system'
	`, serviceID, alias, record.Confidence, existingID, salonID)
	if isServiceAliasOwnershipConflict(err) {
		return false, false, false, ErrValidation
	}
	return false, err == nil, false, err
}

func (r *Repository) listServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT alias.id::text, alias.salon_id::text, alias.category_id::text, cat.name,
		       alias.alias, alias.normalized_alias, alias.source, alias.status, alias.confidence,
		       alias.created_at, alias.updated_at
		FROM service_category_aliases alias
		JOIN service_categories cat ON cat.id = alias.category_id
		                           AND cat.salon_id = alias.salon_id
		WHERE alias.salon_id = $1
		ORDER BY (alias.status = 'archived') ASC, alias.updated_at DESC, alias.alias ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceCategoryAlias, 0)
	for rows.Next() {
		item, err := scanServiceCategoryAlias(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ensureActiveCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) error {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return err
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_categories cat
			WHERE cat.id = $1
			  AND cat.salon_id = $2
			  AND cat.status = 'active'
		)
	`, categoryID, salonID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func scanStaffMember(row rowScanner) (*StaffMember, error) {
	var item StaffMember
	var archivedAt sql.NullTime
	var lastSyncedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.POSProvider,
		&item.POSStaffID,
		&item.Name,
		&item.Phone,
		&item.Email,
		&item.AIBookable,
		&item.Active,
		&item.SyncStatus,
		&archivedAt,
		&lastSyncedAt,
		&item.SyncError,
		&item.Source,
		&item.POSLinked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archivedAt.Valid {
		item.ArchivedAt = &archivedAt.Time
	}
	if lastSyncedAt.Valid {
		item.LastSyncedAt = &lastSyncedAt.Time
	}
	return &item, nil
}

func scanSyncJob(row rowScanner) (*SyncJob, error) {
	var item SyncJob
	var lockedAt sql.NullTime
	var completedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.Provider,
		&item.EntityType,
		&item.EntityID,
		&item.Operation,
		&item.Status,
		&item.AttemptCount,
		&item.MaxAttempts,
		&item.NextAttemptAt,
		&lockedAt,
		&completedAt,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lockedAt.Valid {
		item.LockedAt = &lockedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return &item, nil
}

func scanConnection(row rowScanner) (*Connection, error) {
	var item Connection
	var scopes []string
	var lastSync sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.Provider,
		&item.Status,
		&item.AccessTokenEncrypted,
		&item.RefreshTokenEncrypted,
		&item.MerchantID,
		&item.LocationID,
		&item.SnapshotGeneration,
		pq.Array(&scopes),
		&lastSync,
		&item.ErrorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastSync.Valid {
		item.LastSyncAt = &lastSync.Time
	}
	item.Scopes = scopes
	return &item, nil
}

func scanProviderSwitchRun(row rowScanner) (*ProviderSwitchRun, error) {
	var item ProviderSwitchRun
	var activatedAt sql.NullTime
	var cancelledAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.FromProvider,
		&item.ToProvider,
		&item.Status,
		&item.BlockedReason,
		&item.DryRunReady,
		&activatedAt,
		&cancelledAt,
		&item.CreatedByUserID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if activatedAt.Valid {
		item.ActivatedAt = &activatedAt.Time
	}
	if cancelledAt.Valid {
		item.CancelledAt = &cancelledAt.Time
	}
	item.CanActivate = false
	return &item, nil
}

func scanProviderSwitchMatch(row rowScanner) (*ProviderSwitchMatch, error) {
	var item ProviderSwitchMatch
	err := row.Scan(
		&item.ID,
		&item.RunID,
		&item.SalonID,
		&item.EntityType,
		&item.CanonicalEntityID,
		&item.CanonicalName,
		&item.ProviderEntityID,
		&item.ProviderName,
		&item.ProviderPhone,
		&item.ProviderEmail,
		&item.ProviderDurationMinutes,
		&item.MatchStatus,
		&item.MatchConfidence,
		&item.MatchReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanProviderSwitchCandidateRows(rows *sql.Rows) ([]ProviderSwitchEntityCandidate, error) {
	items := make([]ProviderSwitchEntityCandidate, 0)
	for rows.Next() {
		var item ProviderSwitchEntityCandidate
		if err := rows.Scan(
			&item.ID,
			&item.ProviderEntityID,
			&item.Name,
			&item.Phone,
			&item.Email,
			&item.DurationMinutes,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func findCustomerIDForImport(ctx context.Context, tx *sql.Tx, salonID string, provider string, providerCustomerID string, phone string, email string) (string, error) {
	var customerID string
	err := tx.QueryRowContext(ctx, `
		SELECT c.id::text
		FROM pos_entity_links link
		JOIN customers c ON c.salon_id = link.salon_id AND c.id = link.entity_id
		WHERE link.salon_id = $1
		  AND link.entity_type = 'customer'
		  AND link.provider = $2
		  AND link.provider_entity_id = $3
		LIMIT 1
	`, salonID, provider, providerCustomerID).Scan(&customerID)
	if err == nil {
		return customerID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	if strings.TrimSpace(phone) != "" {
		err = tx.QueryRowContext(ctx, `
			SELECT id::text
			FROM customers
			WHERE salon_id = $1
			  AND archived_at IS NULL
			  AND normalized_phone = $2
			ORDER BY updated_at DESC
			LIMIT 1
		`, salonID, phone).Scan(&customerID)
		if err == nil {
			return customerID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}

	if strings.TrimSpace(email) != "" {
		err = tx.QueryRowContext(ctx, `
			SELECT id::text
			FROM customers
			WHERE salon_id = $1
			  AND archived_at IS NULL
			  AND normalized_email = $2
			ORDER BY updated_at DESC
			LIMIT 1
		`, salonID, email).Scan(&customerID)
		if err == nil {
			return customerID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}

	return "", nil
}

func customerNormalizedValueConflicts(ctx context.Context, tx *sql.Tx, salonID string, customerID string, fieldName string, value string) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}

	query := ""
	switch fieldName {
	case "normalized_phone":
		query = `
			SELECT EXISTS (
				SELECT 1
				FROM customers
				WHERE salon_id = $1
				  AND archived_at IS NULL
				  AND id <> $2::uuid
				  AND normalized_phone = $3
			)
		`
	case "normalized_email":
		query = `
			SELECT EXISTS (
				SELECT 1
				FROM customers
				WHERE salon_id = $1
				  AND archived_at IS NULL
				  AND id <> $2::uuid
				  AND normalized_email = $3
			)
		`
	default:
		return false, ErrValidation
	}

	var conflicts bool
	err := tx.QueryRowContext(ctx, query, salonID, customerID, value).Scan(&conflicts)
	return conflicts, err
}

func importedCustomerName(customer Customer) string {
	name := strings.TrimSpace(customer.Name)
	if name != "" {
		return name
	}
	if phone := validation.NormalizePhone(customer.Phone); phone != "" {
		return phone
	}
	if email := strings.ToLower(strings.TrimSpace(customer.Email)); email != "" {
		return email
	}
	return "Square customer"
}

func normalizeLocalClock(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format("15:04:05"), true
		}
	}
	return "", false
}

func localClockDuration(value string) (time.Duration, bool) {
	normalized, ok := normalizeLocalClock(value)
	if !ok {
		return 0, false
	}
	parsed, err := time.Parse("15:04:05", normalized)
	if err != nil {
		return 0, false
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute + time.Duration(parsed.Second())*time.Second, true
}

func NowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
