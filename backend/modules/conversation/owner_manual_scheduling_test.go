package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type fakeNeutralSchedulingTool struct {
	*fakeBookingTool
	authority          string
	availability       *scheduling.AvailabilityResult
	actionResult       *scheduling.ActionResult
	actionErr          error
	availabilityErr    error
	authorityErr       error
	actionCalls        int
	availabilityChecks int
	authorityChecks    int
	lastActionRequest  scheduling.ActionRequest
}

type ownerCanonicalConversationStore struct {
	*fakeConversationStore
}

func (s *ownerCanonicalConversationStore) ListBookableServices(context.Context, string) ([]ServiceOption, error) {
	return nil, nil
}

func (s *ownerCanonicalConversationStore) ListBookableStaff(context.Context, string) ([]StaffOption, error) {
	return nil, nil
}

func newOwnerManualSchedulingTool(requestID string) *fakeNeutralSchedulingTool {
	return &fakeNeutralSchedulingTool{
		fakeBookingTool: &fakeBookingTool{},
		authority:       booking.SchedulingAuthorityOwnerManual,
		availability: &scheduling.AvailabilityResult{
			Kind:                scheduling.AvailabilityKindRequestOnly,
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		},
		actionResult: &scheduling.ActionResult{
			Kind:                scheduling.ActionKindPendingOwnerReview,
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
			PendingOwnerReview: &scheduling.PendingOwnerReviewResult{
				SchedulingRequestID: requestID,
				Status:              string(scheduling.SchedulingRequestStatusPending),
				Version:             1,
			},
		},
	}
}

func (f *fakeNeutralSchedulingTool) CheckAvailability(context.Context, string, string, booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	f.availabilityChecks++
	if f.availabilityErr != nil {
		return nil, f.availabilityErr
	}
	return f.availability, nil
}

func (f *fakeNeutralSchedulingTool) CheckConversationAvailability(ctx context.Context, salonID string, ownerUserID string, _ scheduling.BookingMode, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	return f.CheckAvailability(ctx, salonID, ownerUserID, req)
}

func (f *fakeNeutralSchedulingTool) ExecuteAction(_ context.Context, _ string, _ string, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	f.actionCalls++
	f.lastActionRequest = req
	if f.actionErr != nil {
		return nil, f.actionErr
	}
	if f.actionResult != nil {
		result := *f.actionResult
		result.OperationType = req.OperationType
		return &result, nil
	}
	return nil, nil
}

func (f *fakeNeutralSchedulingTool) ExecuteConversationAction(ctx context.Context, salonID string, ownerUserID string, _ scheduling.ConversationPolicyFence, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	return f.ExecuteAction(ctx, salonID, ownerUserID, req)
}

func (f *fakeNeutralSchedulingTool) CurrentSchedulingAuthority(context.Context, string, string) (string, error) {
	f.authorityChecks++
	if f.authorityErr != nil {
		return "", f.authorityErr
	}
	return f.authority, nil
}

func ownerManualReadySession(store *fakeConversationStore) Session {
	start := defaultAvailabilityStartTime()
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionBook
	session.CustomerName = "Linh Tran"
	session.CustomerPhone = "+13125550101"
	session.ServiceID = store.services[0].ID
	session.ServiceName = store.services[0].Name
	session.StaffSelectionMode = booking.StaffSelectionAnyone
	session.RequestedDate = "2026-06-10"
	session.RequestedStartTime = &start
	session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          store.services[0].ID,
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	session.DialogState = normalizedDialogState(session.DialogState)
	session.DialogState.ReviewRequired = false
	session.DialogState.SchedulingRequestOnly = true
	return session
}

