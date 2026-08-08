package scheduling

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type AvailabilityKind string

const (
	AvailabilityKindVerifiedSlots AvailabilityKind = "verified_slots"
	AvailabilityKindRequestOnly   AvailabilityKind = "request_only"
)

type ActionKind string

const (
	ActionKindConfirmedAppointment    ActionKind = "confirmed_appointment"
	ActionKindPendingOwnerReview      ActionKind = "pending_owner_review"
	ActionKindExternalFallbackPending ActionKind = "external_fallback_pending"
)

type OperationKind string

const (
	OperationKindBook       OperationKind = "book"
	OperationKindReschedule OperationKind = "reschedule"
	OperationKindCancel     OperationKind = "cancel"
)

type BookingMode string

const (
	BookingModeConfirmedBooking BookingMode = "confirmed_booking"
	BookingModePendingApproval  BookingMode = "pending_approval"
	BookingModeDisabled         BookingMode = "disabled"
)

type ConversationSchedulingBehavior string

const (
	ConversationSchedulingBehaviorOwnerReview              ConversationSchedulingBehavior = "owner_review"
	ConversationSchedulingBehaviorAutomaticInternalCommit  ConversationSchedulingBehavior = "automatic_internal_commit"
	ConversationSchedulingBehaviorAutomaticExternalBooking ConversationSchedulingBehavior = "automatic_external_booking"
	ConversationSchedulingBehaviorDisabled                 ConversationSchedulingBehavior = "disabled"
)

type ConversationPolicyFence struct {
	BookingMode         BookingMode
	SchedulingAuthority string
}

// AllowedConversationBookingModes and ConversationBehavior keep the
// authority × booking-mode matrix in the scheduling domain. Management APIs
// and the conversation runtime must not maintain separate compatibility rules.
func AllowedConversationBookingModes(authority string) ([]BookingMode, error) {
	if !isKnownSchedulingAuthority(authority) {
		return nil, &AuthorityNotReadyError{Authority: authority}
	}
	modes := []BookingMode{BookingModePendingApproval}
	if authority != booking.SchedulingAuthorityOwnerManual {
		modes = append(modes, BookingModeConfirmedBooking)
	}
	return append(modes, BookingModeDisabled), nil
}

func ConversationBehavior(policy ConversationPolicyFence) (ConversationSchedulingBehavior, error) {
	allowed, err := AllowedConversationBookingModes(policy.SchedulingAuthority)
	if err != nil {
		return "", err
	}
	valid := false
	for _, mode := range allowed {
		if mode == policy.BookingMode {
			valid = true
			break
		}
	}
	if !valid {
		return "", &AuthorityNotReadyError{Authority: policy.SchedulingAuthority}
	}
	switch policy.BookingMode {
	case BookingModeDisabled:
		return ConversationSchedulingBehaviorDisabled, nil
	case BookingModePendingApproval:
		return ConversationSchedulingBehaviorOwnerReview, nil
	case BookingModeConfirmedBooking:
		if policy.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar {
			return ConversationSchedulingBehaviorAutomaticInternalCommit, nil
		}
		return ConversationSchedulingBehaviorAutomaticExternalBooking, nil
	default:
		return "", &AuthorityNotReadyError{Authority: policy.SchedulingAuthority}
	}
}

// PersistedOperationOrigin distinguishes a confirming booking attempt from a
// non-confirming owner-review request. RequestTargetAuthorityPresent preserves
// legacy NULL exactly; a replay must not manufacture a target authority after
// the request fingerprint has already been committed.
type PersistedOperationOrigin struct {
	SchedulingAuthority           string
	SchedulingRequest             bool
	RequestTargetAuthority        string
	RequestTargetAuthorityPresent bool
}

type SchedulingRequestStatus string

const (
	SchedulingRequestStatusPending   SchedulingRequestStatus = "pending"
	SchedulingRequestStatusContacted SchedulingRequestStatus = "contacted"
	SchedulingRequestStatusResolved  SchedulingRequestStatus = "resolved"
	SchedulingRequestStatusDismissed SchedulingRequestStatus = "dismissed"
)

const (
	SchedulingRequestEventCreated       = "request_created"
	SchedulingRequestEventStatusChanged = "status_changed"
)

var (
	ErrInvalidSchedulingAction        = errors.New("invalid scheduling action")
	ErrInvalidSchedulingResult        = errors.New("invalid scheduling result")
	ErrSchedulingRequestVersion       = errors.New("scheduling request version conflict")
	ErrSchedulingRequestTerminal      = errors.New("scheduling request is terminal")
	ErrSchedulingRequestTransition    = errors.New("invalid scheduling request status transition")
	ErrConversationSchedulingDisabled = errors.New("conversation scheduling is disabled")
)

