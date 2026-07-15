package pos

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	ProviderSquare = "square"

	StatusNotConnected = "not_connected"
	StatusConnected    = "connected"
	StatusSyncing      = "syncing"
	StatusActive       = "active"
	StatusError        = "error"
	StatusExpiredToken = "expired_token"
	StatusDisabled     = "disabled"

	ErrorTokenExpired         = "POS_TOKEN_EXPIRED"
	ErrorPermissionDenied     = "POS_PERMISSION_DENIED"
	ErrorLocationNotSelected  = "POS_LOCATION_NOT_SELECTED"
	ErrorAvailabilityFailed   = "POS_AVAILABILITY_FAILED"
	ErrorBookingFailed        = "POS_BOOKING_FAILED"
	ErrorBookingConflict      = "POS_BOOKING_CONFLICT"
	ErrorRateLimited          = "POS_RATE_LIMITED"
	ErrorTimeout              = "POS_TIMEOUT"
	ErrorUnknown              = "POS_UNKNOWN_ERROR"
	ErrorCustomerCreateFailed = "POS_CUSTOMER_CREATE_FAILED"

	SyncStatusLocalOnly  = "local_only"
	SyncStatusSyncing    = "syncing"
	SyncStatusSynced     = "synced"
	SyncStatusSyncFailed = "sync_failed"
	SyncStatusUnmapped   = "unmapped"
	SyncStatusArchived   = "archived"

	SyncJobStatusQueued    = "queued"
	SyncJobStatusRunning   = "running"
	SyncJobStatusSucceeded = "succeeded"
	SyncJobStatusFailed    = "failed"

	EntityTypeService  = "service"
	EntityTypeStaff    = "staff"
	EntityTypeCustomer = "customer"

	EntitySourceLocal    = "local"
	EntitySourceImported = "imported"

	ServiceCategoryStatusActive   = "active"
	ServiceCategoryStatusArchived = "archived"

	ServiceCategorySourceManual   = "manual"
	ServiceCategorySourceSystem   = "system"
	ServiceCategorySourceImported = "imported"

	ServiceCategoryAliasSourceOwner    = "owner"
	ServiceCategoryAliasSourceSystem   = "system"
	ServiceCategoryAliasSourceImported = "imported"

	ServiceCategoryAssignmentUnassigned = "unassigned"
	ServiceCategoryAssignmentSuggested  = "suggested"
	ServiceCategoryAssignmentManual     = "manual"

	ConsultationProfileStatusDraft    = "draft"
	ConsultationProfileStatusReady    = "ready"
	ConsultationProfileStatusDisabled = "disabled"

	ConsultationOutcomeMaintain     = "maintain"
	ConsultationOutcomeShorten      = "shorten"
	ConsultationOutcomeAddLength    = "add_length"
	ConsultationOutcomeAddStrength  = "add_strength"
	ConsultationOutcomeRepair       = "repair"
	ConsultationOutcomeRemoval      = "removal"
	ConsultationOutcomeColorRefresh = "color_refresh"

	ConsultationSystemNatural       = "natural"
	ConsultationSystemRegularPolish = "regular_polish"
	ConsultationSystemGel           = "gel"
	ConsultationSystemDip           = "dip"
	ConsultationSystemAcrylic       = "acrylic"
	ConsultationSystemExtension     = "extension"

	ConsultationLengthKeep      = "keep"
	ConsultationLengthShorten   = "shorten"
	ConsultationLengthAddLength = "add_length"

	ConsultationPriorityDurability       = "durability"
	ConsultationPriorityLowerMaintenance = "lower_maintenance"
	ConsultationPriorityLowerCost        = "lower_cost"
	ConsultationPriorityShorterVisit     = "shorter_visit"

	ConsultationFinishNatural       = "natural"
	ConsultationFinishRegularPolish = "regular_polish"
	ConsultationFinishGelPolish     = "gel_polish"
	ConsultationFinishGlossy        = "glossy"
	ConsultationFinishMatte         = "matte"
	ConsultationFinishNailArt       = "nail_art"

	BusinessHourSourceImported      = "imported"
	BusinessHourSourceLocalMigrated = "local_migrated"
	BusinessHourSourceLocalOverride = "local_override"

	SyncOperationUpsertService  = "upsert_service"
	SyncOperationArchiveService = "archive_service"
	SyncOperationUpsertStaff    = "upsert_staff"
	SyncOperationArchiveStaff   = "archive_staff"
	SyncOperationUpsertCustomer = "upsert_customer"

	SwitchRunStatusDraft       = "draft"
	SwitchRunStatusBlocked     = "blocked"
	SwitchRunStatusImporting   = "importing"
	SwitchRunStatusMatching    = "matching"
	SwitchRunStatusNeedsReview = "needs_review"
	SwitchRunStatusReady       = "ready"
	SwitchRunStatusActivated   = "activated"
	SwitchRunStatusCancelled   = "cancelled"
	SwitchRunStatusFailed      = "failed"

	SwitchMatchStatusSuggested = "suggested"
	SwitchMatchStatusUnmatched = "unmatched"
	SwitchMatchStatusConflict  = "conflict"
	SwitchMatchStatusConfirmed = "confirmed"
	SwitchMatchStatusSkipped   = "skipped"
)