func TestOwnerManualBookingRecordsPendingRequestAndReplaysGoldenReply(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("request-owner-1")
	service := NewService(store, tool)
	session := ownerManualReadySession(store)
	store.session = session
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please request the manicure for Wednesday at three.", "event-owner-book", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if tool.actionCalls != 1 || tool.availabilityChecks != 1 {
		t.Fatalf("neutral calls = action %d availability %d, want 1 each", tool.actionCalls, tool.availabilityChecks)
	}
	if got.Outcome != OutcomeOwnerReviewPending || got.SchedulingRequestID != "request-owner-1" || got.AppointmentID != "" || got.BookingAttemptID != "" {
		t.Fatalf("pending result = outcome %q request %q appointment %q attempt %q", got.Outcome, got.SchedulingRequestID, got.AppointmentID, got.BookingAttemptID)
	}
	wantReply := "I recorded your appointment request for the owner to review. This is not a confirmed appointment. Thank you, goodbye."
	if store.lastTurn.AIMessage != wantReply {
		t.Fatalf("golden reply = %q, want %q", store.lastTurn.AIMessage, wantReply)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
	if tool.lastActionRequest.CallSessionID != session.ID || tool.lastActionRequest.AvailabilityQuoteID != "" || tool.lastActionRequest.SlotFingerprint != "" {
		t.Fatalf("owner request linkage/proof = %#v", tool.lastActionRequest)
	}

	replayed, err := service.Message(context.Background(), session.SalonID, "owner_1", session.ID, MessageRequest{Message: "different replay wording", EventKey: "event-owner-book"})
	if err != nil {
		t.Fatalf("Message replay returned error: %v", err)
	}
	if tool.actionCalls != 1 || replayed.ReplayAIMessage != wantReply {
		t.Fatalf("replay = calls %d reply %q", tool.actionCalls, replayed.ReplayAIMessage)
	}
}

func TestOwnerManualAvailabilityCollectsPreferenceWithoutAvailabilityClaim(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("unused")
	service := NewService(store, tool)
	session := ownerManualReadySession(store)
	session.RequestedStartTime = nil
	session.DialogState.SchedulingRequestOnly = false
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Wednesday works.", "availability-request-only", store.services, store.staff, &store.cfg)

	if err := service.offerAvailableSlots(context.Background(), "owner_1", &turn, &session, store.services, store.staff, session.RequestedDate, false, &store.cfg); err != nil {
		t.Fatalf("offerAvailableSlots returned error: %v", err)
	}
	if turn.AIMessage != "What time would you prefer for that day?" || !session.DialogState.SchedulingRequestOnly || len(session.OfferedSlots) != 0 {
		t.Fatalf("request-only availability state = reply %q dialog %#v slots %#v", turn.AIMessage, session.DialogState, session.OfferedSlots)
	}
	lower := strings.ToLower(turn.AIMessage)
	if strings.Contains(lower, "available") || strings.Contains(lower, "opening") || strings.Contains(lower, "confirmed") {
		t.Fatalf("request-only availability claimed provider proof: %q", turn.AIMessage)
	}
}

func TestOwnerManualAnswerContextUsesCanonicalCatalogWithoutProviderSnapshot(t *testing.T) {
	base := newFakeConversationStore()
	base.answerContextFence = AnswerContextFence{SchedulingAuthority: booking.SchedulingAuthorityOwnerManual, Ready: true}
	base.services[0].BookingReady = false
	base.activeStaff = []StaffOption{{ID: "canonical-staff-1", Name: "Anh", AIBookable: true}}
	store := &ownerCanonicalConversationStore{fakeConversationStore: base}
	service := NewService(store, newOwnerManualSchedulingTool("unused"))

	answer, err := service.loadAnswerContext(context.Background(), base.session.SalonID)
	if err != nil {
		t.Fatalf("loadAnswerContext returned error: %v", err)
	}
	if len(answer.Services) != 1 || !answer.Services[0].BookingReady || len(answer.Staff) != 1 || answer.Staff[0].ID != "canonical-staff-1" {
		t.Fatalf("canonical owner context = services %#v staff %#v", answer.Services, answer.Staff)
	}
}

