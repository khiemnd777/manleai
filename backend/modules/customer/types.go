package customer

import (
	"context"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

const (
	SourceAppointment    = "appointment"
	SourceBookingAttempt = "booking_attempt"
	SourceCall           = "call"
	SourceHandoff        = "handoff"
)

var (
	ErrValidation          = errors.New("customer validation failed")
	ErrNotFound            = pos.ErrNotFound
	ErrProviderUnavailable = errors.New("pos provider unavailable")
)

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Record, error)
}

type Record struct {
	Key                   string     `json:"key"`
	Name                  string     `json:"name,omitempty"`
	Phone                 string     `json:"phone,omitempty"`
	Email                 string     `json:"email,omitempty"`
	LastActivityAt        time.Time  `json:"last_activity_at"`
	LastActivitySource    string     `json:"last_activity_source"`
	LastOutcome           string     `json:"last_outcome"`
	ConfirmedAppointments int        `json:"confirmed_appointments"`
	PendingRequests       int        `json:"pending_requests"`
	CallCount             int        `json:"call_count"`
	HandoffCount          int        `json:"handoff_count"`
	AppointmentIDs        []string   `json:"appointment_ids,omitempty"`
	BookingAttemptIDs     []string   `json:"booking_attempt_ids,omitempty"`
	CallSessionIDs        []string   `json:"call_session_ids,omitempty"`
	LatestAppointmentAt   *time.Time `json:"latest_appointment_at,omitempty"`
	LatestRequestAt       *time.Time `json:"latest_request_at,omitempty"`
}

type Summary struct {
	TotalKnownCustomers    int        `json:"total_known_customers"`
	ConfirmedAppointments  int        `json:"confirmed_appointments"`
	PendingRequests        int        `json:"pending_requests"`
	CustomersWithCalls     int        `json:"customers_with_calls"`
	LastCustomerActivityAt *time.Time `json:"last_customer_activity_at,omitempty"`
}

type ListResponse struct {
	Customers []Record `json:"customers"`
	Summary   Summary  `json:"summary"`
}

type SearchResponse struct {
	Customer *pos.Customer `json:"customer,omitempty"`
	Found    bool          `json:"found"`
	Provider string        `json:"provider"`
}
