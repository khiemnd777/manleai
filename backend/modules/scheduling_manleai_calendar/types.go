package scheduling_manleai_calendar

import (
	"errors"
	"time"
)

const (
	CapacityModeStaffOnly = "staff_only"
	CapacityModePooled    = "pooled"

	ExceptionScopeSalon    = "salon"
	ExceptionScopeStaff    = "staff"
	ExceptionScopeResource = "resource"

	ExceptionEffectAvailable        = "available"
	ExceptionEffectUnavailable      = "unavailable"
	ExceptionEffectCapacityOverride = "capacity_override"

	ReadinessDimensionConfiguration = "configuration"
	ReadinessDimensionExecution     = "execution"
)

const (
	BlockerConfigRequired                  = "CONFIG_REQUIRED"
	BlockerLocalHoursRequired              = "LOCAL_HOURS_REQUIRED"
	BlockerEligibleServicesRequired        = "ELIGIBLE_SERVICES_REQUIRED"
	BlockerServicePolicyRequired           = "SERVICE_POLICY_REQUIRED"
	BlockerEnabledServiceRequired          = "ENABLED_SERVICE_REQUIRED"
	BlockerServiceIneligible               = "SERVICE_INELIGIBLE"
	BlockerServiceCapacityModeRequired     = "SERVICE_CAPACITY_MODE_REQUIRED"
	BlockerServiceStaffRequired            = "SERVICE_STAFF_REQUIRED"
	BlockerStaffIneligible                 = "STAFF_INELIGIBLE"
	BlockerStaffScheduleRequired           = "STAFF_SCHEDULE_REQUIRED"
	BlockerPooledResourceRequired          = "POOLED_RESOURCE_REQUIRED"
	BlockerStaffOnlyResourceNotAllowed     = "STAFF_ONLY_RESOURCE_NOT_ALLOWED"
	BlockerResourceArchived                = "RESOURCE_ARCHIVED"
	BlockerResourceCapacityExceeded        = "RESOURCE_CAPACITY_EXCEEDED"
	BlockerConfigNotActivated              = "CONFIG_NOT_ACTIVATED"
	BlockerPooledCapacityEngineUnavailable = "POOLED_CAPACITY_ENGINE_UNAVAILABLE"
	BlockerPartyCreateEngineUnavailable    = "PARTY_CREATE_ENGINE_UNAVAILABLE"
	BlockerLifecycleEngineUnavailable      = "LIFECYCLE_ENGINE_UNAVAILABLE"
)

const (
	EventConfigCreated         = "config_created"
	EventConfigUpdated         = "config_updated"
	EventConfigActivated       = "config_activated"
	EventSalonHoursReplaced    = "salon_hours_replaced"
	EventStaffScheduleReplaced = "staff_schedule_replaced"
	EventServicePolicyUpdated  = "service_policy_updated"
	EventResourcePoolCreated   = "resource_pool_created"
	EventResourcePoolUpdated   = "resource_pool_updated"
	EventResourcePoolArchived  = "resource_pool_archived"
	EventExceptionCreated      = "exception_created"
	EventExceptionCancelled    = "exception_cancelled"
)

var (
	ErrValidation      = errors.New("manleai calendar validation failed")
	ErrNotFound        = errors.New("manleai calendar resource not found")
	ErrConfigRequired  = errors.New("manleai calendar config is required")
	ErrVersionConflict = errors.New("manleai calendar config version conflict")
	ErrActionConflict  = errors.New("manleai calendar action conflict")
	ErrNotReady        = errors.New("manleai calendar configuration is not ready")
)

type MutationMeta struct {
	ActionKey             string `json:"action_key"`
	ExpectedConfigVersion int64  `json:"expected_config_version"`
}

type CalendarConfigInput struct {
	MutationMeta
	SlotStepMinutes             int  `json:"slot_step_minutes"`
	MinimumBookingNoticeMinutes int  `json:"minimum_booking_notice_minutes"`
	BookingHorizonDays          int  `json:"booking_horizon_days"`
	RescheduleCutoffMinutes     *int `json:"reschedule_cutoff_minutes"`
	CancellationCutoffMinutes   *int `json:"cancellation_cutoff_minutes"`
	MaxPartySize                int  `json:"max_party_size"`
	DefaultBufferBeforeMinutes  int  `json:"default_buffer_before_minutes"`
	DefaultBufferAfterMinutes   int  `json:"default_buffer_after_minutes"`
}

