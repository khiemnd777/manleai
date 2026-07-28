package business

import "time"

const (
	ManagementModeLocal            = "local"
	ManagementModeProviderReadOnly = "provider_read_only"
)

type MutationControl struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

type MutationResult struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Version      int64  `json:"version"`
	Replayed     bool   `json:"replayed"`
}

type SalonSummary struct {
	ID                         string `json:"id"`
	Name                       string `json:"name"`
	City                       string `json:"city,omitempty"`
	State                      string `json:"state,omitempty"`
	Timezone                   string `json:"timezone"`
	DataClassification         string `json:"data_classification"`
	PublicSlug                 string `json:"public_slug,omitempty"`
	PublicCatalogEnabled       bool   `json:"public_catalog_enabled"`
	AIEnabled                  bool   `json:"ai_enabled"`
	BusinessAccess             string `json:"business_access"`
	SchedulingAuthority        string `json:"scheduling_authority"`
	SchedulingAuthorityVersion int64  `json:"scheduling_authority_version"`
	ActivePOSProvider          string `json:"active_pos_provider"`
}

type SalonDirectoryResponse struct {
	Salons []SalonSummary `json:"salons"`
}

type SalonProfile struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Phone                string    `json:"phone"`
	Address              string    `json:"address,omitempty"`
	City                 string    `json:"city,omitempty"`
	State                string    `json:"state,omitempty"`
	ZipCode              string    `json:"zip_code,omitempty"`
	Timezone             string    `json:"timezone"`
	DataClassification   string    `json:"data_classification"`
	PrimaryLanguage      string    `json:"primary_language"`
	SecondaryLanguage    string    `json:"secondary_language"`
	HandoffPhone         string    `json:"handoff_phone,omitempty"`
	PublicSlug           string    `json:"public_slug,omitempty"`
	PublicCatalogEnabled bool      `json:"public_catalog_enabled"`
	Version              int64     `json:"version"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type SalonProfileMutationRequest struct {
	MutationControl
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Address           string `json:"address"`
	City              string `json:"city"`
	State             string `json:"state"`
	ZipCode           string `json:"zip_code"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language"`
	HandoffPhone      string `json:"handoff_phone"`
}

type ServiceCategory struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description,omitempty"`
	SortOrder   int        `json:"sort_order"`
	Status      string     `json:"status"`
	Version     int64      `json:"version"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

type ServiceCategoryMutationRequest struct {
	MutationControl
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

type ConsultationProfile struct {
	Status                   string   `json:"status"`
	RecommendedOutcomes      []string `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string `json:"compatible_current_systems"`
	LengthCapabilities       []string `json:"length_capabilities"`
	PriorityTags             []string `json:"priority_tags"`
	FinishOptions            []string `json:"finish_options"`
	MaintenanceNote          string   `json:"maintenance_note,omitempty"`
	OwnerApprovedSummary     string   `json:"owner_approved_summary,omitempty"`
}

type Service struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Description         string               `json:"description,omitempty"`
	AIDescription       string               `json:"ai_description,omitempty"`
	DurationMinutes     int                  `json:"duration_minutes"`
	PriceFrom           *float64             `json:"price_from,omitempty"`
	PriceDisplay        string               `json:"price_display,omitempty"`
	AIBookable          bool                 `json:"ai_bookable"`
	Active              bool                 `json:"active"`
	Category            *ServiceCategory     `json:"category,omitempty"`
	ConsultationProfile *ConsultationProfile `json:"consultation_profile,omitempty"`
	ManagementMode      string               `json:"management_mode"`
	Version             int64                `json:"version"`
	ArchivedAt          *time.Time           `json:"archived_at,omitempty"`
}

type ServiceMutationRequest struct {
	MutationControl
	Name                *string              `json:"name,omitempty"`
	Description         *string              `json:"description,omitempty"`
	AIDescription       *string              `json:"ai_description,omitempty"`
	DurationMinutes     *int                 `json:"duration_minutes,omitempty"`
	PriceFrom           *float64             `json:"price_from,omitempty"`
	PriceDisplay        *string              `json:"price_display,omitempty"`
	AIBookable          *bool                `json:"ai_bookable,omitempty"`
	Active              *bool                `json:"active,omitempty"`
	ServiceCategoryID   *string              `json:"service_category_id,omitempty"`
	ConsultationProfile *ConsultationProfile `json:"consultation_profile,omitempty"`
}

type StaffMember struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Phone              string     `json:"phone,omitempty"`
	Email              string     `json:"email,omitempty"`
	AIBookable         bool       `json:"ai_bookable"`
	Active             bool       `json:"active"`
	ManagementMode     string     `json:"management_mode"`
	ServiceIDs         []string   `json:"service_ids"`
	Version            int64      `json:"version"`
	EligibilityVersion int64      `json:"eligibility_version"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
}

type StaffMutationRequest struct {
	MutationControl
	Name       *string `json:"name,omitempty"`
	Phone      *string `json:"phone,omitempty"`
	Email      *string `json:"email,omitempty"`
	AIBookable *bool   `json:"ai_bookable,omitempty"`
	Active     *bool   `json:"active,omitempty"`
}

type StaffServiceEligibilityMutationRequest struct {
	MutationControl
	StaffID    string   `json:"staff_id"`
	ServiceIDs []string `json:"service_ids"`
}

type BusinessHourPeriod struct {
	ID             string `json:"id"`
	DayOfWeek      int    `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
	EndAtMidnight  bool   `json:"end_at_midnight"`
}

type BusinessHours struct {
	Periods        []BusinessHourPeriod `json:"periods"`
	ManagementMode string               `json:"management_mode"`
	Version        int64                `json:"version"`
}

type BusinessHourPeriodInput struct {
	DayOfWeek      int    `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
	EndAtMidnight  bool   `json:"end_at_midnight"`
}

type BusinessHoursMutationRequest struct {
	MutationControl
	Periods []BusinessHourPeriodInput `json:"periods"`
}

type PublicCatalogSettings struct {
	PublicSlug           string `json:"public_slug,omitempty"`
	PublicCatalogEnabled bool   `json:"public_catalog_enabled"`
	PublicPath           string `json:"public_path,omitempty"`
	CanPublish           bool   `json:"can_publish"`
	BlockedReason        string `json:"blocked_reason,omitempty"`
	Version              int64  `json:"version"`
}

type PublicCatalogMutationRequest struct {
	MutationControl
	PublicSlug           string `json:"public_slug"`
	PublicCatalogEnabled bool   `json:"public_catalog_enabled"`
}

type Customer struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Phone          string     `json:"phone,omitempty"`
	Email          string     `json:"email,omitempty"`
	Notes          string     `json:"notes,omitempty"`
	Active         bool       `json:"active"`
	ManagementMode string     `json:"management_mode"`
	Version        int64      `json:"version"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

type CustomerMutationRequest struct {
	MutationControl
	Name   *string `json:"name,omitempty"`
	Phone  *string `json:"phone,omitempty"`
	Email  *string `json:"email,omitempty"`
	Notes  *string `json:"notes,omitempty"`
	Active *bool   `json:"active,omitempty"`
}

type ServicesResponse struct {
	Services []Service `json:"services"`
}

type ServiceCategoriesResponse struct {
	Categories []ServiceCategory `json:"categories"`
}

type StaffResponse struct {
	Staff []StaffMember `json:"staff"`
}

type CustomersResponse struct {
	Customers []Customer `json:"customers"`
}

type MutationResponse[T any] struct {
	Data     T    `json:"data"`
	Replayed bool `json:"replayed"`
}
