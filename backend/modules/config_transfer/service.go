package configtransfer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/manleai/ai-receptionist/internal/config"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/training"
)

var (
	ErrValidation            = errors.New("configuration transfer validation failed")
	ErrUnsupportedSchema     = errors.New("configuration transfer schema is unsupported")
	ErrImportConflict        = errors.New("configuration import has conflicts")
	ErrOnboardingSalonExists = errors.New("onboarding import requires an owner without a salon")
)

type SalonReader interface {
	Get(ctx context.Context, salonID string, ownerUserID string) (*salon.Salon, error)
	GetSettings(ctx context.Context, salonID string, ownerUserID string) (*salon.Settings, error)
	GetPublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string) (*salon.PublicCatalogSettings, error)
}

type IntegrationConfigReader interface {
	GetAll(ctx context.Context, salonID string, ownerUserID string) (*integrationconfig.IntegrationConfigsResponse, error)
}

type POSConnectionReader interface {
	GetConnection(ctx context.Context, salonID string, provider string) (*pos.Connection, error)
}

type ServiceCategoryReader interface {
	ListServiceCategories(ctx context.Context, salonID string, ownerUserID string) ([]pos.ServiceCategory, error)
}

type ServiceCatalogReader interface {
	ListServices(ctx context.Context, salonID string, provider string) ([]pos.Service, error)
}

type KnowledgeListReader interface {
	ListKnowledge(ctx context.Context, salonID string, ownerUserID string) ([]training.KnowledgeItem, error)
}

type ServiceAliasReader interface {
	ListServiceAliases(ctx context.Context, salonID string, ownerUserID string) ([]training.ServiceAlias, error)
}

type ImportStore interface {
	TargetImportState(ctx context.Context, salonID string, ownerUserID string, current *ConfigurationBundle) (*importTargetState, error)
	PublicSlugTaken(ctx context.Context, salonID string, slug string) (bool, error)
	ApplyImport(ctx context.Context, salonID string, ownerUserID string, plan *importPlan) (string, bool, error)
	OwnerHasSalon(ctx context.Context, ownerUserID string) (bool, error)
	ExistingOnboardingImport(ctx context.Context, ownerUserID string, requestID string, fingerprint string) (string, string, bool, bool, error)
	ApplyOnboardingImport(ctx context.Context, ownerUserID string, plan *importPlan) (string, string, bool, error)
}

type Service struct {
	salons       SalonReader
	integrations IntegrationConfigReader
	pos          POSConnectionReader
	categories   ServiceCategoryReader
	services     ServiceCatalogReader
	knowledge    KnowledgeListReader
	aliases      ServiceAliasReader
	imports      ImportStore
	now          func() time.Time
}

func NewService(salons SalonReader, integrations IntegrationConfigReader, posConnections POSConnectionReader, knowledge KnowledgeListReader, imports ImportStore, categoryReaders ...ServiceCategoryReader) *Service {
	var categories ServiceCategoryReader
	if len(categoryReaders) > 0 {
		categories = categoryReaders[0]
	} else if reader, ok := posConnections.(ServiceCategoryReader); ok {
		categories = reader
	}
	var services ServiceCatalogReader
	if reader, ok := posConnections.(ServiceCatalogReader); ok {
		services = reader
	}
	var aliases ServiceAliasReader
	if reader, ok := knowledge.(ServiceAliasReader); ok {
		aliases = reader
	}
	return &Service{
		salons:       salons,
		integrations: integrations,
		pos:          posConnections,
		categories:   categories,
		services:     services,
		knowledge:    knowledge,
		aliases:      aliases,
		imports:      imports,
		now:          time.Now,
	}
}

func (s *Service) Get(ctx context.Context, salonID string, ownerUserID string) (*ConfigurationBundle, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" || s.salons == nil || s.integrations == nil || s.knowledge == nil {
		return nil, ErrValidation
	}

	item, err := s.salons.Get(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	settings, err := s.salons.GetSettings(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	publicPage, err := s.salons.GetPublicCatalogSettings(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	integrations, err := s.integrations.GetAll(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	knowledge, err := s.knowledge.ListKnowledge(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}

	activeProvider := strings.TrimSpace(item.ActivePOSProvider)
	if activeProvider == "" {
		activeProvider = pos.ProviderSquare
	}
	categories, err := s.serviceCategories(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	services, err := s.serviceCatalog(ctx, salonID, activeProvider)
	if err != nil {
		return nil, err
	}
	serviceAliases, err := s.serviceAliases(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}

	connection, err := s.posConnection(ctx, salonID, activeProvider)
	if err != nil {
		return nil, err
	}
	profile := salonProfileExport(item)
	profile.ActivePOSProvider = activeProvider

	return &ConfigurationBundle{
		SchemaVersion:           SchemaVersion,
		ExportedAt:              s.now().UTC(),
		SecretsExported:         false,
		OperationalDataExported: false,
		ExcludedData:            copyStrings(excludedData),
		RequiresSecretReentry:   secretReentryProviders(*integrations),
		SalonProfile:            profile,
		AIReceptionist:          aiReceptionistExport(settings),
		PublicBookingPage:       publicBookingPageExport(publicPage),
		Integrations:            *integrations,
		POSConnection:           posConnectionExport(activeProvider, connection),
		ServiceCategories:       serviceCategoriesExport(categories),
		ServiceAliases:          serviceAliasesExport(serviceAliases, services),
		KnowledgeBase:           knowledgeBaseExport(knowledge),
	}, nil
}

func (s *Service) PreviewImport(ctx context.Context, salonID string, ownerUserID string, req ImportRequest) (*ImportResponse, error) {
	plan, err := s.buildImportPlan(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	return importResponse(plan, true, ""), nil
}

func (s *Service) ApplyImport(ctx context.Context, salonID string, ownerUserID string, req ImportRequest) (*ImportResponse, error) {
	if s.imports == nil {
		return nil, ErrValidation
	}
	plan, err := s.buildImportPlan(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	if !plan.CanApply {
		return importResponse(plan, false, ""), ErrImportConflict
	}
	runID, _, err := s.imports.ApplyImport(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), plan)
	if err != nil {
		if errors.Is(err, ErrImportConflict) {
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionSalon,
				Code:    "request_id_conflict",
				Message: "This request id was already used for a different configuration payload.",
				Field:   "request_id",
			})
			plan.CanApply = false
			return importResponse(plan, false, runID), ErrImportConflict
		}
		return nil, err
	}
	return importResponse(plan, false, runID), nil
}

func (s *Service) PreviewOnboardingImport(ctx context.Context, ownerUserID string, req ImportRequest) (*ImportResponse, error) {
	bundle, fingerprint, requestID, err := normalizedImportRequest(req)
	if err != nil {
		return nil, err
	}
	plan, err := s.buildOnboardingImportPlan(ctx, ownerUserID, bundle, fingerprint, requestID, false)
	if err != nil {
		return nil, err
	}
	return importResponse(plan, true, ""), nil
}

func (s *Service) ApplyOnboardingImport(ctx context.Context, ownerUserID string, req ImportRequest) (*ImportResponse, error) {
	if s.imports == nil {
		return nil, ErrValidation
	}
	bundle, fingerprint, requestID, err := normalizedImportRequest(req)
	if err != nil {
		return nil, err
	}
	existingSalonID, existingRunID, exists, samePayload, err := s.imports.ExistingOnboardingImport(ctx, strings.TrimSpace(ownerUserID), requestID, fingerprint)
	if err != nil {
		return nil, err
	}
	if exists {
		plan, err := s.buildOnboardingImportPlan(ctx, ownerUserID, bundle, fingerprint, requestID, true)
		if err != nil {
			return nil, err
		}
		plan.SalonID = existingSalonID
		if !samePayload {
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionSalon,
				Code:    "request_id_conflict",
				Message: "This request id was already used for a different configuration payload.",
				Field:   "request_id",
			})
			plan.CanApply = false
			return importResponse(plan, false, existingRunID), ErrImportConflict
		}
		return importResponse(plan, false, existingRunID), nil
	}
	plan, err := s.buildOnboardingImportPlan(ctx, ownerUserID, bundle, fingerprint, requestID, false)
	if err != nil {
		return nil, err
	}
	if !plan.CanApply {
		return importResponse(plan, false, ""), ErrImportConflict
	}
	salonID, runID, _, err := s.imports.ApplyOnboardingImport(ctx, strings.TrimSpace(ownerUserID), plan)
	if err != nil {
		if errors.Is(err, ErrOnboardingSalonExists) {
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionSalon,
				Code:    "owner_salon_exists",
				Message: "This owner already has a salon. Use Settings configuration transfer to import into the existing salon.",
			})
			plan.CanApply = false
			return importResponse(plan, false, runID), ErrImportConflict
		}
		if errors.Is(err, ErrImportConflict) {
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionSalon,
				Code:    "request_id_conflict",
				Message: "This request id was already used for a different configuration payload.",
				Field:   "request_id",
			})
			plan.CanApply = false
			return importResponse(plan, false, runID), ErrImportConflict
		}
		return nil, err
	}
	plan.SalonID = salonID
	return importResponse(plan, false, runID), nil
}

