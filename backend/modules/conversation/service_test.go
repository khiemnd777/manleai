package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestListDefaultsToActiveLifecycle(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	items, err := service.List(context.Background(), " salon_1 ", " owner_1 ", 0, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if store.listLifecycleStatus != LifecycleActive {
		t.Fatalf("lifecycle filter = %q, want active", store.listLifecycleStatus)
	}
	if store.listLimit != 25 {
		t.Fatalf("limit = %d, want 25", store.listLimit)
	}
}

func TestListRejectsInvalidLifecycle(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	_, err := service.List(context.Background(), "salon_1", "owner_1", 25, "deleted")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestListWebhookEventsDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	events, err := service.ListWebhookEvents(context.Background(), " salon_1 ", " owner_1 ", " session_1 ", 500)
	if err != nil {
		t.Fatalf("ListWebhookEvents returned error: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "realtime_failed" {
		t.Fatalf("events = %#v", events)
	}
	if store.webhookSessionID != "session_1" {
		t.Fatalf("webhook session = %q, want session_1", store.webhookSessionID)
	}
	if store.webhookLimit != maxWebhookLimit {
		t.Fatalf("webhook limit = %d, want %d", store.webhookLimit, maxWebhookLimit)
	}
}

func TestArchiveSessionDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	session, err := service.Archive(context.Background(), " salon_1 ", " owner_1 ", " session_1 ")
	if err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	if session.LifecycleStatus != LifecycleArchived {
		t.Fatalf("lifecycle = %s, want archived", session.LifecycleStatus)
	}
	if store.archivedSessionID != "session_1" {
		t.Fatalf("archived session = %q, want session_1", store.archivedSessionID)
	}
}

func TestRedactSessionDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	session, err := service.Redact(context.Background(), " salon_1 ", " owner_1 ", " session_1 ")
	if err != nil {
		t.Fatalf("Redact returned error: %v", err)
	}
	if session.LifecycleStatus != LifecycleRedacted {
		t.Fatalf("lifecycle = %s, want redacted", session.LifecycleStatus)
	}
	if store.redactedSessionID != "session_1" {
		t.Fatalf("redacted session = %q, want session_1", store.redactedSessionID)
	}
}

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

