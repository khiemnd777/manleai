package booking

import "time"

const (
	SourceOwnerDashboard          = "owner_dashboard"
	SourceSquareTestBooking       = "square_test_booking"
	SourceAIConversationSimulator = "ai_conversation_simulator"
	SourceAIVoiceCall             = "ai_voice_call"

	StatusConfirmed       = "confirmed"
	StatusFallbackPending = "fallback_pending"
	StatusRescheduled     = "rescheduled"
	StatusCancelled       = "cancelled"

	NotificationTypeBookingFallback      = "booking_fallback_pending"
	NotificationTypeRescheduleFallback   = "reschedule_fallback_pending"
	NotificationTypeCancellationFallback = "cancel_fallback_pending"
)

type CreateBookingRequest struct {
	Source        string    `json:"source"`
	CustomerName  string    `json:"customer_name"`
	CustomerPhone string    `json:"customer_phone"`
	CustomerEmail string    `json:"customer_email"`
	ServiceID     string    `json:"service_id"`
	StaffID       string    `json:"staff_id"`
	StartTime     time.Time `json:"start_time"`
	Notes         string    `json:"notes"`
}

type RescheduleRequest struct {
	StartTime time.Time `json:"start_time"`
	StaffID   string    `json:"staff_id"`
	Notes     string    `json:"notes"`
	Source    string    `json:"-"`
}

type CancelRequest struct {
	Reason string `json:"reason"`
	Source string `json:"-"`
}

type BookingAttempt struct {
	ID                 string       `json:"id"`
	SalonID            string       `json:"salon_id"`
	Source             string       `json:"source"`
	Status             string       `json:"status"`
	POSProvider        string       `json:"pos_provider"`
	POSBookingID       string       `json:"pos_booking_id,omitempty"`
	CustomerName       string       `json:"customer_name"`
	CustomerPhone      string       `json:"customer_phone"`
	CustomerEmail      string       `json:"customer_email,omitempty"`
	ServiceID          string       `json:"service_id,omitempty"`
	StaffID            string       `json:"staff_id,omitempty"`
	RequestedStartTime time.Time    `json:"requested_start_time"`
	RequestedEndTime   time.Time    `json:"requested_end_time"`
	Notes              string       `json:"notes,omitempty"`
	ErrorCode          string       `json:"error_code,omitempty"`
	ErrorMessage       string       `json:"error_message,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	Appointment        *Appointment `json:"appointment,omitempty"`
}

type Appointment struct {
	ID                    string    `json:"id"`
	SalonID               string    `json:"salon_id"`
	BookingAttemptID      string    `json:"booking_attempt_id"`
	POSProvider           string    `json:"pos_provider"`
	POSAppointmentID      string    `json:"pos_appointment_id"`
	POSAppointmentVersion int       `json:"pos_appointment_version,omitempty"`
	Status                string    `json:"status"`
	CustomerName          string    `json:"customer_name"`
	CustomerPhone         string    `json:"customer_phone"`
	CustomerEmail         string    `json:"customer_email,omitempty"`
	ServiceID             string    `json:"service_id,omitempty"`
	StaffID               string    `json:"staff_id,omitempty"`
	StartTime             time.Time `json:"start_time"`
	EndTime               time.Time `json:"end_time"`
	Notes                 string    `json:"notes,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ServiceRef struct {
	ID                string
	POSProvider       string
	POSServiceID      string
	POSServiceVersion int64
	Name              string
	DurationMinutes   int
	PriceFrom         float64
}

type StaffRef struct {
	ID          string
	POSProvider string
	POSStaffID  string
	Name        string
}

type AppointmentActionRef struct {
	ID                    string
	SalonID               string
	BookingAttemptID      string
	POSProvider           string
	POSAppointmentID      string
	POSAppointmentVersion int
	Status                string
	CustomerName          string
	CustomerPhone         string
	CustomerEmail         string
	Service               ServiceRef
	Staff                 StaffRef
	StartTime             time.Time
	EndTime               time.Time
	Notes                 string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ConfirmedBookingRecord struct {
	SalonID           string
	Source            string
	Provider          string
	CustomerName      string
	CustomerPhone     string
	CustomerEmail     string
	Service           ServiceRef
	Staff             StaffRef
	StartTime         time.Time
	EndTime           time.Time
	Notes             string
	POSBookingID      string
	POSBookingVersion int
}

type FallbackBookingRecord struct {
	SalonID       string
	Source        string
	Provider      string
	Operation     string
	CustomerName  string
	CustomerPhone string
	CustomerEmail string
	Service       ServiceRef
	Staff         StaffRef
	StartTime     time.Time
	EndTime       time.Time
	Notes         string
	ErrorCode     string
	ErrorMessage  string
}

type RescheduledAppointmentRecord struct {
	Appointment       AppointmentActionRef
	Staff             StaffRef
	Source            string
	StartTime         time.Time
	EndTime           time.Time
	Notes             string
	POSBookingVersion int
}

type CancelledAppointmentRecord struct {
	Appointment       AppointmentActionRef
	Source            string
	Reason            string
	POSBookingVersion int
}

type AppointmentActionFallbackRecord struct {
	SalonID            string
	Appointment        AppointmentActionRef
	Provider           string
	Operation          string
	Source             string
	NotificationType   string
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
	Notes              string
	ErrorCode          string
	ErrorMessage       string
}

type TestBookingRecord struct {
	BookingAttemptID      string    `json:"booking_attempt_id,omitempty"`
	AppointmentID         string    `json:"appointment_id,omitempty"`
	Status                string    `json:"status"`
	AppointmentStatus     string    `json:"appointment_status,omitempty"`
	POSBookingID          string    `json:"pos_booking_id,omitempty"`
	POSAppointmentVersion int       `json:"pos_appointment_version,omitempty"`
	CustomerName          string    `json:"customer_name,omitempty"`
	CustomerPhone         string    `json:"customer_phone,omitempty"`
	ServiceID             string    `json:"service_id,omitempty"`
	StaffID               string    `json:"staff_id,omitempty"`
	StartTime             time.Time `json:"start_time,omitempty"`
	EndTime               time.Time `json:"end_time,omitempty"`
	ErrorCode             string    `json:"error_code,omitempty"`
	ErrorMessage          string    `json:"error_message,omitempty"`
	CreatedAt             time.Time `json:"created_at,omitempty"`
	UpdatedAt             time.Time `json:"updated_at,omitempty"`
}