func (s *Service) buildImportPlan(ctx context.Context, salonID string, ownerUserID string, req ImportRequest) (*importPlan, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" || s.imports == nil {
		return nil, ErrValidation
	}
	bundle, fingerprint, requestID, err := normalizedImportRequest(req)
	if err != nil {
		return nil, err
	}
	current, err := s.Get(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	target, err := s.imports.TargetImportState(ctx, salonID, ownerUserID, current)
	if err != nil {
		return nil, err
	}
	plan := newImportPlan(bundle, fingerprint, requestID, salonID, target)
	planSalonProfile(ctx, s.imports, plan)
	planAIReceptionist(plan)
	planPublicBookingPage(ctx, s.imports, salonID, plan)
	planIntegrations(plan)
	planServiceCategories(plan)
	planServiceAliases(plan)
	planKnowledge(plan)
	if len(plan.Conflicts) > 0 {
		plan.CanApply = false
	}
	return plan, nil
}

func (s *Service) buildOnboardingImportPlan(ctx context.Context, ownerUserID string, bundle ConfigurationBundle, fingerprint string, requestID string, skipExistingSalonCheck bool) (*importPlan, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" || s.imports == nil {
		return nil, ErrValidation
	}
	target := onboardingImportTargetState()
	plan := newImportPlan(bundle, fingerprint, requestID, "", target)
	if !skipExistingSalonCheck {
		hasSalon, err := s.imports.OwnerHasSalon(ctx, ownerUserID)
		if err != nil {
			return nil, err
		}
		if hasSalon {
			summary(plan, SectionSalon).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionSalon,
				Code:    "owner_salon_exists",
				Message: "This owner already has a salon. Use Settings configuration transfer to import into the existing salon.",
			})
		}
	}
	planSalonProfile(ctx, s.imports, plan)
	planAIReceptionist(plan)
	planPublicBookingPage(ctx, s.imports, "", plan)
	planIntegrations(plan)
	planServiceCategories(plan)
	planKnowledge(plan)
	if len(plan.Conflicts) > 0 {
		plan.CanApply = false
	}
	return plan, nil
}

func newImportPlan(bundle ConfigurationBundle, fingerprint string, requestID string, salonID string, target *importTargetState) *importPlan {
	plan := &importPlan{
		Bundle:                bundle,
		PayloadFingerprint:    fingerprint,
		SchemaVersion:         bundle.SchemaVersion,
		SalonID:               salonID,
		RequestID:             requestID,
		Summary:               newSummaryMap(),
		RequiresSecretReentry: secretReentryProviders(bundle.Integrations),
		CanApply:              true,
		Target:                target,
		PublicCatalogEnabled:  target.PublicBookingPage.PublicCatalogEnabled,
		AIEnabled:             target.SalonProfile.AIEnabled,
		BookingMode:           target.AIReceptionist.BookingMode,
	}
	if bundle.SchemaVersion == LegacySchemaV1 {
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionKnowledge,
			Code:    "legacy_schema_missing_knowledge_base",
			Message: "This v1 export did not include AI Training knowledge base entries.",
		})
	}
	if bundle.SchemaVersion == LegacySchemaV1 || bundle.SchemaVersion == LegacySchemaV2 || bundle.SchemaVersion == LegacySchemaV3 {
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionCategories,
			Code:    "legacy_schema_missing_service_categories",
			Message: "This export did not include service category taxonomy or category aliases.",
		})
	}
	if bundle.SchemaVersion == LegacySchemaV1 || bundle.SchemaVersion == LegacySchemaV2 || bundle.SchemaVersion == LegacySchemaV3 || bundle.SchemaVersion == LegacySchemaV4 {
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionServiceAliases,
			Code:    "legacy_schema_missing_service_aliases",
			Message: "This export did not include service aliases.",
		})
	}
	if plan.Bundle.POSConnection.Status != "" {
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionIntegrations,
			Code:    "pos_connection_metadata_not_imported",
			Message: "POS connection metadata is exported for reference only. OAuth tokens and provider connection state are not imported.",
			Field:   "pos_connection",
		})
	}
	for _, provider := range plan.RequiresSecretReentry {
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionIntegrations,
			Code:    "secret_reentry_required",
			Message: provider + " secret values are not included in the export. Re-enter secrets or reconnect this provider after import.",
			Field:   provider,
		})
	}
	return plan
}

func onboardingImportTargetState() *importTargetState {
	return &importTargetState{
		SalonProfile: SalonProfileExport{
			Timezone:          "America/Chicago",
			PrimaryLanguage:   "en",
			SecondaryLanguage: "vi",
			ActivePOSProvider: pos.ProviderSquare,
		},
		AIReceptionist: AIReceptionistExport{
			AIVoice:                 "professional_female",
			BookingMode:             "pending_approval",
			RecordingEnabled:        true,
			RecordingConsentMessage: "This call may be recorded to help us manage appointments and improve service.",
			SMSConfirmationEnabled:  true,
			SMSReminderEnabled:      true,
			ReminderHoursBefore:     24,
			HandoffEnabled:          true,
		},
		PublicCanPublish:             false,
		CanEnableAIBooking:           false,
		ServiceCategoryBySlug:        map[string]ServiceCategoryExport{},
		CategoryAliasByKey:           map[string]ServiceCategoryAliasExport{},
		ActiveServiceAliasKeys:       map[string]bool{},
		ActiveCategoryAliasKeys:      map[string]bool{},
		ServiceAliasByKey:            map[string]ServiceAliasExport{},
		ServiceAliasTargetsByKey:     map[string]importServiceAliasTarget{},
		AmbiguousServiceAliasTargets: map[string]bool{},
		KnowledgeByImportKey:         map[string]KnowledgeItemExport{},
		KnowledgeByContentHash:       map[string]KnowledgeItemExport{},
	}
}

