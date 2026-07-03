package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	ChannelSimulator = "simulator"
	ChannelPhone     = "phone"

	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusHandoff   = "handoff"
	StatusFailed    = "failed"

	IntentUnknown = "unknown"
	IntentBooking = "booking"
	IntentHandoff = "handoff"

	OutcomeCollecting             = "collecting"
	OutcomeBookingConfirmed       = "booking_confirmed"
	OutcomeBookingFallbackPending = "booking_fallback_pending"
	OutcomeHandoffRequested       = "handoff_requested"
	OutcomeAIDisabled             = "ai_disabled"
	OutcomeFailed                 = "failed"

	LifecycleActive   = "active"
	LifecycleArchived = "archived"
	LifecycleRedacted = "redacted"

	SpeakerAI       = "ai"
	SpeakerCustomer = "customer"
	SpeakerTool     = "tool"

	HandoffReasonHumanRequested             = "human_requested"
	HandoffReasonAIBookingDisabled          = "ai_booking_disabled"
	HandoffReasonBookingUnavailable         = "booking_unavailable"
	HandoffReasonCustomerDetailsUnavailable = "customer_details_unavailable"
	HandoffReasonGroupBooking               = "group_booking"
)

var (
	ErrValidation    = errors.New("conversation validation failed")
	ErrNotFound      = errors.New("conversation record not found")
	ErrSessionClosed = errors.New("conversation session is closed")
	ErrLifecycle     = errors.New("conversation lifecycle action is not allowed")
)

type BookingTool interface {
	AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
}

type ReplyGenerator interface {
	GenerateReply(ctx context.Context, req ReplyGenerationRequest) (ReplyGenerationResult, error)
}

type Store interface {
	GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error)
	CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error)
	GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*Session, bool, error)
	ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int, offset int) ([]Session, error)
	ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]WebhookEventLog, error)
	ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListStaffAssignmentStats(ctx context.Context, salonID string, staffIDs []string, from time.Time, to time.Time) (map[string]StaffAssignmentStat, error)
	ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error)
	ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error)
	SaveTurn(ctx context.Context, record TurnRecord) (*Session, error)
}

type StartSessionRequest struct {
	Channel       string `json:"channel"`
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	CustomerEmail string `json:"customer_email"`
}

type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
	HasMore  bool      `json:"has_more"`
}

type StartPhoneCallRequest struct {
	Provider       string
	ProviderCallID string
	FromPhone      string
	ToPhone        string
}

type MessageRequest struct {
	Message  string `json:"message"`
	EventKey string `json:"event_key,omitempty"`
}

type ReplyGenerationRequest struct {
	SalonID              string
	SessionID            string
	Channel              string
	Intent               string
	Outcome              string
	CustomerMessage      string
	SafeReply            string
	SalonName            string
	AITone               string
	BookingConfirmed     bool
	FallbackOrHandoff    bool
	MissingBookingField  string
	KnownBookingFields   []string
	NextRequiredField    string
	SelectedServiceNames []string
	Summary              string
	KnowledgeContext     string
}

type ReplyGenerationResult struct {
	Message    string
	Confidence float64
	Handoff    bool
	Reason     string
}

type TranscriptionContext struct {
	Prompt string
}

type RuntimeConfig struct {
	SalonName      string
	Timezone       string
	AIEnabled      bool
	HandoffPhone   string
	HandoffEnabled bool
	AIGreeting     string
	AITone         string
}

type ServiceOption struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	DurationMinutes int     `json:"duration_minutes"`
	PriceFrom       float64 `json:"price_from,omitempty"`
	PriceDisplay    string  `json:"price_display,omitempty"`
}

type ServiceAlias struct {
	ID              string  `json:"id"`
	ServiceID       string  `json:"service_id"`
	ServiceName     string  `json:"service_name"`
	Alias           string  `json:"alias"`
	NormalizedAlias string  `json:"normalized_alias"`
	Source          string  `json:"source"`
	Confidence      float64 `json:"confidence"`
}

type StaffOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AIBookable bool   `json:"ai_bookable"`
}

type StaffAssignmentStat struct {
	StaffID        string     `json:"staff_id"`
	AssignedCount  int        `json:"assigned_count"`
	LastAssignedAt *time.Time `json:"last_assigned_at,omitempty"`
}

type KnowledgeSnippet struct {
	Title    string
	Category string
	Body     string
}