type WriteOutcome string

// AppointmentStatus is the provider-neutral status returned by POS adapters.
// Booking callers must compare the normalized value instead of treating a
// provider booking ID as proof that the provider accepted the appointment.
type AppointmentStatus string

const (
	AppointmentStatusAccepted  AppointmentStatus = "accepted"
	AppointmentStatusPending   AppointmentStatus = "pending"
	AppointmentStatusCancelled AppointmentStatus = "cancelled"
	AppointmentStatusDeclined  AppointmentStatus = "declined"
	AppointmentStatusNoShow    AppointmentStatus = "no_show"
	AppointmentStatusUnknown   AppointmentStatus = "unknown"
)

func NormalizeAppointmentStatus(value string) AppointmentStatus {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	switch normalized {
	case "accepted", "confirmed":
		return AppointmentStatusAccepted
	case "pending":
		return AppointmentStatusPending
	case "cancelled", "canceled", "cancelled_by_customer", "canceled_by_customer", "cancelled_by_seller", "canceled_by_seller":
		return AppointmentStatusCancelled
	case "declined", "rejected":
		return AppointmentStatusDeclined
	case "no_show", "noshow":
		return AppointmentStatusNoShow
	default:
		return AppointmentStatusUnknown
	}
}

const (
	WriteOutcomeDefinitiveFailure WriteOutcome = "definitive_failure"
	WriteOutcomeUnknown           WriteOutcome = "unknown"
)

const (
	WritePhasePrepare       = "prepare"
	WritePhaseDispatch      = "dispatch"
	WritePhaseResponse      = "response"
	WritePhasePostWriteRead = "post_write_read"
)

// WriteError preserves whether a provider mutation is known to have failed or
// may have completed despite the returned error. Callers must treat untyped
// provider-write errors as unknown rather than assuming they are retry-safe.
type WriteError struct {
	Outcome WriteOutcome
	Phase   string
	Err     error
}

func (e *WriteError) Error() string {
	if e == nil || e.Err == nil {
		return "provider write failed"
	}
	return e.Err.Error()
}

func (e *WriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewWriteError(outcome WriteOutcome, phase string, err error) error {
	if err == nil {
		return nil
	}
	if outcome != WriteOutcomeDefinitiveFailure {
		outcome = WriteOutcomeUnknown
	}
	return &WriteError{Outcome: outcome, Phase: phase, Err: err}
}

func WriteOutcomeForError(err error) WriteOutcome {
	if err == nil {
		return WriteOutcomeDefinitiveFailure
	}
	var writeErr *WriteError
	if errors.As(err, &writeErr) && writeErr.Outcome == WriteOutcomeDefinitiveFailure {
		return WriteOutcomeDefinitiveFailure
	}
	return WriteOutcomeUnknown
}

type POSProvider interface {
	Name() string

	Connect(ctx context.Context, input ConnectInput) (*Connection, error)
	HealthCheck(ctx context.Context, salonID string) error

	ListLocations(ctx context.Context, salonID string) ([]Location, error)
	ListServices(ctx context.Context, salonID string) ([]Service, error)
	ListStaff(ctx context.Context, salonID string) ([]StaffMember, error)

	SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*Customer, error)
	CreateCustomer(ctx context.Context, salonID string, input CreateCustomerInput) (*Customer, error)

	CheckAvailability(ctx context.Context, salonID string, input AvailabilityInput) ([]TimeSlot, error)

	CreateAppointment(ctx context.Context, salonID string, input CreateAppointmentInput) (*Appointment, error)
	RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input RescheduleInput) (*Appointment, error)
	CancelAppointment(ctx context.Context, salonID string, appointmentID string, input CancelInput) (*Appointment, error)

	Sync(ctx context.Context, salonID string) error
}