func planSalonProfile(ctx context.Context, store ImportStore, plan *importPlan) {
	target := plan.Target.SalonProfile
	incoming := plan.Bundle.SalonProfile
	fieldChange(plan, SectionSalon, "name", target.Name, incoming.Name)
	fieldChange(plan, SectionSalon, "phone", target.Phone, incoming.Phone)
	fieldChange(plan, SectionSalon, "address", target.Address, incoming.Address)
	fieldChange(plan, SectionSalon, "city", target.City, incoming.City)
	fieldChange(plan, SectionSalon, "state", target.State, incoming.State)
	fieldChange(plan, SectionSalon, "zip_code", target.ZipCode, incoming.ZipCode)
	fieldChange(plan, SectionSalon, "timezone", target.Timezone, incoming.Timezone)
	fieldChange(plan, SectionSalon, "primary_language", target.PrimaryLanguage, incoming.PrimaryLanguage)
	fieldChange(plan, SectionSalon, "secondary_language", target.SecondaryLanguage, incoming.SecondaryLanguage)
	fieldChange(plan, SectionSalon, "handoff_phone", target.HandoffPhone, incoming.HandoffPhone)

	if incoming.AIEnabled && !plan.Target.CanEnableAIBooking {
		summary(plan, SectionSalon).Skipped++
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionSalon,
			Code:    "ai_enabled_skipped_readiness",
			Message: "AI booking was not enabled because the target salon has not passed Square booking readiness checks.",
			Field:   "ai_enabled",
		})
	} else {
		plan.AIEnabled = incoming.AIEnabled
		fieldChange(plan, SectionSalon, "ai_enabled", boolString(target.AIEnabled), boolString(incoming.AIEnabled))
	}

	if incoming.ActivePOSProvider != "" && incoming.ActivePOSProvider != pos.ProviderSquare {
		summary(plan, SectionSalon).Skipped++
		plan.Bundle.SalonProfile.ActivePOSProvider = target.ActivePOSProvider
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionSalon,
			Code:    "active_provider_skipped",
			Message: "Only Square Appointments can be active in the current production release. Active POS provider was not changed.",
			Field:   "active_pos_provider",
		})
	} else {
		if incoming.ActivePOSProvider == "" {
			plan.Bundle.SalonProfile.ActivePOSProvider = pos.ProviderSquare
		}
		fieldChange(plan, SectionSalon, "active_pos_provider", target.ActivePOSProvider, plan.Bundle.SalonProfile.ActivePOSProvider)
	}
	_ = ctx
	_ = store
}

func planAIReceptionist(plan *importPlan) {
	target := plan.Target.AIReceptionist
	incoming := plan.Bundle.AIReceptionist
	fieldChange(plan, SectionAI, "ai_greeting", target.AIGreeting, incoming.AIGreeting)
	fieldChange(plan, SectionAI, "ai_voice", target.AIVoice, incoming.AIVoice)
	fieldChange(plan, SectionAI, "ai_tone", target.AITone, incoming.AITone)
	fieldChange(plan, SectionAI, "recording_enabled", boolString(target.RecordingEnabled), boolString(incoming.RecordingEnabled))
	fieldChange(plan, SectionAI, "recording_consent_message", target.RecordingConsentMessage, incoming.RecordingConsentMessage)
	fieldChange(plan, SectionAI, "sms_confirmation_enabled", boolString(target.SMSConfirmationEnabled), boolString(incoming.SMSConfirmationEnabled))
	fieldChange(plan, SectionAI, "sms_reminder_enabled", boolString(target.SMSReminderEnabled), boolString(incoming.SMSReminderEnabled))
	fieldChange(plan, SectionAI, "reminder_hours_before", intString(target.ReminderHoursBefore), intString(incoming.ReminderHoursBefore))
	fieldChange(plan, SectionAI, "handoff_enabled", boolString(target.HandoffEnabled), boolString(incoming.HandoffEnabled))

	if incoming.BookingMode == "confirmed_booking" && !plan.Target.CanEnableAIBooking {
		summary(plan, SectionAI).Skipped++
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionAI,
			Code:    "confirmed_booking_skipped_readiness",
			Message: "POS-confirmed booking mode was skipped because the target salon has not passed Square booking readiness checks.",
			Field:   "booking_mode",
		})
		return
	}
	plan.BookingMode = incoming.BookingMode
	fieldChange(plan, SectionAI, "booking_mode", target.BookingMode, incoming.BookingMode)
}

func planPublicBookingPage(ctx context.Context, store ImportStore, salonID string, plan *importPlan) {
	target := plan.Target.PublicBookingPage
	incoming := plan.Bundle.PublicBookingPage
	if incoming.PublicSlug != "" {
		taken, err := store.PublicSlugTaken(ctx, salonID, incoming.PublicSlug)
		if err != nil {
			summary(plan, SectionPublic).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionPublic,
				Code:    "public_slug_check_failed",
				Message: "Could not verify whether the public booking page slug is available.",
				Field:   "public_slug",
			})
		} else if taken {
			summary(plan, SectionPublic).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section: SectionPublic,
				Code:    "public_slug_unavailable",
				Message: "The public booking page slug is already used by another salon.",
				Field:   "public_slug",
			})
		} else {
			fieldChange(plan, SectionPublic, "public_slug", target.PublicSlug, incoming.PublicSlug)
		}
	} else {
		fieldChange(plan, SectionPublic, "public_slug", target.PublicSlug, incoming.PublicSlug)
	}

	if incoming.PublicCatalogEnabled && !plan.Target.PublicCanPublish {
		summary(plan, SectionPublic).Skipped++
		plan.Warnings = append(plan.Warnings, ImportIssue{
			Section: SectionPublic,
			Code:    "public_catalog_enabled_skipped_readiness",
			Message: "Public booking page publish state was not enabled because the target salon does not have synced AI-bookable services and staff.",
			Field:   "public_catalog_enabled",
		})
		return
	}
	plan.PublicCatalogEnabled = incoming.PublicCatalogEnabled
	fieldChange(plan, SectionPublic, "public_catalog_enabled", boolString(target.PublicCatalogEnabled), boolString(incoming.PublicCatalogEnabled))
}

func planIntegrations(plan *importPlan) {
	target := plan.Target.Integrations
	incoming := plan.Bundle.Integrations
	fieldChange(plan, SectionIntegrations, "square.environment", target.Square.Environment, incoming.Square.Environment)
	fieldChange(plan, SectionIntegrations, "square.client_id", target.Square.ClientID, incoming.Square.ClientID)
	fieldChange(plan, SectionIntegrations, "square.redirect_url", target.Square.RedirectURL, incoming.Square.RedirectURL)
	fieldChange(plan, SectionIntegrations, "square.api_version", target.Square.APIVersion, incoming.Square.APIVersion)
	fieldChange(plan, SectionIntegrations, "square.api_base_url", target.Square.APIBaseURL, incoming.Square.APIBaseURL)
	fieldChange(plan, SectionIntegrations, "twilio.public_base_url", target.Twilio.PublicBaseURL, incoming.Twilio.PublicBaseURL)
	fieldChange(plan, SectionIntegrations, "twilio.incoming_path", target.Twilio.IncomingPath, incoming.Twilio.IncomingPath)
	fieldChange(plan, SectionIntegrations, "twilio.turn_path", target.Twilio.TurnPath, incoming.Twilio.TurnPath)
	fieldChange(plan, SectionIntegrations, "twilio.recording_path", target.Twilio.RecordingPath, incoming.Twilio.RecordingPath)
	fieldChange(plan, SectionIntegrations, "twilio.stream_path", target.Twilio.StreamPath, incoming.Twilio.StreamPath)
	fieldChange(plan, SectionIntegrations, "twilio.voice_transport", target.Twilio.VoiceTransport, incoming.Twilio.VoiceTransport)
	fieldChange(plan, SectionIntegrations, "openai.enabled", boolString(target.OpenAI.Enabled), boolString(incoming.OpenAI.Enabled))
	fieldChange(plan, SectionIntegrations, "openai.base_url", target.OpenAI.BaseURL, incoming.OpenAI.BaseURL)
	fieldChange(plan, SectionIntegrations, "openai.transcription_model", target.OpenAI.TranscriptionModel, incoming.OpenAI.TranscriptionModel)
	fieldChange(plan, SectionIntegrations, "openai.reply_model", target.OpenAI.ReplyModel, incoming.OpenAI.ReplyModel)
	fieldChange(plan, SectionIntegrations, "openai.speech_model", target.OpenAI.SpeechModel, incoming.OpenAI.SpeechModel)
	fieldChange(plan, SectionIntegrations, "openai.speech_voice", target.OpenAI.SpeechVoice, incoming.OpenAI.SpeechVoice)
	fieldChange(plan, SectionIntegrations, "openai.speech_output_mode", target.OpenAI.SpeechOutputMode, incoming.OpenAI.SpeechOutputMode)
	fieldChange(plan, SectionIntegrations, "openai.realtime_enabled", boolString(target.OpenAI.RealtimeEnabled), boolString(incoming.OpenAI.RealtimeEnabled))
	fieldChange(plan, SectionIntegrations, "openai.realtime_model", target.OpenAI.RealtimeModel, incoming.OpenAI.RealtimeModel)
	fieldChange(plan, SectionIntegrations, "openai.realtime_voice", target.OpenAI.RealtimeVoice, incoming.OpenAI.RealtimeVoice)
	fieldChange(plan, SectionIntegrations, "openai.realtime_noise_profile", target.OpenAI.RealtimeNoiseProfile, incoming.OpenAI.RealtimeNoiseProfile)
	fieldChange(plan, SectionIntegrations, "openai.realtime_instructions", target.OpenAI.RealtimeInstructions, incoming.OpenAI.RealtimeInstructions)
}