func TestMessageParsesWeekdayDateAndDoesNotAskForDayAgain(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:       "service_1",
			ServiceName:     "Classic Manicure",
			PreferredDate:   "2026-06-11",
			DurationMinutes: 45,
			Timezone:        "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime: time.Date(2026, 6, 11, 15, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2026, 6, 11, 15, 45, 0, 0, time.UTC),
				StaffID:   "staff_1",
				StaffName: "Mai Nguyen",
			}},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure on Thursday this week.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.PreferredDate != "2026-06-11" {
		t.Fatalf("preferred date = %s, want 2026-06-11", bookingTool.availabilityRequest.PreferredDate)
	}
	if session.RequestedDate != "2026-06-11" {
		t.Fatalf("requested date = %q, want 2026-06-11", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("AI should not ask for day again after parsing Thursday: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageParsesVietnameseWeekdayDate(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure. Thứ Tư này tuần này.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityRequest.PreferredDate != "2026-06-10" {
		t.Fatalf("preferred date = %s, want 2026-06-10", bookingTool.availabilityRequest.PreferredDate)
	}
	if session.RequestedDate != "2026-06-10" {
		t.Fatalf("requested date = %q, want 2026-06-10", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("AI should not ask for day again after Vietnamese weekday: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageKeepsKnownDateOnUnclearRepairTurn(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-06-11"
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "I have Thursday. What time works best?",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "A...",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("repair turn should not call booking tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.RequestedDate != "2026-06-11" {
		t.Fatalf("requested date = %q, want preserved Thursday", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("repair turn should not ask for date again: %s", store.lastTurn.AIMessage)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Thursday") {
		t.Fatalf("repair reply should preserve last prompt context: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageDedupesProcessedEventKeyBeforeBooking(t *testing.T) {
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
	req := MessageRequest{
		Message:  "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
		EventKey: "twilio:CA123:realtime:item_1",
	}

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", req); err != nil {
		t.Fatalf("first Message returned error: %v", err)
	}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", req); err != nil {
		t.Fatalf("duplicate Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want one after duplicate event key", bookingTool.calls)
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

func TestMessageCreatesHandoffForKnownNonBookableStaff(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	}}
	store.staff = []StaffOption{{
		ID:         "staff_mai",
		Name:       "Mai Nguyen",
		AIBookable: true,
	}}
	store.activeStaff = []StaffOption{
		{ID: "staff_mai", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_jenny", Name: "Jenny Le", AIBookable: false},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want Gel Manicure with Jenny tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 for non-bookable staff", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 for non-bookable staff", bookingTool.calls)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonBookingUnavailable {
		t.Fatalf("handoff = %#v, want booking_unavailable", store.lastTurn.Handoff)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Jenny Le") || !strings.Contains(store.lastTurn.AIMessage, "not enabled for AI booking") {
		t.Fatalf("reply should name non-bookable staff and explain owner review: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "confirmed") && !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "not a confirmed") {
		t.Fatalf("reply must not imply confirmation: %s", store.lastTurn.AIMessage)
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

func TestBookingSafetyEnabledFollowsSalonFlag(t *testing.T) {
	if !bookingSafetyEnabled(true) {
		t.Fatalf("AI booking should be enabled when the salon flag is true")
	}
	if bookingSafetyEnabled(false) {
		t.Fatalf("AI booking should remain disabled when the salon flag is false")
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
	cfg                 RuntimeConfig
	session             Session
	services            []ServiceOption
	staff               []StaffOption
	activeStaff         []StaffOption
	knowledge           []KnowledgeSnippet
	lastTurn            TurnRecord
	processedEventKeys  map[string]bool
	listLifecycleStatus string
	listLimit           int
	webhookSessionID    string
	webhookLimit        int
	archivedSessionID   string
	redactedSessionID   string
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
			ID:                 "session_1",
			SalonID:            "salon_1",
			Channel:            ChannelSimulator,
			Status:             StatusActive,
			Intent:             IntentUnknown,
			Outcome:            OutcomeCollecting,
			LifecycleStatus:    LifecycleActive,
			RetentionExpiresAt: time.Now().UTC().Add(90 * 24 * time.Hour),
		},
		services: []ServiceOption{{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		}},
		staff: []StaffOption{{
			ID:         "staff_1",
			Name:       "Mai Nguyen",
			AIBookable: true,
		}},
		processedEventKeys: map[string]bool{},
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

func (f *fakeConversationStore) GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*Session, bool, error) {
	if f.processedEventKeys[eventKey] {
		session := f.session
		return &session, true, nil
	}
	return nil, false, nil
}

func (f *fakeConversationStore) ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int) ([]Session, error) {
	f.listLifecycleStatus = lifecycleStatus
	f.listLimit = limit
	return []Session{f.session}, nil
}

func (f *fakeConversationStore) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]WebhookEventLog, error) {
	f.webhookSessionID = sessionID
	f.webhookLimit = limit
	return []WebhookEventLog{{
		ID:        "event_1",
		EventType: "realtime_failed",
		Stage:     "openai_event",
	}}, nil
}

func (f *fakeConversationStore) ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	f.archivedSessionID = sessionID
	session := f.session
	session.LifecycleStatus = LifecycleArchived
	f.session = session
	return &session, nil
}

func (f *fakeConversationStore) RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	f.redactedSessionID = sessionID
	session := f.session
	session.LifecycleStatus = LifecycleRedacted
	session.CustomerName = ""
	session.CustomerPhone = ""
	session.CustomerEmail = ""
	f.session = session
	return &session, nil
}

func (f *fakeConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	return f.services, nil
}

func (f *fakeConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	return f.staff, nil
}

func (f *fakeConversationStore) ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	if f.activeStaff != nil {
		return f.activeStaff, nil
	}
	staff := append([]StaffOption(nil), f.staff...)
	for i := range staff {
		staff[i].AIBookable = true
	}
	return staff, nil
}

func (f *fakeConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	return f.knowledge, nil
}

func (f *fakeConversationStore) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	f.lastTurn = record
	if record.EventKey != "" {
		f.processedEventKeys[record.EventKey] = true
	}
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
	session.RequestedDate = record.Update.RequestedDate
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