type NamedProvider interface {
	Name() string
}

type CapabilityProvider interface {
	Capabilities() ProviderCapabilities
}

type POSWriteProvider interface {
	UpsertService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error)
	ArchiveService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error)
	UpsertStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error)
	ArchiveStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error)
	UpsertCustomer(ctx context.Context, salonID string, customer Customer) (*ProviderSyncResult, error)
}

type AppointmentListProvider interface {
	ListAppointments(ctx context.Context, salonID string, input AppointmentListInput) (*AppointmentListResult, error)
}

type ProviderCapabilities struct {
	ServiceUpsert  bool `json:"service_upsert"`
	ServiceArchive bool `json:"service_archive"`
	StaffUpsert    bool `json:"staff_upsert"`
	StaffArchive   bool `json:"staff_archive"`
	CustomerUpsert bool `json:"customer_upsert"`
}

type ProviderOption struct {
	Provider      string               `json:"provider"`
	Label         string               `json:"label"`
	Installed     bool                 `json:"installed"`
	Active        bool                 `json:"active"`
	Status        string               `json:"status"`
	BlockedReason string               `json:"blocked_reason,omitempty"`
	Capabilities  ProviderCapabilities `json:"capabilities"`
}

type ProviderReadinessCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Message  string `json:"message,omitempty"`
}

type ProviderMappingSummary struct {
	ServiceCount         int `json:"service_count"`
	StaffCount           int `json:"staff_count"`
	CustomerCount        int `json:"customer_count"`
	BookableServiceCount int `json:"bookable_service_count"`
	BookableStaffCount   int `json:"bookable_staff_count"`
	LinkedServiceCount   int `json:"linked_service_count"`
	LinkedStaffCount     int `json:"linked_staff_count"`
	LinkedCustomerCount  int `json:"linked_customer_count"`
	UnmappedServiceCount int `json:"unmapped_service_count"`
	UnmappedStaffCount   int `json:"unmapped_staff_count"`
	SyncFailedCount      int `json:"sync_failed_count"`
}

type ProviderSwitchReadiness struct {
	SalonID              string                   `json:"salon_id"`
	ActiveProvider       string                   `json:"active_provider"`
	ActiveProviderLabel  string                   `json:"active_provider_label"`
	InstalledProviders   []ProviderOption         `json:"installed_providers"`
	UnavailableProviders []ProviderOption         `json:"unavailable_providers"`
	Mapping              ProviderMappingSummary   `json:"mapping"`
	Checks               []ProviderReadinessCheck `json:"checks"`
	DryRunBookingReady   bool                     `json:"dry_run_booking_ready"`
	CanStartSwitch       bool                     `json:"can_start_switch"`
	CanActivateProvider  bool                     `json:"can_activate_provider"`
	BlockedReason        string                   `json:"blocked_reason,omitempty"`
}

type ProviderSwitchRunRequest struct {
	ToProvider string `json:"to_provider"`
}

type ProviderSwitchRunMutation struct {
	SalonID         string
	OwnerUserID     string
	FromProvider    string
	ToProvider      string
	Status          string
	BlockedReason   string
	DryRunReady     bool
	CreatedByUserID string
}

