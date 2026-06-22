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
	ErrDuplicate           = errors.New("customer duplicate")
	ErrNotFound            = pos.ErrNotFound
	ErrProviderUnavailable = errors.New("pos provider unavailable")
)

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Record, error)
	CreateCustomer(ctx context.Context, salonID string, ownerUserID string, input Mutation) (*Record, error)
	UpdateCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string, input Mutation) (*Record, error)
	ArchiveCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string) (*Record, error)
}

type Record struct {
	ID                    string     `json:"id,omitempty"`
	SalonID               string     `json:"salon_id,omitempty"`
	Key                   string     `json:"key"`
	Name                  string     `json:"name,omitempty"`
	Phone                 string     `json:"phone,omitempty"`
	Email                 string     `json:"email,omitempty"`
	Notes                 string     `json:"notes,omitempty"`
	Active                bool       `json:"active"`
	SyncStatus            string     `json:"sync_status"`
	ArchivedAt            *time.Time `json:"archived_at,omitempty"`
	LastSyncedAt          *time.Time `json:"last_synced_at,omitempty"`
	SyncError             string     `json:"sync_error,omitempty"`
	Source                string     `json:"source"`
	POSLinked             bool       `json:"pos_linked"`
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

type WriteRequest struct {
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Email  string `json:"email"`
	Notes  string `json:"notes"`
	Active *bool  `json:"active"`
}

type Mutation struct {
	Name            string
	Phone           string
	NormalizedPhone string
	Email           string
	NormalizedEmail string
	Notes           string
	Active          bool
}

type MutationResponse struct {
	Customer Record `json:"customer"`
}

type SearchResponse struct {
	Customer *pos.Customer `json:"customer,omitempty"`
	Found    bool          `json:"found"`
	Provider string        `json:"provider"`
}
