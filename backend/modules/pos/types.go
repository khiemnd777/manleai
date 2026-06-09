package pos

import (
	"context"
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
)

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

type Service struct {
	ID                string  `json:"id,omitempty"`
	SalonID           string  `json:"salon_id,omitempty"`
	POSProvider       string  `json:"pos_provider"`
	POSServiceID      string  `json:"pos_service_id"`
	POSServiceVersion int64   `json:"pos_service_version,omitempty"`
	Name              string  `json:"name"`
	Description       string  `json:"description,omitempty"`
	AIDescription     string  `json:"ai_description,omitempty"`
	DurationMinutes   int     `json:"duration_minutes"`
	PriceFrom         float64 `json:"price_from,omitempty"`
	PriceDisplay      string  `json:"price_display,omitempty"`
	AIBookable        bool    `json:"ai_bookable"`
	Active            bool    `json:"active"`
}

type StaffMember struct {
	ID          string `json:"id,omitempty"`
	SalonID     string `json:"salon_id,omitempty"`
	POSProvider string `json:"pos_provider"`
	POSStaffID  string `json:"pos_staff_id"`
	Name        string `json:"name"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
	AIBookable  bool   `json:"ai_bookable"`
	Active      bool   `json:"active"`
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
}

type Appointment struct {
	ID                    string    `json:"id,omitempty"`
	POSAppointmentID      string    `json:"pos_appointment_id"`
	POSAppointmentVersion int       `json:"pos_appointment_version,omitempty"`
	StartTime             time.Time `json:"start_time"`
	EndTime               time.Time `json:"end_time"`
	Status                string    `json:"status"`
}

type CreateCustomerInput struct {
	Name  string
	Phone string
	Email string
}

type AvailabilityInput struct {
	ServiceID       string
	StaffID         string
	PreferredDate   string
	DurationMinutes int
}

type CreateAppointmentInput struct {
	CustomerID      string
	ServiceID       string
	ServiceVersion  int64
	StaffID         string
	StartTime       time.Time
	DurationMinutes int
	Notes           string
}

type RescheduleInput struct {
	BookingVersion  int
	ServiceID       string
	ServiceVersion  int64
	StaffID         string
	StartTime       time.Time
	DurationMinutes int
	Notes           string
}

type CancelInput struct {
	BookingVersion int
	Reason         string
}

type POSError struct {
	SalonID      string
	Provider     string
	Operation    string
	ErrorCode    string
	ErrorMessage string
	Payload      []byte
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

type OAuthState struct {
	SalonID   string
	Provider  string
	StateHash string
	NonceHash string
	ExpiresAt time.Time
}