func TestOwnerManualPartyCreatesOneOrderedRequest(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{ID: "service_2", Name: "Spa Pedicure", DurationMinutes: 60, BookingReady: true})
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("request-party-1")
	service := NewService(store, tool)
	session := ownerManualReadySession(store)
	session.PartyPlan = &PartyPlan{PartySize: 3, Groups: []PartyPlanGroup{
		{Label: "manicure", Count: 2, ResolvedServiceIDs: []string{"service_1", "service_1"}},
		{Label: "pedicure", Count: 1, ResolvedServiceIDs: []string{"service_2"}},
	}}
	session.BookingSegments = []booking.BookingSegmentRequest{
		{ServiceID: "service_1", StaffSelectionMode: booking.StaffSelectionAnyone},
		{ServiceID: "service_1", StaffSelectionMode: booking.StaffSelectionAnyone},
		{ServiceID: "service_2", StaffSelectionMode: booking.StaffSelectionAnyone},
	}
	store.session = session
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Three guests need two manicures and one spa pedicure.", "event-party", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if tool.actionCalls != 1 || got.SchedulingRequestID != "request-party-1" {
		t.Fatalf("party request = calls %d id %q", tool.actionCalls, got.SchedulingRequestID)
	}
	segments := tool.lastActionRequest.Segments
	if tool.lastActionRequest.PartySize != 3 || len(segments) != 3 || segments[0].ServiceID != "service_1" || segments[1].ServiceID != "service_1" || segments[2].ServiceID != "service_2" {
		t.Fatalf("ordered party payload = party size %d segments %#v", tool.lastActionRequest.PartySize, segments)
	}
	for i, segment := range segments {
		if segment.Quantity != 1 || strings.TrimSpace(segment.GuestReference) == "" {
			t.Fatalf("segment %d lost guest detail: %#v", i, segment)
		}
	}
	wantReply := "I recorded one group appointment request with all of the group details for the owner to review. This is not a confirmed group appointment. Thank you, goodbye."
	if store.lastTurn.AIMessage != wantReply {
		t.Fatalf("party golden reply = %q, want %q", store.lastTurn.AIMessage, wantReply)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
}

