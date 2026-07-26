package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestFuzzyServiceGoldenRequiresExplicitConfirmationAcrossSchedulingAuthorities(t *testing.T) {
	start := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "service_classic", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true}
	otherServices := []ServiceOption{
		serviceOption,
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 50, BookingReady: true},
		{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 70, BookingReady: true},
	}
	staffOption := StaffOption{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true}

	t.Run("external provider", func(t *testing.T) {
		store := fuzzyConfirmationReadyStore(otherServices, staffOption, start)
		tool := &fakeBookingTool{
			availabilityResult: availabilityResultForStart(serviceOption.ID, serviceOption.Name, start),
			attempt: &booking.BookingAttempt{
				ID: "attempt-fuzzy-external", Status: booking.StatusConfirmed, POSBookingID: "provider-booking-fuzzy",
				Appointment: &booking.Appointment{ID: "appointment-fuzzy-external", Status: booking.StatusConfirmed},
			},
		}
		service := NewService(store, tool)
		service.now = fixedNow

		assertFuzzyConfirmationAndBookFlow(t, service, store, "Classis Manikia.", "external-fuzzy-proposal", "external-fuzzy-confirm", "external-fuzzy-slot")
		if tool.availabilityCalls != 2 || tool.calls != 1 {
			t.Fatalf("external flow calls = availability %d booking %d, want two verified availability checks and one booking", tool.availabilityCalls, tool.calls)
		}
	})

	t.Run("manleai calendar neutral flow", func(t *testing.T) {
		store := fuzzyConfirmationReadyStore(otherServices, staffOption, start)
		store.answerContextFence = AnswerContextFence{
			SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
			Ready:               true, SchedulingAuthorityVersion: 3, CalendarConfigVersion: 8, CalendarActivatedVersion: 8,
		}
		store.internalServices = append([]ServiceOption(nil), otherServices...)
		store.internalStaff = []StaffOption{staffOption}
		tool := newQueuedManleAICalendarSchedulingTool()
		tool.availabilityResults = []*scheduling.AvailabilityResult{
			internalVerifiedAvailability(serviceOption, staffOption, start, "quote-fuzzy-internal", strings.Repeat("c", 64)),
		}
		tool.actionResults = []*scheduling.ActionResult{internalConfirmedAction("appointment-fuzzy-internal", scheduling.ActionSegment{
			ServiceID: serviceOption.ID, StaffID: staffOption.ID,
			StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
			RequestedStartTime: start,
			RequestedEndTime:   start.Add(time.Duration(serviceOption.DurationMinutes) * time.Minute),
		})}
		service := NewService(store, tool)
		service.now = fixedNow

		assertFuzzyConfirmationAndBookFlow(t, service, store, "Klasik manikyur.", "internal-fuzzy-proposal", "internal-fuzzy-confirm", "internal-fuzzy-slot")
		if tool.availabilityCalls != 1 || tool.actionCalls != 1 || tool.fakeBookingTool.calls != 0 {
			t.Fatalf("internal neutral flow calls = availability %d action %d legacy %d", tool.availabilityCalls, tool.actionCalls, tool.fakeBookingTool.calls)
		}
	})
}

func TestFuzzyServiceConfirmationWrongStateInputsNeverBook(t *testing.T) {
	start := time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	services := []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true},
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 50, BookingReady: true},
		{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 70, BookingReady: true},
	}
	staffOption := StaffOption{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true}
	store := fuzzyConfirmationReadyStore(services, staffOption, start)
	tool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_classic", "Classic Manicure", start),
		attempt: &booking.BookingAttempt{
			ID: "attempt-must-not-run", Status: booking.StatusConfirmed, POSBookingID: "provider-must-not-run",
			Appointment: &booking.Appointment{ID: "appointment-must-not-run", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, tool)
	service.now = fixedNow

	proposal, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Clatic manicure.", EventKey: "wrong-state-proposal"})
	if err != nil {
		t.Fatalf("proposal Message returned error: %v", err)
	}
	if proposal.DialogState.Pending == nil || proposal.ServiceID != "" {
		t.Fatalf("proposal state = service %q pending %#v", proposal.ServiceID, proposal.DialogState.Pending)
	}

	for index, input := range []string{"My name is Ana Pham.", "One o'clock works."} {
		got, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: input, EventKey: "wrong-state-" + string(rune('1'+index))})
		if err != nil {
			t.Fatalf("wrong-state input %q returned error: %v", input, err)
		}
		if got.ServiceID != "" || got.RequestedStartTime != nil || tool.availabilityCalls != 0 || tool.calls != 0 {
			t.Fatalf("wrong-state input %q advanced booking: session=%#v availability=%d booking=%d", input, got, tool.availabilityCalls, tool.calls)
		}
		if got.DialogState.Pending == nil || got.DialogState.Pending.PromptKey != "fuzzy_service_confirmation" {
			t.Fatalf("wrong-state input %q cleared service confirmation: %#v", input, got.DialogState.Pending)
		}
	}

	store.session.DialogState = resetDialogProgress(store.session.DialogState, DialogPhaseDrafting)
	store.session.ServiceID = ""
	store.session.ServiceName = ""
	store.session.BookingSegments = nil
	got, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Yes.", EventKey: "yes-without-service-confirmation"})
	if err != nil {
		t.Fatalf("wrong-state yes returned error: %v", err)
	}
	if got.ServiceID != "" || tool.availabilityCalls != 0 || tool.calls != 0 || got.Outcome == OutcomeBookingConfirmed {
		t.Fatalf("yes outside service-confirmation state advanced booking: session=%#v availability=%d booking=%d", got, tool.availabilityCalls, tool.calls)
	}
}

