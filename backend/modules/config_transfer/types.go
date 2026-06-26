package configtransfer

import (
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

const (
	SchemaVersion       = "manleai.salon_configuration.v2"
	LegacySchemaV1      = "manleai.salon_configuration.v1"
	StatusPreviewed     = "previewed"
	StatusApplied       = "applied"
	StatusFailed        = "failed"
	SectionSalon        = "salon_profile"
	SectionAI           = "ai_receptionist"
	SectionPublic       = "public_booking_page"
	SectionIntegrations = "integrations"
	SectionKnowledge    = "knowledge_base"
)

var excludedData = []string{
	"services",
	"staff",
	"customers",
	"appointments",
	"booking_attempts",
	"fallback_requests",
	"call_sessions",
	"transcripts",
	"recordings",
	"summaries",
	"owner_corrections",
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
	ExcludedData            []string                                     `json:"excluded_data"`
	RequiresSecretReentry   []string                                     `json:"requires_secret_reentry"`
	SalonProfile            SalonProfileExport                           `json:"salon_profile"`
	AIReceptionist          AIReceptionistExport                         `json:"ai_receptionist"`
	PublicBookingPage       PublicBookingPageExport                      `json:"public_booking_page"`
	Integrations            integrationconfig.IntegrationConfigsResponse `json:"integrations"`
	POSConnection           POSConnectionExport                          `json:"pos_connection"`
	KnowledgeBase           KnowledgeBaseExport                          `json:"knowledge_base"`
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
	BookingMode             string    `json:"booking_mode"`
	RecordingEnabled        bool      `json:"recording_enabled"`
	RecordingConsentMessage string    `json:"recording_consent_message"`
	SMSConfirmationEnabled  bool      `json:"sms_confirmation_enabled"`
	SMSReminderEnabled      bool      `json:"sms_reminder_enabled"`
	ReminderHoursBefore     int       `json:"reminder_hours_before"`
	HandoffEnabled          bool      `json:"handoff_enabled"`
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
	ImportRunID           string                 `json:"import_run_id,omitempty"`
	RequestID             string                 `json:"request_id"`
	DryRun                bool                   `json:"dry_run"`
	Status                string                 `json:"status"`
	SchemaVersion         string                 `json:"schema_version"`
	CanApply              bool                   `json:"can_apply"`
	Summary               []ImportSectionSummary `json:"summary"`
	Warnings              []ImportIssue          `json:"warnings"`
	Conflicts             []ImportIssue          `json:"conflicts"`
	ExcludedData          []string               `json:"excluded_data"`
	RequiresSecretReentry []string               `json:"requires_secret_reentry"`
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

type importPlan struct {
	Bundle                ConfigurationBundle
	PayloadFingerprint    string
	SchemaVersion         string
	RequestID             string
	Summary               map[string]*ImportSectionSummary
	Warnings              []ImportIssue
	Conflicts             []ImportIssue
	RequiresSecretReentry []string
	CanApply              bool
	Target                *importTargetState
	Knowledge             []plannedKnowledgeItem
	PublicCatalogEnabled  bool
	AIEnabled             bool
	BookingMode           string
}

type plannedKnowledgeItem struct {
	Item      KnowledgeItemExport
	Operation string
}

type importTargetState struct {
	SalonProfile           SalonProfileExport
	AIReceptionist         AIReceptionistExport
	PublicBookingPage      PublicBookingPageExport
	PublicCanPublish       bool
	CanEnableAIBooking     bool
	Integrations           integrationconfig.IntegrationConfigsResponse
	KnowledgeByImportKey   map[string]KnowledgeItemExport
	KnowledgeByContentHash map[string]KnowledgeItemExport
}