type CalendarConfig struct {
	SalonID                     string     `json:"salon_id"`
	Version                     int64      `json:"version"`
	SlotStepMinutes             int        `json:"slot_step_minutes"`
	MinimumBookingNoticeMinutes int        `json:"minimum_booking_notice_minutes"`
	BookingHorizonDays          int        `json:"booking_horizon_days"`
	RescheduleCutoffMinutes     *int       `json:"reschedule_cutoff_minutes"`
	CancellationCutoffMinutes   *int       `json:"cancellation_cutoff_minutes"`
	MaxPartySize                int        `json:"max_party_size"`
	DefaultBufferBeforeMinutes  int        `json:"default_buffer_before_minutes"`
	DefaultBufferAfterMinutes   int        `json:"default_buffer_after_minutes"`
	ActivatedAt                 *time.Time `json:"activated_at"`
	ActivatedByUserID           string     `json:"activated_by_user_id,omitempty"`
	ActivatedVersion            *int64     `json:"activated_version"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type BusinessHourPeriodInput struct {
	DayOfWeek   int `json:"day_of_week"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type ReplaceBusinessHoursInput struct {
	MutationMeta
	Periods []BusinessHourPeriodInput `json:"periods"`
}

type BusinessHourPeriod struct {
	ID          string    `json:"id"`
	DayOfWeek   int       `json:"day_of_week"`
	StartMinute int       `json:"start_minute"`
	EndMinute   int       `json:"end_minute"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WeeklyPeriodInput struct {
	DayOfWeek   int `json:"day_of_week"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type StaffProfileInput struct {
	MutationMeta
	WeeklyPeriods      []WeeklyPeriodInput `json:"weekly_periods"`
	EligibleServiceIDs []string            `json:"eligible_service_ids"`
}

type StaffRef struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Active     bool       `json:"active"`
	AIBookable bool       `json:"ai_bookable"`
	ArchivedAt *time.Time `json:"archived_at"`
}

type ServiceRef struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	DurationMinutes int        `json:"duration_minutes"`
	Active          bool       `json:"active"`
	AIBookable      bool       `json:"ai_bookable"`
	ArchivedAt      *time.Time `json:"archived_at"`
}

type WeeklyPeriod struct {
	ID          string    `json:"id"`
	StaffID     string    `json:"staff_id"`
	DayOfWeek   int       `json:"day_of_week"`
	StartMinute int       `json:"start_minute"`
	EndMinute   int       `json:"end_minute"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type StaffProfile struct {
	Staff            StaffRef       `json:"staff"`
	WeeklyPeriods    []WeeklyPeriod `json:"weekly_periods"`
	EligibleServices []ServiceRef   `json:"eligible_services"`
}

type ResourceRequirementInput struct {
	ResourcePoolID string `json:"resource_pool_id"`
	UnitsRequired  int    `json:"units_required"`
}

type ServicePolicyInput struct {
	MutationMeta
	Enabled                     bool                       `json:"enabled"`
	CapacityMode                *string                    `json:"capacity_mode"`
	BufferBeforeMinutesOverride *int                       `json:"buffer_before_minutes_override"`
	BufferAfterMinutesOverride  *int                       `json:"buffer_after_minutes_override"`
	EligibleStaffIDs            []string                   `json:"eligible_staff_ids"`
	ResourceRequirements        []ResourceRequirementInput `json:"resource_requirements"`
}

type ResourceRequirement struct {
	ResourcePoolID string     `json:"resource_pool_id"`
	ResourceName   string     `json:"resource_name"`
	UnitsRequired  int        `json:"units_required"`
	PoolCapacity   int        `json:"pool_capacity"`
	PoolArchivedAt *time.Time `json:"pool_archived_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ServicePolicy struct {
	Service                     ServiceRef            `json:"service"`
	Configured                  bool                  `json:"configured"`
	Enabled                     bool                  `json:"enabled"`
	CapacityMode                *string               `json:"capacity_mode"`
	BufferBeforeMinutesOverride *int                  `json:"buffer_before_minutes_override"`
	BufferAfterMinutesOverride  *int                  `json:"buffer_after_minutes_override"`
	EligibleStaff               []StaffRef            `json:"eligible_staff"`
	ResourceRequirements        []ResourceRequirement `json:"resource_requirements"`
	CreatedAt                   *time.Time            `json:"created_at"`
	UpdatedAt                   *time.Time            `json:"updated_at"`
}

