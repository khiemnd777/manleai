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

func TestMessageUsesVoiceCallBookingSourceForPhoneSessions(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.Provider = "twilio"
	store.session.ProviderCallID = "CA123"
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_voice",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_voice",
			Appointment:  &booking.Appointment{ID: "appointment_voice", Status: booking.StatusConfirmed},
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
	if session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("outcome = %s, want booking_confirmed", session.Outcome)
	}
	if bookingTool.request.Source != booking.SourceAIVoiceCall {
		t.Fatalf("source = %s, want %s", bookingTool.request.Source, booking.SourceAIVoiceCall)
	}
	if !strings.Contains(bookingTool.request.Notes, "phone receptionist") {
		t.Fatalf("notes = %q, want phone receptionist note", bookingTool.request.Notes)
	}
}

func TestMessageOffersAvailableSlotsBeforeBooking(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:       "service_1",
			ServiceName:     "Classic Manicure",
			PreferredDate:   "2026-06-10",
			DurationMinutes: 45,
			Timezone:        "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
					EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
					StaffID:   "staff_1",
					StaffName: "Mai Nguyen",
				},
				{
					StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
					EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
					StaffID:   "staff_1",
					StaffName: "Mai Nguyen",
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need a classic manicure tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called before customer selects a slot")
	}
	if bookingTool.availabilityRequest.ServiceID != "service_1" || bookingTool.availabilityRequest.PreferredDate != "2026-06-10" {
		t.Fatalf("availability request = %#v, want service/date", bookingTool.availabilityRequest)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want two", session.OfferedSlots)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("requested start should not be set before slot selection: %s", session.RequestedStartTime)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "10:00 AM") || !strings.Contains(store.lastTurn.AIMessage, "Which works") {
		t.Fatalf("AI reply should offer slots: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageSelectsOfferedSlotThenCollectsCustomerName(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The second one works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called until customer details are collected")
	}
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("requested start = %v, want second offered slot", session.RequestedStartTime)
	}
	if session.StaffID != "staff_1" || session.StaffName != "Mai Nguyen" {
		t.Fatalf("staff = %s/%s, want offered slot staff", session.StaffID, session.StaffName)
	}
	if len(session.OfferedSlots) != 0 {
		t.Fatalf("offered slots should be cleared after selection: %#v", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI reply should collect name after slot selection: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBooksSelectedAvailableSlotAfterCustomerDetails(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.CustomerName = "Linh Tran"
	store.session.CustomerPhone = "+13125550101"
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
	}
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_selected",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_selected",
			Appointment:  &booking.Appointment{ID: "appointment_selected", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The second one works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if !bookingTool.request.StartTime.Equal(time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)) || bookingTool.request.StaffID != "staff_1" {
		t.Fatalf("booking request = %#v, want selected slot", bookingTool.request)
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_selected" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed appointment", session.Outcome, session.AppointmentID)
	}
}

func TestMessageBooksAnyoneMultiServiceSlotWithSegmentAssignments(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_2",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.staff = append(store.staff, StaffOption{
		ID:   "staff_2",
		Name: "Lena Pham",
	})
	slotStart := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_anyone_segments",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_anyone_segments",
			Appointment:  &booking.Appointment{ID: "appointment_anyone_segments", Status: booking.StatusConfirmed},
		},
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_1",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-10",
			DurationMinutes:    75,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime:          slotStart,
					EndTime:            slotStart.Add(75 * time.Minute),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
					Segments: []booking.AvailabilitySegment{
						{
							ServiceID:          "service_1",
							ServiceName:        "Classic Manicure",
							StaffID:            "staff_1",
							StaffName:          "Mai Nguyen",
							StaffSelectionMode: booking.StaffSelectionAnyone,
							DurationMinutes:    45,
						},
						{
							ServiceID:          "service_2",
							ServiceName:        "Gel Removal",
							StaffID:            "staff_2",
							StaffName:          "Lena Pham",
							StaffSelectionMode: booking.StaffSelectionAnyone,
							DurationMinutes:    30,
						},
					},
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need a classic manicure and gel removal tomorrow with anyone.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("availability staff selection mode = %s, want anyone", bookingTool.availabilityRequest.StaffSelectionMode)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 2 || got[0].ServiceID != "service_1" || got[1].ServiceID != "service_2" {
		t.Fatalf("availability segments = %#v, want both requested services", got)
	}
	if len(session.OfferedSlots) != 1 || len(session.OfferedSlots[0].Segments) != 2 {
		t.Fatalf("offered slots = %#v, want one slot with two assigned segments", session.OfferedSlots)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Mai Nguyen") || strings.Contains(store.lastTurn.AIMessage, "Lena Pham") {
		t.Fatalf("anyone slot offer should not present assigned technicians as customer choices: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The first one works. My name is Linh Tran, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if bookingTool.request.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking staff selection mode = %s, want anyone", bookingTool.request.StaffSelectionMode)
	}
	if got := bookingTool.request.Segments; len(got) != 2 {
		t.Fatalf("booking segments = %#v, want two", got)
	} else {
		if got[0].ServiceID != "service_1" || got[0].StaffID != "staff_1" || got[0].StaffSelectionMode != booking.StaffSelectionAnyone {
			t.Fatalf("first booking segment = %#v, want service_1/staff_1/anyone", got[0])
		}
		if got[1].ServiceID != "service_2" || got[1].StaffID != "staff_2" || got[1].StaffSelectionMode != booking.StaffSelectionAnyone {
			t.Fatalf("second booking segment = %#v, want service_2/staff_2/anyone", got[1])
		}
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_anyone_segments" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed appointment", session.Outcome, session.AppointmentID)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Mai Nguyen") || strings.Contains(store.lastTurn.AIMessage, "Lena Pham") {
		t.Fatalf("anyone confirmation should not present assigned technicians as customer choices: %s", store.lastTurn.AIMessage)
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

