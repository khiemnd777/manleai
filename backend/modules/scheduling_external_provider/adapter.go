package scheduling_external_provider

import (
	"context"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type bookingService interface {
	AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
	RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error)
	Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error)
	Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error)
}

// Adapter preserves the existing external-provider booking behavior exactly.
// Authority selection happens before this boundary in scheduling.Service.
type Adapter struct {
	service bookingService
}

func NewAdapter(service *booking.Service) *Adapter {
	return &Adapter{service: service}
}

func newAdapter(service bookingService) *Adapter {
	return &Adapter{service: service}
}

func (a *Adapter) SchedulingAuthority() string {
	return booking.SchedulingAuthorityExternalProvider
}

func (a *Adapter) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	return a.service.AvailableSlots(ctx, salonID, ownerUserID, req)
}

func (a *Adapter) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	return a.service.Create(ctx, salonID, ownerUserID, req)
}

func (a *Adapter) RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	return a.service.RescheduleCandidates(ctx, salonID, ownerUserID, req)
}

func (a *Adapter) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	return a.service.Reschedule(ctx, salonID, ownerUserID, appointmentID, req)
}

func (a *Adapter) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	return a.service.Cancel(ctx, salonID, ownerUserID, appointmentID, req)
}

var _ scheduling.Executor = (*Adapter)(nil)
