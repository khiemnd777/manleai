package salon

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrNotFound        = errors.New("salon not found")
	ErrSlugUnavailable = errors.New("public slug unavailable")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListForOwner(ctx context.Context, ownerUserID string) ([]Salon, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, name, phone, COALESCE(address, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(zip_code, ''),
		       timezone, owner_user_id::text, primary_language, secondary_language, COALESCE(handoff_phone, ''),
		       ai_enabled, active_pos_provider, COALESCE(public_slug, ''), public_catalog_enabled, created_at, updated_at
		FROM salons
		WHERE owner_user_id = $1
		ORDER BY created_at DESC
	`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	salons := make([]Salon, 0)
	for rows.Next() {
		item, err := scanSalon(rows)
		if err != nil {
			return nil, err
		}
		salons = append(salons, *item)
	}
	return salons, rows.Err()
}

func (r *Repository) GetForOwner(ctx context.Context, id string, ownerUserID string) (*Salon, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, name, phone, COALESCE(address, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(zip_code, ''),
		       timezone, owner_user_id::text, primary_language, secondary_language, COALESCE(handoff_phone, ''),
		       ai_enabled, active_pos_provider, COALESCE(public_slug, ''), public_catalog_enabled, created_at, updated_at
		FROM salons
		WHERE id = $1 AND owner_user_id = $2
	`, id, ownerUserID)
	return scanSalon(row)
}

func (r *Repository) Create(ctx context.Context, ownerUserID string, req CreateSalonRequest) (*Salon, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var salonID string
	row := tx.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, address, city, state, zip_code, timezone, owner_user_id, primary_language, secondary_language, handoff_phone)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, NULLIF($11, ''))
		RETURNING id::text
	`, req.Name, req.Phone, req.Address, req.City, req.State, req.ZipCode, req.Timezone, ownerUserID, req.PrimaryLanguage, req.SecondaryLanguage, req.HandoffPhone)
	if err := row.Scan(&salonID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO salon_settings (salon_id) VALUES ($1)`, salonID); err != nil {
		return nil, err
	}
	for day := 0; day <= 6; day++ {
		isClosed := day == 0
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hours (salon_id, day_of_week, open_time, close_time, is_closed)
			VALUES ($1, $2, '09:30', '19:00', $3)
		`, salonID, day, isClosed); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetForOwner(ctx, salonID, ownerUserID)
}

func (r *Repository) Update(ctx context.Context, id string, ownerUserID string, req UpdateSalonRequest) (*Salon, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE salons
		SET name = $1,
		    phone = $2,
		    address = NULLIF($3, ''),
		    city = NULLIF($4, ''),
		    state = NULLIF($5, ''),
		    zip_code = NULLIF($6, ''),
		    timezone = $7,
		    primary_language = $8,
		    secondary_language = $9,
		    handoff_phone = NULLIF($10, ''),
		    ai_enabled = $11,
		    updated_at = now()
		WHERE id = $12 AND owner_user_id = $13
	`, req.Name, req.Phone, req.Address, req.City, req.State, req.ZipCode, req.Timezone, req.PrimaryLanguage, req.SecondaryLanguage, req.HandoffPhone, req.AIEnabled, id, ownerUserID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.GetForOwner(ctx, id, ownerUserID)
}

func (r *Repository) GetSettings(ctx context.Context, salonID string, ownerUserID string) (*Settings, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT ss.id::text, ss.salon_id::text, ss.ai_greeting, ss.ai_voice, COALESCE(ss.ai_tone, 'professional_warm'), ss.booking_mode, ss.recording_enabled,
		       ss.recording_consent_message, ss.sms_confirmation_enabled, ss.sms_reminder_enabled,
		       ss.reminder_hours_before, ss.handoff_enabled, ss.consultation_enabled, ss.created_at, ss.updated_at
		FROM salon_settings ss
		JOIN salons s ON s.id = ss.salon_id
		WHERE ss.salon_id = $1 AND s.owner_user_id = $2
	`, salonID, ownerUserID)
	return scanSettings(row)
}

func (r *Repository) UpdateSettings(ctx context.Context, salonID string, ownerUserID string, req UpdateSettingsRequest) (*Settings, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE salon_settings
		SET ai_greeting = $1,
		    ai_voice = $2,
		    ai_tone = $3,
		    booking_mode = $4,
		    recording_enabled = $5,
		    recording_consent_message = $6,
		    sms_confirmation_enabled = $7,
		    sms_reminder_enabled = $8,
		    reminder_hours_before = $9,
		    handoff_enabled = $10,
		    consultation_enabled = $11,
		    updated_at = now()
		WHERE salon_id = $12
		  AND EXISTS (SELECT 1 FROM salons WHERE salons.id = salon_settings.salon_id AND salons.owner_user_id = $13)
	`, req.AIGreeting, req.AIVoice, req.AITone, req.BookingMode, req.RecordingEnabled, req.RecordingConsentMessage, req.SMSConfirmationEnabled, req.SMSReminderEnabled, req.ReminderHoursBefore, req.HandoffEnabled, req.ConsultationEnabled, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.GetSettings(ctx, salonID, ownerUserID)
}