func planServiceCategories(plan *importPlan) {
	seenSlugs := map[string]bool{}
	seenAliases := map[string]bool{}
	incomingServiceAliasKeys := activeBundleServiceAliasKeys(plan.Bundle.ServiceAliases.Items)
	for _, item := range plan.Bundle.ServiceCategories.Items {
		if seenSlugs[item.Slug] {
			summary(plan, SectionCategories).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section:   SectionCategories,
				Code:      "duplicate_service_category_slug",
				Message:   "The import file contains more than one service category with the same slug.",
				Field:     "slug",
				SourceKey: item.SourceKey,
			})
			continue
		}
		seenSlugs[item.Slug] = true

		operation := "created"
		if existing, ok := plan.Target.ServiceCategoryBySlug[item.Slug]; ok {
			if serviceCategoryEqual(existing, item) {
				operation = "unchanged"
				summary(plan, SectionCategories).Unchanged++
			} else {
				operation = "updated"
				summary(plan, SectionCategories).Updated++
			}
		} else {
			summary(plan, SectionCategories).Created++
		}
		planned := plannedServiceCategory{Item: item, Operation: operation}

		for _, alias := range item.Aliases {
			if seenAliases[alias.NormalizedAlias] {
				summary(plan, SectionCategories).Conflicts++
				plan.Conflicts = append(plan.Conflicts, ImportIssue{
					Section:   SectionCategories,
					Code:      "duplicate_service_category_alias",
					Message:   "The import file contains more than one service category alias with the same normalized alias.",
					Field:     "normalized_alias",
					SourceKey: alias.SourceKey,
				})
				continue
			}
			seenAliases[alias.NormalizedAlias] = true
			if plan.Target.ActiveServiceAliasKeys[alias.NormalizedAlias] {
				summary(plan, SectionCategories).Conflicts++
				plan.Conflicts = append(plan.Conflicts, ImportIssue{
					Section:   SectionCategories,
					Code:      "category_alias_conflicts_with_service_alias",
					Message:   "A category alias conflicts with an active service alias on the target salon.",
					Field:     "normalized_alias",
					SourceKey: alias.SourceKey,
				})
				continue
			}
			if incomingServiceAliasKeys[alias.NormalizedAlias] {
				summary(plan, SectionCategories).Conflicts++
				plan.Conflicts = append(plan.Conflicts, ImportIssue{
					Section:   SectionCategories,
					Code:      "category_alias_conflicts_with_imported_service_alias",
					Message:   "The import file contains the same active normalized phrase as both a category alias and a service alias.",
					Field:     "normalized_alias",
					SourceKey: alias.SourceKey,
				})
				continue
			}

			aliasOperation := "created"
			if existing, ok := plan.Target.CategoryAliasByKey[alias.NormalizedAlias]; ok {
				if serviceCategoryAliasEqual(existing, alias) {
					aliasOperation = "unchanged"
					summary(plan, SectionCategories).Unchanged++
				} else {
					aliasOperation = "updated"
					summary(plan, SectionCategories).Updated++
				}
			} else {
				summary(plan, SectionCategories).Created++
			}
			planned.Aliases = append(planned.Aliases, plannedServiceCategoryAlias{
				CategorySlug: item.Slug,
				Item:         alias,
				Operation:    aliasOperation,
			})
		}
		plan.ServiceCategories = append(plan.ServiceCategories, planned)
	}
}

func planServiceAliases(plan *importPlan) {
	seen := map[string]bool{}
	incomingCategoryAliasKeys := activeBundleCategoryAliasKeys(plan.Bundle.ServiceCategories.Items)
	for _, item := range plan.Bundle.ServiceAliases.Items {
		if seen[item.NormalizedAlias] {
			summary(plan, SectionServiceAliases).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section:   SectionServiceAliases,
				Code:      "duplicate_service_alias",
				Message:   "The import file contains more than one service alias with the same normalized alias.",
				Field:     "normalized_alias",
				SourceKey: item.SourceKey,
			})
			continue
		}
		seen[item.NormalizedAlias] = true
		if item.Status == training.AliasStatusActive && (plan.Target.ActiveCategoryAliasKeys[item.NormalizedAlias] || incomingCategoryAliasKeys[item.NormalizedAlias]) {
			summary(plan, SectionServiceAliases).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section:   SectionServiceAliases,
				Code:      "service_alias_conflicts_with_category_alias",
				Message:   "A service alias conflicts with an active service category alias.",
				Field:     "normalized_alias",
				SourceKey: item.SourceKey,
			})
			continue
		}

		targetKey := serviceAliasTargetKey(item.TargetService)
		if targetKey == "" {
			summary(plan, SectionServiceAliases).Skipped++
			plan.Warnings = append(plan.Warnings, ImportIssue{
				Section:   SectionServiceAliases,
				Code:      "service_alias_target_missing",
				Message:   "A service alias was skipped because the import file did not include a target service reference.",
				Field:     "target_service",
				SourceKey: item.SourceKey,
			})
			plan.ServiceAliases = append(plan.ServiceAliases, plannedServiceAlias{Item: item, Operation: "skipped"})
			continue
		}
		if plan.Target.AmbiguousServiceAliasTargets[targetKey] {
			summary(plan, SectionServiceAliases).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section:   SectionServiceAliases,
				Code:      "service_alias_target_ambiguous",
				Message:   "A service alias target matched more than one service on the target salon.",
				Field:     "target_service",
				SourceKey: item.SourceKey,
			})
			continue
		}
		target, ok := plan.Target.ServiceAliasTargetsByKey[targetKey]
		if !ok {
			summary(plan, SectionServiceAliases).Skipped++
			plan.Warnings = append(plan.Warnings, ImportIssue{
				Section:   SectionServiceAliases,
				Code:      "service_alias_target_not_found",
				Message:   "A service alias was skipped because its target service was not found on the target salon.",
				Field:     "target_service",
				SourceKey: item.SourceKey,
			})
			plan.ServiceAliases = append(plan.ServiceAliases, plannedServiceAlias{Item: item, Operation: "skipped"})
			continue
		}

		operation := "created"
		if existing, ok := plan.Target.ServiceAliasByKey[item.NormalizedAlias]; ok {
			if serviceAliasEqual(existing, item) {
				operation = "unchanged"
				summary(plan, SectionServiceAliases).Unchanged++
			} else {
				operation = "updated"
				summary(plan, SectionServiceAliases).Updated++
			}
		} else {
			summary(plan, SectionServiceAliases).Created++
		}
		plan.ServiceAliases = append(plan.ServiceAliases, plannedServiceAlias{
			Item:            item,
			Operation:       operation,
			TargetServiceID: target.ServiceID,
		})
	}
}

