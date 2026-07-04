package configtransfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/salon"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) TargetImportState(ctx context.Context, salonID string, ownerUserID string, current *ConfigurationBundle) (*importTargetState, error) {
	if current == nil {
		return nil, ErrValidation
	}
	if err := r.ensureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	publicCanPublish, err := r.publicCanPublish(ctx, salonID)
	if err != nil {
		return nil, err
	}
	canEnableAI, err := r.canEnableAIBooking(ctx, salonID)
	if err != nil {
		return nil, err
	}
	byImportKey := map[string]KnowledgeItemExport{}
	byContentHash := map[string]KnowledgeItemExport{}
	categoryBySlug := map[string]ServiceCategoryExport{}
	categoryAliasByKey := map[string]ServiceCategoryAliasExport{}
	for _, item := range current.KnowledgeBase.Items {
		key := strings.TrimSpace(item.SourceKey)
		if key != "" {
			byImportKey[key] = item
		}
		byContentHash[knowledgeContentHash(item)] = item
	}
	for _, item := range current.ServiceCategories.Items {
		slug := strings.TrimSpace(item.Slug)
		if slug != "" {
			categoryBySlug[slug] = item
		}
		for _, alias := range item.Aliases {
			key := strings.TrimSpace(alias.NormalizedAlias)
			if key != "" {
				categoryAliasByKey[key] = alias
			}
		}
	}
	activeServiceAliasKeys, err := r.activeServiceAliasKeys(ctx, salonID)
	if err != nil {
		return nil, err
	}
	return &importTargetState{
		SalonProfile:           current.SalonProfile,
		AIReceptionist:         current.AIReceptionist,
		PublicBookingPage:      current.PublicBookingPage,
		PublicCanPublish:       publicCanPublish,
		CanEnableAIBooking:     canEnableAI,
		Integrations:           current.Integrations,
		ServiceCategoryBySlug:  categoryBySlug,
		CategoryAliasByKey:     categoryAliasByKey,
		ActiveServiceAliasKeys: activeServiceAliasKeys,
		KnowledgeByImportKey:   byImportKey,
		KnowledgeByContentHash: byContentHash,
	}, nil
}

func (r *Repository) PublicSlugTaken(ctx context.Context, salonID string, slug string) (bool, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false, nil
	}
	var taken bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM salons
			WHERE lower(public_slug) = lower($1)
			  AND ($2 = '' OR id::text <> $2)
		)
	`, slug, salonID).Scan(&taken)
	return taken, err
}

func (r *Repository) OwnerHasSalon(ctx context.Context, ownerUserID string) (bool, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return false, ErrValidation
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM salons
			WHERE owner_user_id = $1
		)
	`, ownerUserID).Scan(&exists)
	return exists, err
}

