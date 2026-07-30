package configtransfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/lib/pq"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

var (
	ErrTransferStale          = errors.New("configuration transfer preview is stale")
	ErrTransferPreview        = errors.New("configuration transfer preview not found")
	ErrTransferActionConflict = errors.New("configuration transfer action conflict")
	ErrTransferTooLarge       = errors.New("configuration transfer JSON is too large")
)

type platformTransferPlan struct {
	Legacy               *importPlan
	SourceType           string
	SourceSalonID        string
	SourceFingerprint    string
	RequestFingerprint   string
	SourceFences         map[string]int64
	TargetFences         map[string]int64
	AIEnabled            bool
	AIEnabledChanged     bool
	AIPolicyChanged      bool
	IntegrationProviders []string
	LocalHoursChanged    bool
}

type storedPlatformTransferRun struct {
	ID                     string
	SalonID                string
	SourceType             string
	SourceSalonID          string
	ActorUserID            string
	SchemaVersion          string
	IncludedSections       []string
	SourceFingerprint      string
	RequestFingerprint     string
	SourceFences           map[string]int64
	TargetFences           map[string]int64
	TargetAuthority        string
	TargetAuthorityVersion int64
	SourceActiveProvider   string
	TargetActiveProvider   string
	RequiresSecretReentry  []string
	Status                 string
	ActionKey              string
	Summary                []ImportSectionSummary
	Warnings               []ImportIssue
	Conflicts              []ImportIssue
	CreatedAt              time.Time
	AppliedAt              *time.Time
}

type PlatformRepository struct {
	db     *sql.DB
	legacy *Repository
}

func NewPlatformRepository(db *sql.DB) *PlatformRepository {
	return &PlatformRepository{db: db, legacy: NewRepository(db)}
}

func (r *PlatformRepository) SalonOwnerID(ctx context.Context, salonID string) (string, error) {
	var ownerID string
	err := r.db.QueryRowContext(ctx, `SELECT owner_user_id::text FROM salons WHERE id=$1`, salonID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTransferPreview
	}
	return ownerID, err
}

func (r *PlatformRepository) LocalBusinessHours(ctx context.Context, salonID string) (LocalBusinessHoursExport, error) {
	var authority string
	if err := r.db.QueryRowContext(ctx, `SELECT scheduling_authority FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority); errors.Is(err, sql.ErrNoRows) {
		return LocalBusinessHoursExport{}, ErrTransferPreview
	} else if err != nil {
		return LocalBusinessHoursExport{}, err
	}
	mode := "local"
	if authority == "external_provider" {
		mode = "provider_read_only"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT day_of_week,to_char(start_local_time,'HH24:MI'),to_char(end_local_time,'HH24:MI'),end_at_midnight
		FROM salon_business_hour_periods
		WHERE salon_id=$1 AND source='local_override'
		ORDER BY day_of_week,start_local_time,id
	`, salonID)
	if err != nil {
		return LocalBusinessHoursExport{}, err
	}
	defer rows.Close()
	periods := []LocalBusinessHourPeriodExport{}
	for rows.Next() {
		var item LocalBusinessHourPeriodExport
		if err := rows.Scan(&item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime, &item.EndAtMidnight); err != nil {
			return LocalBusinessHoursExport{}, err
		}
		periods = append(periods, item)
	}
	return LocalBusinessHoursExport{ManagementMode: mode, Periods: periods}, rows.Err()
}

