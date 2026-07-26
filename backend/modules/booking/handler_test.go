package booking

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type authorityNotReadyHandlerService struct{}

func (authorityNotReadyHandlerService) AvailableSlots(context.Context, string, string, AvailabilityRequest) (*AvailabilityResult, error) {
	return nil, ErrSchedulingAuthorityNotReady
}

func (authorityNotReadyHandlerService) Create(context.Context, string, string, CreateBookingRequest) (*BookingAttempt, error) {
	return nil, ErrSchedulingAuthorityNotReady
}

func (authorityNotReadyHandlerService) Reschedule(context.Context, string, string, string, RescheduleRequest) (*Appointment, *BookingAttempt, error) {
	return nil, nil, ErrSchedulingAuthorityNotReady
}

func (authorityNotReadyHandlerService) Cancel(context.Context, string, string, string, CancelRequest) (*Appointment, *BookingAttempt, error) {
	return nil, nil, ErrSchedulingAuthorityNotReady
}

func (authorityNotReadyHandlerService) Calendar(context.Context, string, string, CalendarRangeRequest) (*CalendarRangeResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) EnsureCalendarEventAccess(context.Context, string, string) error {
	return nil
}

func (authorityNotReadyHandlerService) CalendarEvents(context.Context, string, string, CalendarEventCursor, int) ([]CalendarEvent, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) SyncCalendar(context.Context, string, string, CalendarSyncRequest) (*CalendarSyncResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) Appointments(context.Context, string, string, int, int) (*ListAppointmentsResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) Attempts(context.Context, string, string, string, int, int) (*ListBookingAttemptsResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) ReconciliationTasks(context.Context, string, string, string, int, int) (*ListReconciliationTasksResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) ReconciliationCandidates(context.Context, string, string, string) (*ListReconciliationCandidatesResponse, error) {
	return nil, nil
}

func (authorityNotReadyHandlerService) ResolveReconciliation(context.Context, string, string, string, ResolveReconciliationRequest) (*ReconciliationTask, error) {
	return nil, nil
}

func TestHandlerMapsSchedulingAuthorityNotReadyToConflictForAuthorityOperations(t *testing.T) {
	handler := NewHandler(authorityNotReadyHandlerService{})
	tests := []struct {
		name string
		path string
		body string
		fn   fiber.Handler
	}{
		{name: "availability", path: "/salons/salon-1/availability", body: `{}`, fn: handler.Availability},
		{name: "create_without_quote", path: "/salons/salon-1/booking-attempts", body: `{"operation_key":"operation-1"}`, fn: handler.Create},
		{name: "reschedule_without_quote", path: "/salons/salon-1/appointments/appointment-1/reschedule", body: `{"operation_key":"operation-1"}`, fn: handler.Reschedule},
		{name: "cancel", path: "/salons/salon-1/appointments/appointment-1/cancel", body: `{"operation_key":"operation-1"}`, fn: handler.Cancel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Post(test.path, test.fn)
			req, err := http.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req)
			if err != nil {
				t.Fatalf("execute request: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != fiber.StatusConflict {
				t.Fatalf("status = %d, want %d", res.StatusCode, fiber.StatusConflict)
			}
			var payload respond.ErrorResponse
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error.Code != "SCHEDULING_AUTHORITY_NOT_READY" || payload.Error.Message != "The salon's scheduling authority is not ready for booking actions." {
				t.Fatalf("response = %#v", payload)
			}
		})
	}
}

type availabilityQuoteRequiredHandlerService struct {
	authorityNotReadyHandlerService
}

func (availabilityQuoteRequiredHandlerService) Create(context.Context, string, string, CreateBookingRequest) (*BookingAttempt, error) {
	return nil, ErrAvailabilityQuoteRequired
}

func (availabilityQuoteRequiredHandlerService) Reschedule(context.Context, string, string, string, RescheduleRequest) (*Appointment, *BookingAttempt, error) {
	return nil, nil, ErrAvailabilityQuoteRequired
}

func TestHandlerPreservesExternalAvailabilityQuoteRequiredMapping(t *testing.T) {
	handler := NewHandler(availabilityQuoteRequiredHandlerService{})
	tests := []struct {
		name    string
		path    string
		body    string
		fn      fiber.Handler
		message string
	}{
		{
			name:    "create",
			path:    "/salons/salon-1/booking-attempts",
			body:    `{"operation_key":"operation-1"}`,
			fn:      handler.Create,
			message: "Check current provider availability and choose a returned slot before booking.",
		},
		{
			name:    "reschedule",
			path:    "/salons/salon-1/appointments/appointment-1/reschedule",
			body:    `{"operation_key":"operation-1"}`,
			fn:      handler.Reschedule,
			message: "Check current provider availability and choose a returned slot before rescheduling.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Post(test.path, test.fn)
			req, err := http.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req)
			if err != nil {
				t.Fatalf("execute request: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != fiber.StatusConflict {
				t.Fatalf("status = %d, want %d", res.StatusCode, fiber.StatusConflict)
			}
			var payload respond.ErrorResponse
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error.Code != "AVAILABILITY_QUOTE_REQUIRED" || payload.Error.Message != test.message {
				t.Fatalf("response = %#v", payload)
			}
		})
	}
}
