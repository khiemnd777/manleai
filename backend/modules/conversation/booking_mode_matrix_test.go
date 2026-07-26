package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestBookingModeChangeAfterReviewRequiresFreshReview(t *testing.T) {
	session := Session{
		BookingAction: BookingActionBook,
		DialogState: DialogState{
			ReviewRequired: true, ReviewAccepted: true,
			DraftRevision: 4, ReviewedRevision: 4, AuthorizedRevision: 4,
			ReviewedBookingMode:         scheduling.BookingModePendingApproval,
			SelectedSchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		},
	}
	changed := &RuntimeConfig{BookingMode: scheduling.BookingModeConfirmedBooking, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}
	if action := planNextConversationAction(session, "", changed); action.Kind != AssistantActionReadReview {
		t.Fatalf("mode change action=%#v, want fresh review", action)
	}

	unchanged := &RuntimeConfig{BookingMode: scheduling.BookingModePendingApproval, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}
	if action := planNextConversationAction(session, "", unchanged); action.Kind != AssistantActionExecuteBooking {
		t.Fatalf("unchanged reviewed policy action=%#v, want execute", action)
	}
}

func TestDisabledBookingModePerformsZeroSchedulingCalls(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.BookingMode = scheduling.BookingModeDisabled
	store.cfg.SchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	tool := newOwnerManualSchedulingTool("must-not-exist")
	tool.authority = booking.SchedulingAuthorityExternalProvider
	service := NewService(store, tool)
	session := ownerManualReadySession(store)
	session.DialogState.SchedulingRequestOnly = false
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please take care of the appointment request.", "disabled-mode", store.services, store.staff, &store.cfg)

	result, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.authorityChecks != 0 || tool.availabilityChecks != 0 || tool.actionCalls != 0 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf("disabled scheduling calls authority/availability/action/provider=%d/%d/%d/%d", tool.authorityChecks, tool.availabilityChecks, tool.actionCalls, tool.fakeBookingTool.calls)
	}
	if result.Status != StatusHandoff || result.Outcome != OutcomeAIDisabled || result.AppointmentID != "" || result.SchedulingRequestID != "" {
		t.Fatalf("disabled result=%#v", result)
	}
}

func TestDisabledBookingModeMessageHandoffsBeforeMissingFieldOrSchedulingTools(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.BookingMode = scheduling.BookingModeDisabled
	store.cfg.SchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	tool := newOwnerManualSchedulingTool("must-not-exist")
	tool.authority = booking.SchedulingAuthorityExternalProvider
	service := NewService(store, tool)

	result, err := service.Message(context.Background(), store.session.SalonID, "owner_1", store.session.ID, MessageRequest{
		Message: "Could you arrange an appointment for me?", EventKey: "disabled-incomplete-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.authorityChecks != 0 || tool.availabilityChecks != 0 || tool.actionCalls != 0 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf("disabled incomplete scheduling calls authority/availability/action/provider=%d/%d/%d/%d", tool.authorityChecks, tool.availabilityChecks, tool.actionCalls, tool.fakeBookingTool.calls)
	}
	if result.Status != StatusHandoff || result.Outcome != OutcomeAIDisabled || result.Handoff == nil || result.Handoff.Reason != HandoffReasonAIBookingDisabled {
		t.Fatalf("disabled incomplete result=%#v", result)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which service") {
		t.Fatalf("disabled flow asked a missing-field question: %q", store.lastTurn.AIMessage)
	}
}

func TestPendingExternalPartyPreservesGuestServiceOrderAndSelectedTimes(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "gel", Name: "Gel Manicure", DurationMinutes: 45, BookingReady: true}}
	store.cfg.BookingMode = scheduling.BookingModePendingApproval
	store.cfg.SchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	tool := newOwnerManualSchedulingTool("pending-party-request")
	tool.authority = booking.SchedulingAuthorityExternalProvider
	service := NewService(store, tool)
	first := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	second := first.Add(50 * time.Minute)
	refs := []string{"group-1-guest-1", "group-1-guest-1", "group-2-guest-1"}
	starts := []time.Time{first, first.Add(45 * time.Minute), second}
	blocks := make([]PartySplitBlock, 0, len(starts))
	for index, start := range starts {
		end := start.Add(45 * time.Minute)
		blocks = append(blocks, PartySplitBlock{
			StartTime: start, EndTime: end,
			Segments: []booking.BookingSegmentRequest{{ServiceID: "gel", StaffID: "staff_1", StaffSelectionMode: booking.StaffSelectionSpecific}},
			QuoteRefs: []PartySplitQuoteRef{{
				ServiceID: "gel", GuestReference: refs[index], Quantity: 1,
				RequestedStartTime: start, RequestedEndTime: end,
				AvailabilityQuoteID: "quote-" + refs[index] + time.Duration(index).String(),
				SlotFingerprint:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
		})
	}
	option := PartySplitOption{ID: "pending-option", Blocks: blocks}
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionBook
	session.CustomerName = "Mina"
	session.CustomerPhone = "+13125550118"
	session.ServiceID = "gel"
	session.ServiceName = "Gel Manicure"
	session.RequestedDate = "2026-09-08"
	session.StaffSelectionMode = booking.StaffSelectionSpecific
	session.PartyPlan = &PartyPlan{
		PartySize: 2,
		Groups: []PartyPlanGroup{
			{Label: "Caller", Count: 1, ResolvedServiceIDs: []string{"gel", "gel"}},
			{Label: "Guest", Count: 1, ResolvedServiceIDs: []string{"gel"}},
		},
		SplitOptions: []PartySplitOption{option}, SelectedSplitOptionID: option.ID,
	}
	session.BookingSegments = partySplitOptionSegments(option)
	session.DialogState = normalizedDialogState(session.DialogState)
	session.DialogState.ReviewRequired = false
	session.DialogState.SelectedSchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please submit the group request we reviewed.", "pending-party", store.services, store.staff, &store.cfg)

	result, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeOwnerReviewPending || tool.actionCalls != 1 {
		t.Fatalf("pending party outcome/calls=%q/%d", result.Outcome, tool.actionCalls)
	}
	request := tool.lastActionRequest
	if request.AvailabilityQuoteID != "" || request.SlotFingerprint != "" || len(request.Segments) != 3 || request.PartySize != 2 {
		t.Fatalf("pending party request proof/shape=%#v", request)
	}
	for index, segment := range request.Segments {
		if segment.GuestReference != refs[index] || segment.ServiceID != "gel" || !segment.RequestedStartTime.Equal(starts[index]) || segment.Quantity != 1 {
			t.Fatalf("segment %d=%#v", index, segment)
		}
	}
}
