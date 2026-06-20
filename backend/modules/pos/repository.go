package pos

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO services (salon_id, pos_provider, pos_service_id, pos_service_version, name, description, ai_description, duration_minutes, price_from, price_display, ai_bookable, active)
			VALUES ($1, $2, $3, NULLIF($4::bigint, 0), $5, NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, 0), NULLIF($10, ''), $11, $12)
			ON CONFLICT (salon_id, pos_provider, pos_service_id)
			DO UPDATE SET name = EXCLUDED.name,
			              pos_service_version = EXCLUDED.pos_service_version,
			              description = EXCLUDED.description,
			              ai_description = COALESCE(services.ai_description, EXCLUDED.ai_description),
			              duration_minutes = EXCLUDED.duration_minutes,
			              price_from = EXCLUDED.price_from,
			              price_display = EXCLUDED.price_display,
			              active = EXCLUDED.active,
			              updated_at = now()
		`, salonID, provider, svc.POSServiceID, svc.POSServiceVersion, svc.Name, svc.Description, svc.AIDescription, svc.DurationMinutes, svc.PriceFrom, svc.PriceDisplay, svc.AIBookable, svc.Active); err != nil {
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, phone, email, ai_bookable, active)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
			ON CONFLICT (salon_id, pos_provider, pos_staff_id)
			DO UPDATE SET name = EXCLUDED.name,
			              phone = EXCLUDED.phone,
			              email = EXCLUDED.email,
			              active = EXCLUDED.active,
			              updated_at = now()
		`, salonID, provider, member.POSStaffID, member.Name, member.Phone, member.Email, member.AIBookable, member.Active); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) ListServices(ctx context.Context, salonID string, provider string) ([]Service, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, pos_provider, pos_service_id, COALESCE(pos_service_version, 0),
		       name, COALESCE(description, ''), COALESCE(ai_description, ''), duration_minutes,
		       COALESCE(price_from, 0), COALESCE(price_display, ''), ai_bookable, active
		FROM services
		WHERE salon_id = $1 AND pos_provider = $2
		ORDER BY active DESC, name ASC
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

func (r *Repository) ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, pos_provider, pos_staff_id, name,
		       COALESCE(phone, ''), COALESCE(email, ''), ai_bookable, active
		FROM staff
		WHERE salon_id = $1 AND pos_provider = $2
		ORDER BY active DESC, name ASC
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

func (r *Repository) UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error) {
	current, err := r.getServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if aiBookable && !current.Active {
		return nil, ErrValidation
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE services
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
		RETURNING id::text, salon_id::text, pos_provider, pos_service_id, COALESCE(pos_service_version, 0),
		          name, COALESCE(description, ''), COALESCE(ai_description, ''), duration_minutes,
		          COALESCE(price_from, 0), COALESCE(price_display, ''), ai_bookable, active
	`, aiBookable, serviceID, salonID)
	return scanService(row)
}

func (r *Repository) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	current, err := r.getStaffForOwner(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if aiBookable && !current.Active {
		return nil, ErrValidation
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE staff
		SET ai_bookable = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
		RETURNING id::text, salon_id::text, pos_provider, pos_staff_id, name,
		          COALESCE(phone, ''), COALESCE(email, ''), ai_bookable, active
	`, aiBookable, staffID, salonID)
	return scanStaffMember(row)
}

func (r *Repository) getServiceForOwner(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT svc.id::text, svc.salon_id::text, svc.pos_provider, svc.pos_service_id, COALESCE(svc.pos_service_version, 0),
		       svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''), svc.duration_minutes,
		       COALESCE(svc.price_from, 0), COALESCE(svc.price_display, ''), svc.ai_bookable, svc.active
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
		SELECT st.id::text, st.salon_id::text, st.pos_provider, st.pos_staff_id, st.name,
		       COALESCE(st.phone, ''), COALESCE(st.email, ''), st.ai_bookable, st.active
		FROM staff st
		JOIN salons salon ON salon.id = st.salon_id
		WHERE st.id = $1
		  AND st.salon_id = $2
		  AND salon.owner_user_id = $3
	`, staffID, salonID, ownerUserID)
	return scanStaffMember(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(row rowScanner) (*Service, error) {
	var item Service
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanStaffMember(row rowScanner) (*StaffMember, error) {
	var item StaffMember
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
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

func NowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
