package pos

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/validation"
)

var ErrNotFound = errors.New("pos record not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)`, salonID, ownerUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	var provider string
	err := r.db.QueryRowContext(ctx, `
		SELECT active_pos_provider
		FROM salons
		WHERE id = $1
		  AND owner_user_id = $2
	`, salonID, ownerUserID).Scan(&provider)
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
		       scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
		FROM pos_connections
		WHERE salon_id = $1 AND provider = $2
	`, salonID, provider)
	return scanConnection(row)
}

func (r *Repository) UpsertConnection(ctx context.Context, connection Connection) (*Connection, error) {
	row := r.db.QueryRowContext(ctx, `
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
		          scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
	`, connection.SalonID, connection.Provider, connection.Status, connection.AccessTokenEncrypted, connection.RefreshTokenEncrypted, connection.MerchantID, connection.LocationID, pq.Array(connection.Scopes), connection.ErrorMessage)
	return scanConnection(row)
}

func (r *Repository) UpdateLocation(ctx context.Context, salonID string, provider string, locationID string) (*Connection, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE pos_connections
		SET location_id = $1,
		    status = CASE WHEN status = 'connected' THEN 'active' ELSE status END,
		    updated_at = now()
		WHERE salon_id = $2 AND provider = $3
		RETURNING id::text, salon_id::text, provider, status, COALESCE(access_token_encrypted, ''),
		          COALESCE(refresh_token_encrypted, ''), COALESCE(merchant_id, ''), COALESCE(location_id, ''),
		          scopes, last_sync_at, COALESCE(error_message, ''), created_at, updated_at
	`, locationID, salonID, provider)
	return scanConnection(row)
}

func (r *Repository) MarkSyncing(ctx context.Context, salonID string, provider string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = 'syncing', updated_at = now()
		WHERE salon_id = $1 AND provider = $2
	`, salonID, provider)
	return err
}

func (r *Repository) MarkSyncComplete(ctx context.Context, salonID string, provider string, status string, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = $3, last_sync_at = now(), error_message = NULLIF($4, ''), updated_at = now()
		WHERE salon_id = $1 AND provider = $2
	`, salonID, provider, status, message)
	return err
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
		WITH candidates AS (
			SELECT id
			FROM pos_sync_jobs
			WHERE status IN ('queued', 'failed')
			  AND attempt_count < max_attempts
			  AND next_attempt_at <= now()
			ORDER BY next_attempt_at ASC, created_at ASC
			FOR UPDATE SKIP LOCKED
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
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message, payload)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::jsonb)
	`, item.SalonID, item.Provider, item.Operation, item.ErrorCode, item.ErrorMessage, string(item.Payload))
	return err
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

	for _, svc := range services {
		provider := svc.POSProvider
		if provider == "" {
			provider = ProviderSquare
		}
		var serviceID string
		var archived bool
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
			              ai_bookable = CASE WHEN services.archived_at IS NULL THEN services.ai_bookable ELSE false END,
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
	return tx.Commit()
}

func (r *Repository) UpsertStaff(ctx context.Context, salonID string, staff []StaffMember) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, member := range staff {
		provider := member.POSProvider
		if provider == "" {
			provider = ProviderSquare
		}
		var staffID string
		var archived bool
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
			              ai_bookable = CASE WHEN staff.archived_at IS NULL THEN staff.ai_bookable ELSE false END,
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
	return tx.Commit()
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
	if err := tx.Commit(); err != nil {
		return 0, err
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
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return synced, skipped, nil
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
		       ) AS pos_linked
		FROM services svc
		WHERE svc.salon_id = $1 AND svc.pos_provider = $2
		ORDER BY (svc.archived_at IS NOT NULL) ASC, svc.active DESC, svc.name ASC
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

func (r *Repository) CreateService(ctx context.Context, salonID string, ownerUserID string, provider string, input ServiceMutation) (*Service, error) {
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

	var serviceID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description, ai_description, duration_minutes,
			price_from, price_display, ai_bookable, active, sync_status, archived_at, last_synced_at, sync_error, source
		)
		VALUES ($1, $2, NULL, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, NULL, false, $8, 'local_only', NULL, NULL, NULL, 'local')
		RETURNING id::text
	`, salonID, provider, input.Name, input.Description, input.AIDescription, input.DurationMinutes, servicePriceValue(input.PriceFrom), input.Active).Scan(&serviceID); err != nil {
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
	if _, err := r.db.ExecContext(ctx, `
		UPDATE services
		SET name = $1,
		    description = NULLIF($2, ''),
		    ai_description = NULLIF($3, ''),
		    duration_minutes = $4,
		    price_from = $5,
		    price_display = NULL,
		    active = $6,
		    ai_bookable = CASE WHEN $6 THEN ai_bookable ELSE false END,
		    updated_at = now()
		WHERE id = $7
		  AND salon_id = $8
	`, input.Name, input.Description, input.AIDescription, input.DurationMinutes, servicePriceValue(input.PriceFrom), input.Active, serviceID, salonID); err != nil {
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
	if _, err := r.db.ExecContext(ctx, `
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
		       ) AS pos_linked
		FROM services svc
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
	current, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if aiBookable && !serviceCanEnableAIBooking(current) {
		return nil, ErrValidation
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE services
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
	`, aiBookable, serviceID, salonID); err != nil {
		return nil, err
	}
	return r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
}

func (r *Repository) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	current, err := r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if aiBookable && !staffCanEnableAIBooking(current) {
		return nil, ErrValidation
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE staff
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
	`, aiBookable, staffID, salonID); err != nil {
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

func (r *Repository) getServiceForOwner(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
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
		       ) AS pos_linked
		FROM services svc
		JOIN salons salon ON salon.id = svc.salon_id
		WHERE svc.id = $1
		  AND svc.salon_id = $2
		  AND salon.owner_user_id = $3
	`, serviceID, salonID, ownerUserID)
	return scanService(row)
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

func serviceCanEnableAIBooking(item *Service) bool {
	return item != nil &&
		item.Active &&
		item.ArchivedAt == nil &&
		item.SyncStatus == SyncStatusSynced &&
		item.POSLinked &&
		strings.TrimSpace(item.POSServiceID) != "" &&
		item.POSServiceVersion > 0
}

func staffCanEnableAIBooking(item *StaffMember) bool {
	return item != nil &&
		item.Active &&
		item.ArchivedAt == nil &&
		item.SyncStatus == SyncStatusSynced &&
		item.POSLinked &&
		strings.TrimSpace(item.POSStaffID) != ""
}

func servicePriceValue(price *float64) any {
	if price == nil {
		return nil
	}
	return *price
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(row rowScanner) (*Service, error) {
	var item Service
	var archivedAt sql.NullTime
	var lastSyncedAt sql.NullTime
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
