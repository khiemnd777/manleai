package integrationconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

var (
	ErrNotFound                  = errors.New("integration config not found")
	ErrInvalidSettings           = errors.New("integration config settings are invalid")
	ErrVersionConflict           = errors.New("integration config version conflict")
	ErrActionConflict            = errors.New("integration config action conflict")
	ErrTwilioVoiceNumberConflict = errors.New("Twilio Voice inbound number conflict")
)

type TechnicalMutationCommand struct {
	ActorUserID        string
	ActionKey          string
	ActionType         string
	RequestFingerprint string
	ExpectedVersion    int64
	ChangedFields      []string
}

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
		       COALESCE(sic.secrets_encrypted, ''), sic.created_at, sic.updated_at,
		       COALESCE(version.version, 0)
		FROM salon_integration_configs sic
		JOIN salons s ON s.id = sic.salon_id
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=sic.salon_id AND version.resource_type='integration_config'
		 AND version.resource_id=sic.provider
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

func (r *Repository) ListForSalon(ctx context.Context, salonID string) ([]StoredConfig, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1)`, salonID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT config.id::text,config.salon_id::text,config.provider,config.enabled,config.settings::text,
		       COALESCE(config.secrets_encrypted,''),config.created_at,config.updated_at,
		       COALESCE(version.version, 0)
		FROM salon_integration_configs config
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		WHERE config.salon_id=$1
		ORDER BY config.provider
	`, salonID)
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
		SELECT config.id::text, config.salon_id::text, config.provider, config.enabled, config.settings::text,
		       COALESCE(config.secrets_encrypted, ''), config.created_at, config.updated_at,
		       COALESCE(version.version, 0)
		FROM salon_integration_configs config
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		WHERE config.salon_id = $1
		  AND config.provider = $2
	`, salonID, provider)
	return scanConfig(row)
}

func (r *Repository) LocateTwilioVoiceRouteTenant(ctx context.Context, routeID string) (string, error) {
	var salonID string
	err := r.db.QueryRowContext(ctx, `
		SELECT located.salon_id::text
		FROM (
			SELECT public.app_provider_twilio_voice_route_salon($1::uuid) AS salon_id
		) located
		WHERE located.salon_id IS NOT NULL
	`, strings.TrimSpace(routeID)).Scan(&salonID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(salonID), nil
}

func (r *Repository) GetTwilioVoiceRoute(ctx context.Context, salonID, routeID string) (*StoredConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT config.id::text, config.salon_id::text, config.provider, config.enabled, config.settings::text,
		       COALESCE(config.secrets_encrypted, ''), config.created_at, config.updated_at,
		       COALESCE(version.version, 0)
		FROM salon_integration_configs config
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		WHERE config.id = $1
		  AND config.salon_id = $2
		  AND config.provider = 'twilio'
		  AND config.enabled = true
		  AND config.settings->>'voice_routing_enabled' = 'true'
	`, strings.TrimSpace(routeID), strings.TrimSpace(salonID))
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
		          COALESCE(secrets_encrypted, ''), created_at, updated_at, 0::bigint
	`, cfg.SalonID, cfg.Provider, cfg.Enabled, string(settingsJSON), cfg.SecretsEncrypted)
	item, err := scanConfig(row)
	return item, integrationConfigPersistenceError(err)
}

func (r *Repository) UpsertControlled(ctx context.Context, cfg StoredConfig, command TechnicalMutationCommand) (*StoredConfig, bool, error) {
	settingsJSON, err := json.Marshal(normalizeMap(cfg.Settings))
	if err != nil {
		return nil, false, integrationConfigPersistenceError(err)
	}
	detailsJSON, err := json.Marshal(map[string]any{"provider": cfg.Provider, "changed_fields": command.ChangedFields})
	if err != nil {
		return nil, false, integrationConfigPersistenceError(err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var salonExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1)`, cfg.SalonID).Scan(&salonExists); err != nil {
		return nil, false, err
	}
	if !salonExists {
		return nil, false, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_resource_versions (salon_id,resource_type,resource_id,version)
		VALUES ($1,'integration_config',$2,0)
		ON CONFLICT DO NOTHING
	`, cfg.SalonID, cfg.Provider); err != nil {
		return nil, false, err
	}

	var existingFingerprint, existingResourceID string
	var existingResultVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint,resource_id,result_version
		FROM technical_actions
		WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3
	`, cfg.SalonID, command.ActorUserID, command.ActionKey).Scan(&existingFingerprint, &existingResourceID, &existingResultVersion)
	if err == nil {
		if existingFingerprint != command.RequestFingerprint || existingResourceID != cfg.Provider {
			return nil, false, ErrActionConflict
		}
		item, err := getConfigTx(ctx, tx, cfg.SalonID, cfg.Provider)
		if err != nil {
			return nil, false, err
		}
		item.Version = existingResultVersion
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return item, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM technical_resource_versions
		WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id=$2
		FOR UPDATE
	`, cfg.SalonID, cfg.Provider).Scan(&currentVersion); err != nil {
		return nil, false, err
	}
	if currentVersion != command.ExpectedVersion {
		return nil, false, ErrVersionConflict
	}

	item, err := scanConfig(tx.QueryRowContext(ctx, `
		INSERT INTO salon_integration_configs (salon_id, provider, enabled, settings, secrets_encrypted)
		VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, ''))
		ON CONFLICT (salon_id, provider)
		DO UPDATE SET enabled=EXCLUDED.enabled,settings=EXCLUDED.settings,
		              secrets_encrypted=EXCLUDED.secrets_encrypted,updated_at=now()
		RETURNING id::text,salon_id::text,provider,enabled,settings::text,
		          COALESCE(secrets_encrypted,''),created_at,updated_at,($6 + 1)::bigint
	`, cfg.SalonID, cfg.Provider, cfg.Enabled, string(settingsJSON), cfg.SecretsEncrypted, currentVersion))
	if err != nil {
		return nil, false, integrationConfigPersistenceError(err)
	}
	item.Version = currentVersion + 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE technical_resource_versions
		SET version=$3,updated_by_user_id=$4,updated_at=now()
		WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id=$2
	`, cfg.SalonID, cfg.Provider, item.Version, command.ActorUserID); err != nil {
		return nil, false, err
	}
	var actionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO technical_actions (
			salon_id,actor_user_id,action_key,action_type,request_fingerprint,
			resource_type,resource_id,previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,$5,'integration_config',$6,$7,$8,$9::jsonb)
		RETURNING id::text
	`, cfg.SalonID, command.ActorUserID, command.ActionKey, command.ActionType,
		command.RequestFingerprint, cfg.Provider, currentVersion, item.Version, string(detailsJSON)).Scan(&actionID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_events (
			action_id,salon_id,actor_user_id,event_type,resource_type,resource_id,
			previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,'integration_config',$5,$6,$7,$8::jsonb)
	`, actionID, cfg.SalonID, command.ActorUserID, command.ActionType, cfg.Provider,
		currentVersion, item.Version, string(detailsJSON)); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func integrationConfigPersistenceError(err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pq.Error
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.Constraint == "idx_twilio_voice_active_inbound_number" {
		return ErrTwilioVoiceNumberConflict
	}
	return err
}

func getConfigTx(ctx context.Context, tx *sql.Tx, salonID, provider string) (*StoredConfig, error) {
	return scanConfig(tx.QueryRowContext(ctx, `
		SELECT config.id::text,config.salon_id::text,config.provider,config.enabled,config.settings::text,
		       COALESCE(config.secrets_encrypted,''),config.created_at,config.updated_at,
		       version.version
		FROM salon_integration_configs config
		JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		WHERE config.salon_id=$1 AND config.provider=$2
	`, salonID, provider))
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
		&item.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	settingsRaw = strings.TrimSpace(settingsRaw)
	if settingsRaw == "" {
		return nil, ErrInvalidSettings
	}
	var rawSettings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsRaw), &rawSettings); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}
	if rawSettings == nil {
		return nil, ErrInvalidSettings
	}
	settings := make(map[string]string, len(rawSettings))
	for key, rawValue := range rawSettings {
		var value any
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
		}
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: setting %q must be a string", ErrInvalidSettings, key)
		}
		settings[key] = stringValue
	}
	item.Settings = settings
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
