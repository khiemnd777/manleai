package integrationconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

var ErrNotFound = errors.New("integration config not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM salons
			WHERE id = $1 AND owner_user_id = $2
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

func (r *Repository) ListForOwner(ctx context.Context, salonID string, ownerUserID string) ([]StoredConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT sic.id::text, sic.salon_id::text, sic.provider, sic.enabled, sic.settings::text,
		       COALESCE(sic.secrets_encrypted, ''), sic.created_at, sic.updated_at
		FROM salon_integration_configs sic
		JOIN salons s ON s.id = sic.salon_id
		WHERE sic.salon_id = $1
		  AND s.owner_user_id = $2
		ORDER BY sic.provider
	`, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StoredConfig, 0)
	for rows.Next() {
		item, err := scanConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, salonID string, provider string) (*StoredConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, provider, enabled, settings::text,
		       COALESCE(secrets_encrypted, ''), created_at, updated_at
		FROM salon_integration_configs
		WHERE salon_id = $1
		  AND provider = $2
	`, salonID, provider)
	return scanConfig(row)
}

func (r *Repository) Upsert(ctx context.Context, cfg StoredConfig) (*StoredConfig, error) {
	settingsJSON, err := json.Marshal(normalizeMap(cfg.Settings))
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO salon_integration_configs (salon_id, provider, enabled, settings, secrets_encrypted)
		VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, ''))
		ON CONFLICT (salon_id, provider)
		DO UPDATE SET enabled = EXCLUDED.enabled,
		              settings = EXCLUDED.settings,
		              secrets_encrypted = EXCLUDED.secrets_encrypted,
		              updated_at = now()
		RETURNING id::text, salon_id::text, provider, enabled, settings::text,
		          COALESCE(secrets_encrypted, ''), created_at, updated_at
	`, cfg.SalonID, cfg.Provider, cfg.Enabled, string(settingsJSON), cfg.SecretsEncrypted)
	return scanConfig(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConfig(row rowScanner) (*StoredConfig, error) {
	var item StoredConfig
	var settingsRaw string
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.Provider,
		&item.Enabled,
		&settingsRaw,
		&item.SecretsEncrypted,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Settings = map[string]string{}
	if settingsRaw != "" {
		_ = json.Unmarshal([]byte(settingsRaw), &item.Settings)
	}
	item.Settings = normalizeMap(item.Settings)
	return &item, nil
}

func normalizeMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}