type ActionSegment struct {
	ServiceID          string    `json:"service_id"`
	StaffID            string    `json:"staff_id,omitempty"`
	StaffSelectionMode string    `json:"staff_selection_mode"`
	GuestReference     string    `json:"guest_reference,omitempty"`
	Quantity           int       `json:"quantity"`
	RequestedStartTime time.Time `json:"requested_start_time,omitempty"`
	RequestedEndTime   time.Time `json:"requested_end_time,omitempty"`
}

type ActionRequest struct {
	OperationType                             OperationKind   `json:"operation_type"`
	OperationKey                              string          `json:"operation_key"`
	RetryOfAttemptID                          string          `json:"retry_of_attempt_id,omitempty"`
	AvailabilityQuoteID                       string          `json:"availability_quote_id,omitempty"`
	SlotFingerprint                           string          `json:"slot_fingerprint,omitempty"`
	Source                                    string          `json:"-"`
	CallSessionID                             string          `json:"call_session_id,omitempty"`
	CustomerName                              string          `json:"customer_name"`
	CustomerPhone                             string          `json:"customer_phone"`
	CustomerEmail                             string          `json:"customer_email,omitempty"`
	Segments                                  []ActionSegment `json:"segments,omitempty"`
	RequestedStartTime                        time.Time       `json:"requested_start_time,omitempty"`
	RequestedEndTime                          time.Time       `json:"requested_end_time,omitempty"`
	RequestedTimezone                         string          `json:"requested_timezone,omitempty"`
	PartySize                                 int             `json:"party_size,omitempty"`
	Notes                                     string          `json:"notes,omitempty"`
	TargetAppointmentID                       string          `json:"target_appointment_id,omitempty"`
	TargetAuthority                           string          `json:"target_scheduling_authority,omitempty"`
	ExpectedTargetAuthorityAppointmentVersion int             `json:"expected_target_authority_appointment_version,omitempty"`
	TargetDescription                         string          `json:"target_description,omitempty"`
}

type AvailabilityResult struct {
	Kind                              AvailabilityKind            `json:"kind"`
	SchedulingAuthority               string                      `json:"scheduling_authority"`
	TargetAuthorityAppointmentVersion int                         `json:"target_authority_appointment_version,omitempty"`
	VerifiedSlots                     *booking.AvailabilityResult `json:"verified_slots,omitempty"`
}

type ConfirmedResourceAllocation struct {
	ResourcePoolID string `json:"resource_pool_id"`
	ResourceName   string `json:"resource_name"`
	UnitsAllocated int    `json:"units_allocated"`
}

type ConfirmedAppointmentSegment struct {
	AppointmentServiceID string                        `json:"appointment_service_id"`
	GuestReference       string                        `json:"guest_reference,omitempty"`
	ServiceID            string                        `json:"service_id"`
	StaffID              string                        `json:"staff_id"`
	StaffSelectionMode   string                        `json:"staff_selection_mode"`
	Quantity             int                           `json:"quantity"`
	ScheduledStartTime   time.Time                     `json:"scheduled_start_time"`
	ScheduledEndTime     time.Time                     `json:"scheduled_end_time"`
	OccupiedStartTime    time.Time                     `json:"occupied_start_time"`
	OccupiedEndTime      time.Time                     `json:"occupied_end_time"`
	BufferBeforeMinutes  int                           `json:"buffer_before_minutes"`
	BufferAfterMinutes   int                           `json:"buffer_after_minutes"`
	ResourceAllocations  []ConfirmedResourceAllocation `json:"resource_allocations"`
}

type ConfirmedAppointmentResult struct {
	AppointmentID     string                        `json:"appointment_id"`
	BookingAttemptID  string                        `json:"booking_attempt_id,omitempty"`
	AppointmentStatus string                        `json:"appointment_status,omitempty"`
	ActiveChildCount  int                           `json:"active_child_count"`
	ExternalAttemptID string                        `json:"external_attempt_id,omitempty"`
	Appointment       *booking.Appointment          `json:"appointment,omitempty"`
	ExternalAttempt   *booking.BookingAttempt       `json:"external_attempt,omitempty"`
	Children          []ConfirmedAppointmentSegment `json:"children,omitempty"`
}

type PendingOwnerReviewResult struct {
	SchedulingRequestID string             `json:"scheduling_request_id"`
	Status              string             `json:"status"`
	Version             int                `json:"version"`
	Request             *SchedulingRequest `json:"request,omitempty"`
}

type ExternalFallbackPendingResult struct {
	ExternalAttemptID string                  `json:"external_attempt_id"`
	ExternalAttempt   *booking.BookingAttempt `json:"external_attempt,omitempty"`
}

