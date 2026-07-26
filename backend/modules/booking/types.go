package booking

import (
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

var ErrSchedulingAuthorityNotReady = errors.New("scheduling authority is not ready")

const (
	SourceOwnerDashboard          = "owner_dashboard"
	SourceSquareTestBooking       = "square_test_booking"
	SourceAIConversationSimulator = "ai_conversation_simulator"
	SourceAIVoiceCall             = "ai_voice_call"
	SourcePOSCalendarSync         = "pos_calendar_sync"

	StatusPOSPending      = "pos_pending"
	StatusProviderPending = "provider_pending"
	StatusConfirmed       = "confirmed"
	StatusFallbackPending = "fallback_pending"
	StatusRescheduled     = "rescheduled"
	StatusCancelled       = "cancelled"
	StatusDeclined        = "declined"
	StatusNoShow          = "no_show"
	StatusUnknown         = "unknown"

	POSSyncStatusSynced    = "synced"
	POSSyncStatusNotSynced = "not_synced"
	POSSyncStatusFailed    = "sync_failed"
	POSSyncStatusPending   = "pending"

	BookingActionBook       = "book"
	BookingActionReschedule = "reschedule"
	BookingActionCancel     = "cancel"

	ProviderOutcomeNotStarted = "not_started"
	ProviderOutcomeInFlight   = "in_flight"
	ProviderOutcomeSucceeded  = "succeeded"
	ProviderOutcomeFailed     = "failed"
	ProviderOutcomeUnknown    = "unknown"

	RetryPolicyNone    = "none"
	RetryPolicySafe    = "safe"
	RetryPolicyBlocked = "blocked"

	ReconciliationNotRequired = "not_required"
	ReconciliationRequired    = "required"
	ReconciliationResolved    = "resolved"

	ReconciliationActionProviderAttached = "provider_attached"
	ReconciliationActionNotCreated       = "not_created"
	ReconciliationActionEscalated        = "escalated"
	ReconciliationResolutionSuperseded   = "superseded"

	NotificationTypeBookingFallback      = "booking_fallback_pending"
	NotificationTypeBookingConfirmed     = "booking_confirmed"
	NotificationTypeRescheduleFallback   = "reschedule_fallback_pending"
	NotificationTypeCancellationFallback = "cancel_fallback_pending"

	StaffSelectionSpecific = "specific"
	StaffSelectionAnyone   = "anyone"

	SchedulingAuthorityOwnerManual      = "owner_manual"
	SchedulingAuthorityManleAICalendar  = "manleai_calendar"
	SchedulingAuthorityExternalProvider = "external_provider"
)

type CreateBookingRequest struct {
	OperationKey        string                  `json:"operation_key"`
	RetryOfAttemptID    string                  `json:"retry_of_attempt_id"`
	AvailabilityQuoteID string                  `json:"availability_quote_id"`
	SlotFingerprint     string                  `json:"slot_fingerprint"`
	Source              string                  `json:"source"`
	CustomerName        string                  `json:"customer_name"`
	CustomerPhone       string                  `json:"customer_phone"`
	CustomerEmail       string                  `json:"customer_email"`
	ServiceID           string                  `json:"service_id"`
	StaffID             string                  `json:"staff_id"`
	StaffSelectionMode  string                  `json:"staff_selection_mode"`
	Segments            []BookingSegmentRequest `json:"segments"`
	StartTime           time.Time               `json:"start_time"`
	Notes               string                  `json:"notes"`
}

type BookingSegmentRequest struct {
	ServiceID          string `json:"service_id"`
	StaffID            string `json:"staff_id"`
	StaffSelectionMode string `json:"staff_selection_mode"`
	GuestReference     string `json:"guest_reference,omitempty"`
	Quantity           int    `json:"quantity,omitempty"`
}

type RescheduleRequest struct {
	OperationKey        string    `json:"operation_key"`
	RetryOfAttemptID    string    `json:"retry_of_attempt_id"`
	AvailabilityQuoteID string    `json:"availability_quote_id"`
	SlotFingerprint     string    `json:"slot_fingerprint"`
	StartTime           time.Time `json:"start_time"`
	StaffID             string    `json:"staff_id"`
	Notes               string    `json:"notes"`
	Source              string    `json:"-"`
}

type RescheduleLookupRequest struct {
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	Limit         int    `json:"limit"`
}

type CancelRequest struct {
	OperationKey     string `json:"operation_key"`
	RetryOfAttemptID string `json:"retry_of_attempt_id"`
	Reason           string `json:"reason"`
	Source           string `json:"-"`
}

type AvailabilityRequest struct {
	TargetAppointmentID string                  `json:"target_appointment_id,omitempty"`
	RetryOfAttemptID    string                  `json:"retry_of_attempt_id,omitempty"`
	ServiceID           string                  `json:"service_id"`
	StaffID             string                  `json:"staff_id"`
	StaffSelectionMode  string                  `json:"staff_selection_mode"`
	Segments            []BookingSegmentRequest `json:"segments"`
	PartySize           int                     `json:"party_size,omitempty"`
	PreferredDate       string                  `json:"preferred_date"`
	Limit               int                     `json:"limit"`
}

type AvailabilityResult struct {
	QuoteID                           string                `json:"quote_id,omitempty"`
	RequestFingerprint                string                `json:"request_fingerprint,omitempty"`
	ExpiresAt                         *time.Time            `json:"expires_at,omitempty"`
	TargetAuthorityAppointmentVersion int                   `json:"target_authority_appointment_version,omitempty"`
	ServiceID                         string                `json:"service_id"`
	ServiceName                       string                `json:"service_name"`
	StaffID                           string                `json:"staff_id,omitempty"`
	StaffName                         string                `json:"staff_name,omitempty"`
	StaffSelectionMode                string                `json:"staff_selection_mode"`
	Segments                          []AvailabilitySegment `json:"segments,omitempty"`
	PreferredDate                     string                `json:"preferred_date"`
	DurationMinutes                   int                   `json:"duration_minutes"`
	Timezone                          string                `json:"timezone"`
	Slots                             []AvailabilitySlot    `json:"slots"`
}

type AvailabilitySlot struct {
	Fingerprint        string                `json:"fingerprint"`
	StartTime          time.Time             `json:"start_time"`
	EndTime            time.Time             `json:"end_time"`
	StaffID            string                `json:"staff_id,omitempty"`
	StaffName          string                `json:"staff_name,omitempty"`
	StaffSelectionMode string                `json:"staff_selection_mode"`
	Segments           []AvailabilitySegment `json:"segments,omitempty"`
}

type AvailabilitySegment struct {
	ServiceID           string                           `json:"service_id"`
	ServiceName         string                           `json:"service_name"`
	StaffID             string                           `json:"staff_id,omitempty"`
	StaffName           string                           `json:"staff_name,omitempty"`
	StaffSelectionMode  string                           `json:"staff_selection_mode"`
	GuestReference      string                           `json:"guest_reference,omitempty"`
	Quantity            int                              `json:"quantity"`
	DurationMinutes     int                              `json:"duration_minutes"`
	ScheduledStartTime  time.Time                        `json:"scheduled_start_time"`
	ScheduledEndTime    time.Time                        `json:"scheduled_end_time"`
	OccupiedStartTime   time.Time                        `json:"occupied_start_time"`
	OccupiedEndTime     time.Time                        `json:"occupied_end_time"`
	BufferBeforeMinutes int                              `json:"buffer_before_minutes"`
	BufferAfterMinutes  int                              `json:"buffer_after_minutes"`
	ResourceAllocations []AvailabilityResourceAllocation `json:"resource_allocations"`
}

type AvailabilityResourceAllocation struct {
	ResourcePoolID string `json:"resource_pool_id"`
	ResourceName   string `json:"resource_name"`
	UnitsAllocated int    `json:"units_allocated"`
}

type BookingAttempt struct {
	ID                                string `json:"id"`
	SalonID                           string `json:"salon_id"`
	SchedulingAuthority               string `json:"scheduling_authority"`
	AuthorityProvider                 string `json:"authority_provider,omitempty"`
	AuthorityAppointmentID            string `json:"authority_appointment_id,omitempty"`
	AuthorityAppointmentVersion       int    `json:"authority_appointment_version"`
	TargetAuthorityAppointmentVersion int    `json:"-"`
	AuthorityIdempotencyKey           string `json:"-"`
	AuthorityLocationID               string `json:"-"`
	AuthoritySnapshotGeneration       int64  `json:"-"`
	Source                            string `json:"source"`
	Status                            string `json:"status"`
	// Deprecated: external_provider compatibility alias. Authority selection and
	// status discrimination use SchedulingAuthority and the Authority* evidence.
	POSProvider string `json:"pos_provider,omitempty"`
	// Deprecated: external_provider compatibility alias.
	POSBookingID string `json:"pos_booking_id,omitempty"`
	// Deprecated: external_provider compatibility alias.
	POSBookingVersion         int                      `json:"pos_booking_version,omitempty"`
	TargetPOSBookingVersion   int                      `json:"-"`
	POSIdempotencyKey         string                   `json:"-"`
	OperationKey              string                   `json:"operation_key,omitempty"`
	RequestFingerprint        string                   `json:"-"`
	RetryOfAttemptID          string                   `json:"retry_of_attempt_id,omitempty"`
	SupersededByAttemptID     string                   `json:"superseded_by_attempt_id,omitempty"`
	RetrySequence             int                      `json:"retry_sequence"`
	SupersededAt              *time.Time               `json:"superseded_at,omitempty"`
	AvailabilityQuoteID       string                   `json:"availability_quote_id,omitempty"`
	SlotFingerprint           string                   `json:"availability_slot_fingerprint,omitempty"`
	ProviderFence             pos.ProviderFence        `json:"-"`
	OperationType             string                   `json:"operation_type"`
	ProcessingToken           string                   `json:"-"`
	ProcessingLeaseEnds       *time.Time               `json:"processing_lease_expires_at,omitempty"`
	ProviderOutcome           string                   `json:"provider_outcome"`
	RetryPolicy               string                   `json:"retry_policy"`
	Reconciliation            string                   `json:"reconciliation_status"`
	ReconciliationResolution  string                   `json:"reconciliation_resolution,omitempty"`
	ReconciliationResolvedAt  *time.Time               `json:"reconciliation_resolved_at,omitempty"`
	CustomerName              string                   `json:"customer_name"`
	CustomerPhone             string                   `json:"customer_phone"`
	CustomerEmail             string                   `json:"customer_email,omitempty"`
	ServiceID                 string                   `json:"service_id,omitempty"`
	StaffID                   string                   `json:"staff_id,omitempty"`
	StaffSelectionMode        string                   `json:"staff_selection_mode"`
	Segments                  []BookingSegmentSnapshot `json:"segments,omitempty"`
	RequestedStartTime        time.Time                `json:"requested_start_time"`
	RequestedEndTime          time.Time                `json:"requested_end_time"`
	Notes                     string                   `json:"notes,omitempty"`
	ErrorCode                 string                   `json:"error_code,omitempty"`
	ErrorMessage              string                   `json:"error_message,omitempty"`
	BookingAction             string                   `json:"booking_action,omitempty"`
	TargetAppointmentID       string                   `json:"target_appointment_id,omitempty"`
	NotificationType          string                   `json:"notification_type,omitempty"`
	NotificationStatus        string                   `json:"notification_status,omitempty"`
	SyncWarning               string                   `json:"sync_warning,omitempty"`
	CanRetry                  bool                     `json:"can_retry"`
	RetryBlockedReason        string                   `json:"retry_blocked_reason,omitempty"`
	retryPrerequisitesChecked bool
	retryPrerequisitesCurrent bool
	retryPrerequisitesReason  string
	CreatedAt                 time.Time    `json:"created_at"`
	UpdatedAt                 time.Time    `json:"updated_at"`
	Appointment               *Appointment `json:"appointment,omitempty"`
}

type Appointment struct {
	ID                          string `json:"id"`
	SalonID                     string `json:"salon_id"`
	BookingAttemptID            string `json:"booking_attempt_id"`
	SchedulingAuthority         string `json:"scheduling_authority"`
	AuthorityProvider           string `json:"authority_provider,omitempty"`
	AuthorityAppointmentID      string `json:"authority_appointment_id"`
	AuthorityAppointmentVersion int    `json:"authority_appointment_version"`
	AuthorityCustomerID         string `json:"-"`
	// Deprecated: external_provider compatibility alias. Authority selection and
	// lifecycle gating use SchedulingAuthority and the Authority* evidence.
	POSProvider string `json:"pos_provider,omitempty"`
	// Deprecated: external_provider compatibility alias.
	POSAppointmentID string `json:"pos_appointment_id,omitempty"`
	// Deprecated: external_provider compatibility alias.
	POSAppointmentVersion int                      `json:"pos_appointment_version,omitempty"`
	POSCustomerID         string                   `json:"-"`
	Status                string                   `json:"status"`
	PartySize             int                      `json:"party_size,omitempty"`
	CustomerName          string                   `json:"customer_name"`
	CustomerPhone         string                   `json:"customer_phone"`
	CustomerEmail         string                   `json:"customer_email,omitempty"`
	ServiceID             string                   `json:"service_id,omitempty"`
	StaffID               string                   `json:"staff_id,omitempty"`
	StaffSelectionMode    string                   `json:"staff_selection_mode"`
	Segments              []BookingSegmentSnapshot `json:"segments,omitempty"`
	StartTime             time.Time                `json:"start_time"`
	EndTime               time.Time                `json:"end_time"`
	Notes                 string                   `json:"notes,omitempty"`
	POSSyncStatus         string                   `json:"pos_sync_status,omitempty"`
	LastPOSSyncedAt       *time.Time               `json:"last_pos_synced_at,omitempty"`
	POSSyncError          string                   `json:"pos_sync_error,omitempty"`
	SyncWarning           string                   `json:"sync_warning,omitempty"`
	CanEdit               bool                     `json:"can_edit"`
	CanDelete             bool                     `json:"can_delete"`
	ConfirmedAt           *time.Time               `json:"confirmed_at,omitempty"`
	ConfirmedByUserID     string                   `json:"-"`
	ConfirmationSource    string                   `json:"confirmation_source,omitempty"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

type ListAppointmentsResponse struct {
	Appointments []Appointment `json:"appointments"`
	Limit        int           `json:"limit"`
	Offset       int           `json:"offset"`
	HasMore      bool          `json:"has_more"`
}

type ListBookingAttemptsResponse struct {
	BookingAttempts []BookingAttempt `json:"booking_attempts"`
	Limit           int              `json:"limit"`
	Offset          int              `json:"offset"`
	HasMore         bool             `json:"has_more"`
	Status          string           `json:"status,omitempty"`
}

type CalendarRangeRequest struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	View      string    `json:"view"`
}

type CalendarRangeResponse struct {
	SalonID         string                 `json:"salon_id"`
	StartTime       time.Time              `json:"start_time"`
	EndTime         time.Time              `json:"end_time"`
	View            string                 `json:"view"`
	Appointments    []Appointment          `json:"appointments"`
	PendingRequests []BookingAttempt       `json:"pending_requests"`
	Warnings        CalendarWarningSummary `json:"warnings"`
}

type CalendarWarningSummary struct {
	TotalWarnings   int `json:"total_warnings"`
	SyncFailed      int `json:"sync_failed"`
	NotSynced       int `json:"not_synced"`
	PendingPOSSync  int `json:"pending_pos_sync"`
	FallbackPending int `json:"fallback_pending"`
}

type CalendarEventCursor struct {
	CreatedAt time.Time
	ID        string
}

type CalendarEvent struct {
	ID                 string    `json:"id"`
	Cursor             string    `json:"cursor"`
	SalonID            string    `json:"salon_id"`
	Type               string    `json:"type"`
	NotificationStatus string    `json:"notification_status"`
	Title              string    `json:"title"`
	Message            string    `json:"message"`
	BookingAttemptID   string    `json:"booking_attempt_id,omitempty"`
	AppointmentID      string    `json:"appointment_id,omitempty"`
	Source             string    `json:"source,omitempty"`
	BookingStatus      string    `json:"booking_status,omitempty"`
	CustomerName       string    `json:"customer_name,omitempty"`
	ServiceID          string    `json:"service_id,omitempty"`
	StaffID            string    `json:"staff_id,omitempty"`
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	CreatedAt          time.Time `json:"created_at"`
}

type CalendarSyncRequest struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type CalendarSyncResponse struct {
	Provider string              `json:"provider"`
	Summary  CalendarSyncSummary `json:"summary"`
	Range    CalendarSyncRange   `json:"range"`
}

type CalendarSyncRange struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type CalendarSyncSummary struct {
	Imported     int `json:"imported"`
	Updated      int `json:"updated"`
	Skipped      int `json:"skipped"`
	WarningCount int `json:"warning_count"`
}

type CalendarAppointmentImport struct {
	Provider              string
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
	Segments              []CalendarAppointmentSegmentImport
}

type CalendarAppointmentSegmentImport struct {
	POSServiceID      string
	POSServiceVersion int64
	POSStaffID        string
	DurationMinutes   int
}

type ServiceRef struct {
	ID                string
	POSProvider       string
	POSServiceID      string
	POSServiceVersion int64
	Name              string
	DurationMinutes   int
	PriceFrom         float64
	ProviderFence     pos.ProviderFence
}

type StaffRef struct {
	ID            string
	POSProvider   string
	POSStaffID    string
	Name          string
	ProviderFence pos.ProviderFence
}

type CustomerRef struct {
	ID            string
	Name          string
	Phone         string
	Email         string
	POSProvider   string
	POSCustomerID string
}

type Schedule struct {
	Timezone            string
	BusinessHours       []BusinessHour
	BusinessHourPeriods []BusinessHourPeriod
}

type BusinessHour struct {
	DayOfWeek int
	OpenTime  string
	CloseTime string
	IsClosed  bool
}

type BusinessHourPeriod struct {
	DayOfWeek      int
	StartLocalTime string
	EndLocalTime   string
}

type AppointmentActionRef struct {
	ID                          string
	SalonID                     string
	BookingAttemptID            string
	SchedulingAuthority         string
	AuthorityAppointmentVersion int
	PartySize                   int
	POSProvider                 string
	ProviderLocationID          string
	ProviderFence               pos.ProviderFence
	POSAppointmentID            string
	POSAppointmentVersion       int
	Status                      string
	CustomerName                string
	CustomerPhone               string
	CustomerEmail               string
	Service                     ServiceRef
	Staff                       StaffRef
	StaffSelectionMode          string
	Segments                    []BookingSegmentRecord
	StartTime                   time.Time
	EndTime                     time.Time
	Notes                       string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type PendingBookingRecord struct {
	SalonID             string
	Source              string
	Provider            string
	POSIdempotencyKey   string
	OperationKey        string
	RequestFingerprint  string
	RetryOfAttemptID    string
	AvailabilityQuoteID string
	SlotFingerprint     string
	ProviderFence       pos.ProviderFence
	ProcessingToken     string
	LeaseExpiresAt      time.Time
	CustomerName        string
	CustomerPhone       string
	CustomerEmail       string
	Service             ServiceRef
	Staff               StaffRef
	StaffSelectionMode  string
	Segments            []BookingSegmentRecord
	StartTime           time.Time
	EndTime             time.Time
	Notes               string
}

type ConfirmedBookingRecord struct {
	AttemptID          string
	SalonID            string
	Source             string
	Provider           string
	CustomerName       string
	CustomerPhone      string
	CustomerEmail      string
	POSCustomerID      string
	Service            ServiceRef
	Staff              StaffRef
	StaffSelectionMode string
	Segments           []BookingSegmentRecord
	StartTime          time.Time
	EndTime            time.Time
	Notes              string
	POSBookingID       string
	POSBookingVersion  int
	ProcessingToken    string
	ProviderFence      pos.ProviderFence
}

type FallbackBookingRecord struct {
	AttemptID          string
	SalonID            string
	Source             string
	Provider           string
	POSBookingID       string
	POSBookingVersion  int
	Operation          string
	CustomerName       string
	CustomerPhone      string
	CustomerEmail      string
	Service            ServiceRef
	Staff              StaffRef
	StaffSelectionMode string
	Segments           []BookingSegmentRecord
	StartTime          time.Time
	EndTime            time.Time
	Notes              string
	ErrorCode          string
	ErrorMessage       string
	ProcessingToken    string
	ProviderOutcome    string
	RetryPolicy        string
	Reconciliation     string
	Status             string
}

type PendingAppointmentActionRecord struct {
	SalonID             string
	Appointment         AppointmentActionRef
	Provider            string
	Source              string
	Segments            []BookingSegmentRecord
	RequestedStartTime  time.Time
	RequestedEndTime    time.Time
	Notes               string
	POSIdempotencyKey   string
	OperationKey        string
	RequestFingerprint  string
	RetryOfAttemptID    string
	AvailabilityQuoteID string
	SlotFingerprint     string
	ProviderFence       pos.ProviderFence
	OperationType       string
	ProcessingToken     string
	LeaseExpiresAt      time.Time
}

type RescheduledAppointmentRecord struct {
	AttemptID         string
	Appointment       AppointmentActionRef
	Staff             StaffRef
	Source            string
	Segments          []BookingSegmentRecord
	StartTime         time.Time
	EndTime           time.Time
	Notes             string
	POSBookingVersion int
	ProcessingToken   string
}

type CancelledAppointmentRecord struct {
	AttemptID         string
	Appointment       AppointmentActionRef
	Source            string
	Reason            string
	POSBookingVersion int
	ProcessingToken   string
}

type AppointmentActionFallbackRecord struct {
	AttemptID          string
	SalonID            string
	Appointment        AppointmentActionRef
	Provider           string
	POSBookingID       string
	POSBookingVersion  int
	Operation          string
	Source             string
	NotificationType   string
	Segments           []BookingSegmentRecord
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
	Notes              string
	ErrorCode          string
	ErrorMessage       string
	ProcessingToken    string
	ProviderOutcome    string
	RetryPolicy        string
	Reconciliation     string
	Status             string
}

type BookingOperationClaim struct {
	Attempt  *BookingAttempt
	Acquired bool
}

type AvailabilityQuoteRecord struct {
	SalonID                           string
	Provider                          string
	ProviderFence                     pos.ProviderFence
	RequestFingerprint                string
	OperationType                     string
	RetryOfAttemptID                  string
	TargetAppointmentID               string
	TargetAuthorityAppointmentVersion int
	ExpiresAt                         time.Time
	Slots                             []AvailabilitySlot
}

type AvailabilityQuote struct {
	ID                                string             `json:"id"`
	SalonID                           string             `json:"salon_id"`
	SchedulingAuthority               string             `json:"scheduling_authority"`
	SchedulingAuthorityVersion        int64              `json:"scheduling_authority_version"`
	AuthorityProvider                 string             `json:"authority_provider"`
	AuthorityLocationID               string             `json:"-"`
	AuthoritySnapshotGeneration       int64              `json:"-"`
	Provider                          string             `json:"provider"`
	ProviderFence                     pos.ProviderFence  `json:"-"`
	RequestFingerprint                string             `json:"-"`
	OperationType                     string             `json:"-"`
	TargetAppointmentID               string             `json:"-"`
	TargetAuthorityAppointmentVersion int                `json:"-"`
	ExpiresAt                         time.Time          `json:"expires_at"`
	ConsumedAt                        *time.Time         `json:"consumed_at,omitempty"`
	CreatedAt                         time.Time          `json:"created_at"`
	Slots                             []AvailabilitySlot `json:"slots,omitempty"`
}

type ReconciliationTask struct {
	ID               string          `json:"id"`
	SalonID          string          `json:"salon_id"`
	BookingAttemptID string          `json:"booking_attempt_id"`
	Status           string          `json:"status"`
	Resolution       string          `json:"resolution,omitempty"`
	ResolutionNote   string          `json:"resolution_note,omitempty"`
	ResolvedAt       *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Attempt          *BookingAttempt `json:"booking_attempt,omitempty"`
}

type ListReconciliationTasksResponse struct {
	Tasks   []ReconciliationTask `json:"tasks"`
	Limit   int                  `json:"limit"`
	Offset  int                  `json:"offset"`
	HasMore bool                 `json:"has_more"`
}

type ReconciliationCandidate struct {
	AppointmentID              string    `json:"appointment_id"`
	Provider                   string    `json:"provider"`
	ProviderAppointmentID      string    `json:"provider_appointment_id"`
	ProviderAppointmentVersion int       `json:"provider_appointment_version"`
	ProviderStatus             string    `json:"provider_status"`
	CustomerName               string    `json:"customer_name"`
	CustomerPhone              string    `json:"customer_phone"`
	CustomerEmail              string    `json:"customer_email,omitempty"`
	ServiceID                  string    `json:"service_id"`
	StaffID                    string    `json:"staff_id,omitempty"`
	StartTime                  time.Time `json:"start_time"`
	EndTime                    time.Time `json:"end_time"`
}

type ListReconciliationCandidatesResponse struct {
	Candidates []ReconciliationCandidate `json:"candidates"`
}

type ResolveReconciliationRequest struct {
	ActionKey                  string `json:"action_key"`
	Action                     string `json:"action"`
	ProviderAppointmentID      string `json:"provider_appointment_id"`
	ProviderAppointmentVersion int    `json:"provider_appointment_version"`
	ProviderStatus             string `json:"provider_status"`
	Note                       string `json:"note"`
	PayloadFingerprint         string `json:"-"`
}

type LeaseSweepSummary struct {
	Expired int `json:"expired"`
}

type BookingSegmentRecord struct {
	AppointmentServiceID string
	Service              ServiceRef
	Staff                StaffRef
	StaffSelectionMode   string
	GuestReference       string
	Quantity             int
	PlanVersion          int
	ScheduledStartTime   time.Time
	ScheduledEndTime     time.Time
	OccupiedStartTime    time.Time
	OccupiedEndTime      time.Time
	BufferBeforeMinutes  int
	BufferAfterMinutes   int
	ResourceAllocations  []AvailabilityResourceAllocation
	SortOrder            int
}

type BookingSegmentSnapshot struct {
	AppointmentServiceID    string                           `json:"appointment_service_id,omitempty"`
	SchedulingAuthority     string                           `json:"scheduling_authority"`
	AuthorityProvider       string                           `json:"authority_provider"`
	AuthorityServiceID      string                           `json:"-"`
	AuthorityServiceVersion int64                            `json:"-"`
	AuthorityStaffID        string                           `json:"-"`
	ServiceID               string                           `json:"service_id,omitempty"`
	POSServiceID            string                           `json:"-"`
	POSServiceVersion       int64                            `json:"-"`
	ServiceName             string                           `json:"service_name"`
	StaffID                 string                           `json:"staff_id,omitempty"`
	POSStaffID              string                           `json:"-"`
	StaffName               string                           `json:"staff_name,omitempty"`
	StaffSelectionMode      string                           `json:"staff_selection_mode"`
	GuestReference          string                           `json:"guest_reference,omitempty"`
	Quantity                int                              `json:"quantity"`
	PlanVersion             int                              `json:"plan_version,omitempty"`
	DurationMinutes         int                              `json:"duration_minutes,omitempty"`
	ScheduledStartTime      *time.Time                       `json:"scheduled_start_time,omitempty"`
	ScheduledEndTime        *time.Time                       `json:"scheduled_end_time,omitempty"`
	OccupiedStartTime       *time.Time                       `json:"occupied_start_time,omitempty"`
	OccupiedEndTime         *time.Time                       `json:"occupied_end_time,omitempty"`
	BufferBeforeMinutes     *int                             `json:"buffer_before_minutes,omitempty"`
	BufferAfterMinutes      *int                             `json:"buffer_after_minutes,omitempty"`
	ResourceAllocations     []AvailabilityResourceAllocation `json:"resource_allocations,omitempty"`
	SortOrder               int                              `json:"sort_order"`
}

type TestBookingRecord struct {
	BookingAttemptID            string    `json:"booking_attempt_id,omitempty"`
	OperationType               string    `json:"operation_type"`
	AppointmentID               string    `json:"appointment_id,omitempty"`
	Status                      string    `json:"status"`
	AppointmentStatus           string    `json:"appointment_status,omitempty"`
	SchedulingAuthority         string    `json:"scheduling_authority"`
	AuthorityProvider           string    `json:"authority_provider"`
	AuthorityAppointmentID      string    `json:"authority_appointment_id,omitempty"`
	AuthorityAppointmentVersion int       `json:"authority_appointment_version,omitempty"`
	POSBookingID                string    `json:"pos_booking_id,omitempty"`
	POSAppointmentVersion       int       `json:"pos_appointment_version,omitempty"`
	CustomerName                string    `json:"customer_name,omitempty"`
	CustomerPhone               string    `json:"customer_phone,omitempty"`
	ServiceID                   string    `json:"service_id,omitempty"`
	StaffID                     string    `json:"staff_id,omitempty"`
	StartTime                   time.Time `json:"start_time,omitempty"`
	EndTime                     time.Time `json:"end_time,omitempty"`
	ErrorCode                   string    `json:"error_code,omitempty"`
	ErrorMessage                string    `json:"error_message,omitempty"`
	ProviderOutcome             string    `json:"provider_outcome"`
	RetryPolicy                 string    `json:"retry_policy"`
	Reconciliation              string    `json:"reconciliation_status"`
	CanRetry                    bool      `json:"can_retry"`
	RetryBlockedReason          string    `json:"retry_blocked_reason,omitempty"`
	CreatedAt                   time.Time `json:"created_at,omitempty"`
	UpdatedAt                   time.Time `json:"updated_at,omitempty"`
}