type Session struct {
	ID                 string                          `json:"id"`
	SalonID            string                          `json:"salon_id"`
	Channel            string                          `json:"channel"`
	Provider           string                          `json:"provider,omitempty"`
	ProviderCallID     string                          `json:"provider_call_id,omitempty"`
	InboundPhone       string                          `json:"inbound_phone,omitempty"`
	OutboundPhone      string                          `json:"outbound_phone,omitempty"`
	Status             string                          `json:"status"`
	Intent             string                          `json:"intent"`
	Outcome            string                          `json:"outcome"`
	CustomerName       string                          `json:"customer_name,omitempty"`
	CustomerPhone      string                          `json:"customer_phone,omitempty"`
	CustomerEmail      string                          `json:"customer_email,omitempty"`
	ServiceID          string                          `json:"service_id,omitempty"`
	ServiceName        string                          `json:"service_name,omitempty"`
	StaffID            string                          `json:"staff_id,omitempty"`
	StaffName          string                          `json:"staff_name,omitempty"`
	StaffSelectionMode string                          `json:"staff_selection_mode,omitempty"`
	RequestedDate      string                          `json:"requested_date,omitempty"`
	RequestedStartTime *time.Time                      `json:"requested_start_time,omitempty"`
	OfferedSlots       []OfferedSlot                   `json:"offered_slots,omitempty"`
	BookingSegments    []booking.BookingSegmentRequest `json:"booking_segments,omitempty"`
	BookingAttemptID   string                          `json:"booking_attempt_id,omitempty"`
	AppointmentID      string                          `json:"appointment_id,omitempty"`
	Summary            string                          `json:"summary,omitempty"`
	LifecycleStatus    string                          `json:"lifecycle_status"`
	ArchivedAt         *time.Time                      `json:"archived_at,omitempty"`
	RedactedAt         *time.Time                      `json:"redacted_at,omitempty"`
	RetentionExpiresAt time.Time                       `json:"retention_expires_at"`
	StartedAt          time.Time                       `json:"started_at"`
	EndedAt            *time.Time                      `json:"ended_at,omitempty"`
	CreatedAt          time.Time                       `json:"created_at"`
	UpdatedAt          time.Time                       `json:"updated_at"`
	Transcript         []TranscriptMessage             `json:"transcript,omitempty"`
	Handoff            *HandoffRequest                 `json:"handoff,omitempty"`
}

type OfferedSlot struct {
	StartTime          time.Time            `json:"start_time"`
	EndTime            time.Time            `json:"end_time"`
	StaffID            string               `json:"staff_id"`
	StaffName          string               `json:"staff_name"`
	StaffSelectionMode string               `json:"staff_selection_mode,omitempty"`
	Segments           []OfferedSlotSegment `json:"segments,omitempty"`
}

type OfferedSlotSegment struct {
	ServiceID          string `json:"service_id"`
	ServiceName        string `json:"service_name,omitempty"`
	StaffID            string `json:"staff_id,omitempty"`
	StaffName          string `json:"staff_name,omitempty"`
	StaffSelectionMode string `json:"staff_selection_mode"`
	DurationMinutes    int    `json:"duration_minutes,omitempty"`
}

type TranscriptMessage struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	SalonID   string         `json:"salon_id"`
	Speaker   string         `json:"speaker"`
	Body      string         `json:"body"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Sequence  int            `json:"sequence"`
	CreatedAt time.Time      `json:"created_at"`
}

type HandoffRequest struct {
	ID            string     `json:"id"`
	SalonID       string     `json:"salon_id"`
	CallSessionID string     `json:"call_session_id"`
	Status        string     `json:"status"`
	Reason        string     `json:"reason"`
	CustomerName  string     `json:"customer_name,omitempty"`
	CustomerPhone string     `json:"customer_phone,omitempty"`
	Summary       string     `json:"summary"`
	CreatedAt     time.Time  `json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

type WebhookEventLog struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider"`
	ProviderCallID string    `json:"provider_call_id,omitempty"`
	EventType      string    `json:"event_type"`
	Stage          string    `json:"stage,omitempty"`
	StreamSID      string    `json:"stream_sid,omitempty"`
	StreamEvent    string    `json:"stream_event,omitempty"`
	StreamError    string    `json:"stream_error,omitempty"`
	Error          string    `json:"error,omitempty"`
	Redacted       bool      `json:"redacted,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type NewSessionRecord struct {
	SalonID        string
	OwnerUserID    string
	Channel        string
	Provider       string
	ProviderCallID string
	InboundPhone   string
	OutboundPhone  string
	CustomerName   string
	CustomerPhone  string
	CustomerEmail  string
	InitialReply   string
}

type TurnRecord struct {
	SalonID          string
	OwnerUserID      string
	Session          Session
	CustomerMessage  string
	ToolMessage      string
	AIMessage        string
	EventKey         string
	CustomerMetadata map[string]any
	ToolMetadata     map[string]any
	AIMetadata       map[string]any
	Update           SessionUpdate
	Handoff          *HandoffRecord
}

type SessionUpdate struct {
	Status             string
	Intent             string
	Outcome            string
	CustomerName       string
	CustomerPhone      string
	CustomerEmail      string
	ServiceID          string
	StaffID            string
	StaffSelectionMode string
	RequestedDate      string
	RequestedStartTime *time.Time
	OfferedSlots       []OfferedSlot
	BookingSegments    []booking.BookingSegmentRequest
	BookingAttemptID   string
	AppointmentID      string
	Summary            string
	EndSession         bool
}

type HandoffRecord struct {
	Reason        string
	CustomerName  string
	CustomerPhone string
	Summary       string
}
