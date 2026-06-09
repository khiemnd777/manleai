package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	ChannelSimulator = "simulator"

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

	SpeakerAI       = "ai"
	SpeakerCustomer = "customer"
	SpeakerTool     = "tool"

	HandoffReasonHumanRequested     = "human_requested"
	HandoffReasonAIBookingDisabled  = "ai_booking_disabled"
	HandoffReasonBookingUnavailable = "booking_unavailable"
)

var (
	ErrValidation    = errors.New("conversation validation failed")
	ErrNotFound      = errors.New("conversation record not found")
	ErrSessionClosed = errors.New("conversation session is closed")
)

type BookingTool interface {
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
}

type Store interface {
	GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error)
	CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error)
	GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	ListSessions(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Session, error)
	ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	SaveTurn(ctx context.Context, record TurnRecord) (*Session, error)
}

type StartSessionRequest struct {
	Channel       string `json:"channel"`
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	CustomerEmail string `json:"customer_email"`
}

type MessageRequest struct {
	Message string `json:"message"`
}

type RuntimeConfig struct {
	SalonName      string
	Timezone       string
	AIEnabled      bool
	HandoffPhone   string
	HandoffEnabled bool
	AIGreeting     string
}

type ServiceOption struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	DurationMinutes int     `json:"duration_minutes"`
	PriceFrom       float64 `json:"price_from,omitempty"`
	PriceDisplay    string  `json:"price_display,omitempty"`
}

type StaffOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Session struct {
	ID                 string              `json:"id"`
	SalonID            string              `json:"salon_id"`
	Channel            string              `json:"channel"`
	Status             string              `json:"status"`
	Intent             string              `json:"intent"`
	Outcome            string              `json:"outcome"`
	CustomerName       string              `json:"customer_name,omitempty"`
	CustomerPhone      string              `json:"customer_phone,omitempty"`
	CustomerEmail      string              `json:"customer_email,omitempty"`
	ServiceID          string              `json:"service_id,omitempty"`
	ServiceName        string              `json:"service_name,omitempty"`
	StaffID            string              `json:"staff_id,omitempty"`
	StaffName          string              `json:"staff_name,omitempty"`
	RequestedStartTime *time.Time          `json:"requested_start_time,omitempty"`
	BookingAttemptID   string              `json:"booking_attempt_id,omitempty"`
	AppointmentID      string              `json:"appointment_id,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	StartedAt          time.Time           `json:"started_at"`
	EndedAt            *time.Time          `json:"ended_at,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	Transcript         []TranscriptMessage `json:"transcript,omitempty"`
	Handoff            *HandoffRequest     `json:"handoff,omitempty"`
}

type TranscriptMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	SalonID   string    `json:"salon_id"`
	Speaker   string    `json:"speaker"`
	Body      string    `json:"body"`
	Sequence  int       `json:"sequence"`
	CreatedAt time.Time `json:"created_at"`
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

type NewSessionRecord struct {
	SalonID       string
	OwnerUserID   string
	Channel       string
	CustomerName  string
	CustomerPhone string
	CustomerEmail string
	InitialReply  string
}

type TurnRecord struct {
	SalonID         string
	OwnerUserID     string
	Session         Session
	CustomerMessage string
	ToolMessage     string
	AIMessage       string
	Update          SessionUpdate
	Handoff         *HandoffRecord
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
	RequestedStartTime *time.Time
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
