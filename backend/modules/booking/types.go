package booking

import "time"

const (
	SourceOwnerDashboard          = "owner_dashboard"
	SourceSquareTestBooking       = "square_test_booking"
	SourceAIConversationSimulator = "ai_conversation_simulator"
	SourceAIVoiceCall             = "ai_voice_call"

	StatusPOSPending      = "pos_pending"
	StatusConfirmed       = "confirmed"
	StatusFallbackPending = "fallback_pending"
	StatusRescheduled     = "rescheduled"
	StatusCancelled       = "cancelled"

	NotificationTypeBookingFallback      = "booking_fallback_pending"
	NotificationTypeRescheduleFallback   = "reschedule_fallback_pending"
	NotificationTypeCancellationFallback = "cancel_fallback_pending"

	StaffSelectionSpecific = "specific"
	StaffSelectionAnyone   = "anyone"
)

type CreateBookingRequest struct {
	Source             string                  `json:"source"`
	CustomerName       string                  `json:"customer_name"`
	CustomerPhone      string                  `json:"customer_phone"`
	CustomerEmail      string                  `json:"customer_email"`
	ServiceID          string                  `json:"service_id"`
	StaffID            string                  `json:"staff_id"`
	StaffSelectionMode string                  `json:"staff_selection_mode"`
	Segments           []BookingSegmentRequest `json:"segments"`
	StartTime          time.Time               `json:"start_time"`
	Notes              string                  `json:"notes"`
}

type BookingSegmentRequest struct {
	ServiceID          string `json:"service_id"`
	StaffID            string `json:"staff_id"`
	StaffSelectionMode string `json:"staff_selection_mode"`
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

type AvailabilityRequest struct {
	ServiceID          string                  `json:"service_id"`
	StaffID            string                  `json:"staff_id"`
	StaffSelectionMode string                  `json:"staff_selection_mode"`
	Segments           []BookingSegmentRequest `json:"segments"`
	PreferredDate      string                  `json:"preferred_date"`
	Limit              int                     `json:"limit"`
}

type AvailabilityResult struct {
	ServiceID          string                `json:"service_id"`
	ServiceName        string                `json:"service_name"`
	StaffID            string                `json:"staff_id,omitempty"`
	StaffName          string                `json:"staff_name,omitempty"`
	StaffSelectionMode string                `json:"staff_selection_mode"`
	Segments           []AvailabilitySegment `json:"segments,omitempty"`
	PreferredDate      string                `json:"preferred_date"`
	DurationMinutes    int                   `json:"duration_minutes"`
	Timezone           string                `json:"timezone"`
	Slots              []AvailabilitySlot    `json:"slots"`
}

type AvailabilitySlot struct {
	StartTime          time.Time             `json:"start_time"`
	EndTime            time.Time             `json:"end_time"`
	StaffID            string                `json:"staff_id,omitempty"`
	StaffName          string                `json:"staff_name,omitempty"`
	StaffSelectionMode string                `json:"staff_selection_mode"`
	Segments           []AvailabilitySegment `json:"segments,omitempty"`
}

type AvailabilitySegment struct {
	ServiceID          string `json:"service_id"`
	ServiceName        string `json:"service_name"`
	StaffID            string `json:"staff_id,omitempty"`
	StaffName          string `json:"staff_name,omitempty"`
	StaffSelectionMode string `json:"staff_selection_mode"`
	DurationMinutes    int    `json:"duration_minutes"`
}

type BookingAttempt struct {
	ID                 string                   `json:"id"`
	SalonID            string                   `json:"salon_id"`
	Source             string                   `json:"source"`
	Status             string                   `json:"status"`
	POSProvider        string                   `json:"pos_provider"`
	POSBookingID       string                   `json:"pos_booking_id,omitempty"`
	POSIdempotencyKey  string                   `json:"-"`
	CustomerName       string                   `json:"customer_name"`
	CustomerPhone      string                   `json:"customer_phone"`
	CustomerEmail      string                   `json:"customer_email,omitempty"`
	ServiceID          string                   `json:"service_id,omitempty"`
	StaffID            string                   `json:"staff_id,omitempty"`
	StaffSelectionMode string                   `json:"staff_selection_mode"`
	Segments           []BookingSegmentSnapshot `json:"segments,omitempty"`
	RequestedStartTime time.Time                `json:"requested_start_time"`
	RequestedEndTime   time.Time                `json:"requested_end_time"`
	Notes              string                   `json:"notes,omitempty"`
	ErrorCode          string                   `json:"error_code,omitempty"`
	ErrorMessage       string                   `json:"error_message,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	Appointment        *Appointment             `json:"appointment,omitempty"`
}

type Appointment struct {
	ID                    string                   `json:"id"`
	SalonID               string                   `json:"salon_id"`
	BookingAttemptID      string                   `json:"booking_attempt_id"`
	POSProvider           string                   `json:"pos_provider"`
	POSAppointmentID      string                   `json:"pos_appointment_id"`
	POSAppointmentVersion int                      `json:"pos_appointment_version,omitempty"`
	Status                string                   `json:"status"`
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
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
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

type Schedule struct {
	Timezone      string
	BusinessHours []BusinessHour
}

type BusinessHour struct {
	DayOfWeek int
	OpenTime  string
	CloseTime string
	IsClosed  bool
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
	StaffSelectionMode    string
	Segments              []BookingSegmentRecord
	StartTime             time.Time
	EndTime               time.Time
	Notes                 string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type PendingBookingRecord struct {
	SalonID            string
	Source             string
	Provider           string
	POSIdempotencyKey  string
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
}

type ConfirmedBookingRecord struct {
	AttemptID          string
	SalonID            string
	Source             string
	Provider           string
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
	POSBookingID       string
	POSBookingVersion  int
}

type FallbackBookingRecord struct {
	AttemptID          string
	SalonID            string
	Source             string
	Provider           string
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
}

type PendingAppointmentActionRecord struct {
	SalonID            string
	Appointment        AppointmentActionRef
	Provider           string
	Source             string
	Segments           []BookingSegmentRecord
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
	Notes              string
	POSIdempotencyKey  string
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
}

type CancelledAppointmentRecord struct {
	AttemptID         string
	Appointment       AppointmentActionRef
	Source            string
	Reason            string
	POSBookingVersion int
}

type AppointmentActionFallbackRecord struct {
	AttemptID          string
	SalonID            string
	Appointment        AppointmentActionRef
	Provider           string
	Operation          string
	Source             string
	NotificationType   string
	Segments           []BookingSegmentRecord
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
	Notes              string
	ErrorCode          string
	ErrorMessage       string
}

type BookingSegmentRecord struct {
	Service            ServiceRef
	Staff              StaffRef
	StaffSelectionMode string
	SortOrder          int
}

type BookingSegmentSnapshot struct {
	ServiceID          string `json:"service_id,omitempty"`
	ServiceName        string `json:"service_name"`
	StaffID            string `json:"staff_id,omitempty"`
	StaffName          string `json:"staff_name,omitempty"`
	StaffSelectionMode string `json:"staff_selection_mode"`
	DurationMinutes    int    `json:"duration_minutes,omitempty"`
	SortOrder          int    `json:"sort_order"`
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
