package configtransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

const (
	PlatformSourceTenant = "tenant"
	PlatformSourceJSON   = "json_upload"
	platformJSONMaxBytes = 3 * 1024 * 1024
)

var platformLegacyV7ContentSections = []string{
	SectionCategories,
	SectionServiceAliases,
	SectionConsultation,
	SectionKnowledge,
}

type PlatformService struct {
	legacy       *Service
	repo         *PlatformRepository
	integrations PlatformIntegrationConfigReader
	now          func() time.Time
}

type PlatformIntegrationConfigReader interface {
	GetAllPersistedForPlatform(context.Context, string) (*integrationconfig.IntegrationConfigsResponse, []string, error)
}

func NewPlatformService(legacy *Service, repo *PlatformRepository, integrations PlatformIntegrationConfigReader) *PlatformService {
	return &PlatformService{legacy: legacy, repo: repo, integrations: integrations, now: time.Now}
}

func (s *PlatformService) Export(ctx context.Context, salonID string, sections []string) (*ConfigurationBundle, error) {
	if !validUUID(salonID) || s == nil || s.legacy == nil || s.repo == nil || s.integrations == nil {
		return nil, ErrValidation
	}
	sections, err := normalizeSectionSelection(sections, platformConfigurationSections)
	if err != nil || len(sections) == 0 {
		return nil, ErrValidation
	}
	bundle, _, err := s.exportSalon(ctx, salonID, sections)
	if err != nil {
		return nil, err
	}
	if encoded, marshalErr := json.Marshal(bundle); marshalErr != nil {
		return nil, marshalErr
	} else if len(encoded) > platformJSONMaxBytes {
		return nil, ErrTransferTooLarge
	}
	bundle.ExportedAt = s.now().UTC()
	return &bundle, nil
}

func (s *PlatformService) Preview(ctx context.Context, targetSalonID, actorUserID string, req PlatformTransferRequest) (*PlatformTransferResponse, error) {
	if !validUUID(targetSalonID) || !validUUID(actorUserID) {
		return nil, ErrValidation
	}
	plan, err := s.preparePlan(ctx, targetSalonID, req)
	if err != nil {
		return nil, err
	}
	run, err := s.repo.CreatePreview(ctx, targetSalonID, actorUserID, plan)
	if err != nil {
		return nil, err
	}
	return platformResponseFromPlan(run, plan, false), nil
}