type ProviderSwitchRun struct {
	ID              string                     `json:"id"`
	SalonID         string                     `json:"salon_id"`
	FromProvider    string                     `json:"from_provider"`
	ToProvider      string                     `json:"to_provider"`
	Status          string                     `json:"status"`
	BlockedReason   string                     `json:"blocked_reason,omitempty"`
	DryRunReady     bool                       `json:"dry_run_ready"`
	CanActivate     bool                       `json:"can_activate"`
	ActivatedAt     *time.Time                 `json:"activated_at,omitempty"`
	CancelledAt     *time.Time                 `json:"cancelled_at,omitempty"`
	CreatedByUserID string                     `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
	MatchSummary    ProviderSwitchMatchSummary `json:"match_summary"`
	Matches         []ProviderSwitchMatch      `json:"matches,omitempty"`
}

type ProviderSwitchDryRunReadiness struct {
	RunID         string                   `json:"run_id"`
	SalonID       string                   `json:"salon_id"`
	FromProvider  string                   `json:"from_provider"`
	ToProvider    string                   `json:"to_provider"`
	Status        string                   `json:"status"`
	Checks        []ProviderReadinessCheck `json:"checks"`
	CanRunDryRun  bool                     `json:"can_run_dry_run"`
	DryRunReady   bool                     `json:"dry_run_ready"`
	CanActivate   bool                     `json:"can_activate"`
	BlockedReason string                   `json:"blocked_reason,omitempty"`
}

type ProviderSwitchMatchSummary struct {
	Total     int `json:"total"`
	Suggested int `json:"suggested"`
	Unmatched int `json:"unmatched"`
	Conflicts int `json:"conflicts"`
	Confirmed int `json:"confirmed"`
	Skipped   int `json:"skipped"`
}

type ProviderSwitchMatch struct {
	ID                      string    `json:"id"`
	RunID                   string    `json:"run_id"`
	SalonID                 string    `json:"salon_id"`
	EntityType              string    `json:"entity_type"`
	CanonicalEntityID       string    `json:"canonical_entity_id,omitempty"`
	CanonicalName           string    `json:"canonical_name,omitempty"`
	ProviderEntityID        string    `json:"provider_entity_id"`
	ProviderName            string    `json:"provider_name"`
	ProviderPhone           string    `json:"provider_phone,omitempty"`
	ProviderEmail           string    `json:"provider_email,omitempty"`
	ProviderDurationMinutes int       `json:"provider_duration_minutes,omitempty"`
	MatchStatus             string    `json:"match_status"`
	MatchConfidence         int       `json:"match_confidence"`
	MatchReason             string    `json:"match_reason,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type ProviderSwitchMatchUpdateRequest struct {
	MatchStatus string `json:"match_status"`
}

type ProviderSwitchMatchUpdateMutation struct {
	SalonID           string
	OwnerUserID       string
	RunID             string
	MatchID           string
	MatchStatus       string
	CanonicalEntityID string
	CanonicalName     string
	MatchConfidence   int
	MatchReason       string
}

type ProviderSwitchMatchMutation struct {
	EntityType              string
	CanonicalEntityID       string
	CanonicalName           string
	ProviderEntityID        string
	ProviderName            string
	ProviderPhone           string
	ProviderEmail           string
	ProviderDurationMinutes int
	MatchStatus             string
	MatchConfidence         int
	MatchReason             string
}

type ProviderSwitchEntityCandidate struct {
	ID               string
	ProviderEntityID string
	Name             string
	Phone            string
	Email            string
	DurationMinutes  int
}

type ProviderSyncResult struct {
	ProviderEntityID string
	ProviderVersion  int64
}

type ConnectInput struct {
	SalonID     string
	Code        string
	RedirectURL string
	State       string
}

type Connection struct {
	ID                    string     `json:"id"`
	SalonID               string     `json:"salon_id"`
	Provider              string     `json:"provider"`
	Status                string     `json:"status"`
	SnapshotGeneration    int64      `json:"-"`
	AccessTokenEncrypted  string     `json:"-"`
	RefreshTokenEncrypted string     `json:"-"`
	MerchantID            string     `json:"merchant_id,omitempty"`
	LocationID            string     `json:"location_id,omitempty"`
	Scopes                []string   `json:"scopes"`
	LastSyncAt            *time.Time `json:"last_sync_at,omitempty"`
	ErrorMessage          string     `json:"error_message,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type Location struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone,omitempty"`
	Address  string `json:"address,omitempty"`
	Status   string `json:"status,omitempty"`
}

type BusinessHourPeriod struct {
	ID                  string     `json:"id,omitempty"`
	SalonID             string     `json:"salon_id,omitempty"`
	DayOfWeek           int        `json:"day_of_week"`
	StartLocalTime      string     `json:"start_local_time"`
	EndLocalTime        string     `json:"end_local_time"`
	Source              string     `json:"source"`
	Provider            string     `json:"provider,omitempty"`
	ProviderLocationID  string     `json:"provider_location_id,omitempty"`
	ProviderPeriodIndex int        `json:"provider_period_index"`
	LastSyncedAt        *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at,omitempty"`
}