func TestFuzzyServiceConfirmationEventReplayIsIdempotent(t *testing.T) {
	start := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "service_classic", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true}
	store := fuzzyConfirmationReadyStore([]ServiceOption{
		serviceOption,
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 50, BookingReady: true},
		{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 70, BookingReady: true},
	}, StaffOption{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true}, start)
	tool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart(serviceOption.ID, serviceOption.Name, start),
		attempt: &booking.BookingAttempt{
			ID: "attempt-fuzzy-replay", Status: booking.StatusConfirmed, POSBookingID: "provider-fuzzy-replay",
			Appointment: &booking.Appointment{ID: "appointment-fuzzy-replay", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, tool)
	service.now = fixedNow

	proposalReq := MessageRequest{Message: "Klasos manicure.", EventKey: "fuzzy-replay-proposal"}
	firstProposal, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", proposalReq)
	if err != nil {
		t.Fatalf("proposal Message returned error: %v", err)
	}
	if firstProposal.ServiceID != "" || firstProposal.DialogState.Pending == nil || firstProposal.DialogState.Pending.PromptKey != PendingFuzzyServiceConfirmation {
		t.Fatalf("proposal did not persist the fuzzy confirmation fence: service=%q pending=%#v", firstProposal.ServiceID, firstProposal.DialogState.Pending)
	}
	replayedProposal, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Different payload is ignored.", EventKey: proposalReq.EventKey})
	if err != nil {
		t.Fatalf("proposal replay returned error: %v", err)
	}
	if replayedProposal.ReplayAIMessage != "Did you mean Classic Manicure?" {
		t.Fatalf("proposal replay reply = %q", replayedProposal.ReplayAIMessage)
	}
	if tool.availabilityCalls != 0 || tool.calls != 0 {
		t.Fatalf("proposal replay invoked tools: availability=%d booking=%d", tool.availabilityCalls, tool.calls)
	}

	confirmReq := MessageRequest{Message: "Yes, correct.", EventKey: "fuzzy-replay-confirm"}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", confirmReq); err != nil {
		t.Fatalf("confirmation Message returned error: %v", err)
	}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "A replay must not be reinterpreted.", EventKey: confirmReq.EventKey}); err != nil {
		t.Fatalf("confirmation replay returned error: %v", err)
	}
	if tool.availabilityCalls != 1 || tool.calls != 0 {
		t.Fatalf("confirmation replay calls = availability %d booking %d", tool.availabilityCalls, tool.calls)
	}

	slotReq := MessageRequest{Message: "1 PM.", EventKey: "fuzzy-replay-slot"}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", slotReq); err != nil {
		t.Fatalf("slot Message returned error: %v", err)
	}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", slotReq); err != nil {
		t.Fatalf("slot replay returned error: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("slot replay booking calls = %d, want one", tool.calls)
	}
}

func assertFuzzyConfirmationAndBookFlow(t *testing.T, service *Service, store *fakeConversationStore, fuzzyMessage string, proposalEvent string, confirmationEvent string, slotEvent string) {
	t.Helper()
	proposal, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: fuzzyMessage, EventKey: proposalEvent})
	if err != nil {
		t.Fatalf("fuzzy proposal returned error: %v", err)
	}
	if proposal.ServiceID != "" || proposal.DialogState.Pending == nil || proposal.DialogState.Pending.PromptKey != "fuzzy_service_confirmation" {
		t.Fatalf("fuzzy proposal selected without confirmation: service=%q pending=%#v", proposal.ServiceID, proposal.DialogState.Pending)
	}
	if store.lastTurn.AIMessage != "Did you mean Classic Manicure?" {
		t.Fatalf("proposal reply = %q", store.lastTurn.AIMessage)
	}

	confirmed, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Yes, that's right.", EventKey: confirmationEvent})
	if err != nil {
		t.Fatalf("service confirmation returned error: %v", err)
	}
	if confirmed.ServiceID != "service_classic" || confirmed.DialogState.Pending != nil || len(confirmed.OfferedSlots) == 0 {
		t.Fatalf("service confirmation did not offer availability: service=%q pending=%#v slots=%#v", confirmed.ServiceID, confirmed.DialogState.Pending, confirmed.OfferedSlots)
	}

	booked, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "1 PM.", EventKey: slotEvent})
	if err != nil {
		t.Fatalf("slot selection returned error: %v", err)
	}
	if booked.Outcome != OutcomeBookingConfirmed || booked.AppointmentID == "" {
		t.Fatalf("confirmed service did not complete booking: %#v", booked)
	}
}

func fuzzyConfirmationReadyStore(services []ServiceOption, staff StaffOption, start time.Time) *fakeConversationStore {
	store := newFakeConversationStore()
	store.services = append([]ServiceOption(nil), services...)
	store.staff = []StaffOption{staff}
	store.session.Intent = IntentBooking
	store.session.CustomerName = "Jade Nguyen"
	store.session.CustomerPhone = "+13125550172"
	store.session.StaffID = staff.ID
	store.session.StaffName = staff.Name
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.session.RequestedDate = start.In(timezoneLocation(store.cfg.Timezone)).Format("2006-01-02")
	store.session.DialogState.ReviewRequired = false
	return store
}