func planKnowledge(plan *importPlan) {
	seen := map[string]bool{}
	for _, item := range plan.Bundle.KnowledgeBase.Items {
		if item.SourceKey == "" {
			item.SourceKey = contentSourceKey(item)
			plan.Warnings = append(plan.Warnings, ImportIssue{
				Section: SectionKnowledge,
				Code:    "knowledge_source_key_generated",
				Message: "A knowledge item was missing source_key, so a content-based key was generated.",
			})
		}
		if seen[item.SourceKey] {
			summary(plan, SectionKnowledge).Conflicts++
			plan.Conflicts = append(plan.Conflicts, ImportIssue{
				Section:   SectionKnowledge,
				Code:      "duplicate_knowledge_source_key",
				Message:   "The import file contains more than one knowledge item with the same source_key.",
				SourceKey: item.SourceKey,
			})
			continue
		}
		seen[item.SourceKey] = true
		if existing, ok := plan.Target.KnowledgeByImportKey[item.SourceKey]; ok {
			if knowledgeEqual(existing, item) {
				summary(plan, SectionKnowledge).Unchanged++
				plan.Knowledge = append(plan.Knowledge, plannedKnowledgeItem{Item: item, Operation: "unchanged"})
			} else {
				summary(plan, SectionKnowledge).Updated++
				plan.Knowledge = append(plan.Knowledge, plannedKnowledgeItem{Item: item, Operation: "updated"})
			}
			continue
		}
		if _, ok := plan.Target.KnowledgeByContentHash[knowledgeContentHash(item)]; ok {
			summary(plan, SectionKnowledge).Unchanged++
			plan.Knowledge = append(plan.Knowledge, plannedKnowledgeItem{Item: item, Operation: "unchanged"})
			continue
		}
		summary(plan, SectionKnowledge).Created++
		plan.Knowledge = append(plan.Knowledge, plannedKnowledgeItem{Item: item, Operation: "created"})
	}
}

func normalizeImportBundle(bundle ConfigurationBundle) (ConfigurationBundle, error) {
	bundle.SchemaVersion = strings.TrimSpace(bundle.SchemaVersion)
	if bundle.SchemaVersion == "" {
		return bundle, ErrValidation
	}
	if bundle.SchemaVersion != SchemaVersion && bundle.SchemaVersion != LegacySchemaV4 && bundle.SchemaVersion != LegacySchemaV3 && bundle.SchemaVersion != LegacySchemaV2 && bundle.SchemaVersion != LegacySchemaV1 {
		return bundle, ErrUnsupportedSchema
	}
	if bundle.SecretsExported || bundle.OperationalDataExported {
		return bundle, ErrValidation
	}
	bundle.ExcludedData = copyStrings(excludedData)
	bundle.RequiresSecretReentry = secretReentryProviders(bundle.Integrations)
	bundle.SalonProfile = normalizeSalonProfile(bundle.SalonProfile)
	if bundle.SalonProfile.Name == "" || bundle.SalonProfile.Phone == "" {
		return bundle, ErrValidation
	}
	bundle.AIReceptionist = normalizeAIReceptionist(bundle.AIReceptionist)
	if bundle.AIReceptionist.AIGreeting == "" || bundle.AIReceptionist.RecordingConsentMessage == "" || bundle.AIReceptionist.ReminderHoursBefore <= 0 {
		return bundle, ErrValidation
	}
	if !validBookingMode(bundle.AIReceptionist.BookingMode) {
		return bundle, ErrValidation
	}
	bundle.PublicBookingPage.PublicSlug = normalizePublicSlug(bundle.PublicBookingPage.PublicSlug)
	if bundle.PublicBookingPage.PublicCatalogEnabled && bundle.PublicBookingPage.PublicSlug == "" {
		return bundle, ErrValidation
	}
	bundle.PublicBookingPage.PublicPath = publicPath(bundle.PublicBookingPage.PublicSlug)
	bundle.Integrations = normalizeIntegrationConfigs(bundle.Integrations)
	if err := validateIntegrationURLs(bundle.Integrations); err != nil {
		return bundle, err
	}
	for i := range bundle.ServiceCategories.Items {
		item := normalizeServiceCategoryItem(bundle.ServiceCategories.Items[i])
		if item.Name == "" || item.Slug == "" || !validServiceCategoryStatus(item.Status) || !validServiceCategorySource(item.Source) {
			return bundle, ErrValidation
		}
		for j := range item.Aliases {
			alias := normalizeServiceCategoryAliasItem(item.Aliases[j])
			if alias.Alias == "" || alias.NormalizedAlias == "" || !validServiceCategoryStatus(alias.Status) || !validServiceCategoryAliasSource(alias.Source) || alias.Confidence <= 0 || alias.Confidence > 1 {
				return bundle, ErrValidation
			}
			item.Aliases[j] = alias
		}
		bundle.ServiceCategories.Items[i] = item
	}
	bundle.ServiceCategories.Count = len(bundle.ServiceCategories.Items)
	for i := range bundle.ServiceAliases.Items {
		item := normalizeServiceAliasItem(bundle.ServiceAliases.Items[i])
		if item.Alias == "" || item.NormalizedAlias == "" || item.TargetService.Name == "" || !validServiceAliasStatus(item.Status) || !validServiceAliasSource(item.Source) || item.Confidence <= 0 || item.Confidence > 1 {
			return bundle, ErrValidation
		}
		bundle.ServiceAliases.Items[i] = item
	}
	bundle.ServiceAliases.Count = len(bundle.ServiceAliases.Items)
	for i := range bundle.KnowledgeBase.Items {
		item := normalizeKnowledgeItem(bundle.KnowledgeBase.Items[i])
		if item.Title == "" || item.Body == "" || !validKnowledgeCategory(item.Category) || !validKnowledgeStatus(item.Status) {
			return bundle, ErrValidation
		}
		bundle.KnowledgeBase.Items[i] = item
	}
	bundle.KnowledgeBase.Count = len(bundle.KnowledgeBase.Items)
	return bundle, nil
}

func normalizedImportRequest(req ImportRequest) (ConfigurationBundle, string, string, error) {
	bundle, err := normalizeImportBundle(req.Configuration)
	if err != nil {
		return bundle, "", "", err
	}
	fingerprint, err := fingerprintBundle(bundle)
	if err != nil {
		return bundle, "", "", err
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = randomRequestID()
	}
	return bundle, fingerprint, requestID, nil
}

func normalizeSalonProfile(profile SalonProfileExport) SalonProfileExport {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Phone = strings.TrimSpace(profile.Phone)
	profile.Address = strings.TrimSpace(profile.Address)
	profile.City = strings.TrimSpace(profile.City)
	profile.State = strings.TrimSpace(profile.State)
	profile.ZipCode = strings.TrimSpace(profile.ZipCode)
	profile.Timezone = defaultString(strings.TrimSpace(profile.Timezone), "America/Chicago")
	profile.PrimaryLanguage = defaultString(strings.TrimSpace(profile.PrimaryLanguage), "en")
	profile.SecondaryLanguage = defaultString(strings.TrimSpace(profile.SecondaryLanguage), "vi")
	profile.HandoffPhone = strings.TrimSpace(profile.HandoffPhone)
	profile.ActivePOSProvider = defaultString(strings.TrimSpace(profile.ActivePOSProvider), pos.ProviderSquare)
	return profile
}

func normalizeAIReceptionist(settings AIReceptionistExport) AIReceptionistExport {
	settings.AIGreeting = strings.TrimSpace(settings.AIGreeting)
	settings.AIVoice = defaultString(strings.TrimSpace(settings.AIVoice), "professional_female")
	settings.AITone = normalizeAITone(settings.AITone)
	settings.BookingMode = defaultString(strings.TrimSpace(settings.BookingMode), "pending_approval")
	settings.RecordingConsentMessage = strings.TrimSpace(settings.RecordingConsentMessage)
	return settings
}