type SyncSummary struct {
	ServicesSynced            int   `json:"services_synced"`
	StaffSynced               int   `json:"staff_synced"`
	BusinessHourPeriodsSynced int   `json:"business_hour_periods_synced"`
	CustomersSynced           int   `json:"customers_synced"`
	CustomersSkipped          int   `json:"customers_skipped"`
	SnapshotGeneration        int64 `json:"-"`
}

type ProviderSnapshot struct {
	Provider            string
	LocationID          string
	Generation          int64
	Services            []Service
	Staff               []StaffMember
	BusinessHourPeriods []BusinessHourPeriod
	Customers           []Customer
}

type Service struct {
	ID                  string                      `json:"id,omitempty"`
	SalonID             string                      `json:"salon_id,omitempty"`
	POSProvider         string                      `json:"pos_provider"`
	POSServiceID        string                      `json:"pos_service_id"`
	POSServiceVersion   int64                       `json:"pos_service_version,omitempty"`
	Name                string                      `json:"name"`
	Description         string                      `json:"description,omitempty"`
	AIDescription       string                      `json:"ai_description,omitempty"`
	DurationMinutes     int                         `json:"duration_minutes"`
	PriceFrom           float64                     `json:"price_from,omitempty"`
	PriceDisplay        string                      `json:"price_display,omitempty"`
	AIBookable          bool                        `json:"ai_bookable"`
	Active              bool                        `json:"active"`
	SyncStatus          string                      `json:"sync_status"`
	ArchivedAt          *time.Time                  `json:"archived_at,omitempty"`
	LastSyncedAt        *time.Time                  `json:"last_synced_at,omitempty"`
	SyncError           string                      `json:"sync_error,omitempty"`
	Source              string                      `json:"source"`
	POSLinked           bool                        `json:"pos_linked"`
	ServiceCategoryID   string                      `json:"service_category_id,omitempty"`
	CategoryName        string                      `json:"category_name,omitempty"`
	CategorySlug        string                      `json:"category_slug,omitempty"`
	CategorySource      string                      `json:"category_source"`
	CategoryConfidence  float64                     `json:"category_confidence,omitempty"`
	CategoryReviewedAt  *time.Time                  `json:"category_reviewed_at,omitempty"`
	ConsultationProfile *ServiceConsultationProfile `json:"consultation_profile,omitempty"`
}

