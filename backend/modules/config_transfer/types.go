package configtransfer

import (
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

const (
	PlatformSchemaVersion  = "manleai.salon_configuration.v10"
	LegacyPlatformSchemaV9 = "manleai.salon_configuration.v9"
	SchemaVersion          = "manleai.salon_configuration.v8"
	LegacySchemaV7         = "manleai.salon_configuration.v7"
	LegacySchemaV6         = "manleai.salon_configuration.v6"
	LegacySchemaV5         = "manleai.salon_configuration.v5"
	LegacySchemaV4         = "manleai.salon_configuration.v4"
	LegacySchemaV3         = "manleai.salon_configuration.v3"
	LegacySchemaV2         = "manleai.salon_configuration.v2"
	LegacySchemaV1         = "manleai.salon_configuration.v1"
	StatusPreviewed        = "previewed"
	StatusApplied          = "applied"
	StatusFailed           = "failed"
	SectionSalon           = "salon_profile"
	SectionAI              = "ai_receptionist"
	SectionPublic          = "public_booking_page"
	SectionIntegrations    = "integrations"
	SectionKnowledge       = "knowledge_base"
	SectionCategories      = "service_categories"
	SectionServiceAliases  = "service_aliases"
	SectionConsultation    = "service_consultation_profiles"
	SectionLocalHours      = "local_business_hours"
)

var excludedData = []string{
	"services",
	"staff",
	"customers",
	"appointments",
	"appointment_services",
	"appointment_resources",
	"booking_attempts",
	"booking_attempt_segments",
	"booking_attempt_segment_resource_allocations",
	"availability_quotes",
	"availability_quote_slots",
	"availability_quote_slot_segments",
	"availability_quote_slot_resource_allocations",
	"fallback_requests",
	"scheduling_authority",
	"scheduling_authority_version",
	"scheduling_authority_switch_runs",
	"scheduling_authority_switch_events",
	"scheduling_requests",
	"scheduling_request_segments",
	"scheduling_request_events",
	"manleai_calendar_configs",
	"manleai_calendar_staff_weekly_periods",
	"manleai_calendar_service_policies",
	"manleai_calendar_service_staff",
	"manleai_calendar_resource_pools",
	"manleai_calendar_service_resources",
	"manleai_calendar_exceptions",
	"manleai_calendar_config_events",
	"manleai_calendar_appointment_resource_allocations",
	"manleai_calendar_execution_events",
	"owner_notifications",
	"call_sessions",
	"transcripts",
	"recordings",
	"summaries",
	"owner_corrections",
	"pos_connections",
	"pos_entity_links",
	"pos_sync_jobs",
	"pos_sync_logs",
	"pos_errors",
	"provider_switch_runs",
	"provider_switch_matches",
	"salon_business_hour_periods",
	"party_booking_requests",
	"voice_webhook_events",
	"voice_audio_outputs",
	"twilio_voice_route_id",
	"twilio_voice_inbound_number",
	"twilio_voice_live_verification",
	"twilio_public_callback_base",
	"openai_destination_profile",
	"openai_credential_identity",
	"openai_runtime_verification",
	"pos_oauth_tokens",
	"api_keys",
	"client_secrets",
	"encrypted_secrets",
}

type ConfigurationBundle struct {
	SchemaVersion           string                                       `json:"schema_version"`
	ExportedAt              time.Time                                    `json:"exported_at"`
	SecretsExported         bool                                         `json:"secrets_exported"`
	OperationalDataExported bool                                         `json:"operational_data_exported"`
	IncludedSections        []string                                     `json:"included_sections,omitempty"`
	ExcludedData            []string                                     `json:"excluded_data"`
	RequiresSecretReentry   []string                                     `json:"requires_secret_reentry"`
	SalonProfile            SalonProfileExport                           `json:"salon_profile"`
	AIReceptionist          AIReceptionistExport                         `json:"ai_receptionist"`
	PublicBookingPage       PublicBookingPageExport                      `json:"public_booking_page"`
	Integrations            integrationconfig.IntegrationConfigsResponse `json:"integrations"`
	IntegrationProviders    []string                                     `json:"integration_providers,omitempty"`
	// POSConnection is accepted only for schema-v7-and-earlier compatibility.
	// Schema v8 exports omit provider connection state because it is operational,
	// destination-scoped evidence rather than portable configuration intent.
	POSConnection        *POSConnectionExport                   `json:"pos_connection,omitempty"`
	ServiceCategories    ServiceCategoryBundleExport            `json:"service_categories"`
	ServiceAliases       ServiceAliasBundleExport               `json:"service_aliases"`
	ConsultationProfiles ServiceConsultationProfileBundleExport `json:"service_consultation_profiles"`
	KnowledgeBase        KnowledgeBaseExport                    `json:"knowledge_base"`
	LocalBusinessHours   LocalBusinessHoursExport               `json:"local_business_hours,omitempty"`
}

type LocalBusinessHoursExport struct {
	ManagementMode string                          `json:"management_mode"`
	Periods        []LocalBusinessHourPeriodExport `json:"periods"`
}

type LocalBusinessHourPeriodExport struct {
	DayOfWeek      int    `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
	EndAtMidnight  bool   `json:"end_at_midnight"`
}

type SalonProfileExport struct {
	Name              string    `json:"name"`
	Phone             string    `json:"phone"`
	Address           string    `json:"address,omitempty"`
	City              string    `json:"city,omitempty"`
	State             string    `json:"state,omitempty"`
	ZipCode           string    `json:"zip_code,omitempty"`
	Timezone          string    `json:"timezone"`
	PrimaryLanguage   string    `json:"primary_language"`
	SecondaryLanguage string    `json:"secondary_language"`
	HandoffPhone      string    `json:"handoff_phone,omitempty"`
	AIEnabled         bool      `json:"ai_enabled"`
	ActivePOSProvider string    `json:"active_pos_provider"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AIReceptionistExport struct {
	AIGreeting              string    `json:"ai_greeting"`
	AIVoice                 string    `json:"ai_voice"`
	AITone                  string    `json:"ai_tone"`
	BookingMode             string    `json:"booking_mode"`
	RecordingEnabled        bool      `json:"recording_enabled"`
	RecordingConsentMessage string    `json:"recording_consent_message"`
	SMSConfirmationEnabled  bool      `json:"sms_confirmation_enabled"`
	SMSReminderEnabled      bool      `json:"sms_reminder_enabled"`
	ReminderHoursBefore     int       `json:"reminder_hours_before"`
	HandoffEnabled          bool      `json:"handoff_enabled"`
	ConsultationEnabled     bool      `json:"consultation_enabled"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type PublicBookingPageExport struct {
	PublicSlug           string    `json:"public_slug,omitempty"`
	PublicCatalogEnabled bool      `json:"public_catalog_enabled"`
	PublicPath           string    `json:"public_path,omitempty"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type POSConnectionExport struct {
	Provider   string     `json:"provider"`
	Status     string     `json:"status"`
	MerchantID string     `json:"merchant_id,omitempty"`
	LocationID string     `json:"location_id,omitempty"`
	Scopes     []string   `json:"scopes"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

type KnowledgeBaseExport struct {
	Items []KnowledgeItemExport `json:"items"`
	Count int                   `json:"count"`
}

type ServiceCategoryBundleExport struct {
	Items []ServiceCategoryExport `json:"items"`
	Count int                     `json:"count"`
}

type ServiceCategoryExport struct {
	SourceKey   string                       `json:"source_key"`
	Name        string                       `json:"name"`
	Slug        string                       `json:"slug"`
	Description string                       `json:"description,omitempty"`
	Status      string                       `json:"status"`
	Source      string                       `json:"source"`
	SortOrder   int                          `json:"sort_order"`
	Aliases     []ServiceCategoryAliasExport `json:"aliases"`
	CreatedAt   time.Time                    `json:"created_at"`
	UpdatedAt   time.Time                    `json:"updated_at"`
}

type ServiceCategoryAliasExport struct {
	SourceKey       string    `json:"source_key"`
	Alias           string    `json:"alias"`
	NormalizedAlias string    `json:"normalized_alias"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	Confidence      float64   `json:"confidence"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ServiceAliasBundleExport struct {
	Items []ServiceAliasExport `json:"items"`
	Count int                  `json:"count"`
}

type ServiceAliasExport struct {
	SourceKey       string                   `json:"source_key"`
	Alias           string                   `json:"alias"`
	NormalizedAlias string                   `json:"normalized_alias"`
	TargetService   ServiceAliasTargetExport `json:"target_service"`
	Source          string                   `json:"source"`
	Status          string                   `json:"status"`
	Confidence      float64                  `json:"confidence"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

type ServiceAliasTargetExport struct {
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes,omitempty"`
	PriceDisplay    string `json:"price_display,omitempty"`
}

type ServiceConsultationProfileBundleExport struct {
	Items []ServiceConsultationProfileExport `json:"items"`
	Count int                                `json:"count"`
}

type ServiceConsultationProfileExport struct {
	SourceKey                string                   `json:"source_key"`
	TargetService            ServiceAliasTargetExport `json:"target_service"`
	Status                   string                   `json:"status"`
	RecommendedOutcomes      []string                 `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string                 `json:"compatible_current_systems"`
	LengthCapabilities       []string                 `json:"length_capabilities"`
	PriorityTags             []string                 `json:"priority_tags"`
	FinishOptions            []string                 `json:"finish_options"`
	MaintenanceNote          string                   `json:"maintenance_note,omitempty"`
	OwnerApprovedSummary     string                   `json:"owner_approved_summary,omitempty"`
	CreatedAt                *time.Time               `json:"created_at,omitempty"`
	UpdatedAt                *time.Time               `json:"updated_at,omitempty"`
}

type KnowledgeItemExport struct {
	SourceKey string    `json:"source_key"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ImportRequest struct {
	RequestID     string              `json:"request_id,omitempty"`
	Configuration ConfigurationBundle `json:"configuration"`
}

type ImportResponse struct {
	ImportRunID             string                 `json:"import_run_id,omitempty"`
	SalonID                 string                 `json:"salon_id,omitempty"`
	RequestID               string                 `json:"request_id"`
	DryRun                  bool                   `json:"dry_run"`
	Status                  string                 `json:"status"`
	SchemaVersion           string                 `json:"schema_version"`
	IncludedSections        []string               `json:"included_sections"`
	TargetAuthority         string                 `json:"target_scheduling_authority"`
	TargetAuthorityVersion  int64                  `json:"target_scheduling_authority_version"`
	SourceActivePOSProvider string                 `json:"source_active_pos_provider"`
	TargetActivePOSProvider string                 `json:"target_active_pos_provider"`
	ResultActivePOSProvider string                 `json:"result_active_pos_provider"`
	SourceBookingMode       string                 `json:"source_booking_mode"`
	TargetBookingMode       string                 `json:"target_booking_mode"`
	ResultBookingMode       string                 `json:"result_booking_mode"`
	CanApply                bool                   `json:"can_apply"`
	Summary                 []ImportSectionSummary `json:"summary"`
	Warnings                []ImportIssue          `json:"warnings"`
	Conflicts               []ImportIssue          `json:"conflicts"`
	ExcludedData            []string               `json:"excluded_data"`
	RequiresSecretReentry   []string               `json:"requires_secret_reentry"`
}

type ImportSectionSummary struct {
	Section   string `json:"section"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Unchanged int    `json:"unchanged"`
	Skipped   int    `json:"skipped"`
	Conflicts int    `json:"conflicts"`
}

type ImportIssue struct {
	Section   string `json:"section"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	SourceKey string `json:"source_key,omitempty"`
}

type PlatformTransferRequest struct {
	SourceType       string               `json:"source_type"`
	SourceSalonID    string               `json:"source_tenant_id,omitempty"`
	IncludedSections []string             `json:"included_sections"`
	Configuration    *ConfigurationBundle `json:"configuration,omitempty"`
}

type PlatformTransferApplyRequest struct {
	PlatformTransferRequest
	PreviewID string `json:"preview_id"`
	ActionKey string `json:"action_key"`
}

type PlatformTransferResponse struct {
	RunID                   string                 `json:"run_id"`
	TargetSalonID           string                 `json:"target_tenant_id"`
	SourceType              string                 `json:"source_type"`
	SourceSalonID           string                 `json:"source_tenant_id,omitempty"`
	SchemaVersion           string                 `json:"schema_version"`
	IncludedSections        []string               `json:"included_sections"`
	Status                  string                 `json:"status"`
	CanApply                bool                   `json:"can_apply"`
	Replayed                bool                   `json:"replayed,omitempty"`
	TargetAuthority         string                 `json:"target_scheduling_authority"`
	TargetAuthorityVersion  int64                  `json:"target_scheduling_authority_version"`
	SourceActivePOSProvider string                 `json:"source_active_pos_provider,omitempty"`
	TargetActivePOSProvider string                 `json:"target_active_pos_provider,omitempty"`
	Summary                 []ImportSectionSummary `json:"summary"`
	Warnings                []ImportIssue          `json:"warnings"`
	Conflicts               []ImportIssue          `json:"conflicts"`
	ExcludedData            []string               `json:"excluded_data"`
	RequiresSecretReentry   []string               `json:"requires_secret_reentry"`
	CreatedAt               time.Time              `json:"created_at"`
	AppliedAt               *time.Time             `json:"applied_at,omitempty"`
}

type PlatformTransferRunsResponse struct {
	Runs []PlatformTransferResponse `json:"runs"`
}

type importPlan struct {
	Bundle                  ConfigurationBundle
	PayloadFingerprint      string
	SchemaVersion           string
	SalonID                 string
	RequestID               string
	Summary                 map[string]*ImportSectionSummary
	Warnings                []ImportIssue
	Conflicts               []ImportIssue
	RequiresSecretReentry   []string
	CanApply                bool
	Target                  *importTargetState
	Knowledge               []plannedKnowledgeItem
	PublicCatalogEnabled    bool
	AIEnabled               bool
	BookingMode             string
	ConsultationEnabled     bool
	ServiceCategories       []plannedServiceCategory
	ServiceAliases          []plannedServiceAlias
	ConsultationProfiles    []plannedServiceConsultationProfile
	ConsultationReady       bool
	Onboarding              bool
	IncludedSections        map[string]bool
	TargetAuthority         string
	TargetAuthorityVersion  int64
	SourceActivePOSProvider string
}

type plannedKnowledgeItem struct {
	Item      KnowledgeItemExport
	Operation string
}

type plannedServiceCategory struct {
	Item      ServiceCategoryExport
	Operation string
	Aliases   []plannedServiceCategoryAlias
}

type plannedServiceCategoryAlias struct {
	CategorySlug string
	Item         ServiceCategoryAliasExport
	Operation    string
}

type plannedServiceAlias struct {
	Item            ServiceAliasExport
	Operation       string
	TargetServiceID string
}

type plannedServiceConsultationProfile struct {
	Item            ServiceConsultationProfileExport
	Operation       string
	TargetServiceID string
}

type importServiceTarget struct {
	ServiceID            string
	Name                 string
	DurationMinutes      int
	PriceDisplay         string
	ConsultationEligible bool
}

type importTargetState struct {
	SalonProfile                 SalonProfileExport
	AIReceptionist               AIReceptionistExport
	PublicBookingPage            PublicBookingPageExport
	PublicCanPublish             bool
	SchedulingAuthority          string
	SchedulingAuthorityVersion   int64
	Integrations                 integrationconfig.IntegrationConfigsResponse
	ServiceCategoryBySlug        map[string]ServiceCategoryExport
	CategoryAliasByKey           map[string]ServiceCategoryAliasExport
	ActiveServiceAliasKeys       map[string]bool
	ActiveCategoryAliasKeys      map[string]bool
	ServiceAliasByKey            map[string]ServiceAliasExport
	ConsultationProfileByTarget  map[string]ServiceConsultationProfileExport
	ServiceTargetsByKey          map[string]importServiceTarget
	AmbiguousServiceTargets      map[string]bool
	ConsultationTargetsByKey     map[string]importServiceTarget
	AmbiguousConsultationTargets map[string]bool
	KnowledgeByImportKey         map[string]KnowledgeItemExport
	KnowledgeByContentHash       map[string]KnowledgeItemExport
}