func (r *Repository) GetPublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string) (*PublicCatalogSettings, error) {
	row := r.db.QueryRowContext(ctx, publicCatalogSettingsQuery(), salonID, ownerUserID)
	return scanPublicCatalogSettings(row)
}

func (r *Repository) UpdatePublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string, req UpdatePublicCatalogRequest) (*PublicCatalogSettings, error) {
	slug := strings.TrimSpace(req.PublicSlug)
	if slug != "" {
		var taken bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM salons
				WHERE lower(public_slug) = lower($1)
				  AND id <> $2
			)
		`, slug, salonID).Scan(&taken); err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrSlugUnavailable
		}
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE salons
		SET public_slug = NULLIF($1, ''),
		    public_catalog_enabled = $2,
		    updated_at = now()
		WHERE id = $3
		  AND owner_user_id = $4
	`, slug, req.PublicCatalogEnabled, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.GetPublicCatalogSettings(ctx, salonID, ownerUserID)
}

func (r *Repository) GetBusinessHours(ctx context.Context, salonID string, ownerUserID string) ([]BusinessHour, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT bh.id::text, bh.salon_id::text, bh.day_of_week, COALESCE(bh.open_time::text, ''),
		       COALESCE(bh.close_time::text, ''), bh.is_closed, bh.created_at, bh.updated_at
		FROM salon_business_hours bh
		JOIN salons s ON s.id = bh.salon_id
		WHERE bh.salon_id = $1 AND s.owner_user_id = $2
		ORDER BY bh.day_of_week
	`, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hours := make([]BusinessHour, 0)
	for rows.Next() {
		var hour BusinessHour
		if err := rows.Scan(&hour.ID, &hour.SalonID, &hour.DayOfWeek, &hour.OpenTime, &hour.CloseTime, &hour.IsClosed, &hour.CreatedAt, &hour.UpdatedAt); err != nil {
			return nil, err
		}
		hours = append(hours, hour)
	}
	return hours, rows.Err()
}

func (r *Repository) GetBusinessHourPeriods(ctx context.Context, salonID string, ownerUserID string) ([]BusinessHourPeriod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT bhp.id::text, bhp.salon_id::text, bhp.day_of_week, bhp.start_local_time::text,
		       bhp.end_local_time::text, bhp.source, bhp.provider, bhp.provider_location_id,
		       bhp.provider_period_index, bhp.last_synced_at, bhp.created_at, bhp.updated_at
		FROM salon_business_hour_periods bhp
		JOIN salons s ON s.id = bhp.salon_id
		WHERE bhp.salon_id = $1
		  AND s.owner_user_id = $2
		  AND bhp.source = 'imported'
		  AND bhp.provider = s.active_pos_provider
		ORDER BY bhp.day_of_week ASC, bhp.start_local_time ASC, bhp.provider_period_index ASC
	`, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	periods := make([]BusinessHourPeriod, 0)
	for rows.Next() {
		var period BusinessHourPeriod
		var lastSyncedAt sql.NullTime
		if err := rows.Scan(
			&period.ID,
			&period.SalonID,
			&period.DayOfWeek,
			&period.StartLocalTime,
			&period.EndLocalTime,
			&period.Source,
			&period.Provider,
			&period.ProviderLocationID,
			&period.ProviderPeriodIndex,
			&lastSyncedAt,
			&period.CreatedAt,
			&period.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastSyncedAt.Valid {
			period.LastSyncedAt = &lastSyncedAt.Time
		}
		periods = append(periods, period)
	}
	return periods, rows.Err()
}

func (r *Repository) UpdateBusinessHours(ctx context.Context, salonID string, ownerUserID string, req UpdateBusinessHoursRequest) ([]BusinessHour, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)`, salonID, ownerUserID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	for _, hour := range req.Hours {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hours (salon_id, day_of_week, open_time, close_time, is_closed)
			VALUES ($1, $2, NULLIF($3, '')::time, NULLIF($4, '')::time, $5)
			ON CONFLICT (salon_id, day_of_week)
			DO UPDATE SET open_time = EXCLUDED.open_time,
			              close_time = EXCLUDED.close_time,
			              is_closed = EXCLUDED.is_closed,
			              updated_at = now()
		`, salonID, hour.DayOfWeek, hour.OpenTime, hour.CloseTime, hour.IsClosed); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetBusinessHours(ctx, salonID, ownerUserID)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSalon(row rowScanner) (*Salon, error) {
	var item Salon
	err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Phone,
		&item.Address,
		&item.City,
		&item.State,
		&item.ZipCode,
		&item.Timezone,
		&item.OwnerUserID,
		&item.PrimaryLanguage,
		&item.SecondaryLanguage,
		&item.HandoffPhone,
		&item.AIEnabled,
		&item.ActivePOSProvider,
		&item.PublicSlug,
		&item.PublicCatalogEnabled,
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

func scanSettings(row rowScanner) (*Settings, error) {
	var settings Settings
	err := row.Scan(
		&settings.ID,
		&settings.SalonID,
		&settings.AIGreeting,
		&settings.AIVoice,
		&settings.AITone,
		&settings.BookingMode,
		&settings.RecordingEnabled,
		&settings.RecordingConsentMessage,
		&settings.SMSConfirmationEnabled,
		&settings.SMSReminderEnabled,
		&settings.ReminderHoursBefore,
		&settings.HandoffEnabled,
		&settings.ConsultationEnabled,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func publicCatalogSettingsQuery() string {
	return `
		WITH owned_salon AS (
			SELECT id, owner_user_id, active_pos_provider, public_slug, public_catalog_enabled, updated_at
			FROM salons
			WHERE id = $1 AND owner_user_id = $2
		),
		service_rows AS (
			SELECT svc.id,
			       EXISTS (
			           SELECT 1
			           FROM pos_entity_links link
			           WHERE link.salon_id = svc.salon_id
			             AND link.entity_type = 'service'
			             AND link.entity_id = svc.id
			             AND link.provider = (SELECT active_pos_provider FROM owned_salon)
			             AND link.sync_status = 'synced'
			             AND link.provider_entity_id IS NOT NULL
			             AND link.provider_entity_id <> ''
			       ) AS linked
			FROM services svc
			WHERE svc.salon_id = (SELECT id FROM owned_salon)
			  AND svc.pos_provider = (SELECT active_pos_provider FROM owned_salon)
			  AND svc.active = true
			  AND svc.ai_bookable = true
			  AND svc.archived_at IS NULL
			  AND svc.sync_status = 'synced'
			  AND svc.duration_minutes > 0
			  AND COALESCE(svc.pos_service_version, 0) > 0
		),
		staff_rows AS (
			SELECT st.id,
			       EXISTS (
			           SELECT 1
			           FROM pos_entity_links link
			           WHERE link.salon_id = st.salon_id
			             AND link.entity_type = 'staff'
			             AND link.entity_id = st.id
			             AND link.provider = (SELECT active_pos_provider FROM owned_salon)
			             AND link.sync_status = 'synced'
			             AND link.provider_entity_id IS NOT NULL
			             AND link.provider_entity_id <> ''
			       ) AS linked
			FROM staff st
			WHERE st.salon_id = (SELECT id FROM owned_salon)
			  AND st.pos_provider = (SELECT active_pos_provider FROM owned_salon)
			  AND st.active = true
			  AND st.ai_bookable = true
			  AND st.archived_at IS NULL
			  AND st.sync_status = 'synced'
		)
		SELECT id::text, COALESCE(public_slug, ''), public_catalog_enabled,
		       (SELECT count(*)::int FROM service_rows WHERE linked = true),
		       (SELECT count(*)::int FROM staff_rows WHERE linked = true),
		       updated_at
		FROM owned_salon
	`
}

func scanPublicCatalogSettings(row rowScanner) (*PublicCatalogSettings, error) {
	var settings PublicCatalogSettings
	err := row.Scan(
		&settings.SalonID,
		&settings.PublicSlug,
		&settings.PublicCatalogEnabled,
		&settings.BookableServiceCount,
		&settings.BookableStaffCount,
		&settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	settings.PublicPath = publicPath(settings.PublicSlug)
	settings.CanPublish = settings.PublicSlug != "" && settings.BookableServiceCount > 0 && settings.BookableStaffCount > 0
	if settings.PublicSlug == "" {
		settings.BlockedReason = "Add a public page slug before publishing."
	} else if settings.BookableServiceCount == 0 {
		settings.BlockedReason = "At least one active AI-bookable service must be synced and linked to the active POS provider."
	} else if settings.BookableStaffCount == 0 {
		settings.BlockedReason = "At least one active AI-bookable staff member must be synced and linked to the active POS provider."
	}
	return &settings, nil
}

func publicPath(slug string) string {
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	return "/s/" + strings.TrimSpace(slug)
}