type ServiceConsultationProfile struct {
	ID                       string     `json:"id,omitempty"`
	SalonID                  string     `json:"salon_id,omitempty"`
	ServiceID                string     `json:"service_id,omitempty"`
	Status                   string     `json:"status"`
	RecommendedOutcomes      []string   `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string   `json:"compatible_current_systems"`
	LengthCapabilities       []string   `json:"length_capabilities"`
	PriorityTags             []string   `json:"priority_tags"`
	FinishOptions            []string   `json:"finish_options"`
	MaintenanceNote          string     `json:"maintenance_note,omitempty"`
	OwnerApprovedSummary     string     `json:"owner_approved_summary,omitempty"`
	Revision                 int        `json:"revision"`
	UpdatedBy                string     `json:"updated_by,omitempty"`
	CreatedAt                *time.Time `json:"created_at,omitempty"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

type ServiceConsultationProfileWriteRequest struct {
	Status                   string   `json:"status"`
	RecommendedOutcomes      []string `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string `json:"compatible_current_systems"`
	LengthCapabilities       []string `json:"length_capabilities"`
	PriorityTags             []string `json:"priority_tags"`
	FinishOptions            []string `json:"finish_options"`
	MaintenanceNote          string   `json:"maintenance_note"`
	OwnerApprovedSummary     string   `json:"owner_approved_summary"`
}

type ServiceConsultationProfileMutation struct {
	Status                   string
	RecommendedOutcomes      []string
	CompatibleCurrentSystems []string
	LengthCapabilities       []string
	PriorityTags             []string
	FinishOptions            []string
	MaintenanceNote          string
	OwnerApprovedSummary     string
}

type ServiceWriteRequest struct {
	Name                string                                  `json:"name"`
	Description         string                                  `json:"description"`
	AIDescription       string                                  `json:"ai_description"`
	DurationMinutes     int                                     `json:"duration_minutes"`
	PriceFrom           *float64                                `json:"price_from"`
	Active              *bool                                   `json:"active"`
	ServiceCategoryID   string                                  `json:"service_category_id"`
	ConsultationProfile *ServiceConsultationProfileWriteRequest `json:"consultation_profile,omitempty"`
}

type ServiceMutation struct {
	Name                string
	Description         string
	AIDescription       string
	DurationMinutes     int
	PriceFrom           *float64
	Active              bool
	ServiceCategoryID   string
	ConsultationProfile *ServiceConsultationProfileMutation
}

type ServiceCategory struct {
	ID           string                 `json:"id"`
	SalonID      string                 `json:"salon_id"`
	Name         string                 `json:"name"`
	Slug         string                 `json:"slug"`
	Description  string                 `json:"description,omitempty"`
	Status       string                 `json:"status"`
	SortOrder    int                    `json:"sort_order"`
	Source       string                 `json:"source"`
	ServiceCount int                    `json:"service_count"`
	Aliases      []ServiceCategoryAlias `json:"aliases,omitempty"`
	ReviewedAt   *time.Time             `json:"reviewed_at,omitempty"`
	ArchivedAt   *time.Time             `json:"archived_at,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type ServiceCategoryAlias struct {
	ID              string    `json:"id"`
	SalonID         string    `json:"salon_id"`
	CategoryID      string    `json:"category_id"`
	CategoryName    string    `json:"category_name,omitempty"`
	Alias           string    `json:"alias"`
	NormalizedAlias string    `json:"normalized_alias"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	Confidence      float64   `json:"confidence"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ServiceCategoryWriteRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

type ServiceCategoryMutation struct {
	Name        string
	Slug        string
	Description string
	SortOrder   int
}

type ServiceCategoryAliasWriteRequest struct {
	Alias      string   `json:"alias"`
	Confidence *float64 `json:"confidence"`
}

type ServiceCategoryAliasMutation struct {
	CategoryID      string
	Alias           string
	NormalizedAlias string
	Confidence      float64
}

type ServiceCategorySeed struct {
	Name        string
	Slug        string
	Description string
	SortOrder   int
	Aliases     []string
}

type ServiceCategorySuggestionRefresh struct {
	CreatedCategories           int `json:"created_categories"`
	RestoredSystemCategories    int `json:"restored_system_categories"`
	CreatedAliases              int `json:"created_aliases"`
	UpdatedSystemAliases        int `json:"updated_system_aliases"`
	SkippedAliasConflicts       int `json:"skipped_alias_conflicts"`
	SuggestedServices           int `json:"suggested_services"`
	SkippedReviewedServices     int `json:"skipped_reviewed_services"`
	SkippedAmbiguousServices    int `json:"skipped_ambiguous_services"`
	UnmatchedUnreviewedServices int `json:"unmatched_unreviewed_services"`
}

type ServiceCategoryAssignRequest struct {
	ServiceCategoryID string `json:"service_category_id"`
}

type StaffMember struct {
	ID           string     `json:"id,omitempty"`
	SalonID      string     `json:"salon_id,omitempty"`
	POSProvider  string     `json:"pos_provider"`
	POSStaffID   string     `json:"pos_staff_id"`
	Name         string     `json:"name"`
	Phone        string     `json:"phone,omitempty"`
	Email        string     `json:"email,omitempty"`
	AIBookable   bool       `json:"ai_bookable"`
	Active       bool       `json:"active"`
	SyncStatus   string     `json:"sync_status"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	SyncError    string     `json:"sync_error,omitempty"`
	Source       string     `json:"source"`
	POSLinked    bool       `json:"pos_linked"`
}

type StaffWriteRequest struct {
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Email  string `json:"email"`
	Active *bool  `json:"active"`
}

type StaffMutation struct {
	Name   string
	Phone  string
	Email  string
	Active bool
}

type Customer struct {
	ID            string `json:"id,omitempty"`
	POSCustomerID string `json:"pos_customer_id"`
	Name          string `json:"name"`
	Phone         string `json:"phone"`
	Email         string `json:"email,omitempty"`
}

type TimeSlot struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	StaffID   string    `json:"staff_id,omitempty"`
	StaffName string    `json:"staff_name,omitempty"`
	Segments  []TimeSlotSegment
}

type TimeSlotSegment struct {
	ServiceID       string
	StaffID         string
	DurationMinutes int
}

type Appointment struct {
	ID                    string    `json:"id,omitempty"`
	POSAppointmentID      string    `json:"pos_appointment_id"`
	POSAppointmentVersion int       `json:"pos_appointment_version,omitempty"`
	StartTime             time.Time `json:"start_time"`
	EndTime               time.Time `json:"end_time"`
	Status                string    `json:"status"`
}

type AppointmentListInput struct {
	StartTime     time.Time
	EndTime       time.Time
	Limit         int
	Cursor        string
	ProviderFence ProviderFence
}

type AppointmentListResult struct {
	Appointments []ListedAppointment
	Cursor       string
}

type ListedAppointment struct {
	POSAppointmentID      string
	POSAppointmentVersion int
	Status                string
	POSCustomerID         string
	CustomerName          string
	CustomerPhone         string
	CustomerEmail         string
	StartTime             time.Time
	EndTime               time.Time
	Notes                 string
	Segments              []ListedAppointmentSegment
}

type ListedAppointmentSegment struct {
	POSServiceID      string
	POSServiceVersion int64
	POSStaffID        string
	DurationMinutes   int
}

type CreateCustomerInput struct {
	Name  string
	Phone string
	Email string
}

// ProviderFence binds a provider operation to the exact catalog snapshot that
// supplied its service and staff identifiers.
type ProviderFence struct {
	LocationID         string
	SnapshotGeneration int64
}

type AvailabilityInput struct {
	ServiceID       string
	StaffID         string
	PreferredDate   string
	Timezone        string
	DurationMinutes int
	Segments        []AvailabilitySegmentInput
	ProviderFence   ProviderFence
}

type AvailabilitySegmentInput struct {
	ServiceID       string
	StaffID         string
	DurationMinutes int
}

type CreateAppointmentInput struct {
	IdempotencyKey  string
	CustomerID      string
	ServiceID       string
	ServiceVersion  int64
	StaffID         string
	StartTime       time.Time
	DurationMinutes int
	Notes           string
	Segments        []AppointmentSegmentInput
	ProviderFence   ProviderFence
}

type AppointmentSegmentInput struct {
	ServiceID       string
	ServiceVersion  int64
	StaffID         string
	DurationMinutes int
}

type RescheduleInput struct {
	IdempotencyKey  string
	BookingVersion  int
	ServiceID       string
	ServiceVersion  int64
	StaffID         string
	StartTime       time.Time
	DurationMinutes int
	Notes           string
	Segments        []AppointmentSegmentInput
	ProviderFence   ProviderFence
}

type CancelInput struct {
	IdempotencyKey string
	BookingVersion int
	Reason         string
	ProviderFence  ProviderFence
}

type POSError struct {
	SalonID      string
	Provider     string
	Operation    string
	ErrorCode    string
	ErrorMessage string
	Payload      []byte
}

type POSErrorRecord struct {
	ID           string    `json:"id"`
	SalonID      string    `json:"salon_id"`
	Provider     string    `json:"provider"`
	Operation    string    `json:"operation"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}

type SyncLog struct {
	ID          string     `json:"id"`
	SalonID     string     `json:"salon_id"`
	Provider    string     `json:"provider"`
	SyncType    string     `json:"sync_type"`
	Status      string     `json:"status"`
	Message     string     `json:"message,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type SyncJob struct {
	ID            string     `json:"id"`
	SalonID       string     `json:"salon_id"`
	Provider      string     `json:"provider"`
	EntityType    string     `json:"entity_type"`
	EntityID      string     `json:"entity_id"`
	Operation     string     `json:"operation"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LockedAt      *time.Time `json:"locked_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type SyncJobMutation struct {
	SalonID     string
	Provider    string
	EntityType  string
	EntityID    string
	Operation   string
	MaxAttempts int
}

type OAuthState struct {
	SalonID   string
	Provider  string
	StateHash string
	NonceHash string
	ExpiresAt time.Time
}
