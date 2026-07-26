package scheduling_external_provider

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type fakeBookingService struct {
	calls []string
	err   error

	availabilityRequest booking.AvailabilityRequest
	availabilityResult  *booking.AvailabilityResult
	createRequest       booking.CreateBookingRequest
	createResult        *booking.BookingAttempt
	candidatesRequest   booking.RescheduleLookupRequest
	candidatesResult    []booking.AppointmentActionRef

	rescheduleAppointmentID string
	rescheduleRequest       booking.RescheduleRequest
	rescheduleAppointment   *booking.Appointment
	rescheduleFallback      *booking.BookingAttempt

	cancelAppointmentID string
	cancelRequest       booking.CancelRequest
	cancelAppointment   *booking.Appointment
	cancelFallback      *booking.BookingAttempt
}

func (f *fakeBookingService) AvailableSlots(_ context.Context, _ string, _ string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.calls = append(f.calls, "availability")
	f.availabilityRequest = req
	return f.availabilityResult, f.err
}

func (f *fakeBookingService) Create(_ context.Context, _ string, _ string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls = append(f.calls, "create")
	f.createRequest = req
	return f.createResult, f.err
}

func (f *fakeBookingService) RescheduleCandidates(_ context.Context, _ string, _ string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	f.calls = append(f.calls, "candidates")
	f.candidatesRequest = req
	return f.candidatesResult, f.err
}

func (f *fakeBookingService) Reschedule(_ context.Context, _ string, _ string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.calls = append(f.calls, "reschedule")
	f.rescheduleAppointmentID = appointmentID
	f.rescheduleRequest = req
	return f.rescheduleAppointment, f.rescheduleFallback, f.err
}

func (f *fakeBookingService) Cancel(_ context.Context, _ string, _ string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.calls = append(f.calls, "cancel")
	f.cancelAppointmentID = appointmentID
	f.cancelRequest = req
	return f.cancelAppointment, f.cancelFallback, f.err
}

func TestAdapterDelegatesAllExternalProviderOperationsWithoutTransformation(t *testing.T) {
	delegateErr := errors.New("delegate error")
	fake := &fakeBookingService{
		err:                   delegateErr,
		availabilityResult:    &booking.AvailabilityResult{QuoteID: "quote-1"},
		createResult:          &booking.BookingAttempt{ID: "attempt-1"},
		candidatesResult:      []booking.AppointmentActionRef{{ID: "candidate-1"}},
		rescheduleAppointment: &booking.Appointment{ID: "appointment-rescheduled"},
		rescheduleFallback:    &booking.BookingAttempt{ID: "fallback-rescheduled"},
		cancelAppointment:     &booking.Appointment{ID: "appointment-cancelled"},
		cancelFallback:        &booking.BookingAttempt{ID: "fallback-cancelled"},
	}
	adapter := newAdapter(fake)
	ctx := context.Background()
	if adapter.SchedulingAuthority() != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("authority = %q", adapter.SchedulingAuthority())
	}

	availabilityReq := booking.AvailabilityRequest{ServiceID: "service-1", PreferredDate: "2026-08-03"}
	availability, err := adapter.AvailableSlots(ctx, "salon-1", "owner-1", availabilityReq)
	if availability != fake.availabilityResult || !errors.Is(err, delegateErr) || !reflect.DeepEqual(fake.availabilityRequest, availabilityReq) {
		t.Fatalf("availability = %#v/%v request=%#v", availability, err, fake.availabilityRequest)
	}

	createReq := booking.CreateBookingRequest{OperationKey: "operation-create", StartTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	attempt, err := adapter.Create(ctx, "salon-1", "owner-1", createReq)
	if attempt != fake.createResult || !errors.Is(err, delegateErr) || !reflect.DeepEqual(fake.createRequest, createReq) {
		t.Fatalf("create = %#v/%v request=%#v", attempt, err, fake.createRequest)
	}

	candidatesReq := booking.RescheduleLookupRequest{CustomerPhone: "+13125550101", Limit: 5}
	candidates, err := adapter.RescheduleCandidates(ctx, "salon-1", "owner-1", candidatesReq)
	if !reflect.DeepEqual(candidates, fake.candidatesResult) || !errors.Is(err, delegateErr) || !reflect.DeepEqual(fake.candidatesRequest, candidatesReq) {
		t.Fatalf("candidates = %#v/%v request=%#v", candidates, err, fake.candidatesRequest)
	}

	rescheduleReq := booking.RescheduleRequest{OperationKey: "operation-reschedule", StartTime: time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)}
	appointment, fallback, err := adapter.Reschedule(ctx, "salon-1", "owner-1", "appointment-1", rescheduleReq)
	if appointment != fake.rescheduleAppointment || fallback != fake.rescheduleFallback || !errors.Is(err, delegateErr) || fake.rescheduleAppointmentID != "appointment-1" || !reflect.DeepEqual(fake.rescheduleRequest, rescheduleReq) {
		t.Fatalf("reschedule = %#v/%#v/%v appointment=%q request=%#v", appointment, fallback, err, fake.rescheduleAppointmentID, fake.rescheduleRequest)
	}

	cancelReq := booking.CancelRequest{OperationKey: "operation-cancel", Reason: "customer request"}
	appointment, fallback, err = adapter.Cancel(ctx, "salon-1", "owner-1", "appointment-2", cancelReq)
	if appointment != fake.cancelAppointment || fallback != fake.cancelFallback || !errors.Is(err, delegateErr) || fake.cancelAppointmentID != "appointment-2" || !reflect.DeepEqual(fake.cancelRequest, cancelReq) {
		t.Fatalf("cancel = %#v/%#v/%v appointment=%q request=%#v", appointment, fallback, err, fake.cancelAppointmentID, fake.cancelRequest)
	}

	wantCalls := []string{"availability", "create", "candidates", "reschedule", "cancel"}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, wantCalls)
	}
}