func normalizeIntegrationConfigs(configs integrationconfig.IntegrationConfigsResponse) integrationconfig.IntegrationConfigsResponse {
	configs.Square.Provider = integrationconfig.ProviderSquare
	configs.Square.Environment = defaultString(normalizeEnvironment(configs.Square.Environment), "sandbox")
	configs.Square.ClientID = strings.TrimSpace(configs.Square.ClientID)
	configs.Square.RedirectURL = strings.TrimSpace(configs.Square.RedirectURL)
	configs.Square.APIVersion = strings.TrimSpace(configs.Square.APIVersion)
	configs.Square.APIBaseURL = strings.TrimRight(strings.TrimSpace(configs.Square.APIBaseURL), "/")
	configs.Twilio.Provider = integrationconfig.ProviderTwilio
	configs.Twilio.PublicBaseURL = strings.TrimRight(strings.TrimSpace(configs.Twilio.PublicBaseURL), "/")
	configs.Twilio.IncomingPath = defaultString(strings.TrimSpace(configs.Twilio.IncomingPath), "/api/voice/twilio/incoming")
	configs.Twilio.TurnPath = defaultString(strings.TrimSpace(configs.Twilio.TurnPath), "/api/voice/twilio/turn")
	configs.Twilio.RecordingPath = defaultString(strings.TrimSpace(configs.Twilio.RecordingPath), "/api/voice/twilio/recording")
	configs.Twilio.StreamPath = defaultString(strings.TrimSpace(configs.Twilio.StreamPath), "/api/voice/twilio/stream")
	configs.Twilio.VoiceTransport = normalizeVoiceTransport(configs.Twilio.VoiceTransport)
	configs.OpenAI.Provider = integrationconfig.ProviderOpenAI
	configs.OpenAI.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(configs.OpenAI.BaseURL), "/"), "https://api.openai.com/v1")
	configs.OpenAI.TranscriptionModel = defaultString(strings.TrimSpace(configs.OpenAI.TranscriptionModel), "gpt-4o-mini-transcribe")
	configs.OpenAI.ReplyModel = defaultString(strings.TrimSpace(configs.OpenAI.ReplyModel), "gpt-4.1-mini")
	configs.OpenAI.SpeechModel = defaultString(strings.TrimSpace(configs.OpenAI.SpeechModel), "tts-1")
	configs.OpenAI.SpeechVoice = defaultString(strings.TrimSpace(configs.OpenAI.SpeechVoice), "alloy")
	configs.OpenAI.SpeechOutputMode = config.NormalizeOpenAISpeechOutputMode(configs.OpenAI.SpeechOutputMode)
	configs.OpenAI.RealtimeModel = strings.TrimSpace(configs.OpenAI.RealtimeModel)
	configs.OpenAI.RealtimeVoice = defaultString(strings.TrimSpace(configs.OpenAI.RealtimeVoice), configs.OpenAI.SpeechVoice)
	configs.OpenAI.RealtimeNoiseProfile = strings.TrimSpace(configs.OpenAI.RealtimeNoiseProfile)
	configs.OpenAI.RealtimeInstructions = strings.TrimSpace(configs.OpenAI.RealtimeInstructions)
	return configs
}

func normalizeKnowledgeItem(item KnowledgeItemExport) KnowledgeItemExport {
	item.SourceKey = strings.TrimSpace(item.SourceKey)
	item.Title = strings.TrimSpace(item.Title)
	item.Category = strings.ToLower(strings.TrimSpace(item.Category))
	item.Body = strings.TrimSpace(item.Body)
	item.Status = strings.ToLower(strings.TrimSpace(item.Status))
	item.Source = strings.ToLower(strings.TrimSpace(item.Source))
	if item.Category == "" {
		item.Category = training.CategoryFAQ
	}
	if item.Status == "" {
		item.Status = training.StatusDraft
	}
	if item.Source != training.SourceCorrection {
		item.Source = training.SourceOwner
	}
	return item
}

func normalizeServiceCategoryItem(item ServiceCategoryExport) ServiceCategoryExport {
	item.Name = strings.TrimSpace(item.Name)
	item.Slug = normalizeConfigSlug(defaultString(strings.TrimSpace(item.Slug), item.Name))
	item.SourceKey = defaultString(strings.TrimSpace(item.SourceKey), serviceCategorySourceKey(item.Slug))
	item.Description = strings.TrimSpace(item.Description)
	item.Status = defaultString(strings.ToLower(strings.TrimSpace(item.Status)), pos.ServiceCategoryStatusActive)
	item.Source = defaultString(strings.ToLower(strings.TrimSpace(item.Source)), pos.ServiceCategorySourceImported)
	for i := range item.Aliases {
		item.Aliases[i] = normalizeServiceCategoryAliasItem(item.Aliases[i])
	}
	return item
}

func normalizeServiceCategoryAliasItem(item ServiceCategoryAliasExport) ServiceCategoryAliasExport {
	item.Alias = strings.TrimSpace(item.Alias)
	item.NormalizedAlias = normalizeConfigAlias(defaultString(strings.TrimSpace(item.NormalizedAlias), item.Alias))
	item.SourceKey = defaultString(strings.TrimSpace(item.SourceKey), serviceCategoryAliasSourceKey(item.NormalizedAlias))
	item.Source = defaultString(strings.ToLower(strings.TrimSpace(item.Source)), pos.ServiceCategoryAliasSourceImported)
	item.Status = defaultString(strings.ToLower(strings.TrimSpace(item.Status)), pos.ServiceCategoryStatusActive)
	if item.Confidence == 0 {
		item.Confidence = 0.94
	}
	return item
}

func normalizeServiceAliasItem(item ServiceAliasExport) ServiceAliasExport {
	item.Alias = strings.TrimSpace(item.Alias)
	item.NormalizedAlias = normalizeConfigAlias(defaultString(strings.TrimSpace(item.NormalizedAlias), item.Alias))
	item.SourceKey = defaultString(strings.TrimSpace(item.SourceKey), serviceAliasSourceKey(item.NormalizedAlias))
	item.TargetService.Name = strings.TrimSpace(item.TargetService.Name)
	item.TargetService.PriceDisplay = strings.TrimSpace(item.TargetService.PriceDisplay)
	item.Source = defaultString(strings.ToLower(strings.TrimSpace(item.Source)), training.AliasSourceImport)
	item.Status = defaultString(strings.ToLower(strings.TrimSpace(item.Status)), training.AliasStatusActive)
	if item.Confidence == 0 {
		item.Confidence = 0.94
	}
	return item
}

func validateIntegrationURLs(configs integrationconfig.IntegrationConfigsResponse) error {
	if configs.Square.RedirectURL == "" || configs.Square.APIVersion == "" {
		return ErrValidation
	}
	if configs.OpenAI.Enabled && (configs.OpenAI.BaseURL == "" || configs.OpenAI.TranscriptionModel == "" || configs.OpenAI.ReplyModel == "" || configs.OpenAI.SpeechModel == "" || configs.OpenAI.SpeechVoice == "") {
		return ErrValidation
	}
	if configs.OpenAI.Enabled && configs.OpenAI.RealtimeEnabled && (configs.OpenAI.RealtimeModel == "" || configs.OpenAI.RealtimeVoice == "") {
		return ErrValidation
	}
	for _, value := range []string{configs.Square.RedirectURL, configs.Twilio.PublicBaseURL, configs.OpenAI.BaseURL} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ErrValidation
		}
	}
	return nil
}

func (s *Service) posConnection(ctx context.Context, salonID string, provider string) (*pos.Connection, error) {
	if s.pos == nil {
		return nil, nil
	}
	connection, err := s.pos.GetConnection(ctx, salonID, provider)
	if errors.Is(err, pos.ErrNotFound) {
		return nil, nil
	}
	return connection, err
}

func (s *Service) serviceCategories(ctx context.Context, salonID string, ownerUserID string) ([]pos.ServiceCategory, error) {
	if s.categories == nil {
		return []pos.ServiceCategory{}, nil
	}
	return s.categories.ListServiceCategories(ctx, salonID, ownerUserID)
}

func (s *Service) serviceCatalog(ctx context.Context, salonID string, provider string) ([]pos.Service, error) {
	if s.services == nil {
		return []pos.Service{}, nil
	}
	return s.services.ListServices(ctx, salonID, provider)
}

func (s *Service) serviceAliases(ctx context.Context, salonID string, ownerUserID string) ([]training.ServiceAlias, error) {
	if s.aliases == nil {
		return []training.ServiceAlias{}, nil
	}
	return s.aliases.ListServiceAliases(ctx, salonID, ownerUserID)
}