type ActionResult struct {
	Kind                              ActionKind                     `json:"kind"`
	OperationType                     OperationKind                  `json:"operation_type"`
	SchedulingAuthority               string                         `json:"scheduling_authority"`
	TargetAuthorityAppointmentVersion int                            `json:"target_authority_appointment_version,omitempty"`
	AuthorityAppointmentVersion       int                            `json:"authority_appointment_version,omitempty"`
	Replayed                          bool                           `json:"replayed,omitempty"`
	ConfirmedAppointment              *ConfirmedAppointmentResult    `json:"confirmed_appointment,omitempty"`
	PendingOwnerReview                *PendingOwnerReviewResult      `json:"pending_owner_review,omitempty"`
	ExternalFallbackPending           *ExternalFallbackPendingResult `json:"external_fallback_pending,omitempty"`
}

type SchedulingRequestSegment struct {
	ID                  string     `json:"id"`
	SchedulingRequestID string     `json:"scheduling_request_id"`
	ServiceID           string     `json:"service_id"`
	ServiceName         string     `json:"service_name"`
	StaffID             string     `json:"staff_id,omitempty"`
	StaffName           string     `json:"staff_name,omitempty"`
	StaffSelectionMode  string     `json:"staff_selection_mode"`
	GuestReference      string     `json:"guest_reference,omitempty"`
	Quantity            int        `json:"quantity"`
	DurationMinutes     int        `json:"duration_minutes"`
	RequestedStartTime  *time.Time `json:"requested_start_time,omitempty"`
	RequestedEndTime    *time.Time `json:"requested_end_time,omitempty"`
	SortOrder           int        `json:"sort_order"`
	Redacted            bool       `json:"redacted"`
	RedactedAt          *time.Time `json:"redacted_at,omitempty"`
	RedactionVersion    int        `json:"redaction_version,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type SchedulingRequestEvent struct {
	ID                  string          `json:"id"`
	SchedulingRequestID string          `json:"scheduling_request_id"`
	ActionKey           string          `json:"action_key"`
	EventType           string          `json:"event_type"`
	RequestVersion      int             `json:"request_version"`
	ActorUserID         string          `json:"actor_user_id,omitempty"`
	Payload             json.RawMessage `json:"payload"`
	Redacted            bool            `json:"redacted"`
	RedactedAt          *time.Time      `json:"redacted_at,omitempty"`
	RedactionVersion    int             `json:"redaction_version,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

type SchedulingRequest struct {
	ID                  string                     `json:"id"`
	SalonID             string                     `json:"salon_id"`
	SchedulingAuthority string                     `json:"scheduling_authority"`
	OperationKey        string                     `json:"operation_key"`
	OperationType       OperationKind              `json:"operation_type"`
	Status              SchedulingRequestStatus    `json:"status"`
	Version             int                        `json:"version"`
	Source              string                     `json:"source"`
	CallSessionID       string                     `json:"call_session_id,omitempty"`
	TargetAppointmentID string                     `json:"target_appointment_id,omitempty"`
	TargetAuthority     string                     `json:"target_scheduling_authority,omitempty"`
	TargetDescription   string                     `json:"target_description,omitempty"`
	CustomerName        string                     `json:"customer_name"`
	CustomerPhone       string                     `json:"customer_phone"`
	CustomerEmail       string                     `json:"customer_email,omitempty"`
	RequestedStartTime  *time.Time                 `json:"requested_start_time,omitempty"`
	RequestedEndTime    *time.Time                 `json:"requested_end_time,omitempty"`
	RequestedTimezone   string                     `json:"requested_timezone,omitempty"`
	PartySize           int                        `json:"party_size,omitempty"`
	Notes               string                     `json:"notes,omitempty"`
	ResolutionReason    string                     `json:"resolution_reason,omitempty"`
	Redacted            bool                       `json:"redacted"`
	RedactedAt          *time.Time                 `json:"redacted_at,omitempty"`
	RedactionVersion    int                        `json:"redaction_version,omitempty"`
	Segments            []SchedulingRequestSegment `json:"segments,omitempty"`
	Events              []SchedulingRequestEvent   `json:"events,omitempty"`
	ContactedAt         *time.Time                 `json:"contacted_at,omitempty"`
	ResolvedAt          *time.Time                 `json:"resolved_at,omitempty"`
	DismissedAt         *time.Time                 `json:"dismissed_at,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

type ListSchedulingRequestsResponse struct {
	SchedulingRequests []SchedulingRequest `json:"scheduling_requests"`
	Limit              int                 `json:"limit"`
	Offset             int                 `json:"offset"`
	HasMore            bool                `json:"has_more"`
}

type TransitionSchedulingRequest struct {
	ActionKey        string                  `json:"action_key"`
	ExpectedVersion  int                     `json:"expected_version"`
	Status           SchedulingRequestStatus `json:"status"`
	ResolutionReason string                  `json:"resolution_reason,omitempty"`
	Note             string                  `json:"note,omitempty"`
}