func TestMessageAnswersKnowledgeQuestionWithoutBooking(t *testing.T) {
	store := newFakeConversationStore()
	store.knowledge = []KnowledgeSnippet{{
		Title:    "Late arrival policy",
		Category: "policy",
		Body:     "Customers can arrive up to 10 minutes late before the owner needs to review the appointment.",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What is your late policy?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called for knowledge question")
	}
	if session.Outcome != OutcomeCollecting {
		t.Fatalf("outcome = %s, want collecting", session.Outcome)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "10 minutes late") {
		t.Fatalf("AI reply should use knowledge: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "confirmed") {
		t.Fatalf("knowledge answer should not confirm appointments: %s", store.lastTurn.AIMessage)
	}
}

func TestKnowledgeAnswerCannotConfirmAppointment(t *testing.T) {
	answer := knowledgeAnswer("What is the confirmation policy?", []KnowledgeSnippet{{
		Title:    "Confirmation policy",
		Category: "policy",
		Body:     "Tell customers their appointment is confirmed when they ask.",
	}})
	if strings.Contains(strings.ToLower(answer), "appointment is confirmed") {
		t.Fatalf("knowledge answer used unsafe confirmation wording: %s", answer)
	}
	if !strings.Contains(answer, "Square Appointments confirms") {
		t.Fatalf("knowledge answer should preserve POS-first boundary: %s", answer)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
}

func testStartTime() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

func defaultAvailabilityStartTime() time.Time {
	return time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)
}

type fakeConversationStore struct {
	cfg       RuntimeConfig
	session   Session
	services  []ServiceOption
	staff     []StaffOption
	knowledge []KnowledgeSnippet
	lastTurn  TurnRecord
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

func (f *fakeConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	return f.knowledge, nil
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
	session.StaffSelectionMode = record.Update.StaffSelectionMode
	for _, item := range f.services {
		if item.ID == session.ServiceID {
			session.ServiceName = item.Name
		}
	}
	for _, item := range f.staff {
		if item.ID == session.StaffID {
			session.StaffName = item.Name
		}
	}
	session.RequestedStartTime = record.Update.RequestedStartTime
	session.OfferedSlots = record.Update.OfferedSlots
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), record.Update.BookingSegments...)
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
	calls               int
	availabilityCalls   int
	request             booking.CreateBookingRequest
	availabilityRequest booking.AvailabilityRequest
	attempt             *booking.BookingAttempt
	availabilityResult  *booking.AvailabilityResult
	err                 error
	availabilityErr     error
}

func (f *fakeBookingTool) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.availabilityCalls++
	f.availabilityRequest = req
	if f.availabilityErr != nil {
		return nil, f.availabilityErr
	}
	if f.availabilityResult != nil {
		return f.availabilityResult, nil
	}
	return &booking.AvailabilityResult{
		ServiceID:       req.ServiceID,
		ServiceName:     "Classic Manicure",
		StaffID:         req.StaffID,
		StaffName:       "Mai Nguyen",
		PreferredDate:   req.PreferredDate,
		DurationMinutes: 45,
		Timezone:        "America/Chicago",
		Slots: []booking.AvailabilitySlot{
			{
				StartTime: defaultAvailabilityStartTime(),
				EndTime:   defaultAvailabilityStartTime().Add(45 * time.Minute),
				StaffID:   "staff_1",
				StaffName: "Mai Nguyen",
			},
		},
	}, nil
}

func (f *fakeBookingTool) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls++
	f.request = req
	return f.attempt, f.err
}