func (s *PlatformService) Apply(ctx context.Context, targetSalonID, actorUserID string, req PlatformTransferApplyRequest) (*PlatformTransferResponse, error) {
	req.PreviewID = strings.TrimSpace(req.PreviewID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.SourceSalonID = strings.TrimSpace(req.SourceSalonID)
	sections, sectionErr := normalizeSectionSelection(req.IncludedSections, platformConfigurationSections)
	if !validUUID(targetSalonID) || !validUUID(actorUserID) || !validUUID(req.PreviewID) || !validPlatformActionKey(req.ActionKey) {
		return nil, ErrValidation
	}
	if sectionErr != nil || len(sections) == 0 {
		return nil, ErrValidation
	}
	req.IncludedSections = sections
	existing, err := s.repo.GetRun(ctx, targetSalonID, req.PreviewID)
	if err != nil {
		return nil, err
	}
	if existing.Status == StatusApplied {
		if existing.ActorUserID != actorUserID || existing.ActionKey != req.ActionKey || existing.SourceType != strings.TrimSpace(req.SourceType) || existing.SourceSalonID != strings.TrimSpace(req.SourceSalonID) || !sameStrings(existing.IncludedSections, req.IncludedSections) {
			return nil, ErrTransferActionConflict
		}
		if existing.SourceType == PlatformSourceTenant && req.Configuration != nil {
			return nil, ErrTransferActionConflict
		}
		if existing.SourceType == PlatformSourceJSON {
			if req.Configuration == nil {
				return nil, ErrTransferActionConflict
			}
			bundle, _, canonicalErr := canonicalizePlatformJSONBundle(*req.Configuration)
			if canonicalErr != nil {
				return nil, ErrTransferActionConflict
			}
			if !selectionAvailable(bundle.IncludedSections, req.IncludedSections) {
				return nil, ErrTransferActionConflict
			}
			bundle.IncludedSections = append([]string{}, req.IncludedSections...)
			normalized, normalizeErr := normalizeImportBundle(bundle)
			if normalizeErr != nil {
				return nil, ErrTransferActionConflict
			}
			normalized.ExportedAt = time.Time{}
			fingerprint, fingerprintErr := fingerprintBundle(normalized)
			if fingerprintErr != nil || fingerprint != existing.SourceFingerprint {
				return nil, ErrTransferActionConflict
			}
		}
		response := platformResponseFromRun(existing)
		response.Replayed = true
		return response, nil
	}
	plan, err := s.preparePlan(ctx, targetSalonID, req.PlatformTransferRequest)
	if err != nil {
		return nil, err
	}
	if !plan.Legacy.CanApply {
		return nil, ErrTransferStale
	}
	run, replayed, err := s.repo.Apply(ctx, targetSalonID, actorUserID, req.PreviewID, req.ActionKey, plan)
	if err != nil {
		return nil, err
	}
	return platformResponseFromPlan(run, plan, replayed), nil
}

func (s *PlatformService) Runs(ctx context.Context, targetSalonID string, limit int) (*PlatformTransferRunsResponse, error) {
	if !validUUID(targetSalonID) {
		return nil, ErrValidation
	}
	items, err := s.repo.ListRuns(ctx, targetSalonID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]PlatformTransferResponse, 0, len(items))
	for i := range items {
		result = append(result, *platformResponseFromRun(&items[i]))
	}
	return &PlatformTransferRunsResponse{Runs: result}, nil
}

