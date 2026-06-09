package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestMessageConfirmsOnlyAfterBookingToolSuccess(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_1",
			Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if bookingTool.request.Source != booking.SourceAIConversationSimulator {
		t.Fatalf("source = %s, want %s", bookingTool.request.Source, booking.SourceAIConversationSimulator)
	}
	if session.Status != StatusCompleted || session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("session status/outcome = %s/%s, want completed/booking_confirmed", session.Status, session.Outcome)
	}
	if session.BookingAttemptID != "attempt_1" || session.AppointmentID != "appointment_1" {
		t.Fatalf("booking linkage = %s/%s", session.BookingAttemptID, session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "confirmed in Square Appointments") {
		t.Fatalf("AI reply should confirm only after booking success: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageUsesFallbackPendingTextWhenBookingToolFallsBack(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:     "attempt_2",
			Status: booking.StatusFallbackPending,
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Outcome != OutcomeBookingFallbackPending {
		t.Fatalf("outcome = %s, want booking_fallback_pending", session.Outcome)
	}
	if session.AppointmentID != "" {
		t.Fatalf("fallback should not link a confirmed appointment: %s", session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("fallback reply must avoid confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageCreatesHandoffWithoutBookingWhenAIDisabled(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.AIEnabled = false
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called when AI booking is disabled")
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeAIDisabled {
		t.Fatalf("session status/outcome = %s/%s, want handoff/ai_disabled", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonAIBookingDisabled {
		t.Fatalf("handoff = %#v, want ai_booking_disabled", store.lastTurn.Handoff)
	}
}

func TestMessageCreatesHandoffForHumanRequest(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need to speak to the owner about a refund.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called for human handoff")
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonHumanRequested {
		t.Fatalf("handoff = %#v, want human_requested", store.lastTurn.Handoff)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
}

type fakeConversationStore struct {
	cfg      RuntimeConfig
	session  Session
	services []ServiceOption
	staff    []StaffOption
	lastTurn TurnRecord
}

func newFakeConversationStore() *fakeConversationStore {
	return &fakeConversationStore{
		cfg: RuntimeConfig{
			SalonName:      "Lotus Nails",
			Timezone:       "America/Chicago",
			AIEnabled:      true,
			HandoffEnabled: true,
			AIGreeting:     defaultGreeting,
		},
		session: Session{
			ID:      "session_1",
			SalonID: "salon_1",
			Channel: ChannelSimulator,
			Status:  StatusActive,
			Intent:  IntentUnknown,
			Outcome: OutcomeCollecting,
		},
		services: []ServiceOption{{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		}},
		staff: []StaffOption{{
			ID:   "staff_1",
			Name: "Mai Nguyen",
		}},
	}
}

func (f *fakeConversationStore) GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error) {
	return &f.cfg, nil
}

func (f *fakeConversationStore) CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error) {
	return &f.session, nil
}

func (f *fakeConversationStore) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	session := f.session
	return &session, nil
}

func (f *fakeConversationStore) ListSessions(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Session, error) {
	return []Session{f.session}, nil
}

func (f *fakeConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	return f.services, nil
}

func (f *fakeConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	return f.staff, nil
}

func (f *fakeConversationStore) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	f.lastTurn = record
	session := record.Session
	session.Status = record.Update.Status
	session.Intent = record.Update.Intent
	session.Outcome = record.Update.Outcome
	session.CustomerName = record.Update.CustomerName
	session.CustomerPhone = record.Update.CustomerPhone
	session.CustomerEmail = record.Update.CustomerEmail
	session.ServiceID = record.Update.ServiceID
	session.StaffID = record.Update.StaffID
	session.RequestedStartTime = record.Update.RequestedStartTime
	session.BookingAttemptID = record.Update.BookingAttemptID
	session.AppointmentID = record.Update.AppointmentID
	session.Summary = record.Update.Summary
	if record.Handoff != nil {
		session.Handoff = &HandoffRequest{
			Reason:        record.Handoff.Reason,
			CustomerName:  record.Handoff.CustomerName,
			CustomerPhone: record.Handoff.CustomerPhone,
			Summary:       record.Handoff.Summary,
		}
	}
	f.session = session
	return &session, nil
}

type fakeBookingTool struct {
	calls   int
	request booking.CreateBookingRequest
	attempt *booking.BookingAttempt
	err     error
}

func (f *fakeBookingTool) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls++
	f.request = req
	return f.attempt, f.err
}