func (r *PlatformRepository) SnapshotFences(ctx context.Context, salonID string, sections []string) (map[string]int64, string, int64, error) {
	return snapshotTransferFences(ctx, r.db, salonID, sections, false)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func snapshotTransferFences(ctx context.Context, q queryer, salonID string, sections []string, lock bool) (map[string]int64, string, int64, error) {
	selected := sectionSet(sections)
	businessTypes := []string{}
	if selected[SectionSalon] {
		businessTypes = append(businessTypes, "salon_profile")
	}
	if selected[SectionPublic] {
		businessTypes = append(businessTypes, "public_catalog")
	}
	if selected[SectionLocalHours] {
		businessTypes = append(businessTypes, "business_hours")
	}
	if selected[SectionCategories] {
		businessTypes = append(businessTypes, "service_category", "service_categories")
	}
	if selected[SectionServiceAliases] {
		businessTypes = append(businessTypes, "service_aliases")
	}
	if selected[SectionConsultation] {
		businessTypes = append(businessTypes, "consultation_profiles", "service")
	}
	if selected[SectionAI] {
		// AI consultation enablement may depend on existing destination service
		// profiles even when the consultation-profile section is not selected.
		businessTypes = append(businessTypes, "consultation_profiles", "service")
	}
	if selected[SectionKnowledge] {
		businessTypes = append(businessTypes, "knowledge_base")
	}
	technicalTypes := []string{}
	if selected[SectionAI] {
		technicalTypes = append(technicalTypes, "ai_receptionist", "ai_runtime")
	}
	if selected[SectionIntegrations] {
		technicalTypes = append(technicalTypes, "integration_config")
	}
	result := map[string]int64{}
	forUpdate := ""
	if lock {
		forUpdate = " FOR UPDATE"
	}
	if len(businessTypes) > 0 {
		rows, err := q.QueryContext(ctx, `SELECT resource_type,resource_id,version FROM business_resource_versions WHERE salon_id=$1 AND resource_type=ANY($2)`+forUpdate, salonID, stringArray(businessTypes))
		if err != nil {
			return nil, "", 0, err
		}
		for rows.Next() {
			var resourceType, resourceID string
			var version int64
			if err := rows.Scan(&resourceType, &resourceID, &version); err != nil {
				rows.Close()
				return nil, "", 0, err
			}
			result["business:"+resourceType+":"+resourceID] = version
		}
		if err := rows.Close(); err != nil {
			return nil, "", 0, err
		}
	}
	if len(technicalTypes) > 0 {
		rows, err := q.QueryContext(ctx, `SELECT resource_type,resource_id,version FROM technical_resource_versions WHERE salon_id=$1 AND resource_type=ANY($2)`+forUpdate, salonID, stringArray(technicalTypes))
		if err != nil {
			return nil, "", 0, err
		}
		for rows.Next() {
			var resourceType, resourceID string
			var version int64
			if err := rows.Scan(&resourceType, &resourceID, &version); err != nil {
				rows.Close()
				return nil, "", 0, err
			}
			result["technical:"+resourceType+":"+resourceID] = version
		}
		if err := rows.Close(); err != nil {
			return nil, "", 0, err
		}
	}
	var authority string
	var authorityVersion int64
	if err := q.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`+forUpdate, salonID).Scan(&authority, &authorityVersion); errors.Is(err, sql.ErrNoRows) {
		return nil, "", 0, ErrTransferPreview
	} else if err != nil {
		return nil, "", 0, err
	}
	result["scheduling_authority"] = authorityVersion
	return result, authority, authorityVersion, nil
}

// pq.Array is deliberately kept behind this tiny adapter so fence queries stay
// explicit and deterministic.
func stringArray(values []string) any { return pq.Array(values) }

func (r *PlatformRepository) CreatePreview(ctx context.Context, salonID, actorUserID string, plan *platformTransferPlan) (*storedPlatformTransferRun, error) {
	if plan == nil || plan.Legacy == nil {
		return nil, ErrValidation
	}
	response := importResponse(plan.Legacy, true, "")
	fences, err := json.Marshal(map[string]any{"source": plan.SourceFences, "target": plan.TargetFences})
	if err != nil {
		return nil, err
	}
	summary, _ := json.Marshal(response.Summary)
	warnings, _ := json.Marshal(response.Warnings)
	conflicts, _ := json.Marshal(response.Conflicts)
	details, _ := json.Marshal(map[string]any{
		"source_type": plan.SourceType, "source_salon_id": emptyAsNil(plan.SourceSalonID),
		"included_sections": plan.Legacy.Bundle.IncludedSections, "schema_version": plan.Legacy.SchemaVersion,
	})
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var runID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO configuration_transfer_runs (
			salon_id,source_type,source_salon_id,actor_user_id,schema_version,included_sections,
			source_fingerprint,request_fingerprint,target_fences,target_scheduling_authority,
			target_scheduling_authority_version,source_active_pos_provider,target_active_pos_provider,
			requires_secret_reentry,status,summary,warnings,conflicts
		) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,'previewed',$15::jsonb,$16::jsonb,$17::jsonb)
		RETURNING id::text
	`, salonID, plan.SourceType, plan.SourceSalonID, actorUserID, plan.Legacy.SchemaVersion,
		pq.Array(plan.Legacy.Bundle.IncludedSections), plan.SourceFingerprint, plan.RequestFingerprint,
		string(fences), plan.Legacy.TargetAuthority, plan.Legacy.TargetAuthorityVersion,
		plan.Legacy.SourceActivePOSProvider, plan.Legacy.Target.SalonProfile.ActivePOSProvider,
		pq.Array(response.RequiresSecretReentry), string(summary), string(warnings), string(conflicts)).Scan(&runID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO configuration_transfer_events(run_id,salon_id,actor_user_id,event_type,details) VALUES($1,$2,$3,'configuration_transfer.previewed',$4::jsonb)`, runID, salonID, actorUserID, string(details)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRun(ctx, salonID, runID)
}

func emptyAsNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (r *PlatformRepository) GetRun(ctx context.Context, salonID, runID string) (*storedPlatformTransferRun, error) {
	return scanPlatformTransferRun(r.db.QueryRowContext(ctx, platformRunSelect+` WHERE salon_id=$1 AND id=$2`, salonID, runID))
}

func (r *PlatformRepository) ListRuns(ctx context.Context, salonID string, limit int) ([]storedPlatformTransferRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := r.db.QueryContext(ctx, platformRunSelect+` WHERE salon_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, salonID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []storedPlatformTransferRun{}
	for rows.Next() {
		item, err := scanPlatformTransferRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

const platformRunSelect = `SELECT id::text,salon_id::text,source_type,COALESCE(source_salon_id::text,''),actor_user_id::text,schema_version,included_sections,source_fingerprint,request_fingerprint,target_fences,target_scheduling_authority,target_scheduling_authority_version,source_active_pos_provider,target_active_pos_provider,requires_secret_reentry,status,COALESCE(action_key,''),summary,warnings,conflicts,created_at,applied_at FROM configuration_transfer_runs`

type rowScanner interface{ Scan(...any) error }

func scanPlatformTransferRun(row rowScanner) (*storedPlatformTransferRun, error) {
	var item storedPlatformTransferRun
	var fences, summary, warnings, conflicts []byte
	if err := row.Scan(&item.ID, &item.SalonID, &item.SourceType, &item.SourceSalonID, &item.ActorUserID,
		&item.SchemaVersion, pq.Array(&item.IncludedSections), &item.SourceFingerprint, &item.RequestFingerprint,
		&fences, &item.TargetAuthority, &item.TargetAuthorityVersion, &item.SourceActiveProvider,
		&item.TargetActiveProvider, pq.Array(&item.RequiresSecretReentry), &item.Status, &item.ActionKey,
		&summary, &warnings, &conflicts, &item.CreatedAt, &item.AppliedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTransferPreview
		}
		return nil, err
	}
	var fenceEnvelope struct {
		Source map[string]int64 `json:"source"`
		Target map[string]int64 `json:"target"`
	}
	if err := json.Unmarshal(fences, &fenceEnvelope); err != nil {
		return nil, err
	}
	item.SourceFences, item.TargetFences = fenceEnvelope.Source, fenceEnvelope.Target
	if err := json.Unmarshal(summary, &item.Summary); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(warnings, &item.Warnings); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(conflicts, &item.Conflicts); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PlatformRepository) Apply(ctx context.Context, salonID, actorUserID, previewID, actionKey string, plan *platformTransferPlan) (*storedPlatformTransferRun, bool, error) {
	if plan == nil || plan.Legacy == nil || !plan.Legacy.CanApply {
		return nil, false, ErrImportConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fence.AdvisoryKey(salonID)); err != nil {
		return nil, false, err
	}
	if plan.SourceSalonID != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "configuration-transfer-source:"+plan.SourceSalonID); err != nil {
			return nil, false, err
		}
	}
	var existingRunID, existingFingerprint, existingStatus string
	err = tx.QueryRowContext(ctx, `SELECT id::text,request_fingerprint,status FROM configuration_transfer_runs WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3`, salonID, actorUserID, actionKey).Scan(&existingRunID, &existingFingerprint, &existingStatus)
	if err == nil {
		if existingRunID != previewID || existingStatus != StatusApplied {
			return nil, false, ErrTransferActionConflict
		}
		existingRun, getErr := scanPlatformTransferRun(tx.QueryRowContext(ctx, platformRunSelect+` WHERE salon_id=$1 AND id=$2 FOR UPDATE`, salonID, existingRunID))
		if getErr != nil {
			return nil, false, getErr
		}
		if existingRun.SourceType != plan.SourceType || existingRun.SourceSalonID != plan.SourceSalonID || existingRun.SourceFingerprint != plan.SourceFingerprint || !sameStrings(existingRun.IncludedSections, plan.Legacy.Bundle.IncludedSections) {
			return nil, false, ErrTransferActionConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		item, err := r.GetRun(ctx, salonID, existingRunID)
		return item, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	run, err := scanPlatformTransferRun(tx.QueryRowContext(ctx, platformRunSelect+` WHERE salon_id=$1 AND id=$2 FOR UPDATE`, salonID, previewID))
	if err != nil {
		return nil, false, err
	}
	if run.ActorUserID != actorUserID || run.Status != StatusPreviewed || run.SourceType != plan.SourceType || run.SourceSalonID != plan.SourceSalonID || run.SourceFingerprint != plan.SourceFingerprint || run.RequestFingerprint != plan.RequestFingerprint || !sameStrings(run.IncludedSections, plan.Legacy.Bundle.IncludedSections) {
		return nil, false, ErrTransferStale
	}
	currentTarget, authority, authorityVersion, err := snapshotTransferFences(ctx, tx, salonID, run.IncludedSections, true)
	if err != nil {
		return nil, false, err
	}
	if authority != run.TargetAuthority || authorityVersion != run.TargetAuthorityVersion || !reflect.DeepEqual(currentTarget, run.TargetFences) {
		return nil, false, ErrTransferStale
	}
	if run.SourceSalonID != "" {
		currentSource, _, _, err := snapshotTransferFences(ctx, tx, run.SourceSalonID, run.IncludedSections, true)
		if err != nil {
			return nil, false, err
		}
		if !reflect.DeepEqual(currentSource, run.SourceFences) {
			return nil, false, ErrTransferStale
		}
	}
	if err := r.applyPlanTx(ctx, tx, salonID, actorUserID, "configuration-transfer:"+previewID, plan); err != nil {
		return nil, false, err
	}
	details, _ := json.Marshal(map[string]any{
		"source_type": run.SourceType, "source_salon_id": emptyAsNil(run.SourceSalonID),
		"included_sections": run.IncludedSections, "schema_version": run.SchemaVersion,
	})
	result, err := tx.ExecContext(ctx, `UPDATE configuration_transfer_runs SET status='applied',action_key=$1,applied_at=now() WHERE id=$2 AND status='previewed'`, actionKey, previewID)
	if err != nil {
		return nil, false, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, false, ErrTransferStale
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO configuration_transfer_events(run_id,salon_id,actor_user_id,event_type,details) VALUES($1,$2,$3,'configuration_transfer.applied',$4::jsonb)`, previewID, salonID, actorUserID, string(details)); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	item, err := r.GetRun(ctx, salonID, previewID)
	return item, false, err
}

func (r *PlatformRepository) applyPlanTx(ctx context.Context, tx *sql.Tx, salonID, actorUserID, actionKey string, plan *platformTransferPlan) error {
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.configuration_transfer','on',true)`); err != nil {
		return err
	}
	legacy := plan.Legacy
	ownerID, err := salonOwnerIDTx(ctx, tx, salonID)
	if err != nil {
		return err
	}
	if legacy.includes(SectionSalon) && sectionChanged(legacy, SectionSalon) {
		profile := legacy.Bundle.SalonProfile
		result, err := tx.ExecContext(ctx, `UPDATE salons SET name=$1,phone=$2,address=NULLIF($3,''),city=NULLIF($4,''),state=NULLIF($5,''),zip_code=NULLIF($6,''),timezone=$7,primary_language=$8,secondary_language=$9,handoff_phone=NULLIF($10,''),updated_at=now() WHERE id=$11`, profile.Name, profile.Phone, profile.Address, profile.City, profile.State, profile.ZipCode, profile.Timezone, profile.PrimaryLanguage, profile.SecondaryLanguage, profile.HandoffPhone, salonID)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrTransferPreview
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":profile", "salon_profile", salonID, plan.RequestFingerprint, []string{"name", "phone", "address", "city", "state", "zip_code", "timezone", "primary_language", "secondary_language", "handoff_phone"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionPublic) && sectionChanged(legacy, SectionPublic) {
		if legacy.PublicCatalogEnabled {
			readiness, err := salon.PublicCatalogSettingsForSalon(ctx, tx, salonID)
			if err != nil {
				return err
			}
			if !readiness.CanPublish {
				return ErrTransferStale
			}
		}
		if err := r.legacy.updatePublicBookingPage(ctx, tx, salonID, ownerID, legacy.Bundle.PublicBookingPage, legacy.PublicCatalogEnabled); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "idx_salons_public_slug_unique" {
				return ErrTransferStale
			}
			return err
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":public", "public_catalog", salonID, plan.RequestFingerprint, []string{"public_slug", "public_catalog_enabled"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionIntegrations) && sectionChanged(legacy, SectionIntegrations) {
		for _, provider := range plan.IntegrationProviders {
			switch provider {
			case integrationconfig.ProviderSquare:
				if err := upsertPlatformSquareConfig(ctx, tx, salonID, legacy.Bundle.Integrations.Square); err != nil {
					return err
				}
			case integrationconfig.ProviderTwilio:
				if err := upsertPlatformTwilioConfig(ctx, tx, salonID, legacy.Bundle.Integrations.Twilio); err != nil {
					return err
				}
			case integrationconfig.ProviderOpenAI:
				if err := upsertPlatformOpenAIConfig(ctx, tx, salonID, legacy.Bundle.Integrations.OpenAI); err != nil {
					return err
				}
			default:
				return ErrValidation
			}
			if err := recordTechnicalTransferTx(ctx, tx, salonID, actorUserID, actionKey+":integration:"+provider, "integration_config", provider, plan.RequestFingerprint, provider, []string{"non_secret_settings"}); err != nil {
				return err
			}
		}
	}
	if legacy.includes(SectionCategories) && sectionChanged(legacy, SectionCategories) {
		if err := r.legacy.upsertServiceCategories(ctx, tx, salonID, legacy.ServiceCategories); err != nil {
			return err
		}
		resources, err := changedCategoryResources(ctx, tx, salonID, legacy.ServiceCategories)
		if err != nil {
			return err
		}
		for i, resource := range resources {
			record := recordBusinessTransferTx
			if resource.Created {
				record = recordBusinessTransferCreateTx
			}
			if err := record(ctx, tx, salonID, actorUserID, fmt.Sprintf("%s:category:%d", actionKey, i), "service_category", resource.ID, plan.RequestFingerprint, []string{"category", "aliases"}); err != nil {
				return err
			}
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":categories", "service_categories", "collection", plan.RequestFingerprint, []string{"categories", "aliases"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionServiceAliases) && sectionChanged(legacy, SectionServiceAliases) {
		if err := r.legacy.upsertServiceAliases(ctx, tx, salonID, legacy.ServiceAliases); err != nil {
			return err
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":aliases", "service_aliases", "collection", plan.RequestFingerprint, []string{"aliases"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionConsultation) && sectionChanged(legacy, SectionConsultation) {
		if err := r.legacy.upsertServiceConsultationProfiles(ctx, tx, salonID, ownerID, legacy.ConsultationProfiles); err != nil {
			return err
		}
		serviceIDs := changedConsultationServiceIDs(legacy.ConsultationProfiles)
		for i, serviceID := range serviceIDs {
			if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, fmt.Sprintf("%s:consultation-service:%d", actionKey, i), "service", serviceID, plan.RequestFingerprint, []string{"consultation_eligible"}); err != nil {
				return err
			}
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":consultation", "consultation_profiles", "collection", plan.RequestFingerprint, []string{"profiles"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionKnowledge) && sectionChanged(legacy, SectionKnowledge) {
		if err := r.legacy.upsertKnowledge(ctx, tx, salonID, legacy.Knowledge); err != nil {
			return err
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":knowledge", "knowledge_base", "collection", plan.RequestFingerprint, []string{"items"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionAI) && plan.AIPolicyChanged {
		if err := r.legacy.updateAIReceptionist(ctx, tx, salonID, ownerID, legacy.Bundle.AIReceptionist, legacy.BookingMode, legacy.ConsultationEnabled); err != nil {
			return err
		}
		if err := recordTechnicalTransferTx(ctx, tx, salonID, actorUserID, actionKey+":ai-policy", "ai_receptionist", "policy", plan.RequestFingerprint, "", []string{"policy"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionAI) && plan.AIEnabledChanged {
		if _, err := tx.ExecContext(ctx, `UPDATE salons SET ai_enabled=$1,updated_at=now() WHERE id=$2`, plan.AIEnabled, salonID); err != nil {
			return err
		}
		if err := recordTechnicalTransferTx(ctx, tx, salonID, actorUserID, actionKey+":ai-runtime", "ai_runtime", "ai_booking", plan.RequestFingerprint, "", []string{"ai_enabled"}); err != nil {
			return err
		}
	}
	if legacy.includes(SectionLocalHours) && plan.LocalHoursChanged {
		if _, err := tx.ExecContext(ctx, `DELETE FROM salon_business_hour_periods WHERE salon_id=$1 AND source='local_override'`, salonID); err != nil {
			return err
		}
		indexes := map[int]int{}
		for _, period := range legacy.Bundle.LocalBusinessHours.Periods {
			indexes[period.DayOfWeek]++
			if _, err := tx.ExecContext(ctx, `INSERT INTO salon_business_hour_periods(salon_id,day_of_week,start_local_time,end_local_time,end_at_midnight,source,provider,provider_location_id,provider_period_index,last_synced_at) VALUES($1,$2,$3::time,$4::time,$5,'local_override','','',$6,NULL)`, salonID, period.DayOfWeek, period.StartLocalTime, period.EndLocalTime, period.EndAtMidnight, indexes[period.DayOfWeek]); err != nil {
				return err
			}
		}
		if err := recordBusinessTransferTx(ctx, tx, salonID, actorUserID, actionKey+":hours", "business_hours", salonID, plan.RequestFingerprint, []string{"periods"}); err != nil {
			return err
		}
	}
	return nil
}

func upsertPlatformSquareConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.SquareSettingsResponse) error {
	settings, err := json.Marshal(map[string]string{
		"environment": cfg.Environment, "client_id": cfg.ClientID, "redirect_url": cfg.RedirectURL,
		"api_version": cfg.APIVersion, "api_base_url": cfg.APIBaseURL,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings,secrets_encrypted)
		VALUES($1,'square',false,$2::jsonb,NULL)
		ON CONFLICT(salon_id,provider) DO UPDATE
		SET settings=salon_integration_configs.settings || EXCLUDED.settings,updated_at=now()
	`, salonID, string(settings))
	return err
}

func upsertPlatformTwilioConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.TwilioSettingsResponse) error {
	settings, err := json.Marshal(map[string]string{
		"voice_transport": cfg.VoiceTransport,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings,secrets_encrypted)
		VALUES($1,'twilio',false,$2::jsonb,NULL)
		ON CONFLICT(salon_id,provider) DO UPDATE
		SET settings=salon_integration_configs.settings || EXCLUDED.settings,updated_at=now()
	`, salonID, string(settings))
	return err
}

func upsertPlatformOpenAIConfig(ctx context.Context, tx *sql.Tx, salonID string, cfg integrationconfig.OpenAISettingsResponse) error {
	settings, err := json.Marshal(map[string]string{
		"base_url": cfg.BaseURL, "transcription_model": cfg.TranscriptionModel,
		"reply_model": cfg.ReplyModel, "speech_model": cfg.SpeechModel,
		"speech_voice": cfg.SpeechVoice, "speech_output_mode": cfg.SpeechOutputMode,
		"realtime_enabled": boolString(cfg.RealtimeEnabled), "realtime_model": cfg.RealtimeModel,
		"realtime_voice": cfg.RealtimeVoice, "realtime_noise_profile": cfg.RealtimeNoiseProfile,
		"realtime_instructions": cfg.RealtimeInstructions,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings,secrets_encrypted)
		VALUES($1,'openai',$2,$3::jsonb,NULL)
		ON CONFLICT(salon_id,provider) DO UPDATE
		SET enabled=EXCLUDED.enabled,settings=salon_integration_configs.settings || EXCLUDED.settings,updated_at=now()
	`, salonID, cfg.Enabled, string(settings))
	return err
}

func salonOwnerIDTx(ctx context.Context, tx *sql.Tx, salonID string) (string, error) {
	var ownerID string
	if err := tx.QueryRowContext(ctx, `SELECT owner_user_id::text FROM salons WHERE id=$1 FOR UPDATE`, salonID).Scan(&ownerID); errors.Is(err, sql.ErrNoRows) {
		return "", ErrTransferPreview
	} else if err != nil {
		return "", err
	}
	return ownerID, nil
}

func sectionChanged(plan *importPlan, section string) bool {
	item := plan.Summary[section]
	return item != nil && (item.Created > 0 || item.Updated > 0)
}

type changedCategoryResource struct {
	ID      string
	Created bool
}

func changedCategoryResources(ctx context.Context, tx *sql.Tx, salonID string, plans []plannedServiceCategory) ([]changedCategoryResource, error) {
	createdBySlug := map[string]bool{}
	for _, plan := range plans {
		changed := plan.Operation != "unchanged"
		for _, alias := range plan.Aliases {
			changed = changed || alias.Operation != "unchanged"
		}
		if changed {
			createdBySlug[plan.Item.Slug] = plan.Operation == "created"
		}
	}
	if len(createdBySlug) == 0 {
		return []changedCategoryResource{}, nil
	}
	slugs := make([]string, 0, len(createdBySlug))
	for slug := range createdBySlug {
		slugs = append(slugs, slug)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,slug FROM service_categories WHERE salon_id=$1 AND slug=ANY($2) ORDER BY id`, salonID, pq.Array(slugs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resources := []changedCategoryResource{}
	for rows.Next() {
		var id, slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, err
		}
		resources = append(resources, changedCategoryResource{ID: id, Created: createdBySlug[slug]})
	}
	return resources, rows.Err()
}

func changedConsultationServiceIDs(plans []plannedServiceConsultationProfile) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, plan := range plans {
		if plan.Operation == "unchanged" || plan.TargetServiceID == "" || seen[plan.TargetServiceID] {
			continue
		}
		seen[plan.TargetServiceID] = true
		ids = append(ids, plan.TargetServiceID)
	}
	return ids
}

func recordBusinessTransferTx(ctx context.Context, tx *sql.Tx, salonID, actorUserID, actionKey, resourceType, resourceID, fingerprint string, changedFields []string) error {
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM business_resource_versions WHERE salon_id=$1 AND resource_type=$2 AND resource_id=$3 FOR UPDATE`, salonID, resourceType, resourceID).Scan(&previous); err != nil {
		return err
	}
	result := previous + 1
	if _, err := tx.ExecContext(ctx, `UPDATE business_resource_versions SET version=$1,updated_by_user_id=$2,updated_at=now() WHERE salon_id=$3 AND resource_type=$4 AND resource_id=$5`, result, actorUserID, salonID, resourceType, resourceID); err != nil {
		return err
	}
	response, _ := json.Marshal(map[string]any{"resource_type": resourceType, "resource_id": resourceID, "version": result})
	details, _ := json.Marshal(map[string]any{"changed_fields": changedFields})
	var actionID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO business_actions(salon_id,actor_user_id,surface,action_key,action_type,request_fingerprint,resource_type,resource_id,previous_version,result_version,response_payload) VALUES($1,$2,'platform',$3,'configuration_transfer.applied',$4,$5,$6,$7,$8,$9::jsonb) RETURNING id::text`, salonID, actorUserID, actionKey, fingerprint, resourceType, resourceID, previous, result, string(response)).Scan(&actionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO business_events(action_id,salon_id,actor_user_id,surface,event_type,resource_type,resource_id,previous_version,result_version,details) VALUES($1,$2,$3,'platform','configuration_transfer.applied',$4,$5,$6,$7,$8::jsonb)`, actionID, salonID, actorUserID, resourceType, resourceID, previous, result, string(details))
	return err
}

func recordBusinessTransferCreateTx(ctx context.Context, tx *sql.Tx, salonID, actorUserID, actionKey, resourceType, resourceID, fingerprint string, changedFields []string) error {
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM business_resource_versions WHERE salon_id=$1 AND resource_type=$2 AND resource_id=$3 FOR UPDATE`, salonID, resourceType, resourceID).Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return ErrTransferStale
	}
	if _, err := tx.ExecContext(ctx, `UPDATE business_resource_versions SET updated_by_user_id=$1,updated_at=now() WHERE salon_id=$2 AND resource_type=$3 AND resource_id=$4`, actorUserID, salonID, resourceType, resourceID); err != nil {
		return err
	}
	response, _ := json.Marshal(map[string]any{"resource_type": resourceType, "resource_id": resourceID, "version": int64(1)})
	details, _ := json.Marshal(map[string]any{"changed_fields": changedFields})
	var actionID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO business_actions(salon_id,actor_user_id,surface,action_key,action_type,request_fingerprint,resource_type,resource_id,previous_version,result_version,response_payload) VALUES($1,$2,'platform',$3,'configuration_transfer.applied',$4,$5,$6,0,1,$7::jsonb) RETURNING id::text`, salonID, actorUserID, actionKey, fingerprint, resourceType, resourceID, string(response)).Scan(&actionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO business_events(action_id,salon_id,actor_user_id,surface,event_type,resource_type,resource_id,previous_version,result_version,details) VALUES($1,$2,$3,'platform','configuration_transfer.applied',$4,$5,0,1,$6::jsonb)`, actionID, salonID, actorUserID, resourceType, resourceID, string(details))
	return err
}

func recordTechnicalTransferTx(ctx context.Context, tx *sql.Tx, salonID, actorUserID, actionKey, resourceType, resourceID, fingerprint, provider string, changedFields []string) error {
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM technical_resource_versions WHERE salon_id=$1 AND resource_type=$2 AND resource_id=$3 FOR UPDATE`, salonID, resourceType, resourceID).Scan(&previous); err != nil {
		return err
	}
	result := previous + 1
	if _, err := tx.ExecContext(ctx, `UPDATE technical_resource_versions SET version=$1,updated_by_user_id=$2,updated_at=now() WHERE salon_id=$3 AND resource_type=$4 AND resource_id=$5`, result, actorUserID, salonID, resourceType, resourceID); err != nil {
		return err
	}
	detailMap := map[string]any{"changed_fields": changedFields}
	if provider != "" {
		detailMap["provider"] = provider
	}
	details, _ := json.Marshal(detailMap)
	var actionID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO technical_actions(salon_id,actor_user_id,action_key,action_type,request_fingerprint,resource_type,resource_id,previous_version,result_version,details) VALUES($1,$2,$3,'configuration_transfer.applied',$4,$5,$6,$7,$8,$9::jsonb) RETURNING id::text`, salonID, actorUserID, actionKey, fingerprint, resourceType, resourceID, previous, result, string(details)).Scan(&actionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO technical_events(action_id,salon_id,actor_user_id,event_type,resource_type,resource_id,previous_version,result_version,details) VALUES($1,$2,$3,'configuration_transfer.applied',$4,$5,$6,$7,$8::jsonb)`, actionID, salonID, actorUserID, resourceType, resourceID, previous, result, string(details))
	return err
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