func salonProfileExport(item *salon.Salon) SalonProfileExport {
	return SalonProfileExport{
		Name:              item.Name,
		Phone:             item.Phone,
		Address:           item.Address,
		City:              item.City,
		State:             item.State,
		ZipCode:           item.ZipCode,
		Timezone:          item.Timezone,
		PrimaryLanguage:   item.PrimaryLanguage,
		SecondaryLanguage: item.SecondaryLanguage,
		HandoffPhone:      item.HandoffPhone,
		AIEnabled:         item.AIEnabled,
		ActivePOSProvider: item.ActivePOSProvider,
		UpdatedAt:         item.UpdatedAt,
	}
}

func aiReceptionistExport(settings *salon.Settings) AIReceptionistExport {
	return AIReceptionistExport{
		AIGreeting:              settings.AIGreeting,
		AIVoice:                 settings.AIVoice,
		AITone:                  normalizeAITone(settings.AITone),
		BookingMode:             settings.BookingMode,
		RecordingEnabled:        settings.RecordingEnabled,
		RecordingConsentMessage: settings.RecordingConsentMessage,
		SMSConfirmationEnabled:  settings.SMSConfirmationEnabled,
		SMSReminderEnabled:      settings.SMSReminderEnabled,
		ReminderHoursBefore:     settings.ReminderHoursBefore,
		HandoffEnabled:          settings.HandoffEnabled,
		UpdatedAt:               settings.UpdatedAt,
	}
}

func normalizeAITone(value string) string {
	switch strings.TrimSpace(value) {
	case "natural_human", "friendly_young", "concise_calm":
		return strings.TrimSpace(value)
	default:
		return "professional_warm"
	}
}

func publicBookingPageExport(settings *salon.PublicCatalogSettings) PublicBookingPageExport {
	return PublicBookingPageExport{
		PublicSlug:           settings.PublicSlug,
		PublicCatalogEnabled: settings.PublicCatalogEnabled,
		PublicPath:           settings.PublicPath,
		UpdatedAt:            settings.UpdatedAt,
	}
}

func posConnectionExport(provider string, connection *pos.Connection) POSConnectionExport {
	if connection == nil {
		return POSConnectionExport{
			Provider: provider,
			Status:   pos.StatusNotConnected,
			Scopes:   []string{},
		}
	}
	updatedAt := connection.UpdatedAt
	return POSConnectionExport{
		Provider:   connection.Provider,
		Status:     connection.Status,
		MerchantID: connection.MerchantID,
		LocationID: connection.LocationID,
		Scopes:     append([]string{}, connection.Scopes...),
		LastSyncAt: connection.LastSyncAt,
		UpdatedAt:  &updatedAt,
	}
}

func serviceCategoriesExport(items []pos.ServiceCategory) ServiceCategoryBundleExport {
	out := make([]ServiceCategoryExport, 0, len(items))
	for _, item := range items {
		category := ServiceCategoryExport{
			SourceKey:   serviceCategorySourceKey(item.Slug),
			Name:        item.Name,
			Slug:        item.Slug,
			Description: item.Description,
			Status:      item.Status,
			Source:      item.Source,
			SortOrder:   item.SortOrder,
			Aliases:     make([]ServiceCategoryAliasExport, 0, len(item.Aliases)),
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		}
		for _, alias := range item.Aliases {
			category.Aliases = append(category.Aliases, ServiceCategoryAliasExport{
				SourceKey:       serviceCategoryAliasSourceKey(alias.NormalizedAlias),
				Alias:           alias.Alias,
				NormalizedAlias: alias.NormalizedAlias,
				Source:          alias.Source,
				Status:          alias.Status,
				Confidence:      alias.Confidence,
				CreatedAt:       alias.CreatedAt,
				UpdatedAt:       alias.UpdatedAt,
			})
		}
		out = append(out, category)
	}
	return ServiceCategoryBundleExport{Items: out, Count: len(out)}
}

func serviceAliasesExport(items []training.ServiceAlias, services []pos.Service) ServiceAliasBundleExport {
	servicesByID := map[string]pos.Service{}
	for _, service := range services {
		if strings.TrimSpace(service.ID) == "" {
			continue
		}
		servicesByID[service.ID] = service
	}
	out := make([]ServiceAliasExport, 0, len(items))
	for _, item := range items {
		target := ServiceAliasTargetExport{Name: item.ServiceName}
		if service, ok := servicesByID[item.ServiceID]; ok {
			target.Name = service.Name
			target.DurationMinutes = service.DurationMinutes
			target.PriceDisplay = service.PriceDisplay
		}
		out = append(out, ServiceAliasExport{
			SourceKey:       serviceAliasSourceKey(item.NormalizedAlias),
			Alias:           item.Alias,
			NormalizedAlias: item.NormalizedAlias,
			TargetService:   target,
			Source:          item.Source,
			Status:          item.Status,
			Confidence:      item.Confidence,
			CreatedAt:       item.CreatedAt,
			UpdatedAt:       item.UpdatedAt,
		})
	}
	return ServiceAliasBundleExport{Items: out, Count: len(out)}
}

