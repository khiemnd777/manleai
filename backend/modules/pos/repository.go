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

	var logs []SyncLog
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

func (r *Repository) UpsertServices(ctx context.Context, salonID string, services []Service) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, svc := range services {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO services (salon_id, pos_provider, pos_service_id, name, description, ai_description, duration_minutes, price_from, price_display, ai_bookable, active)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, NULLIF($8, 0), NULLIF($9, ''), $10, $11)
			ON CONFLICT (salon_id, pos_provider, pos_service_id)
			DO UPDATE SET name = EXCLUDED.name,
			              description = EXCLUDED.description,
			              ai_description = COALESCE(services.ai_description, EXCLUDED.ai_description),
			              duration_minutes = EXCLUDED.duration_minutes,
			              price_from = EXCLUDED.price_from,
			              price_display = EXCLUDED.price_display,
			              active = EXCLUDED.active,
			              updated_at = now()
		`, salonID, ProviderSquare, svc.POSServiceID, svc.Name, svc.Description, svc.AIDescription, svc.DurationMinutes, svc.PriceFrom, svc.PriceDisplay, svc.AIBookable, svc.Active); err != nil {
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, phone, email, ai_bookable, active)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
			ON CONFLICT (salon_id, pos_provider, pos_staff_id)
			DO UPDATE SET name = EXCLUDED.name,
			              phone = EXCLUDED.phone,
			              email = EXCLUDED.email,
			              active = EXCLUDED.active,
			              updated_at = now()
		`, salonID, ProviderSquare, member.POSStaffID, member.Name, member.Phone, member.Email, member.AIBookable, member.Active); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
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