func (r *Repository) activeServiceAliasKeys(ctx context.Context, salonID string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT normalized_alias
		FROM service_aliases
		WHERE salon_id = $1
		  AND status = 'active'
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

func (r *Repository) ExistingOnboardingImport(ctx context.Context, ownerUserID string, requestID string, fingerprint string) (string, string, bool, bool, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	requestID = strings.TrimSpace(requestID)
	if ownerUserID == "" || requestID == "" {
		return "", "", false, false, ErrValidation
	}
	var salonID string
	var runID string
	var existingFingerprint string
	err := r.db.QueryRowContext(ctx, `
		SELECT run.salon_id::text, run.id::text, run.payload_fingerprint
		FROM configuration_import_runs run
		WHERE run.owner_user_id = $1
		  AND run.request_id = $2
		ORDER BY run.created_at DESC
		LIMIT 1
	`, ownerUserID, requestID).Scan(&salonID, &runID, &existingFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, false, nil
	}
	if err != nil {
		return "", "", false, false, err
	}
	return salonID, runID, true, existingFingerprint == fingerprint, nil
}

func (r *Repository) ApplyImport(ctx context.Context, salonID string, ownerUserID string, plan *importPlan) (string, bool, error) {
	if plan == nil || !plan.CanApply {
		return "", false, ErrImportConflict
	}
	if plan.RequestID == "" || plan.PayloadFingerprint == "" {
		return "", false, ErrValidation
	}
	existing, samePayload, err := r.existingImportRun(ctx, salonID, ownerUserID, plan.RequestID, plan.PayloadFingerprint)
	if err != nil {
		return "", false, err
	}
	if existing != "" {
		if !samePayload {
			return existing, true, ErrImportConflict
		}
		return existing, true, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()

	if err := r.ensureSalonOwnerTx(ctx, tx, salonID, ownerUserID); err != nil {
		return "", false, err
	}
	if err := r.updateSalonProfile(ctx, tx, salonID, ownerUserID, plan.Bundle.SalonProfile, plan.AIEnabled); err != nil {
		return "", false, err
	}
	if err := r.updateAIReceptionist(ctx, tx, salonID, ownerUserID, plan.Bundle.AIReceptionist, plan.BookingMode); err != nil {
		return "", false, err
	}
	if err := r.updatePublicBookingPage(ctx, tx, salonID, ownerUserID, plan.Bundle.PublicBookingPage, plan.PublicCatalogEnabled); err != nil {
		return "", false, err
	}
	if err := r.upsertIntegrationConfigs(ctx, tx, salonID, plan.Bundle.Integrations); err != nil {
		return "", false, err
	}
	if err := r.upsertServiceCategories(ctx, tx, salonID, plan.ServiceCategories); err != nil {
		return "", false, err
	}
	if err := r.upsertKnowledge(ctx, tx, salonID, plan.Knowledge); err != nil {
		return "", false, err
	}
	runID, err := r.createImportRun(ctx, tx, salonID, ownerUserID, plan)
	if err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return runID, false, nil
}

func (r *Repository) ApplyOnboardingImport(ctx context.Context, ownerUserID string, plan *importPlan) (string, string, bool, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if plan == nil || !plan.CanApply {
		return "", "", false, ErrImportConflict
	}
	if ownerUserID == "" || plan.RequestID == "" || plan.PayloadFingerprint == "" {
		return "", "", false, ErrValidation
	}
	existingSalonID, existingRunID, exists, samePayload, err := r.ExistingOnboardingImport(ctx, ownerUserID, plan.RequestID, plan.PayloadFingerprint)
	if err != nil {
		return "", "", false, err
	}
	if exists {
		if !samePayload {
			return existingSalonID, existingRunID, true, ErrImportConflict
		}
		return existingSalonID, existingRunID, true, nil
	}
	hasSalon, err := r.OwnerHasSalon(ctx, ownerUserID)
	if err != nil {
		return "", "", false, err
	}
	if hasSalon {
		return "", "", false, ErrOnboardingSalonExists
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback()

	var salonID string
	profile := plan.Bundle.SalonProfile
	err = tx.QueryRowContext(ctx, `
		INSERT INTO salons (
			name, phone, address, city, state, zip_code, timezone, owner_user_id,
			primary_language, secondary_language, handoff_phone, ai_enabled,
			active_pos_provider, public_slug, public_catalog_enabled
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8,
		        $9, $10, NULLIF($11, ''), $12, $13, NULLIF($14, ''), $15)
		RETURNING id::text
	`, profile.Name, profile.Phone, profile.Address, profile.City, profile.State, profile.ZipCode, profile.Timezone, ownerUserID, profile.PrimaryLanguage, profile.SecondaryLanguage, profile.HandoffPhone, plan.AIEnabled, profile.ActivePOSProvider, plan.Bundle.PublicBookingPage.PublicSlug, plan.PublicCatalogEnabled).Scan(&salonID)
	if err != nil {
		return "", "", false, err
	}

	settings := plan.Bundle.AIReceptionist
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (
			salon_id, ai_greeting, ai_voice, ai_tone, booking_mode, recording_enabled,
			recording_consent_message, sms_confirmation_enabled, sms_reminder_enabled,
			reminder_hours_before, handoff_enabled
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, salonID, settings.AIGreeting, settings.AIVoice, settings.AITone, plan.BookingMode, settings.RecordingEnabled, settings.RecordingConsentMessage, settings.SMSConfirmationEnabled, settings.SMSReminderEnabled, settings.ReminderHoursBefore, settings.HandoffEnabled); err != nil {
		return "", "", false, err
	}
	if err := insertDefaultBusinessHours(ctx, tx, salonID); err != nil {
		return "", "", false, err
	}
	if err := r.upsertIntegrationConfigs(ctx, tx, salonID, plan.Bundle.Integrations); err != nil {
		return "", "", false, err
	}
	if err := r.upsertServiceCategories(ctx, tx, salonID, plan.ServiceCategories); err != nil {
		return "", "", false, err
	}
	if err := r.upsertKnowledge(ctx, tx, salonID, plan.Knowledge); err != nil {
		return "", "", false, err
	}
	runID, err := r.createImportRun(ctx, tx, salonID, ownerUserID, plan)
	if err != nil {
		return "", "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", "", false, err
	}
	return salonID, runID, false, nil
}

func (r *Repository) existingImportRun(ctx context.Context, salonID string, ownerUserID string, requestID string, fingerprint string) (string, bool, error) {
	var id string
	var existingFingerprint string
	err := r.db.QueryRowContext(ctx, `
		SELECT run.id::text, run.payload_fingerprint
		FROM configuration_import_runs run
		JOIN salons salon ON salon.id = run.salon_id
		WHERE run.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND run.request_id = $3
	`, salonID, ownerUserID, requestID).Scan(&id, &existingFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, existingFingerprint == fingerprint, nil
}

func insertDefaultBusinessHours(ctx context.Context, tx *sql.Tx, salonID string) error {
	for day := 0; day <= 6; day++ {
		isClosed := day == 0
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hours (salon_id, day_of_week, open_time, close_time, is_closed)
			VALUES ($1, $2, '09:30', '19:00', $3)
		`, salonID, day, isClosed); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ensureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)
	`, salonID, ownerUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return salon.ErrNotFound
	}
	return nil
}

func (r *Repository) ensureSalonOwnerTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM salons
		WHERE id = $1
		  AND owner_user_id = $2
		FOR UPDATE
	`, salonID, ownerUserID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return salon.ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) publicCanPublish(ctx context.Context, salonID string) (bool, error) {
	var serviceCount int
	var staffCount int
	err := r.db.QueryRowContext(ctx, publicReadinessQuery(), salonID).Scan(&serviceCount, &staffCount)
	return serviceCount > 0 && staffCount > 0, err
}

func (r *Repository) canEnableAIBooking(ctx context.Context, salonID string) (bool, error) {
	var connected bool
	var serviceCount int
	var staffCount int
	var periodCount int
	err := r.db.QueryRowContext(ctx, aiBookingReadinessQuery(), salonID).Scan(&connected, &serviceCount, &staffCount, &periodCount)
	return connected && serviceCount > 0 && staffCount > 0 && periodCount > 0, err
}

func (r *Repository) updateSalonProfile(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, profile SalonProfileExport, aiEnabled bool) error {
	result, err := tx.ExecContext(ctx, `
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
		    active_pos_provider = $12,
		    updated_at = now()
		WHERE id = $13
		  AND owner_user_id = $14
	`, profile.Name, profile.Phone, profile.Address, profile.City, profile.State, profile.ZipCode, profile.Timezone, profile.PrimaryLanguage, profile.SecondaryLanguage, profile.HandoffPhone, aiEnabled, profile.ActivePOSProvider, salonID, ownerUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) updateAIReceptionist(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, settings AIReceptionistExport, bookingMode string) error {
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
		    updated_at = now()
		WHERE salon_id = $11
		  AND EXISTS (SELECT 1 FROM salons WHERE salons.id = salon_settings.salon_id AND salons.owner_user_id = $12)
	`, settings.AIGreeting, settings.AIVoice, settings.AITone, bookingMode, settings.RecordingEnabled, settings.RecordingConsentMessage, settings.SMSConfirmationEnabled, settings.SMSReminderEnabled, settings.ReminderHoursBefore, settings.HandoffEnabled, salonID, ownerUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) updatePublicBookingPage(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, page PublicBookingPageExport, enabled bool) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE salons
		SET public_slug = NULLIF($1, ''),
		    public_catalog_enabled = $2,
		    updated_at = now()
		WHERE id = $3
		  AND owner_user_id = $4
	`, page.PublicSlug, enabled, salonID, ownerUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) upsertIntegrationConfigs(ctx context.Context, tx *sql.Tx, salonID string, configs integrationconfig.IntegrationConfigsResponse) error {
	if err := upsertSquareConfig(ctx, tx, salonID, configs.Square); err != nil {
		return err
	}
	if err := upsertTwilioConfig(ctx, tx, salonID, configs.Twilio); err != nil {
		return err
	}
	return upsertOpenAIConfig(ctx, tx, salonID, configs.OpenAI)
}

func upsertSquareConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.SquareSettingsResponse) error {
	settings := map[string]string{
		"environment":  cfg.Environment,
		"client_id":    cfg.ClientID,
		"redirect_url": cfg.RedirectURL,
		"api_version":  cfg.APIVersion,
		"api_base_url": cfg.APIBaseURL,
	}
	return upsertConfigSettings(ctx, tx, salonID, "square", true, settings)
}

func upsertTwilioConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.TwilioSettingsResponse) error {
	settings := map[string]string{
		"public_base_url": cfg.PublicBaseURL,
		"incoming_path":   cfg.IncomingPath,
		"turn_path":       cfg.TurnPath,
		"recording_path":  cfg.RecordingPath,
	}
	return upsertConfigSettings(ctx, tx, salonID, "twilio", true, settings)
}

func upsertOpenAIConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.OpenAISettingsResponse) error {
	settings := map[string]string{
		"base_url":            cfg.BaseURL,
		"transcription_model": cfg.TranscriptionModel,
		"reply_model":         cfg.ReplyModel,
		"speech_model":        cfg.SpeechModel,
		"speech_voice":        cfg.SpeechVoice,
	}
	return upsertConfigSettings(ctx, tx, salonID, "openai", cfg.Enabled, settings)
}

func upsertConfigSettings(ctx context.Context, tx *sql.Tx, salonID string, provider string, enabled bool, settings map[string]string) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO salon_integration_configs (salon_id, provider, enabled, settings, secrets_encrypted)
		VALUES ($1, $2, $3, $4::jsonb, NULL)
		ON CONFLICT (salon_id, provider)
		DO UPDATE SET enabled = EXCLUDED.enabled,
		              settings = EXCLUDED.settings,
		              updated_at = now()
	`, salonID, provider, enabled, string(settingsJSON))
	return err
}

func (r *Repository) upsertServiceCategories(ctx context.Context, tx *sql.Tx, salonID string, items []plannedServiceCategory) error {
	categoryIDs := map[string]string{}
	for _, planned := range items {
		item := planned.Item
		if item.Slug == "" {
			continue
		}
		var categoryID string
		if planned.Operation == "unchanged" {
			err := tx.QueryRowContext(ctx, `
				SELECT id::text
				FROM service_categories
				WHERE salon_id = $1
				  AND slug = $2
			`, salonID, item.Slug).Scan(&categoryID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if categoryID == "" {
			err := tx.QueryRowContext(ctx, `
				INSERT INTO service_categories (
					salon_id, name, slug, description, status, source, sort_order, archived_at
				)
				VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7,
				        CASE WHEN $5 = 'archived' THEN now() ELSE NULL END)
				ON CONFLICT (salon_id, slug)
				DO UPDATE SET name = EXCLUDED.name,
				              description = EXCLUDED.description,
				              status = EXCLUDED.status,
				              source = EXCLUDED.source,
				              sort_order = EXCLUDED.sort_order,
				              archived_at = CASE WHEN EXCLUDED.status = 'archived' THEN COALESCE(service_categories.archived_at, now()) ELSE NULL END,
				              updated_at = now()
				RETURNING id::text
			`, salonID, item.Name, item.Slug, item.Description, item.Status, item.Source, item.SortOrder).Scan(&categoryID)
			if err != nil {
				return err
			}
		}
		categoryIDs[item.Slug] = categoryID
	}

	for _, planned := range items {
		categoryID := categoryIDs[planned.Item.Slug]
		if categoryID == "" {
			continue
		}
		for _, aliasPlan := range planned.Aliases {
			if aliasPlan.Operation == "unchanged" {
				continue
			}
			alias := aliasPlan.Item
			if alias.NormalizedAlias == "" {
				continue
			}
			conflict, err := activeServiceAliasExistsTx(ctx, tx, salonID, alias.NormalizedAlias)
			if err != nil {
				return err
			}
			if conflict {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO service_category_aliases (
					salon_id, category_id, alias, normalized_alias, source, status, confidence
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (salon_id, normalized_alias)
				DO UPDATE SET category_id = EXCLUDED.category_id,
				              alias = EXCLUDED.alias,
				              source = EXCLUDED.source,
				              status = EXCLUDED.status,
				              confidence = EXCLUDED.confidence,
				              updated_at = now()
			`, salonID, categoryID, alias.Alias, alias.NormalizedAlias, alias.Source, alias.Status, alias.Confidence); err != nil {
				return err
			}
		}
	}
	return nil
}

func activeServiceAliasExistsTx(ctx context.Context, tx *sql.Tx, salonID string, normalizedAlias string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_aliases
			WHERE salon_id = $1
			  AND normalized_alias = $2
			  AND status = 'active'
		)
	`, salonID, normalizedAlias).Scan(&exists)
	return exists, err
}

func (r *Repository) upsertKnowledge(ctx context.Context, tx *sql.Tx, salonID string, items []plannedKnowledgeItem) error {
	for _, planned := range items {
		if planned.Operation == "unchanged" || planned.Operation == "skipped" {
			continue
		}
		item := planned.Item
		_, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_items (salon_id, import_key, title, category, body, status, source)
			VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7)
			ON CONFLICT (salon_id, import_key) WHERE import_key IS NOT NULL
			DO UPDATE SET title = EXCLUDED.title,
			              category = EXCLUDED.category,
			              body = EXCLUDED.body,
			              status = EXCLUDED.status,
			              source = EXCLUDED.source,
			              updated_at = now()
		`, salonID, item.SourceKey, item.Title, item.Category, item.Body, item.Status, item.Source)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) createImportRun(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, plan *importPlan) (string, error) {
	summaryJSON, err := json.Marshal(summaryValues(plan.Summary))
	if err != nil {
		return "", err
	}
	warningsJSON, err := json.Marshal(plan.Warnings)
	if err != nil {
		return "", err
	}
	conflictsJSON, err := json.Marshal(plan.Conflicts)
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO configuration_import_runs (
			salon_id, owner_user_id, request_id, schema_version, payload_fingerprint, status, summary, warnings, conflicts
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb)
		RETURNING id::text
	`, salonID, ownerUserID, plan.RequestID, plan.SchemaVersion, plan.PayloadFingerprint, StatusApplied, string(summaryJSON), string(warningsJSON), string(conflictsJSON)).Scan(&id)
	return id, err
}

func publicReadinessQuery() string {
	return `
		WITH owned_salon AS (
			SELECT id, active_pos_provider
			FROM salons
			WHERE id = $1
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
		SELECT (SELECT count(*)::int FROM service_rows WHERE linked = true),
		       (SELECT count(*)::int FROM staff_rows WHERE linked = true)
	`
}

func aiBookingReadinessQuery() string {
	return `
		WITH owned_salon AS (
			SELECT id, active_pos_provider
			FROM salons
			WHERE id = $1
		),
		connection_ready AS (
			SELECT EXISTS (
				SELECT 1
				FROM pos_connections pc
				WHERE pc.salon_id = (SELECT id FROM owned_salon)
				  AND pc.provider = (SELECT active_pos_provider FROM owned_salon)
				  AND pc.status NOT IN ('not_connected', 'error', 'expired_token', 'disabled')
				  AND COALESCE(pc.location_id, '') <> ''
				  AND pc.last_sync_at IS NOT NULL
			) AS ready
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
		),
		business_periods AS (
			SELECT count(*)::int AS count
			FROM salon_business_hour_periods
			WHERE salon_id = (SELECT id FROM owned_salon)
			  AND source = 'imported'
			  AND provider = (SELECT active_pos_provider FROM owned_salon)
		)
		SELECT (SELECT ready FROM connection_ready),
		       (SELECT count(*)::int FROM service_rows WHERE linked = true),
		       (SELECT count(*)::int FROM staff_rows WHERE linked = true),
		       (SELECT count FROM business_periods)
	`
}