func TestOwnerManualCancelCollectsDurableTargetDescription(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("request-cancel-1")
	service := NewService(store, tool)
	before := store.session
	before.Intent = IntentBooking
	before.BookingAction = BookingActionCancel
	before.CustomerName = "Thanh Nguyen"
	before.CustomerPhone = "+17735550144"
	store.session = before

	first, err := service.handleCancelMessage(context.Background(), "owner_1", before, before, "I need to cancel it.", "cancel-1", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("first cancel turn returned error: %v", err)
	}
	if first.DialogState.Pending == nil || first.DialogState.Pending.PromptKey != PendingManualAppointmentTarget || tool.actionCalls != 0 {
		t.Fatalf("manual target prompt state = %#v calls %d", first.DialogState, tool.actionCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "describe the appointment's day, time, and service") {
		t.Fatalf("manual target prompt = %q", store.lastTurn.AIMessage)
	}

	description := "The dip manicure booked for Friday around 4 PM under Thanh."
	second, err := service.handleCancelMessage(context.Background(), "owner_1", *first, *first, description, "cancel-2", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("second cancel turn returned error: %v", err)
	}
	if tool.actionCalls != 1 || tool.lastActionRequest.OperationType != scheduling.OperationKindCancel || tool.lastActionRequest.TargetAppointmentID != "" || tool.lastActionRequest.TargetDescription != description {
		t.Fatalf("manual cancel payload = calls %d request %#v", tool.actionCalls, tool.lastActionRequest)
	}
	if second.SchedulingRequestID != "request-cancel-1" || second.AppointmentID != "" {
		t.Fatalf("manual cancel result = request %q appointment %q", second.SchedulingRequestID, second.AppointmentID)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
}

func TestOwnerManualRescheduleCollectsTargetAndRequestedTime(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("request-reschedule-1")
	service := NewService(store, tool)
	before := store.session
	before.Intent = IntentBooking
	before.BookingAction = BookingActionReschedule
	before.CustomerName = "Mai Pham"
	before.CustomerPhone = "+17085550177"
	store.session = before

	first, err := service.handleRescheduleMessage(context.Background(), "owner_1", before, before, "Please move my appointment.", "move-1", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("first reschedule turn returned error: %v", err)
	}
	description := "My classic manicure is this Saturday morning at ten."
	parsed := *first
	parsed.ServiceID = store.services[0].ID
	parsed.ServiceName = store.services[0].Name
	parsed.StaffSelectionMode = booking.StaffSelectionAnyone
	parsed.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: store.services[0].ID, StaffSelectionMode: booking.StaffSelectionAnyone}}
	oldAppointmentTime := time.Date(2026, 6, 13, 15, 0, 0, 0, time.UTC)
	parsed.RequestedDate = "2026-06-13"
	parsed.RequestedStartTime = &oldAppointmentTime
	second, err := service.handleRescheduleMessage(context.Background(), "owner_1", *first, parsed, description, "move-2", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("second reschedule turn returned error: %v", err)
	}
	if second.DialogState.ManualTarget == nil || second.DialogState.ManualTarget.Description != description || second.RequestedStartTime != nil || second.RequestedDate != "" || tool.actionCalls != 0 {
		t.Fatalf("manual reschedule target = %#v requested=%q/%v calls %d", second.DialogState.ManualTarget, second.RequestedDate, second.RequestedStartTime, tool.actionCalls)
	}
	start := time.Date(2026, 6, 17, 21, 30, 0, 0, time.UTC)
	ready := *second
	ready.RequestedDate = "2026-06-17"
	ready.RequestedStartTime = &start
	third, err := service.handleRescheduleMessage(context.Background(), "owner_1", *second, ready, "Next Wednesday at four thirty works.", "move-3", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("third reschedule turn returned error: %v", err)
	}
	if tool.actionCalls != 1 || tool.lastActionRequest.OperationType != scheduling.OperationKindReschedule || tool.lastActionRequest.TargetDescription != description || !tool.lastActionRequest.RequestedStartTime.Equal(start) {
		t.Fatalf("manual reschedule payload = calls %d request %#v", tool.actionCalls, tool.lastActionRequest)
	}
	if third.SchedulingRequestID != "request-reschedule-1" || third.AppointmentID != "" {
		t.Fatalf("manual reschedule result = request %q appointment %q", third.SchedulingRequestID, third.AppointmentID)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
}

func TestOwnerManualCancelPublicMessageCollectsManualTargetAndConfirmsPhoneNameWithoutInterpreter(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+12145550186"
	tool := newOwnerManualSchedulingTool("request-cancel-public-1")
	service := NewService(store, tool)
	if service.turnInterpreter != nil {
		t.Fatal("test requires an unavailable semantic interpreter")
	}

	first, err := service.Message(context.Background(), store.session.SalonID, "owner_1", store.session.ID, MessageRequest{
		Message:  "I cannot make my appointment and need to cancel it.",
		EventKey: "cancel-public-1",
	})
	if err != nil {
		t.Fatalf("initial cancel message returned error: %v", err)
	}
	if first.DialogState.Pending == nil || first.DialogState.Pending.PromptKey != PendingManualAppointmentTarget || expectedInputForSession(*first) != ExpectedInputAppointmentTarget {
		t.Fatalf("initial manual target state = %#v expected input %q", first.DialogState, expectedInputForSession(*first))
	}

	description := "It was the appointment from last Thursday around four in the afternoon."
	second, err := service.Message(context.Background(), first.SalonID, "owner_1", first.ID, MessageRequest{
		Message:  description,
		EventKey: "cancel-public-2",
	})
	if err != nil {
		t.Fatalf("manual target message returned error: %v", err)
	}
	if second.DialogState.ManualTarget == nil || second.DialogState.ManualTarget.Description != description || expectedInputForSession(*second) != ExpectedInputCustomerName {
		t.Fatalf("captured target = %#v expected input %q", second.DialogState.ManualTarget, expectedInputForSession(*second))
	}
	if second.CustomerName != "" || tool.actionCalls != 0 || !strings.Contains(store.lastTurn.AIMessage, "What name is on that appointment") {
		t.Fatalf("post-target cancel state = name %q calls %d reply %q", second.CustomerName, tool.actionCalls, store.lastTurn.AIMessage)
	}

	third, err := service.Message(context.Background(), second.SalonID, "owner_1", second.ID, MessageRequest{
		Message:  "Around four in the afternoon.",
		EventKey: "cancel-public-3",
	})
	if err != nil {
		t.Fatalf("non-name message returned error: %v", err)
	}
	if third.CustomerName != "" || tool.actionCalls != 0 || expectedInputForSession(*third) != ExpectedInputCustomerName {
		t.Fatalf("non-name was guessed as customer = %q calls %d expected %q", third.CustomerName, tool.actionCalls, expectedInputForSession(*third))
	}

	fourth, err := service.Message(context.Background(), third.SalonID, "owner_1", third.ID, MessageRequest{
		Message:  "Jordan Lee.",
		EventKey: "cancel-public-4",
	})
	if err != nil {
		t.Fatalf("phone name message returned error: %v", err)
	}
	if fourth.CustomerName != "" || fourth.DialogState.Pending == nil || fourth.DialogState.Pending.PromptKey != PendingCustomerNameConfirmation || pendingCustomerName(*fourth) != "Jordan Lee" {
		t.Fatalf("phone name confirmation state = name %q pending %#v candidate %q", fourth.CustomerName, fourth.DialogState.Pending, pendingCustomerName(*fourth))
	}
	if tool.actionCalls != 0 || expectedInputForSession(*fourth) != ExpectedInputCustomerNameConfirmation {
		t.Fatalf("phone name confirmation calls %d expected %q", tool.actionCalls, expectedInputForSession(*fourth))
	}

	completed, err := service.Message(context.Background(), fourth.SalonID, "owner_1", fourth.ID, MessageRequest{
		Message:  "Yes, that is right.",
		EventKey: "cancel-public-5",
	})
	if err != nil {
		t.Fatalf("phone name confirmation returned error: %v", err)
	}
	if completed.Status != StatusCompleted || completed.Outcome != OutcomeOwnerReviewPending || completed.CustomerName != "Jordan Lee" || completed.SchedulingRequestID != "request-cancel-public-1" {
		t.Fatalf("completed cancel state = status %q outcome %q name %q request %q", completed.Status, completed.Outcome, completed.CustomerName, completed.SchedulingRequestID)
	}
	if tool.actionCalls != 1 || tool.lastActionRequest.OperationType != scheduling.OperationKindCancel || tool.lastActionRequest.TargetDescription != description {
		t.Fatalf("cancel action = calls %d request %#v", tool.actionCalls, tool.lastActionRequest)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
}

func TestOwnerManualReschedulePublicMessageProgressesTargetServiceNameAndNewTimeWithoutInterpreter(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	store.session.CustomerPhone = "+14695550172"
	store.services = []ServiceOption{{
		ID:              "service_deluxe_pedicure",
		Name:            "Deluxe Pedicure",
		DurationMinutes: 60,
		PriceFrom:       58,
		BookingReady:    true,
	}}
	tool := newOwnerManualSchedulingTool("request-reschedule-public-1")
	service := NewService(store, tool)
	service.now = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }
	if service.turnInterpreter != nil {
		t.Fatal("test requires an unavailable semantic interpreter")
	}

	first, err := service.Message(context.Background(), store.session.SalonID, "owner_1", store.session.ID, MessageRequest{
		Message:  "Could you move my appointment to another day?",
		EventKey: "reschedule-public-1",
	})
	if err != nil {
		t.Fatalf("initial reschedule message returned error: %v", err)
	}
	if first.DialogState.Pending == nil || first.DialogState.Pending.PromptKey != PendingManualAppointmentTarget || expectedInputForSession(*first) != ExpectedInputAppointmentTarget {
		t.Fatalf("initial reschedule target state = %#v expected %q", first.DialogState, expectedInputForSession(*first))
	}

	description := "The current visit is this Saturday morning around eleven."
	second, err := service.Message(context.Background(), first.SalonID, "owner_1", first.ID, MessageRequest{
		Message:  description,
		EventKey: "reschedule-public-2",
	})
	if err != nil {
		t.Fatalf("reschedule target message returned error: %v", err)
	}
	if second.DialogState.ManualTarget == nil || second.DialogState.ManualTarget.Description != description || expectedInputForSession(*second) != ExpectedInputService {
		t.Fatalf("captured reschedule target = %#v expected %q", second.DialogState.ManualTarget, expectedInputForSession(*second))
	}

	third, err := service.Message(context.Background(), second.SalonID, "owner_1", second.ID, MessageRequest{
		Message:  "Whatever you think is fine.",
		EventKey: "reschedule-public-3",
	})
	if err != nil {
		t.Fatalf("unknown service message returned error: %v", err)
	}
	if third.ServiceID != "" || tool.actionCalls != 0 || expectedInputForSession(*third) != ExpectedInputService {
		t.Fatalf("unknown service was guessed = service %q calls %d expected %q", third.ServiceID, tool.actionCalls, expectedInputForSession(*third))
	}

	fourth, err := service.Message(context.Background(), third.SalonID, "owner_1", third.ID, MessageRequest{
		Message:  "Deluxe Pedicure.",
		EventKey: "reschedule-public-4",
	})
	if err != nil {
		t.Fatalf("catalog service message returned error: %v", err)
	}
	if fourth.ServiceID != "service_deluxe_pedicure" || expectedInputForSession(*fourth) != ExpectedInputCustomerName || !strings.Contains(store.lastTurn.AIMessage, "What name is on that appointment") {
		t.Fatalf("catalog service state = service %q expected %q reply %q", fourth.ServiceID, expectedInputForSession(*fourth), store.lastTurn.AIMessage)
	}

	fifth, err := service.Message(context.Background(), fourth.SalonID, "owner_1", fourth.ID, MessageRequest{
		Message:  "Morgan Patel.",
		EventKey: "reschedule-public-5",
	})
	if err != nil {
		t.Fatalf("customer name message returned error: %v", err)
	}
	if fifth.CustomerName != "Morgan Patel" || expectedInputForSession(*fifth) != ExpectedInputRequestedDate || !strings.Contains(store.lastTurn.AIMessage, "What new day and time would you like") {
		t.Fatalf("reschedule identity state = name %q expected %q reply %q", fifth.CustomerName, expectedInputForSession(*fifth), store.lastTurn.AIMessage)
	}

	completed, err := service.Message(context.Background(), fifth.SalonID, "owner_1", fifth.ID, MessageRequest{
		Message:  "Next Thursday at 2:30 PM.",
		EventKey: "reschedule-public-6",
	})
	if err != nil {
		t.Fatalf("new reschedule time returned error: %v", err)
	}
	if completed.Status != StatusCompleted || completed.Outcome != OutcomeOwnerReviewPending || completed.SchedulingRequestID != "request-reschedule-public-1" {
		t.Fatalf("completed reschedule state = status %q outcome %q request %q", completed.Status, completed.Outcome, completed.SchedulingRequestID)
	}
	if tool.actionCalls != 1 || tool.lastActionRequest.OperationType != scheduling.OperationKindReschedule || tool.lastActionRequest.TargetDescription != description || tool.lastActionRequest.CustomerName != "Morgan Patel" || len(tool.lastActionRequest.Segments) != 1 || tool.lastActionRequest.Segments[0].ServiceID != "service_deluxe_pedicure" {
		t.Fatalf("reschedule action = calls %d request %#v", tool.actionCalls, tool.lastActionRequest)
	}
	if tool.lastActionRequest.RequestedStartTime.IsZero() {
		t.Fatalf("reschedule action lost new requested time: %#v", tool.lastActionRequest)
	}
	assertOwnerReviewCopySafety(t, store.lastTurn.AIMessage)
}

func TestCurrentOwnerManualDoesNotReinterpretExternalTarget(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("unused")
	tool.candidates = []booking.AppointmentActionRef{{
		ID:                 "external-appointment-1",
		Status:             booking.StatusConfirmed,
		CustomerName:       "Linh Tran",
		CustomerPhone:      "+13125550101",
		Service:            booking.ServiceRef{ID: "service_1", Name: "Classic Manicure", DurationMinutes: 45},
		Staff:              booking.StaffRef{ID: "staff_1", Name: "Mai Nguyen"},
		StaffSelectionMode: booking.StaffSelectionSpecific,
		StartTime:          defaultAvailabilityStartTime(),
		EndTime:            defaultAvailabilityStartTime().Add(45 * time.Minute),
	}}
	service := NewService(store, tool)
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionCancel
	session.CustomerPhone = "+13125550101"

	got, err := service.handleCancelMessage(context.Background(), "owner_1", session, session, "Cancel my upcoming appointment.", "external-target", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("cancel target lookup returned error: %v", err)
	}
	if got.TargetAppointmentID != "external-appointment-1" || got.DialogState.ManualTarget != nil || got.DialogState.Pending != nil && got.DialogState.Pending.PromptKey == PendingManualAppointmentTarget {
		t.Fatalf("external target was reinterpreted: target %q dialog %#v", got.TargetAppointmentID, got.DialogState)
	}
	if tool.actionCalls != 0 {
		t.Fatalf("cancel executed before target confirmation: %d", tool.actionCalls)
	}
}

func TestExternalProviderZeroHistoryDoesNotCreateManualTarget(t *testing.T) {
	store := newFakeConversationStore()
	tool := newOwnerManualSchedulingTool("unused")
	tool.authority = booking.SchedulingAuthorityExternalProvider
	service := NewService(store, tool)
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionCancel
	session.CustomerName = "Nora Le"
	session.CustomerPhone = "+12145550188"

	got, err := service.handleCancelMessage(context.Background(), "owner_1", session, session, "Cancel my appointment.", "external-zero", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("cancel zero-history returned error: %v", err)
	}
	if got.Status != StatusHandoff || got.DialogState.ManualTarget != nil || got.DialogState.Pending != nil && got.DialogState.Pending.PromptKey == PendingManualAppointmentTarget || tool.actionCalls != 0 {
		t.Fatalf("external zero-history was treated as manual: status %q dialog %#v calls %d", got.Status, got.DialogState, tool.actionCalls)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	if strings.Contains(lower, "sent") || strings.Contains(lower, "delivered") || strings.Contains(lower, "notified") {
		t.Fatalf("external zero-history handoff claimed delivery: %q", store.lastTurn.AIMessage)
	}
}

func TestOwnerManualDependencyFailureDoesNotClaimRequestWasRecorded(t *testing.T) {
	store := newFakeConversationStore()
	tool := newOwnerManualSchedulingTool("unused")
	tool.actionErr = errors.New("request store unavailable")
	service := NewService(store, tool)
	session := ownerManualReadySession(store)
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Book this request.", "failure-1", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Status != StatusHandoff || got.SchedulingRequestID != "" || got.AppointmentID != "" {
		t.Fatalf("failure result = status %q request %q appointment %q", got.Status, got.SchedulingRequestID, got.AppointmentID)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	if strings.Contains(lower, "i recorded") || strings.Contains(lower, "sent") || strings.Contains(lower, "delivered") || strings.Contains(lower, "appointment is confirmed") {
		t.Fatalf("failure copy claimed durable success: %q", store.lastTurn.AIMessage)
	}
}

func TestOwnerManualMalformedActionResultFailsClosed(t *testing.T) {
	store := newFakeConversationStore()
	tool := newOwnerManualSchedulingTool("request-wrong-operation")
	tool.actionResult.OperationType = scheduling.OperationKindCancel
	session := ownerManualReadySession(store)
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Book this request.", "malformed-result", store.services, store.staff, &store.cfg)

	// Preserve the deliberately malformed operation instead of the fake's
	// normal request mirroring for this contract counterexample.
	toolOverride := &fixedResultNeutralSchedulingTool{fakeNeutralSchedulingTool: tool}
	service := NewService(store, toolOverride)
	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Status != StatusHandoff || got.SchedulingRequestID != "" || got.AppointmentID != "" {
		t.Fatalf("malformed result was accepted: status %q request %q appointment %q", got.Status, got.SchedulingRequestID, got.AppointmentID)
	}
}

type fixedResultNeutralSchedulingTool struct {
	*fakeNeutralSchedulingTool
}

func (f *fixedResultNeutralSchedulingTool) ExecuteAction(_ context.Context, _ string, _ string, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	f.actionCalls++
	f.lastActionRequest = req
	return f.actionResult, f.actionErr
}

func (f *fixedResultNeutralSchedulingTool) ExecuteConversationAction(ctx context.Context, salonID string, ownerUserID string, _ scheduling.ConversationPolicyFence, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	return f.ExecuteAction(ctx, salonID, ownerUserID, req)
}

func TestOwnerManualEmptyCatalogDoesNotFabricateServiceOrExecute(t *testing.T) {
	store := newFakeConversationStore()
	store.services = nil
	store.guidanceServices = []ServiceOption{}
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("unused")
	service := NewService(store, tool)

	got, err := service.Message(context.Background(), store.session.SalonID, "owner_1", store.session.ID, MessageRequest{Message: "I would like to make an appointment.", EventKey: "empty-catalog"})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if tool.actionCalls != 0 || got.ServiceID != "" {
		t.Fatalf("empty catalog fabricated scheduling data: calls %d service %q", tool.actionCalls, got.ServiceID)
	}
}

func assertOwnerReviewCopySafety(t *testing.T, reply string) {
	t.Helper()
	lower := strings.ToLower(reply)
	for _, forbidden := range []string{"sent", "delivered", "notified", "you're confirmed", "is confirmed"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("owner review copy contains forbidden claim %q: %q", forbidden, reply)
		}
	}
}