func knowledgeBaseExport(items []training.KnowledgeItem) KnowledgeBaseExport {
	out := make([]KnowledgeItemExport, 0, len(items))
	for _, item := range items {
		out = append(out, KnowledgeItemExport{
			SourceKey: knowledgeSourceKey(item),
			Title:     item.Title,
			Category:  item.Category,
			Body:      item.Body,
			Status:    item.Status,
			Source:    item.Source,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return KnowledgeBaseExport{Items: out, Count: len(out)}
}

func knowledgeSourceKey(item training.KnowledgeItem) string {
	if strings.TrimSpace(item.ImportKey) != "" {
		return strings.TrimSpace(item.ImportKey)
	}
	hash := sha256.Sum256([]byte("manleai:v2:knowledge:" + strings.TrimSpace(item.ID)))
	return "knowledge:" + hex.EncodeToString(hash[:])[:32]
}

func serviceCategorySourceKey(slug string) string {
	return "service_category:" + normalizeConfigSlug(slug)
}

func serviceCategoryAliasSourceKey(normalizedAlias string) string {
	return "service_category_alias:" + normalizeConfigAlias(normalizedAlias)
}

func serviceAliasSourceKey(normalizedAlias string) string {
	return "service_alias:" + normalizeConfigAlias(normalizedAlias)
}

func knowledgeContentHash(item KnowledgeItemExport) string {
	normalized := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(item.Title)),
		strings.ToLower(strings.TrimSpace(item.Category)),
		strings.TrimSpace(item.Body),
		strings.ToLower(strings.TrimSpace(item.Status)),
	}, "\x00")
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func contentSourceKey(item KnowledgeItemExport) string {
	return "knowledge:" + knowledgeContentHash(item)[:32]
}

func knowledgeEqual(a KnowledgeItemExport, b KnowledgeItemExport) bool {
	return strings.TrimSpace(a.Title) == strings.TrimSpace(b.Title) &&
		strings.TrimSpace(a.Category) == strings.TrimSpace(b.Category) &&
		strings.TrimSpace(a.Body) == strings.TrimSpace(b.Body) &&
		strings.TrimSpace(a.Status) == strings.TrimSpace(b.Status) &&
		strings.TrimSpace(a.Source) == strings.TrimSpace(b.Source)
}

func serviceCategoryEqual(a ServiceCategoryExport, b ServiceCategoryExport) bool {
	return strings.TrimSpace(a.Name) == strings.TrimSpace(b.Name) &&
		strings.TrimSpace(a.Slug) == strings.TrimSpace(b.Slug) &&
		strings.TrimSpace(a.Description) == strings.TrimSpace(b.Description) &&
		strings.TrimSpace(a.Status) == strings.TrimSpace(b.Status) &&
		strings.TrimSpace(a.Source) == strings.TrimSpace(b.Source) &&
		a.SortOrder == b.SortOrder
}

func serviceCategoryAliasEqual(a ServiceCategoryAliasExport, b ServiceCategoryAliasExport) bool {
	return strings.TrimSpace(a.Alias) == strings.TrimSpace(b.Alias) &&
		strings.TrimSpace(a.NormalizedAlias) == strings.TrimSpace(b.NormalizedAlias) &&
		strings.TrimSpace(a.Status) == strings.TrimSpace(b.Status) &&
		strings.TrimSpace(a.Source) == strings.TrimSpace(b.Source) &&
		a.Confidence == b.Confidence
}

func serviceAliasEqual(a ServiceAliasExport, b ServiceAliasExport) bool {
	return strings.TrimSpace(a.Alias) == strings.TrimSpace(b.Alias) &&
		strings.TrimSpace(a.NormalizedAlias) == strings.TrimSpace(b.NormalizedAlias) &&
		serviceAliasTargetKey(a.TargetService) == serviceAliasTargetKey(b.TargetService) &&
		strings.TrimSpace(a.Status) == strings.TrimSpace(b.Status) &&
		strings.TrimSpace(a.Source) == strings.TrimSpace(b.Source) &&
		a.Confidence == b.Confidence
}

func secretReentryProviders(configs integrationconfig.IntegrationConfigsResponse) []string {
	providers := []string{}
	if configs.Square.ClientSecretConfigured {
		providers = append(providers, integrationconfig.ProviderSquare)
	}
	if configs.Twilio.AuthTokenConfigured {
		providers = append(providers, integrationconfig.ProviderTwilio)
	}
	if configs.OpenAI.APIKeyConfigured {
		providers = append(providers, integrationconfig.ProviderOpenAI)
	}
	return providers
}

func importResponse(plan *importPlan, dryRun bool, runID string) *ImportResponse {
	status := StatusPreviewed
	if !dryRun {
		status = StatusApplied
		if !plan.CanApply {
			status = StatusFailed
		}
	}
	return &ImportResponse{
		ImportRunID:           runID,
		SalonID:               plan.SalonID,
		RequestID:             plan.RequestID,
		DryRun:                dryRun,
		Status:                status,
		SchemaVersion:         plan.SchemaVersion,
		CanApply:              plan.CanApply,
		Summary:               summaryValues(plan.Summary),
		Warnings:              issueValues(plan.Warnings),
		Conflicts:             issueValues(plan.Conflicts),
		ExcludedData:          copyStrings(excludedData),
		RequiresSecretReentry: copyStrings(plan.RequiresSecretReentry),
	}
}

func newSummaryMap() map[string]*ImportSectionSummary {
	out := map[string]*ImportSectionSummary{}
	for _, section := range []string{SectionSalon, SectionAI, SectionPublic, SectionIntegrations, SectionCategories, SectionServiceAliases, SectionKnowledge} {
		out[section] = &ImportSectionSummary{Section: section}
	}
	return out
}

func summary(plan *importPlan, section string) *ImportSectionSummary {
	item := plan.Summary[section]
	if item == nil {
		item = &ImportSectionSummary{Section: section}
		plan.Summary[section] = item
	}
	return item
}

func summaryValues(items map[string]*ImportSectionSummary) []ImportSectionSummary {
	order := []string{SectionSalon, SectionAI, SectionPublic, SectionIntegrations, SectionCategories, SectionServiceAliases, SectionKnowledge}
	out := make([]ImportSectionSummary, 0, len(order))
	for _, section := range order {
		if item := items[section]; item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func issueValues(items []ImportIssue) []ImportIssue {
	if items == nil {
		return []ImportIssue{}
	}
	return append([]ImportIssue{}, items...)
}

func fieldChange(plan *importPlan, section string, field string, current string, incoming string) {
	current = strings.TrimSpace(current)
	incoming = strings.TrimSpace(incoming)
	if current == incoming {
		summary(plan, section).Unchanged++
		return
	}
	summary(plan, section).Updated++
}

func fingerprintBundle(bundle ConfigurationBundle) (string, error) {
	raw, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func randomRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "import-" + time.Now().UTC().Format("20060102150405")
	}
	return "import-" + hex.EncodeToString(bytes[:])
}

func copyStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func validBookingMode(value string) bool {
	switch value {
	case "confirmed_booking", "pending_approval", "disabled":
		return true
	default:
		return false
	}
}

func validKnowledgeCategory(category string) bool {
	switch category {
	case training.CategoryFAQ, training.CategoryPolicy, training.CategoryServices, training.CategoryHours, training.CategoryHandoff, training.CategoryOperations:
		return true
	default:
		return false
	}
}

func validServiceCategoryStatus(status string) bool {
	switch status {
	case pos.ServiceCategoryStatusActive, pos.ServiceCategoryStatusArchived:
		return true
	default:
		return false
	}
}

func validServiceCategorySource(source string) bool {
	switch source {
	case pos.ServiceCategorySourceManual, pos.ServiceCategorySourceSystem, pos.ServiceCategorySourceImported:
		return true
	default:
		return false
	}
}

func validServiceCategoryAliasSource(source string) bool {
	switch source {
	case pos.ServiceCategoryAliasSourceOwner, pos.ServiceCategoryAliasSourceSystem, pos.ServiceCategoryAliasSourceImported:
		return true
	default:
		return false
	}
}

func validServiceAliasStatus(status string) bool {
	switch status {
	case training.AliasStatusActive, training.AliasStatusArchived:
		return true
	default:
		return false
	}
}

func validServiceAliasSource(source string) bool {
	switch source {
	case training.AliasSourceOwner, training.AliasSourceCorrection, training.AliasSourceImport:
		return true
	default:
		return false
	}
}

func validKnowledgeStatus(status string) bool {
	switch status {
	case training.StatusDraft, training.StatusActive, training.StatusArchived:
		return true
	default:
		return false
	}
}

func normalizeEnvironment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "production" {
		return "production"
	}
	return "sandbox"
}

func normalizeVoiceTransport(value string) string {
	if strings.TrimSpace(value) == "realtime_stream" {
		return "realtime_stream"
	}
	return "recording"
}

func normalizePublicSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	previousHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			previousHyphen = false
		case unicode.IsDigit(r):
			builder.WriteRune(r)
			previousHyphen = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if builder.Len() > 0 && !previousHyphen {
				builder.WriteByte('-')
				previousHyphen = true
			}
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if len(normalized) < 3 || len(normalized) > 64 {
		return ""
	}
	return normalized
}

func normalizeConfigSlug(value string) string {
	return strings.Trim(normalizeConfigText(value, "-"), "-")
}

func normalizeConfigAlias(value string) string {
	return strings.TrimSpace(normalizeConfigText(value, " "))
}

func serviceAliasTargetKey(target ServiceAliasTargetExport) string {
	name := normalizeConfigAlias(target.Name)
	if name == "" {
		return ""
	}
	return name + "|" + strconv.Itoa(target.DurationMinutes)
}

func activeBundleServiceAliasKeys(items []ServiceAliasExport) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		if item.Status != "" && item.Status != training.AliasStatusActive {
			continue
		}
		key := normalizeConfigAlias(defaultString(item.NormalizedAlias, item.Alias))
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func activeBundleCategoryAliasKeys(items []ServiceCategoryExport) map[string]bool {
	out := map[string]bool{}
	for _, category := range items {
		for _, item := range category.Aliases {
			if item.Status != "" && item.Status != pos.ServiceCategoryStatusActive {
				continue
			}
			key := normalizeConfigAlias(defaultString(item.NormalizedAlias, item.Alias))
			if key != "" {
				out[key] = true
			}
		}
	}
	return out
}

func normalizeConfigText(value string, separator string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	previousSeparator := true
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			previousSeparator = false
		default:
			if !previousSeparator {
				builder.WriteString(separator)
				previousSeparator = true
			}
		}
	}
	return strings.Trim(builder.String(), separator)
}

func publicPath(slug string) string {
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	return "/s/" + strings.TrimSpace(slug)
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func intString(value int) string {
	return strings.TrimSpace(jsonNumber(value))
}

func jsonNumber(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
