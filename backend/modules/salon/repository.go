package salon

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

var (
	ErrNotFound        = errors.New("salon not found")
	ErrSlugUnavailable = errors.New("public slug unavailable")
)

type Repository struct {
	db *sql.DB
}

const onboardingOperationLockPrefix = "salon-onboarding:"

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListForOwner(ctx context.Context, ownerUserID string) ([]Salon, error) {
	rows, err := r.db.QueryContext(ctx, salonSelect+`
		WHERE salon.owner_user_id = $1
		ORDER BY salon.created_at DESC
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
	row := r.db.QueryRowContext(ctx, salonSelect+`
		WHERE salon.id = $1 AND salon.owner_user_id = $2
	`, id, ownerUserID)
	return scanSalon(row)
}

func (r *Repository) Create(ctx context.Context, ownerUserID string, req CreateSalonRequest, payloadFingerprint string) (*Salon, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, onboardingOperationLockPrefix+ownerUserID+":"+req.OperationKey); err != nil {
		return nil, err
	}

	var existingSalonID string
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, creation_payload_fingerprint
		FROM salons
		WHERE owner_user_id = $1 AND creation_operation_key = $2
		FOR UPDATE
	`, ownerUserID, req.OperationKey).Scan(&existingSalonID, &existingFingerprint)
	if err == nil {
		if existingFingerprint != payloadFingerprint {
			return nil, ErrCreateOperationConflict
		}
		item, err := getSalonForOwnerTx(ctx, tx, existingSalonID, ownerUserID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, classifySalonConstraint(err)
	}

	var salonID string
	row := tx.QueryRowContext(ctx, `
		INSERT INTO salons (
			name, phone, address, city, state, zip_code, timezone, owner_user_id,
			primary_language, secondary_language, handoff_phone,
			creation_operation_key, creation_payload_fingerprint
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, NULLIF($11, ''), $12, $13)
		RETURNING id::text
	`, req.Name, req.Phone, req.Address, req.City, req.State, req.ZipCode, req.Timezone, ownerUserID, req.PrimaryLanguage, req.SecondaryLanguage, req.HandoffPhone, req.OperationKey, payloadFingerprint)
	if err := row.Scan(&salonID); err != nil {
		return nil, classifySalonConstraint(err)
	}

	// V64 also installs a trigger so salon creation by the previous compatible
	// image receives the same membership. Keep the insert explicit here because
	// membership is part of the new application's onboarding transaction, while
	// ON CONFLICT makes the trigger and repository paths converge safely.
	result, err := tx.ExecContext(ctx, `
		INSERT INTO salon_memberships (
			salon_id, user_id, role_id, status, is_owner,
			created_by_user_id, updated_by_user_id
		)
		SELECT $1, $2, role.id, 'active', true, $2, $2
		FROM roles AS role
		WHERE role.name = 'tenant_owner'
		  AND role.scope = 'tenant'
		ON CONFLICT (salon_id, user_id) DO NOTHING
	`, salonID, ownerUserID)
	if err != nil {
		return nil, classifySalonConstraint(err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		var ownerMembershipReady bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM salon_memberships AS membership
				JOIN roles AS role ON role.id = membership.role_id
				WHERE membership.salon_id = $1
				  AND membership.user_id = $2
				  AND membership.status = 'active'
				  AND membership.is_owner
				  AND role.name = 'tenant_owner'
			)
		`, salonID, ownerUserID).Scan(&ownerMembershipReady); err != nil {
			return nil, err
		}
		if !ownerMembershipReady {
			return nil, ErrNotFound
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, booking_mode, scheduling_authority)
		VALUES ($1, 'pending_approval', $2)
	`, salonID, req.SchedulingAuthority); err != nil {
		return nil, classifySalonConstraint(err)
	}
	for day := 0; day <= 6; day++ {
		isClosed := day == 0
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hours (salon_id, day_of_week, open_time, close_time, is_closed)
			VALUES ($1, $2, '09:30', '19:00', $3)
		`, salonID, day, isClosed); err != nil {
			return nil, classifySalonConstraint(err)
		}
	}
	item, err := getSalonForOwnerTx(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
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
	row := r.db.QueryRowContext(ctx, settingsSelect+`
		WHERE ss.salon_id = $1 AND public.has_active_tenant_membership(s.id, $2::uuid)
	`, salonID, ownerUserID)
	return scanSettings(row)
}

func (r *Repository) UpdateSettings(ctx context.Context, salonID string, ownerUserID string, req UpdateSettingsRequest) (*Settings, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID)); err != nil {
		return nil, err
	}
	var schedulingAuthority string
	err = tx.QueryRowContext(ctx, `
		SELECT ss.scheduling_authority
		FROM salon_settings ss
		JOIN salons s ON s.id = ss.salon_id
		WHERE ss.salon_id = $1 AND public.has_active_tenant_membership(s.id, $2::uuid)
		FOR UPDATE OF ss, s
	`, salonID, ownerUserID).Scan(&schedulingAuthority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if schedulingAuthority == "owner_manual" && req.BookingMode == "confirmed_booking" {
		return nil, ErrValidation
	}

	result, err := tx.ExecContext(ctx, `
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
	`, req.AIGreeting, req.AIVoice, req.AITone, req.BookingMode, req.RecordingEnabled, req.RecordingConsentMessage, req.SMSConfirmationEnabled, req.SMSReminderEnabled, req.ReminderHoursBefore, req.HandoffEnabled, req.ConsultationEnabled, salonID)
	if err != nil {
		return nil, classifySalonConstraint(err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	settings, err := scanSettings(tx.QueryRowContext(ctx, settingsSelect+`
		WHERE ss.salon_id = $1 AND public.has_active_tenant_membership(s.id, $2::uuid)
	`, salonID, ownerUserID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, classifySalonConstraint(err)
	}
	return settings, nil
}

func (r *Repository) CountConsultationReadyServices(ctx context.Context, salonID string, ownerUserID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT (
			SELECT COUNT(DISTINCT svc.id)::int
			FROM services svc
			JOIN pos_entity_links link
			  ON link.salon_id = svc.salon_id
			 AND link.entity_type = 'service'
			 AND link.entity_id = svc.id
			 AND link.provider = salon.active_pos_provider
			 AND link.sync_status = 'synced'
			 AND COALESCE(link.provider_entity_id, '') <> ''
			 AND COALESCE(link.provider_version, svc.pos_service_version, 0) > 0
			JOIN service_consultation_profiles profile
			  ON profile.salon_id = svc.salon_id
			 AND profile.service_id = svc.id
			 AND profile.status = 'ready'
			 AND jsonb_array_length(profile.recommended_outcomes) > 0
			 AND jsonb_array_length(profile.compatible_current_systems) > 0
			WHERE svc.salon_id = salon.id
			  AND svc.pos_provider = salon.active_pos_provider
			  AND svc.active = true
			  AND svc.ai_bookable = true
			  AND svc.archived_at IS NULL
			  AND svc.sync_status = 'synced'
			  AND svc.duration_minutes > 0
		)
		FROM salons salon
		WHERE salon.id = $1
		  AND salon.owner_user_id = $2
	`, salonID, ownerUserID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return count, err
}

func (r *Repository) GetPublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string) (*PublicCatalogSettings, error) {
	row := r.db.QueryRowContext(ctx, publicCatalogSettingsQuery(true), salonID, ownerUserID)
	return scanPublicCatalogSettings(row)
}

// GetPublicCatalogSettingsForSalon is the owner-neutral domain read used by
// already-authorized internal callers such as the shared SaaS Business module.
// HTTP handlers must authorize the Tenant or Platform surface before calling
// it; keeping readiness calculation here prevents a second authority policy.
func (r *Repository) GetPublicCatalogSettingsForSalon(ctx context.Context, salonID string) (*PublicCatalogSettings, error) {
	return PublicCatalogSettingsForSalon(ctx, r.db, salonID)
}

type publicCatalogQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// PublicCatalogSettingsForSalon exposes the canonical selected-authority
// readiness calculation to an already-authorized caller, including callers
// operating inside a transaction that holds the shared scheduling fence.
func PublicCatalogSettingsForSalon(ctx context.Context, queryer publicCatalogQueryer, salonID string) (*PublicCatalogSettings, error) {
	return scanPublicCatalogSettings(queryer.QueryRowContext(ctx, publicCatalogSettingsQuery(false), salonID))
}

func (r *Repository) UpdatePublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string, req UpdatePublicCatalogRequest) (*PublicCatalogSettings, error) {
	slug := strings.TrimSpace(req.PublicSlug)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Authority switches, provider snapshot writes, calendar configuration, and
	// publishing all share this salon fence. Readiness is reloaded under the
	// lock so a switch cannot create an ABA window between validation and write.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID)); err != nil {
		return nil, err
	}
	current, err := scanPublicCatalogSettings(tx.QueryRowContext(ctx, publicCatalogSettingsQuery(true), salonID, ownerUserID))
	if err != nil {
		return nil, err
	}
	if req.ExpectedSchedulingAuthorityVersion > 0 && req.ExpectedSchedulingAuthorityVersion != current.SchedulingAuthorityVersion {
		return nil, ErrSchedulingAuthorityChanged
	}
	if req.PublicCatalogEnabled {
		if slug == "" || hasPublicCatalogBlockerOtherThanSlug(current.ReadinessBlockers) {
			return nil, ErrPublicCatalogNotReady
		}
	}

	if slug != "" {
		var taken bool
		if err := tx.QueryRowContext(ctx, `
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

	result, err := tx.ExecContext(ctx, `
		UPDATE salons
		SET public_slug = NULLIF($1, ''),
		    public_catalog_enabled = $2,
		    updated_at = now()
		WHERE id = $3
		  AND owner_user_id = $4
	`, slug, req.PublicCatalogEnabled, salonID, ownerUserID)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.Constraint == "idx_salons_public_slug_unique" {
			return nil, ErrSlugUnavailable
		}
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	updated, err := scanPublicCatalogSettings(tx.QueryRowContext(ctx, publicCatalogSettingsQuery(true), salonID, ownerUserID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
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
		&item.SchedulingAuthority,
		&item.SchedulingAuthorityVersion,
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

func getSalonForOwnerTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) (*Salon, error) {
	return scanSalon(tx.QueryRowContext(ctx, salonSelect+`
		WHERE salon.id = $1 AND salon.owner_user_id = $2
	`, salonID, ownerUserID))
}

func classifySalonConstraint(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return err
	}
	switch pqErr.Constraint {
	case "salons_owner_creation_operation_key":
		return ErrCreateOperationConflict
	case "salon_settings_owner_manual_booking_mode_guard":
		return ErrValidation
	default:
		return err
	}
}

const salonSelect = `
	SELECT salon.id::text, salon.name, salon.phone,
	       COALESCE(salon.address, ''), COALESCE(salon.city, ''),
	       COALESCE(salon.state, ''), COALESCE(salon.zip_code, ''),
	       salon.timezone, salon.owner_user_id::text,
	       salon.primary_language, salon.secondary_language,
	       COALESCE(salon.handoff_phone, ''), salon.ai_enabled,
	       salon.active_pos_provider, COALESCE(salon.public_slug, ''),
	       salon.public_catalog_enabled, settings.scheduling_authority,
	       settings.scheduling_authority_version, salon.created_at, salon.updated_at
	FROM salons salon
	JOIN salon_settings settings ON settings.salon_id = salon.id
`

const settingsSelect = `
	SELECT ss.id::text, ss.salon_id::text, ss.scheduling_authority,
	       ss.scheduling_authority_version, ss.ai_greeting, ss.ai_voice,
	       COALESCE(ss.ai_tone, 'professional_warm'), ss.booking_mode,
	       ss.recording_enabled, ss.recording_consent_message,
	       ss.sms_confirmation_enabled, ss.sms_reminder_enabled,
	       ss.reminder_hours_before, ss.handoff_enabled,
	       ss.consultation_enabled, ss.created_at, ss.updated_at
	FROM salon_settings ss
	JOIN salons s ON s.id = ss.salon_id
`

func scanSettings(row rowScanner) (*Settings, error) {
	var settings Settings
	err := row.Scan(
		&settings.ID,
		&settings.SalonID,
		&settings.SchedulingAuthority,
		&settings.SchedulingAuthorityVersion,
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

func publicCatalogSettingsQuery(ownerScoped bool) string {
	ownerPredicate := ""
	if ownerScoped {
		ownerPredicate = " AND salon.owner_user_id = $2"
	}
	return `
		WITH owned_salon AS (
			SELECT salon.id, salon.active_pos_provider, salon.public_slug,
			       salon.public_catalog_enabled, salon.updated_at,
			       settings.scheduling_authority, settings.scheduling_authority_version
			FROM salons salon
			JOIN salon_settings settings ON settings.salon_id = salon.id
			WHERE salon.id = $1` + ownerPredicate + `
		),
		canonical_services AS (
			SELECT svc.id
			FROM services svc
			WHERE svc.salon_id = (SELECT id FROM owned_salon)
			  AND svc.active = true
			  AND svc.ai_bookable = true
			  AND svc.archived_at IS NULL
			  AND svc.duration_minutes > 0
		),
		canonical_staff AS (
			SELECT st.id
			FROM staff st
			WHERE st.salon_id = (SELECT id FROM owned_salon)
			  AND st.active = true
			  AND st.ai_bookable = true
			  AND st.archived_at IS NULL
		),
		external_services AS (
			SELECT svc.id
			FROM services svc
			JOIN pos_entity_links link
			  ON link.salon_id = svc.salon_id
			 AND link.entity_type = 'service'
			 AND link.entity_id = svc.id
			 AND link.provider = (SELECT active_pos_provider FROM owned_salon)
			 AND link.sync_status = 'synced'
			 AND NULLIF(link.provider_entity_id, '') IS NOT NULL
			WHERE svc.id IN (SELECT id FROM canonical_services)
			  AND svc.pos_provider = (SELECT active_pos_provider FROM owned_salon)
			  AND svc.sync_status = 'synced'
			  AND COALESCE(svc.pos_service_version, 0) > 0
		),
		external_staff AS (
			SELECT st.id
			FROM staff st
			JOIN pos_entity_links link
			  ON link.salon_id = st.salon_id
			 AND link.entity_type = 'staff'
			 AND link.entity_id = st.id
			 AND link.provider = (SELECT active_pos_provider FROM owned_salon)
			 AND link.sync_status = 'synced'
			 AND NULLIF(link.provider_entity_id, '') IS NOT NULL
			WHERE st.id IN (SELECT id FROM canonical_staff)
			  AND st.pos_provider = (SELECT active_pos_provider FROM owned_salon)
			  AND st.sync_status = 'synced'
		),
		internal_services AS (
			SELECT service.id
			FROM canonical_services service
			JOIN manleai_calendar_service_policies policy
			  ON policy.salon_id = (SELECT id FROM owned_salon)
			 AND policy.service_id = service.id
			 AND policy.enabled = true
		),
		internal_staff AS (
			SELECT DISTINCT staff.id
			FROM canonical_staff staff
			JOIN manleai_calendar_service_staff eligible
			  ON eligible.salon_id = (SELECT id FROM owned_salon)
			 AND eligible.staff_id = staff.id
			JOIN internal_services service ON service.id = eligible.service_id
			WHERE EXISTS (
				SELECT 1 FROM manleai_calendar_staff_weekly_periods weekly
				WHERE weekly.salon_id = eligible.salon_id AND weekly.staff_id = staff.id
			)
		),
		authority_facts AS (
			SELECT
				EXISTS (
					SELECT 1 FROM pos_connections connection
					WHERE connection.salon_id = (SELECT id FROM owned_salon)
					  AND connection.provider = (SELECT active_pos_provider FROM owned_salon)
					  AND connection.status = 'active'
					  AND NULLIF(connection.location_id, '') IS NOT NULL
					  AND connection.last_sync_at IS NOT NULL
					  AND connection.snapshot_generation > 0
				) AS external_connection_ready,
				EXISTS (
					SELECT 1 FROM manleai_calendar_configs config
					WHERE config.salon_id = (SELECT id FROM owned_salon)
					  AND config.activated_at IS NOT NULL
					  AND config.activated_version = config.version
				) AS internal_activation_current,
				(SELECT count(*)::int FROM salon_business_hour_periods period
				 WHERE period.salon_id = (SELECT id FROM owned_salon)
				   AND period.source = 'local_override') AS local_hour_count,
				(SELECT count(*)::int FROM salon_business_hour_periods period
				 WHERE period.salon_id = (SELECT id FROM owned_salon)
				   AND period.source = 'imported'
				   AND period.provider = (SELECT active_pos_provider FROM owned_salon)) AS external_hour_count,
				(SELECT count(*)::int FROM salon_business_hour_periods period
				 WHERE period.salon_id = (SELECT id FROM owned_salon)
				   AND period.source IN ('local_override', 'local_migrated')) AS owner_hour_count
		)
		SELECT salon.id::text, COALESCE(salon.public_slug, ''), salon.public_catalog_enabled,
		       salon.scheduling_authority, salon.scheduling_authority_version,
		       CASE salon.scheduling_authority
		         WHEN 'owner_manual' THEN (SELECT count(*)::int FROM canonical_services)
		         WHEN 'manleai_calendar' THEN (SELECT count(*)::int FROM internal_services)
		         WHEN 'external_provider' THEN (SELECT count(*)::int FROM external_services)
		         ELSE 0
		       END,
		       CASE salon.scheduling_authority
		         WHEN 'owner_manual' THEN (SELECT count(*)::int FROM canonical_staff)
		         WHEN 'manleai_calendar' THEN (SELECT count(*)::int FROM internal_staff)
		         WHEN 'external_provider' THEN (SELECT count(*)::int FROM external_staff)
		         ELSE 0
		       END,
		       CASE salon.scheduling_authority
		         WHEN 'owner_manual' THEN facts.owner_hour_count
		         WHEN 'manleai_calendar' THEN facts.local_hour_count
		         WHEN 'external_provider' THEN facts.external_hour_count
		         ELSE 0
		       END,
		       facts.internal_activation_current, facts.external_connection_ready,
		       salon.updated_at
		FROM owned_salon salon
		CROSS JOIN authority_facts facts
	`
}

func scanPublicCatalogSettings(row rowScanner) (*PublicCatalogSettings, error) {
	var settings PublicCatalogSettings
	var internalActivationCurrent bool
	var externalConnectionReady bool
	err := row.Scan(
		&settings.SalonID,
		&settings.PublicSlug,
		&settings.PublicCatalogEnabled,
		&settings.SchedulingAuthority,
		&settings.SchedulingAuthorityVersion,
		&settings.EligibleServiceCount,
		&settings.EligibleStaffCount,
		&settings.PublishedHoursCount,
		&internalActivationCurrent,
		&externalConnectionReady,
		&settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	settings.BookableServiceCount = settings.EligibleServiceCount
	settings.BookableStaffCount = settings.EligibleStaffCount
	settings.PublicPath = publicPath(settings.PublicSlug)
	applyPublicCatalogReadinessWithFacts(&settings, internalActivationCurrent, externalConnectionReady)
	return &settings, nil
}

func hasPublicCatalogBlockerOtherThanSlug(blockers []PublicCatalogReadinessBlocker) bool {
	for _, blocker := range blockers {
		if blocker.Code != "PUBLIC_SLUG_REQUIRED" {
			return true
		}
	}
	return false
}

func applyPublicCatalogReadinessWithFacts(settings *PublicCatalogSettings, internalActivationCurrent bool, externalConnectionReady bool) {
	settings.ReadinessBlockers = make([]PublicCatalogReadinessBlocker, 0)
	add := func(code string, scope string, message string) {
		settings.ReadinessBlockers = append(settings.ReadinessBlockers, PublicCatalogReadinessBlocker{Code: code, Scope: scope, Message: message})
	}
	if settings.PublicSlug == "" {
		add("PUBLIC_SLUG_REQUIRED", "public_page", "Add a public page slug before publishing.")
	}
	if settings.EligibleServiceCount == 0 {
		add("PUBLIC_SERVICE_REQUIRED", "services", "Add at least one active service that is eligible for the selected scheduling method.")
	}
	switch settings.SchedulingAuthority {
	case booking.SchedulingAuthorityOwnerManual:
		settings.ReadinessLabel = "Owner-managed appointment requests"
	case booking.SchedulingAuthorityManleAICalendar:
		settings.ReadinessLabel = "ManleAI Calendar catalog and hours"
		if settings.PublishedHoursCount == 0 {
			add("PUBLIC_LOCAL_HOURS_REQUIRED", "hours", "Add local business hours before publishing the ManleAI Calendar page.")
		}
		if !internalActivationCurrent {
			add("PUBLIC_CALENDAR_ACTIVATION_REQUIRED", "calendar", "Activate the current ManleAI Calendar configuration before publishing.")
		}
	case booking.SchedulingAuthorityExternalProvider:
		settings.ReadinessLabel = "Connected scheduling catalog"
		if !externalConnectionReady {
			add("PUBLIC_EXTERNAL_CATALOG_NOT_READY", "integration", "Complete the connected scheduling catalog sync before publishing.")
		}
		if settings.EligibleStaffCount == 0 {
			add("PUBLIC_EXTERNAL_STAFF_REQUIRED", "staff", "Sync at least one eligible staff member for connected scheduling.")
		}
	default:
		settings.ReadinessLabel = "Scheduling method unavailable"
		add("PUBLIC_AUTHORITY_UNSUPPORTED", "scheduling", "Select a supported scheduling method before publishing.")
	}
	settings.CanPublish = len(settings.ReadinessBlockers) == 0
	settings.BlockedReason = ""
	if len(settings.ReadinessBlockers) > 0 {
		settings.BlockedReason = settings.ReadinessBlockers[0].Message
	}
}

func publicPath(slug string) string {
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	return "/s/" + strings.TrimSpace(slug)
}