func (s *PlatformService) preparePlan(ctx context.Context, targetSalonID string, req PlatformTransferRequest) (*platformTransferPlan, error) {
	if s == nil || s.legacy == nil || s.repo == nil || s.integrations == nil {
		return nil, ErrValidation
	}
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.SourceSalonID = strings.TrimSpace(req.SourceSalonID)
	sections, err := normalizeSectionSelection(req.IncludedSections, platformConfigurationSections)
	if err != nil || len(sections) == 0 {
		return nil, ErrValidation
	}
	req.IncludedSections = sections

	var source ConfigurationBundle
	var sourceFences map[string]int64
	sourceHoursUnavailable := false
	legacyV7Adapted := false
	switch req.SourceType {
	case PlatformSourceTenant:
		if !validUUID(req.SourceSalonID) || req.SourceSalonID == targetSalonID || req.Configuration != nil {
			return nil, ErrValidation
		}
		source, sourceHoursUnavailable, err = s.exportSalon(ctx, req.SourceSalonID, sections)
		if err != nil {
			return nil, err
		}
		sourceFences, _, _, err = s.repo.SnapshotFences(ctx, req.SourceSalonID, sections)
		if err != nil {
			return nil, err
		}
	case PlatformSourceJSON:
		if req.SourceSalonID != "" || req.Configuration == nil {
			return nil, ErrValidation
		}
		if encoded, marshalErr := json.Marshal(req.Configuration); marshalErr != nil {
			return nil, ErrValidation
		} else if len(encoded) > platformJSONMaxBytes {
			return nil, ErrTransferTooLarge
		}
		source, legacyV7Adapted, err = canonicalizePlatformJSONBundle(*req.Configuration)
		if err != nil {
			return nil, err
		}
		if source.SchemaVersion == PlatformSchemaVersion && len(source.IncludedSections) == 0 {
			return nil, ErrValidation
		}
		if !selectionAvailable(source.IncludedSections, sections) {
			return nil, ErrValidation
		}
		source.IncludedSections = sections
		sourceFences = map[string]int64{}
	default:
		return nil, ErrValidation
	}

	if source.SchemaVersion == SchemaVersion && sectionSet(sections)[SectionLocalHours] {
		return nil, ErrValidation
	}
	source.IncludedSections = sections
	sourceForFingerprint, err := normalizeImportBundle(source)
	if err != nil {
		return nil, err
	}
	sourceForFingerprint.ExportedAt = time.Time{}
	sourceFingerprint, err := fingerprintBundle(sourceForFingerprint)
	if err != nil {
		return nil, err
	}

	targetOwnerID, err := s.repo.SalonOwnerID(ctx, targetSalonID)
	if err != nil {
		return nil, err
	}
	targetIntegrations := integrationconfig.IntegrationConfigsResponse{}
	if sectionSet(sections)[SectionIntegrations] {
		integrations, _, err := s.integrations.GetAllPersistedForPlatform(ctx, targetSalonID)
		if err != nil {
			return nil, err
		}
		targetIntegrations = normalizeIntegrationConfigs(*integrations)
	}
	targetBundle, err := s.legacy.getWithIntegrations(ctx, targetSalonID, targetOwnerID, &targetIntegrations)
	if err != nil {
		return nil, err
	}
	sourceActiveProvider := sourceForFingerprint.SalonProfile.ActivePOSProvider
	sourceAIEnabled := sourceForFingerprint.SalonProfile.AIEnabled
	applyBundle := sourceForFingerprint
	// These are destination technical selections, not portable profile fields.
	applyBundle.SalonProfile.ActivePOSProvider = ""
	applyBundle.SalonProfile.AIEnabled = targetBundle.SalonProfile.AIEnabled
	if sectionSet(sections)[SectionIntegrations] {
		mergeUnselectedIntegrationProviders(&applyBundle, *targetBundle)
	}
	legacyPlan, err := s.legacy.buildImportPlanWithCurrent(ctx, targetSalonID, targetOwnerID, ImportRequest{
		RequestID:     "platform-preview",
		Configuration: applyBundle,
	}, targetBundle)
	if err != nil {
		return nil, err
	}
	if legacyV7Adapted {
		legacyPlan.Warnings = append([]ImportIssue{{
			Section: "configuration",
			Code:    "legacy_v7_content_pack_adapted_to_v8",
			Message: "The uploaded scoped v7 content pack was canonicalized to the v8 compatibility contract before review. Runtime, provider, scheduling, and operational sections remain unsupported.",
			Field:   "schema_version",
		}}, legacyPlan.Warnings...)
	}
	if sectionSet(sections)[SectionIntegrations] {
		legacyPlan.Warnings = withoutIssueCode(legacyPlan.Warnings, "secret_reentry_required")
		legacyPlan.RequiresSecretReentry = platformSecretReentry(sourceForFingerprint.Integrations, targetBundle.Integrations, sourceForFingerprint.IntegrationProviders)
		for _, provider := range legacyPlan.RequiresSecretReentry {
			legacyPlan.Warnings = append(legacyPlan.Warnings, ImportIssue{
				Section: SectionIntegrations, Code: "secret_reentry_required",
				Message: provider + " credentials are not included and are not configured on the destination. Re-enter them or reconnect the provider after apply.", Field: provider,
			})
		}
	}
	legacyPlan.SourceActivePOSProvider = sourceActiveProvider
	legacyPlan.Bundle.SalonProfile.ActivePOSProvider = targetBundle.SalonProfile.ActivePOSProvider

	targetFences, _, _, err := s.repo.SnapshotFences(ctx, targetSalonID, sections)
	if err != nil {
		return nil, err
	}
	plan := &platformTransferPlan{
		Legacy:               legacyPlan,
		SourceType:           req.SourceType,
		SourceSalonID:        req.SourceSalonID,
		SourceFingerprint:    sourceFingerprint,
		SourceFences:         sourceFences,
		TargetFences:         targetFences,
		AIEnabled:            sourceAIEnabled,
		AIEnabledChanged:     sectionSet(sections)[SectionAI] && sourceAIEnabled != targetBundle.SalonProfile.AIEnabled,
		AIPolicyChanged:      sectionChanged(legacyPlan, SectionAI),
		IntegrationProviders: changedIntegrationProviders(applyBundle.Integrations, targetBundle.Integrations, sourceForFingerprint.IntegrationProviders),
	}
	if plan.AIEnabledChanged {
		summary(plan.Legacy, SectionAI).Updated++
	}
	if sectionSet(sections)[SectionSalon] && sourceActiveProvider != "" {
		summary(plan.Legacy, SectionSalon).Skipped++
		plan.Legacy.Warnings = append(plan.Legacy.Warnings, ImportIssue{
			Section: SectionSalon, Code: "active_pos_provider_report_only",
			Message: "The source active POS provider is shown for review but is never imported. Use the explicit provider-switch workflow.", Field: "active_pos_provider",
		})
	}
	if sectionSet(sections)[SectionLocalHours] {
		targetHours, err := s.repo.LocalBusinessHours(ctx, targetSalonID)
		if err != nil {
			return nil, err
		}
		if sourceHoursUnavailable {
			summary(plan.Legacy, SectionLocalHours).Conflicts++
			plan.Legacy.Conflicts = append(plan.Legacy.Conflicts, ImportIssue{
				Section: SectionLocalHours, Code: "source_local_hours_unavailable",
				Message: "The source salon is provider-managed, so provider-imported hours are not portable local configuration.",
			})
		} else if targetHours.ManagementMode != "local" {
			summary(plan.Legacy, SectionLocalHours).Conflicts++
			plan.Legacy.Conflicts = append(plan.Legacy.Conflicts, ImportIssue{
				Section: SectionLocalHours, Code: "target_hours_provider_managed",
				Message: "The destination currently uses external-provider scheduling. Local hours cannot be applied until authority is changed explicitly.",
			})
		} else if reflect.DeepEqual(targetHours.Periods, sourceForFingerprint.LocalBusinessHours.Periods) {
			summary(plan.Legacy, SectionLocalHours).Unchanged++
		} else {
			summary(plan.Legacy, SectionLocalHours).Updated++
			plan.LocalHoursChanged = true
		}
	}
	if sectionSet(sections)[SectionPublic] && sourceForFingerprint.PublicBookingPage.PublicCatalogEnabled {
		readinessInvalidated := false
		switch legacyPlan.TargetAuthority {
		case "manleai_calendar":
			readinessInvalidated = plan.LocalHoursChanged || sectionChanged(legacyPlan, SectionConsultation) ||
				(sectionSet(sections)[SectionSalon] && sourceForFingerprint.SalonProfile.Timezone != targetBundle.SalonProfile.Timezone)
		case "external_provider":
			readinessInvalidated = len(plan.IntegrationProviders) > 0
		}
		if readinessInvalidated {
			summary(plan.Legacy, SectionPublic).Conflicts++
			plan.Legacy.Conflicts = append(plan.Legacy.Conflicts, ImportIssue{
				Section: SectionPublic, Code: "public_catalog_requires_post_transfer_readiness_review",
				Message: "Selected changes invalidate destination publishing readiness. Apply the configuration without enabling the public page, complete activation or provider readiness review, then publish explicitly.", Field: "public_catalog_enabled",
			})
		}
	}
	plan.Legacy.CanApply = len(plan.Legacy.Conflicts) == 0
	plan.RequestFingerprint, err = platformRequestFingerprint(targetSalonID, plan)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func canonicalizePlatformJSONBundle(bundle ConfigurationBundle) (ConfigurationBundle, bool, error) {
	bundle.SchemaVersion = strings.TrimSpace(bundle.SchemaVersion)
	switch bundle.SchemaVersion {
	case PlatformSchemaVersion, SchemaVersion:
		return bundle, false, nil
	case LegacySchemaV7:
		sections, err := normalizeSectionSelection(bundle.IncludedSections, platformLegacyV7ContentSections)
		if err != nil || len(sections) == 0 {
			return bundle, false, ErrUnsupportedSchema
		}
		bundle.SchemaVersion = SchemaVersion
		bundle.IncludedSections = sections
		bundle.POSConnection = nil
		return bundle, true, nil
	default:
		return bundle, false, ErrUnsupportedSchema
	}
}

func (s *PlatformService) exportSalon(ctx context.Context, salonID string, sections []string) (ConfigurationBundle, bool, error) {
	ownerID, err := s.repo.SalonOwnerID(ctx, salonID)
	if err != nil {
		return ConfigurationBundle{}, false, err
	}
	integrations := integrationconfig.IntegrationConfigsResponse{}
	providers := []string{}
	if sectionSet(sections)[SectionIntegrations] {
		persisted, persistedProviders, err := s.integrations.GetAllPersistedForPlatform(ctx, salonID)
		if err != nil {
			return ConfigurationBundle{}, false, err
		}
		integrations = *persisted
		providers = persistedProviders
	}
	bundle, err := s.legacy.getWithIntegrations(ctx, salonID, ownerID, &integrations)
	if err != nil {
		return ConfigurationBundle{}, false, err
	}
	bundle.SchemaVersion = PlatformSchemaVersion
	bundle.IncludedSections = append([]string{}, sections...)
	bundle.ExcludedData = platformExcludedData()
	bundle.ExportedAt = s.now().UTC()
	bundle.IntegrationProviders = providers
	bundle.RequiresSecretReentry = secretReentryProviders(integrations)
	hoursUnavailable := false
	if sectionSet(sections)[SectionLocalHours] {
		hours, err := s.repo.LocalBusinessHours(ctx, salonID)
		if err != nil {
			return ConfigurationBundle{}, false, err
		}
		hoursUnavailable = hours.ManagementMode != "local"
		if hoursUnavailable {
			hours.ManagementMode = "local"
			hours.Periods = []LocalBusinessHourPeriodExport{}
		}
		bundle.LocalBusinessHours = hours
	}
	return scopedBundle(*bundle, sections), hoursUnavailable, nil
}

func scopedBundle(bundle ConfigurationBundle, sections []string) ConfigurationBundle {
	selected := sectionSet(sections)
	bundle.IncludedSections = append([]string{}, sections...)
	if !selected[SectionSalon] {
		aiEnabled := bundle.SalonProfile.AIEnabled
		activeProvider := bundle.SalonProfile.ActivePOSProvider
		bundle.SalonProfile = SalonProfileExport{}
		if selected[SectionAI] {
			bundle.SalonProfile.AIEnabled = aiEnabled
		}
		bundle.SalonProfile.ActivePOSProvider = activeProvider
	}
	if !selected[SectionAI] {
		bundle.AIReceptionist = AIReceptionistExport{}
	}
	if !selected[SectionPublic] {
		bundle.PublicBookingPage = PublicBookingPageExport{}
	}
	if !selected[SectionIntegrations] {
		bundle.Integrations = integrationconfig.IntegrationConfigsResponse{}
		bundle.IntegrationProviders = []string{}
		bundle.RequiresSecretReentry = []string{}
	}
	if !selected[SectionCategories] {
		bundle.ServiceCategories = ServiceCategoryBundleExport{Items: []ServiceCategoryExport{}}
	}
	if !selected[SectionServiceAliases] {
		bundle.ServiceAliases = ServiceAliasBundleExport{Items: []ServiceAliasExport{}}
	}
	if !selected[SectionConsultation] {
		bundle.ConsultationProfiles = ServiceConsultationProfileBundleExport{Items: []ServiceConsultationProfileExport{}}
	}
	if !selected[SectionKnowledge] {
		bundle.KnowledgeBase = KnowledgeBaseExport{Items: []KnowledgeItemExport{}}
	}
	if !selected[SectionLocalHours] {
		bundle.LocalBusinessHours = LocalBusinessHoursExport{}
	}
	return bundle
}

func mergeUnselectedIntegrationProviders(source *ConfigurationBundle, target ConfigurationBundle) {
	if source == nil {
		return
	}
	selected := sectionSet(source.IntegrationProviders)
	if !selected[integrationconfig.ProviderSquare] {
		source.Integrations.Square = target.Integrations.Square
	}
	if !selected[integrationconfig.ProviderTwilio] {
		source.Integrations.Twilio = target.Integrations.Twilio
	}
	if !selected[integrationconfig.ProviderOpenAI] {
		source.Integrations.OpenAI = target.Integrations.OpenAI
	}
}

func changedIntegrationProviders(source, target integrationconfig.IntegrationConfigsResponse, available []string) []string {
	result := []string{}
	for _, provider := range available {
		changed := false
		switch provider {
		case integrationconfig.ProviderSquare:
			changed = source.Square.Environment != target.Square.Environment ||
				source.Square.ClientID != target.Square.ClientID ||
				source.Square.RedirectURL != target.Square.RedirectURL ||
				source.Square.APIVersion != target.Square.APIVersion ||
				source.Square.APIBaseURL != target.Square.APIBaseURL
		case integrationconfig.ProviderTwilio:
			changed = source.Twilio.PublicBaseURL != target.Twilio.PublicBaseURL ||
				source.Twilio.IncomingPath != target.Twilio.IncomingPath ||
				source.Twilio.TurnPath != target.Twilio.TurnPath ||
				source.Twilio.RecordingPath != target.Twilio.RecordingPath ||
				source.Twilio.StreamPath != target.Twilio.StreamPath ||
				source.Twilio.VoiceTransport != target.Twilio.VoiceTransport
		case integrationconfig.ProviderOpenAI:
			changed = source.OpenAI.Enabled != target.OpenAI.Enabled ||
				source.OpenAI.BaseURL != target.OpenAI.BaseURL ||
				source.OpenAI.TranscriptionModel != target.OpenAI.TranscriptionModel ||
				source.OpenAI.ReplyModel != target.OpenAI.ReplyModel ||
				source.OpenAI.SpeechModel != target.OpenAI.SpeechModel ||
				source.OpenAI.SpeechVoice != target.OpenAI.SpeechVoice ||
				source.OpenAI.SpeechOutputMode != target.OpenAI.SpeechOutputMode ||
				source.OpenAI.RealtimeEnabled != target.OpenAI.RealtimeEnabled ||
				source.OpenAI.RealtimeModel != target.OpenAI.RealtimeModel ||
				source.OpenAI.RealtimeVoice != target.OpenAI.RealtimeVoice ||
				source.OpenAI.RealtimeNoiseProfile != target.OpenAI.RealtimeNoiseProfile ||
				source.OpenAI.RealtimeInstructions != target.OpenAI.RealtimeInstructions
		}
		if changed {
			result = append(result, provider)
		}
	}
	return result
}

func platformSecretReentry(source, target integrationconfig.IntegrationConfigsResponse, available []string) []string {
	result := []string{}
	for _, provider := range available {
		missing := false
		switch provider {
		case integrationconfig.ProviderSquare:
			missing = source.Square.ClientSecretConfigured && !target.Square.ClientSecretConfigured
		case integrationconfig.ProviderTwilio:
			missing = source.Twilio.AuthTokenConfigured && !target.Twilio.AuthTokenConfigured
		case integrationconfig.ProviderOpenAI:
			missing = source.OpenAI.APIKeyConfigured && !target.OpenAI.APIKeyConfigured
		}
		if missing {
			result = append(result, provider)
		}
	}
	return result
}

func withoutIssueCode(items []ImportIssue, code string) []ImportIssue {
	result := make([]ImportIssue, 0, len(items))
	for _, item := range items {
		if item.Code != code {
			result = append(result, item)
		}
	}
	return result
}

func selectionAvailable(available, requested []string) bool {
	if len(available) == 0 {
		return true
	}
	set := sectionSet(available)
	for _, item := range requested {
		if !set[item] {
			return false
		}
	}
	return true
}

func platformRequestFingerprint(targetSalonID string, plan *platformTransferPlan) (string, error) {
	payload, err := json.Marshal(struct {
		TargetSalonID     string           `json:"target_salon_id"`
		SourceType        string           `json:"source_type"`
		SourceSalonID     string           `json:"source_salon_id,omitempty"`
		SourceFingerprint string           `json:"source_fingerprint"`
		Sections          []string         `json:"sections"`
		SourceFences      map[string]int64 `json:"source_fences"`
		TargetFences      map[string]int64 `json:"target_fences"`
	}{targetSalonID, plan.SourceType, plan.SourceSalonID, plan.SourceFingerprint, plan.Legacy.Bundle.IncludedSections, plan.SourceFences, plan.TargetFences})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func platformResponseFromPlan(run *storedPlatformTransferRun, plan *platformTransferPlan, replayed bool) *PlatformTransferResponse {
	legacy := importResponse(plan.Legacy, run == nil || run.Status == StatusPreviewed, "")
	result := &PlatformTransferResponse{
		TargetSalonID: plan.Legacy.SalonID, SourceType: plan.SourceType, SourceSalonID: plan.SourceSalonID,
		SchemaVersion: plan.Legacy.SchemaVersion, IncludedSections: append([]string{}, plan.Legacy.Bundle.IncludedSections...),
		Status: StatusPreviewed, CanApply: plan.Legacy.CanApply && (run == nil || run.Status == StatusPreviewed), Replayed: replayed,
		TargetAuthority: plan.Legacy.TargetAuthority, TargetAuthorityVersion: plan.Legacy.TargetAuthorityVersion,
		SourceActivePOSProvider: plan.Legacy.SourceActivePOSProvider,
		TargetActivePOSProvider: plan.Legacy.Target.SalonProfile.ActivePOSProvider,
		Summary:                 legacy.Summary, Warnings: legacy.Warnings, Conflicts: legacy.Conflicts,
		ExcludedData: platformExcludedData(), RequiresSecretReentry: legacy.RequiresSecretReentry,
	}
	if run != nil {
		result.RunID, result.Status, result.CreatedAt, result.AppliedAt = run.ID, run.Status, run.CreatedAt, run.AppliedAt
	}
	return result
}

func platformResponseFromRun(run *storedPlatformTransferRun) *PlatformTransferResponse {
	return &PlatformTransferResponse{
		RunID: run.ID, TargetSalonID: run.SalonID, SourceType: run.SourceType, SourceSalonID: run.SourceSalonID,
		SchemaVersion: run.SchemaVersion, IncludedSections: append([]string{}, run.IncludedSections...), Status: run.Status,
		CanApply:        run.Status == StatusPreviewed && len(run.Conflicts) == 0,
		TargetAuthority: run.TargetAuthority, TargetAuthorityVersion: run.TargetAuthorityVersion,
		SourceActivePOSProvider: run.SourceActiveProvider, TargetActivePOSProvider: run.TargetActiveProvider,
		Summary: run.Summary, Warnings: run.Warnings, Conflicts: run.Conflicts,
		ExcludedData: platformExcludedData(), RequiresSecretReentry: append([]string{}, run.RequiresSecretReentry...),
		CreatedAt: run.CreatedAt, AppliedAt: run.AppliedAt,
	}
}

func platformExcludedData() []string {
	items := []string{}
	for _, item := range excludedData {
		if item != "salon_business_hour_periods" {
			items = append(items, item)
		}
	}
	return append(items, "provider_imported_business_hours", "legacy_business_hours")
}

func validUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func validPlatformActionKey(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 1 && len(value) <= 120 && !strings.ContainsAny(value, "\r\n\t ")
}