type ResourcePoolInput struct {
	MutationMeta
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

type ResourcePool struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Capacity   int        `json:"capacity"`
	ArchivedAt *time.Time `json:"archived_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ExceptionInput struct {
	MutationMeta
	ScopeType        string    `json:"scope_type"`
	StaffID          string    `json:"staff_id,omitempty"`
	ResourcePoolID   string    `json:"resource_pool_id,omitempty"`
	Effect           string    `json:"effect"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	CapacityOverride *int      `json:"capacity_override"`
	Reason           string    `json:"reason,omitempty"`
}

type CalendarException struct {
	ID                string     `json:"id"`
	ScopeType         string     `json:"scope_type"`
	StaffID           string     `json:"staff_id,omitempty"`
	ResourcePoolID    string     `json:"resource_pool_id,omitempty"`
	Effect            string     `json:"effect"`
	StartsAt          time.Time  `json:"starts_at"`
	EndsAt            time.Time  `json:"ends_at"`
	CapacityOverride  *int       `json:"capacity_override"`
	Reason            string     `json:"reason,omitempty"`
	CreatedByUserID   string     `json:"created_by_user_id"`
	CancelledAt       *time.Time `json:"cancelled_at"`
	CancelledByUserID string     `json:"cancelled_by_user_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type ReadinessBlocker struct {
	Code      string `json:"code"`
	Dimension string `json:"dimension"`
	Scope     string `json:"scope"`
	EntityID  string `json:"entity_id,omitempty"`
	Message   string `json:"message"`
}

type Readiness struct {
	ConfigurationReady bool                  `json:"configuration_ready"`
	ExecutionReady     bool                  `json:"execution_ready"`
	AuthorityVersion   int64                 `json:"authority_version"`
	ConfigVersion      int64                 `json:"config_version"`
	Capabilities       OperationCapabilities `json:"capabilities"`
	Blockers           []ReadinessBlocker    `json:"blockers"`
}

type OperationCapabilities struct {
	StaffOnlyAvailability bool `json:"staff_only_availability"`
	StaffOnlyCreate       bool `json:"staff_only_create"`
	PooledCapacity        bool `json:"pooled_capacity"`
	PartyCreate           bool `json:"party_create"`
	Reschedule            bool `json:"reschedule"`
	Cancel                bool `json:"cancel"`
}

type IntegerConstraint struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
}

type SlotStepConstraint struct {
	IntegerConstraint
	MustDivideMinutes int `json:"must_divide_minutes"`
}

type Constraints struct {
	SlotStepMinutes             SlotStepConstraint `json:"slot_step_minutes"`
	MinimumBookingNoticeMinutes IntegerConstraint  `json:"minimum_booking_notice_minutes"`
	BookingHorizonDays          IntegerConstraint  `json:"booking_horizon_days"`
	CutoffMinutes               IntegerConstraint  `json:"cutoff_minutes"`
	MaxPartySize                IntegerConstraint  `json:"max_party_size"`
	BufferMinutes               IntegerConstraint  `json:"buffer_minutes"`
	PeriodMinutes               IntegerConstraint  `json:"period_minutes"`
	ResourceCapacity            IntegerConstraint  `json:"resource_capacity"`
	ResourceUnitsRequired       IntegerConstraint  `json:"resource_units_required"`
	ExceptionCapacityOverride   IntegerConstraint  `json:"exception_capacity_override"`
	ResourceNameMaxCharacters   int                `json:"resource_name_max_characters"`
	ActionKeyMaxBytes           int                `json:"action_key_max_bytes"`
	ExceptionReasonMaxBytes     int                `json:"exception_reason_max_bytes"`
	CapacityModes               []string           `json:"capacity_modes"`
	ExceptionScopeTypes         []string           `json:"exception_scope_types"`
	ExceptionEffects            []string           `json:"exception_effects"`
	// ExecutionEngineAvailable is a legacy aggregate signal meaning at least
	// one execution slice exists. Consumers must use Readiness.Capabilities for
	// operation- and capacity-mode-specific gating.
	ExecutionEngineAvailable bool `json:"execution_engine_available"`
}

type Aggregate struct {
	SalonID             string               `json:"salon_id"`
	Timezone            string               `json:"timezone"`
	SchedulingAuthority string               `json:"scheduling_authority"`
	AuthorityVersion    int64                `json:"authority_version"`
	ConfigVersion       int64                `json:"config_version"`
	Config              *CalendarConfig      `json:"config"`
	Hours               []BusinessHourPeriod `json:"hours"`
	StaffProfiles       []StaffProfile       `json:"staff_profiles"`
	ServicePolicies     []ServicePolicy      `json:"service_policies"`
	Resources           []ResourcePool       `json:"resources"`
	Exceptions          []CalendarException  `json:"exceptions"`
	Readiness           Readiness            `json:"readiness"`
	Constraints         Constraints          `json:"constraints"`
}

type AggregateResponse struct {
	ManleaiCalendar *Aggregate `json:"manleai_calendar"`
}

type MutationResponse struct {
	ManleaiCalendar *Aggregate `json:"manleai_calendar"`
	Replayed        bool       `json:"replayed"`
}

type StaffProfileResponse struct {
	StaffProfile  *StaffProfile `json:"staff_profile"`
	ConfigVersion int64         `json:"config_version"`
	Readiness     Readiness     `json:"readiness"`
}

type ServicePolicyResponse struct {
	ServicePolicy *ServicePolicy `json:"service_policy"`
	ConfigVersion int64          `json:"config_version"`
	Readiness     Readiness      `json:"readiness"`
}

type ResourceListResponse struct {
	Resources     []ResourcePool `json:"resources"`
	ConfigVersion int64          `json:"config_version"`
	Readiness     Readiness      `json:"readiness"`
}

func DefaultConstraints() Constraints {
	return Constraints{
		SlotStepMinutes:             SlotStepConstraint{IntegerConstraint: IntegerConstraint{Minimum: 1, Maximum: 1440}, MustDivideMinutes: 1440},
		MinimumBookingNoticeMinutes: IntegerConstraint{Minimum: 0, Maximum: 525600},
		BookingHorizonDays:          IntegerConstraint{Minimum: 1, Maximum: 366},
		CutoffMinutes:               IntegerConstraint{Minimum: 0, Maximum: 525600},
		MaxPartySize:                IntegerConstraint{Minimum: 1, Maximum: 100},
		BufferMinutes:               IntegerConstraint{Minimum: 0, Maximum: 1440},
		PeriodMinutes:               IntegerConstraint{Minimum: 0, Maximum: 1440},
		ResourceCapacity:            IntegerConstraint{Minimum: 1, Maximum: 1000},
		ResourceUnitsRequired:       IntegerConstraint{Minimum: 1, Maximum: 1000},
		ExceptionCapacityOverride:   IntegerConstraint{Minimum: 0, Maximum: 1000},
		ResourceNameMaxCharacters:   200,
		ActionKeyMaxBytes:           256,
		ExceptionReasonMaxBytes:     2000,
		CapacityModes:               []string{CapacityModeStaffOnly, CapacityModePooled},
		ExceptionScopeTypes:         []string{ExceptionScopeSalon, ExceptionScopeStaff, ExceptionScopeResource},
		ExceptionEffects:            []string{ExceptionEffectAvailable, ExceptionEffectUnavailable, ExceptionEffectCapacityOverride},
		ExecutionEngineAvailable:    true,
	}
}
