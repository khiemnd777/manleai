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

func TestInternalLifecycleSchedulingActionRequestPreservesWholeRootVersionAndShape(t *testing.T) {
	start := time.Date(2026, 12, 3, 18, 0, 0, 0, time.UTC)
	candidate := RescheduleCandidate{
		AppointmentID:               "2ad95f6f-e5b2-4bd8-a6a5-c53c1a021691",
		SchedulingAuthority:         booking.SchedulingAuthorityManleAICalendar,
		AuthorityAppointmentVersion: 4,
		PartySize:                   2,
		Status:                      booking.StatusRescheduled,
		ActiveChildCount:            3,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: "2caac61a-b4ad-4dc7-817f-ef25ee08da20", StaffID: "fc3dddae-4fc4-44d0-85a4-f8b55e373eb1", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller", Quantity: 1},
			{ServiceID: "b8948478-e45e-427e-a42a-dc5c5ba37b6a", StaffID: "8cf9bd21-e8a2-4e63-945a-71d39c3d244d", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-two", Quantity: 1},
			{ServiceID: "70e99d52-d5fc-4e3c-adc1-84503417d4e2", StaffID: "fc3dddae-4fc4-44d0-85a4-f8b55e373eb1", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller", Quantity: 1},
		},
	}
	option := PartySplitOption{ID: "whole-root-replacement", Blocks: []PartySplitBlock{
		{
			StartTime: start, EndTime: start.Add(50 * time.Minute),
			Segments: []booking.BookingSegmentRequest{
				{ServiceID: candidate.Segments[0].ServiceID, StaffID: candidate.Segments[0].StaffID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller", Quantity: 1},
				{ServiceID: candidate.Segments[1].ServiceID, StaffID: candidate.Segments[1].StaffID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-two", Quantity: 1},
			},
			QuoteRefs: []PartySplitQuoteRef{
				{ServiceID: candidate.Segments[0].ServiceID, GuestReference: "caller", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute), AvailabilityQuoteID: "f68dc1b8-55d2-4ef8-baf2-ac6a38684811", SlotFingerprint: strings.Repeat("a", 64)},
				{ServiceID: candidate.Segments[1].ServiceID, GuestReference: "guest-two", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(50 * time.Minute), AvailabilityQuoteID: "f68dc1b8-55d2-4ef8-baf2-ac6a38684811", SlotFingerprint: strings.Repeat("a", 64)},
			},
		},
		{
			StartTime: start.Add(45 * time.Minute), EndTime: start.Add(75 * time.Minute),
			Segments: []booking.BookingSegmentRequest{
				{ServiceID: candidate.Segments[2].ServiceID, StaffID: candidate.Segments[2].StaffID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller", Quantity: 1},
			},
			QuoteRefs: []PartySplitQuoteRef{
				{ServiceID: candidate.Segments[2].ServiceID, GuestReference: "caller", Quantity: 1, RequestedStartTime: start.Add(45 * time.Minute), RequestedEndTime: start.Add(75 * time.Minute), AvailabilityQuoteID: "f68dc1b8-55d2-4ef8-baf2-ac6a38684811", SlotFingerprint: strings.Repeat("a", 64)},
			},
		},
	}}
	session := Session{
		ID: "67b0d4da-1b3a-4c10-a35a-ec89485d8114", SalonID: "salon-1",
		BookingAction: BookingActionReschedule, TargetAppointmentID: candidate.AppointmentID,
		RescheduleCandidates: []RescheduleCandidate{candidate},
		CustomerName:         "Mai Tran", CustomerPhone: "+13125550123", RequestedDate: "2026-12-03",
		AvailabilityQuoteID: "f68dc1b8-55d2-4ef8-baf2-ac6a38684811", SlotFingerprint: strings.Repeat("a", 64),
		PartyPlan: &PartyPlan{PartySize: candidate.PartySize, SplitOptions: []PartySplitOption{option}, SelectedSplitOptionID: option.ID},
	}

	req, ok := schedulingActionRequest(session, nil, &RuntimeConfig{Timezone: "America/Chicago"}, scheduling.OperationKindReschedule)
	if !ok {
		t.Fatal("whole-root lifecycle action was rejected")
	}
	if req.TargetAuthority != booking.SchedulingAuthorityManleAICalendar || req.ExpectedTargetAuthorityAppointmentVersion != 4 || req.PartySize != 2 {
		t.Fatalf("target authority/version/party = %q/%d/%d", req.TargetAuthority, req.ExpectedTargetAuthorityAppointmentVersion, req.PartySize)
	}
	if len(req.Segments) != 3 {
		t.Fatalf("segments = %#v, want complete three-child replacement", req.Segments)
	}
	for index, segment := range req.Segments {
		want := candidate.Segments[index]
		if segment.ServiceID != want.ServiceID || segment.GuestReference != want.GuestReference || segment.Quantity != 1 || segment.RequestedStartTime.IsZero() || !segment.RequestedEndTime.After(segment.RequestedStartTime) {
			t.Fatalf("segment %d lost immutable target shape: got %#v want %#v", index, segment, want)
		}
	}
}

func TestInternalLifecycleCandidateMappingPreservesBackendSourceOfTruth(t *testing.T) {
	items := []booking.AppointmentActionRef{{
		ID: "root-history", SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityAppointmentVersion: 7, PartySize: 2, Status: booking.StatusConfirmed,
		Segments: []booking.BookingSegmentRecord{
			{Service: booking.ServiceRef{ID: "service-ombre"}, Staff: booking.StaffRef{ID: "staff-lan"}, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-red", Quantity: 1},
			{Service: booking.ServiceRef{ID: "service-chrome"}, Staff: booking.StaffRef{ID: "staff-mai"}, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-blue", Quantity: 1},
		},
	}}
	candidates := rescheduleCandidatesFromAppointments(items)
	if len(candidates) != 1 || !validInternalLifecycleCandidate(candidates[0]) {
		t.Fatalf("authoritative candidate was not preserved: %#v", candidates)
	}
	got := candidates[0]
	if got.AuthorityAppointmentVersion != 7 || got.PartySize != 2 || got.ActiveChildCount != 2 || got.Segments[1].GuestReference != "guest-blue" || got.Segments[1].Quantity != 1 {
		t.Fatalf("candidate evidence was flattened: %#v", got)
	}
}

func lifecycleConversationFixture(t *testing.T) (*fakeConversationStore, *queuedManleAICalendarSchedulingTool, *Service, Session, RescheduleCandidate, *scheduling.AvailabilityResult) {
	t.Helper()
	store := newFakeConversationStore()
	store.cfg.Timezone = "America/Chicago"
	store.services = []ServiceOption{
		{ID: "service_builder", Name: "Builder Gel Refill", DurationMinutes: 50},
		{ID: "service_pedicure", Name: "Botanical Pedicure", DurationMinutes: 65},
	}
	store.staff = []StaffOption{{ID: "staff_nhi", Name: "Nhi"}, {ID: "staff_uyen", Name: "Uyen"}}
	original := time.Date(2026, 12, 3, 20, 0, 0, 0, time.UTC)
	candidate := RescheduleCandidate{
		AppointmentID: "appointment-internal-root", SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityAppointmentVersion: 4, PartySize: 2, Status: booking.StatusRescheduled, ActiveChildCount: 2,
		ServiceID: "service_builder", StaffID: "staff_nhi", StaffSelectionMode: booking.StaffSelectionSpecific,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: "service_builder", StaffID: "staff_nhi", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller-jade", Quantity: 1},
			{ServiceID: "service_pedicure", StaffID: "staff_uyen", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "aunt-linh", Quantity: 1},
		},
		StartTime: original, EndTime: original.Add(65 * time.Minute),
	}
	replacement := time.Date(2026, 12, 10, 21, 30, 0, 0, time.UTC)
	availability := &scheduling.AvailabilityResult{
		Kind: scheduling.AvailabilityKindVerifiedSlots, SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		TargetAuthorityAppointmentVersion: candidate.AuthorityAppointmentVersion,
		VerifiedSlots: &booking.AvailabilityResult{QuoteID: "quote-internal-replacement", PreferredDate: "2026-12-10", Timezone: store.cfg.Timezone, Slots: []booking.AvailabilitySlot{{
			Fingerprint: strings.Repeat("b", 64), StartTime: replacement, EndTime: replacement.Add(65 * time.Minute),
			Segments: []booking.AvailabilitySegment{
				{ServiceID: "service_builder", StaffID: "staff_nhi", StaffName: "Nhi", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller-jade", Quantity: 1, DurationMinutes: 50, ScheduledStartTime: replacement, ScheduledEndTime: replacement.Add(50 * time.Minute), OccupiedStartTime: replacement.Add(-5 * time.Minute), OccupiedEndTime: replacement.Add(55 * time.Minute), BufferBeforeMinutes: 5, BufferAfterMinutes: 5},
				{ServiceID: "service_pedicure", StaffID: "staff_uyen", StaffName: "Uyen", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "aunt-linh", Quantity: 1, DurationMinutes: 65, ScheduledStartTime: replacement, ScheduledEndTime: replacement.Add(65 * time.Minute), OccupiedStartTime: replacement, OccupiedEndTime: replacement.Add(75 * time.Minute), BufferAfterMinutes: 10},
			},
		}}},
	}
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.authority = booking.SchedulingAuthorityExternalProvider
	tool.availabilityResults = []*scheduling.AvailabilityResult{availability}
	service := NewService(store, tool)
	session := store.session
	session.Channel = ChannelPhone
	session.BookingAction = BookingActionReschedule
	session.Intent = IntentBooking
	session.CustomerName = "Jade Nguyen"
	session.CustomerPhone = "+13125550172"
	session.RescheduleCandidates = []RescheduleCandidate{candidate}
	applyRescheduleCandidate(&session, candidate)
	session.RequestedDate = "2026-12-10"
	return store, tool, service, session, candidate, availability
}

func lifecycleConfirmedResult(req scheduling.ActionRequest, status string, activeChildren int, replayed bool) *scheduling.ActionResult {
	children := make([]scheduling.ConfirmedAppointmentSegment, 0, len(req.Segments))
	if req.OperationType == scheduling.OperationKindCancel {
		// Cancellation returns the released target snapshot, not a new action plan.
		children = []scheduling.ConfirmedAppointmentSegment{
			{AppointmentServiceID: "child-builder", ServiceID: "service_builder", StaffID: "staff_nhi", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller-jade", Quantity: 1, ScheduledStartTime: time.Date(2026, 12, 3, 20, 0, 0, 0, time.UTC), ScheduledEndTime: time.Date(2026, 12, 3, 20, 50, 0, 0, time.UTC), OccupiedStartTime: time.Date(2026, 12, 3, 19, 55, 0, 0, time.UTC), OccupiedEndTime: time.Date(2026, 12, 3, 20, 55, 0, 0, time.UTC)},
			{AppointmentServiceID: "child-pedicure", ServiceID: "service_pedicure", StaffID: "staff_uyen", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "aunt-linh", Quantity: 1, ScheduledStartTime: time.Date(2026, 12, 3, 20, 0, 0, 0, time.UTC), ScheduledEndTime: time.Date(2026, 12, 3, 21, 5, 0, 0, time.UTC), OccupiedStartTime: time.Date(2026, 12, 3, 20, 0, 0, 0, time.UTC), OccupiedEndTime: time.Date(2026, 12, 3, 21, 15, 0, 0, time.UTC)},
		}
	} else {
		for index, segment := range req.Segments {
			children = append(children, scheduling.ConfirmedAppointmentSegment{
				AppointmentServiceID: "replacement-child-" + string(rune('1'+index)), ServiceID: segment.ServiceID, StaffID: segment.StaffID,
				StaffSelectionMode: segment.StaffSelectionMode, GuestReference: segment.GuestReference, Quantity: segment.Quantity,
				ScheduledStartTime: segment.RequestedStartTime, ScheduledEndTime: segment.RequestedEndTime,
				OccupiedStartTime: segment.RequestedStartTime.Add(-5 * time.Minute), OccupiedEndTime: segment.RequestedEndTime.Add(5 * time.Minute),
				BufferBeforeMinutes: 5, BufferAfterMinutes: 5,
			})
		}
	}
	return &scheduling.ActionResult{
		Kind: scheduling.ActionKindConfirmedAppointment, OperationType: req.OperationType,
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar, TargetAuthorityAppointmentVersion: 4,
		AuthorityAppointmentVersion: 5, Replayed: replayed,
		ConfirmedAppointment: &scheduling.ConfirmedAppointmentResult{
			AppointmentID: "appointment-internal-root", BookingAttemptID: "attempt-lifecycle-v5",
			AppointmentStatus: status, ActiveChildCount: activeChildren, Children: children,
		},
	}
}

func TestInternalLifecyclePhoneGoldenRescheduleAfterCurrentAuthoritySwitch(t *testing.T) {
	store, tool, service, session, candidate, _ := lifecycleConversationFixture(t)

	first, err := service.handleRescheduleMessage(context.Background(), "owner_1", session, session, "Thursday the tenth works for the replacement.", "lifecycle-reschedule-1", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.availabilityCalls != 1 || tool.authorityCalls != 0 || len(tool.availabilityRequests) != 1 {
		t.Fatalf("target-origin availability dispatch = calls %d authority reads %d requests %#v", tool.availabilityCalls, tool.authorityCalls, tool.availabilityRequests)
	}
	req := tool.availabilityRequests[0]
	if req.TargetAppointmentID != candidate.AppointmentID || req.PartySize != 2 || len(req.Segments) != 2 || req.Segments[1].GuestReference != "aunt-linh" {
		t.Fatalf("target shape lost in availability: %#v", req)
	}
	if first.PartyPlan == nil || len(first.PartyPlan.SplitOptions) != 1 || tool.actionCalls != 0 {
		t.Fatalf("complete replacement was not offered before action: %#v", first.PartyPlan)
	}

	second, err := service.handleRescheduleMessage(context.Background(), "owner_1", *first, *first, "Use the first full replacement.", "lifecycle-reschedule-2", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !internalLifecyclePending(*second, PendingInternalRescheduleConfirmation) || tool.actionCalls != 0 || !strings.Contains(store.lastTurn.AIMessage, "replace the whole appointment") {
		t.Fatalf("whole-root confirmation was not isolated: pending=%#v actions=%d reply=%q", second.DialogState.Pending, tool.actionCalls, store.lastTurn.AIMessage)
	}
	action, _, reviewed := reviewedInternalLifecycleAction(*second)
	if !reviewed {
		t.Fatal("selected replacement lost its quote-backed whole-root proof")
	}
	tool.actionResults = []*scheduling.ActionResult{lifecycleConfirmedResult(scheduling.ActionRequest{OperationType: scheduling.OperationKindReschedule, Segments: action.Segments}, booking.StatusRescheduled, len(action.Segments), false)}

	third, err := service.handleRescheduleMessage(context.Background(), "owner_1", *second, *second, "Yes.", "lifecycle-reschedule-3", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.actionCalls != 1 || third.Outcome != OutcomeBookingRescheduled || third.AppointmentID != candidate.AppointmentID {
		t.Fatalf("reschedule outcome = %#v actions=%d", third, tool.actionCalls)
	}
	actionReq := tool.actionRequests[0]
	if actionReq.TargetAuthority != booking.SchedulingAuthorityManleAICalendar || actionReq.ExpectedTargetAuthorityAppointmentVersion != 4 || actionReq.PartySize != 2 || len(actionReq.Segments) != 2 {
		t.Fatalf("whole-root action contract = %#v", actionReq)
	}
}

func TestInternalLifecyclePhoneGoldenCancelReasonAndMinimalAction(t *testing.T) {
	store, tool, service, session, candidate, _ := lifecycleConversationFixture(t)
	session.BookingAction = BookingActionCancel
	applyCancelCandidate(&session, candidate, timezoneLocation(store.cfg.Timezone))

	first, err := service.handleCancelMessage(context.Background(), "owner_1", session, session, "Cancel the appointment.", "lifecycle-cancel-1", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !internalLifecyclePending(*first, PendingInternalCancelReason) || tool.actionCalls != 0 {
		t.Fatalf("cancel reason was not requested first: %#v", first.DialogState.Pending)
	}
	second, err := service.handleCancelMessage(context.Background(), "owner_1", *first, *first, "Our flight now leaves before lunch.", "lifecycle-cancel-2", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !internalLifecyclePending(*second, PendingInternalCancelConfirmation) || tool.actionCalls != 0 {
		t.Fatalf("whole cancellation was not confirmed explicitly: %#v", second.DialogState.Pending)
	}
	preview, ok := schedulingActionRequest(*second, store.services, &store.cfg, scheduling.OperationKindCancel)
	if !ok {
		t.Fatal("minimal cancel request was rejected")
	}
	tool.actionResults = []*scheduling.ActionResult{lifecycleConfirmedResult(preview, booking.StatusCancelled, 0, false)}
	third, err := service.handleCancelMessage(context.Background(), "owner_1", *second, *second, "Yes.", "lifecycle-cancel-3", store.services, nil, nil, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.actionCalls != 1 || third.Outcome != OutcomeBookingCancelled {
		t.Fatalf("cancel outcome = %#v actions=%d", third, tool.actionCalls)
	}
	req := tool.actionRequests[0]
	if req.AvailabilityQuoteID != "" || req.SlotFingerprint != "" || len(req.Segments) != 0 || req.PartySize != 0 || !req.RequestedStartTime.IsZero() || !req.RequestedEndTime.IsZero() || req.RequestedTimezone != "" {
		t.Fatalf("internal cancel leaked create/reschedule evidence: %#v", req)
	}
	if req.Notes != "Our flight now leaves before lunch." || req.ExpectedTargetAuthorityAppointmentVersion != 4 {
		t.Fatalf("cancel reason/version lost: %#v", req)
	}
}

func TestInternalLifecycleCancellationReasonPassesFullTurnKernelWithoutPhraseMatching(t *testing.T) {
	store, tool, service, session, candidate, _ := lifecycleConversationFixture(t)
	session.BookingAction = BookingActionCancel
	applyCancelCandidate(&session, candidate, timezoneLocation(store.cfg.Timezone))
	setInternalLifecyclePending(&session, PendingInternalCancelReason, "")
	store.session = session

	got, err := service.Message(context.Background(), session.SalonID, "owner_1", session.ID, MessageRequest{
		Message: "The visiting relatives changed their rail itinerary.", EventKey: "kernel-cancel-reason",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.actionCalls != 0 || !internalLifecyclePending(*got, PendingInternalCancelConfirmation) ||
		selectedInternalLifecycleCancelReason(*got) != "The visiting relatives changed their rail itinerary." {
		t.Fatalf("state-scoped reason was not preserved through the turn kernel: pending=%#v actions=%d", got.DialogState.Pending, tool.actionCalls)
	}
}

func TestInternalLifecycleExactRescheduleReplaySkipsAvailabilityRefreshAfterLaterCancel(t *testing.T) {
	store, tool, service, session, _, availability := lifecycleConversationFixture(t)
	options := internalLifecycleOptionsFromAvailability(availability.VerifiedSlots, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone))
	if len(options) != 1 || !prepareSelectedInternalLifecycleOption(&session, options[0]) {
		t.Fatal("fixture did not produce reviewed lifecycle action")
	}
	setInternalLifecyclePending(&session, PendingInternalRescheduleConfirmation, options[0].ID)
	action, _, _ := reviewedInternalLifecycleAction(session)
	result := lifecycleConfirmedResult(scheduling.ActionRequest{OperationType: scheduling.OperationKindReschedule, Segments: action.Segments}, booking.StatusRescheduled, len(action.Segments), true)
	tool.actionResults = []*scheduling.ActionResult{result}
	for index, wording := range []string{"Yes, move the whole appointment.", "The reply was lost after the appointment was later cancelled; recover the same operation."} {
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, wording, "lifecycle-replay-"+string(rune('1'+index)), store.services, store.staff, &store.cfg)
		got, err := service.tryNeutralReschedule(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg)
		if err != nil || got.Outcome != OutcomeBookingRescheduled {
			t.Fatalf("replay %d = %#v err=%v", index+1, got, err)
		}
		reply := strings.ToLower(store.lastTurn.AIMessage)
		if strings.Contains(reply, "has been rescheduled") ||
			!strings.Contains(reply, "succeeded at that time") ||
			!strings.Contains(reply, "current status may have changed") ||
			!strings.Contains(reply, "not confirmation of its current status") {
			t.Fatalf("historical replay %d used current-state confirmation copy: %q", index+1, store.lastTurn.AIMessage)
		}
	}
	if tool.actionCalls != 2 || tool.availabilityCalls != 0 || tool.actionRequests[0].OperationKey != tool.actionRequests[1].OperationKey {
		t.Fatalf("exact lifecycle replay did not reach executor unchanged: actions=%d availability=%d requests=%#v", tool.actionCalls, tool.availabilityCalls, tool.actionRequests)
	}
}

func TestInternalLifecycleStaleTargetReoffersAndCutoffNeverConfirms(t *testing.T) {
	t.Run("stale target reloads authoritative version and reoffers", func(t *testing.T) {
		store, tool, service, session, candidate, availability := lifecycleConversationFixture(t)
		options := internalLifecycleOptionsFromAvailability(availability.VerifiedSlots, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone))
		if len(options) != 1 || !prepareSelectedInternalLifecycleOption(&session, options[0]) {
			t.Fatal("fixture did not produce reviewed lifecycle action")
		}
		setInternalLifecyclePending(&session, PendingInternalRescheduleConfirmation, options[0].ID)
		refreshed := candidate
		refreshed.AuthorityAppointmentVersion = 5
		tool.fakeBookingTool.candidates = []booking.AppointmentActionRef{{
			ID: refreshed.AppointmentID, SchedulingAuthority: refreshed.SchedulingAuthority, AuthorityAppointmentVersion: 5, PartySize: 2,
			Status: booking.StatusRescheduled, CustomerName: session.CustomerName, CustomerPhone: session.CustomerPhone, StartTime: refreshed.StartTime, EndTime: refreshed.EndTime,
			Segments: []booking.BookingSegmentRecord{
				{Service: booking.ServiceRef{ID: "service_builder"}, Staff: booking.StaffRef{ID: "staff_nhi"}, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "caller-jade", Quantity: 1},
				{Service: booking.ServiceRef{ID: "service_pedicure"}, Staff: booking.StaffRef{ID: "staff_uyen"}, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "aunt-linh", Quantity: 1},
			},
		}}
		freshAvailability := *availability
		freshAvailability.TargetAuthorityAppointmentVersion = 5
		tool.availabilityResults = []*scheduling.AvailabilityResult{&freshAvailability}
		tool.actionErrors = []error{booking.ErrOperationConflict}
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes.", "lifecycle-stale", store.services, store.staff, &store.cfg)
		got, err := service.tryNeutralReschedule(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if tool.actionCalls != 1 || tool.availabilityCalls != 1 || got.Outcome == OutcomeBookingRescheduled || got.DialogState.Pending != nil || len(got.PartyPlan.SplitOptions) != 1 {
			t.Fatalf("stale target was not safely reoffered: %#v actions=%d availability=%d", got, tool.actionCalls, tool.availabilityCalls)
		}
		if tool.availabilityRequests[0].PreferredDate != "2026-12-10" {
			t.Fatalf("stale reoffer changed the caller's replacement date: %#v", tool.availabilityRequests[0])
		}
		if got.RescheduleCandidates[0].AuthorityAppointmentVersion != 5 || strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "rescheduled") && !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "nothing was changed") {
			t.Fatalf("stale target produced false confirmation: %#v reply=%q", got.RescheduleCandidates, store.lastTurn.AIMessage)
		}
	})

	t.Run("cutoff equality error produces handoff without confirmation", func(t *testing.T) {
		store, tool, service, session, _, availability := lifecycleConversationFixture(t)
		options := internalLifecycleOptionsFromAvailability(availability.VerifiedSlots, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone))
		if len(options) != 1 || !prepareSelectedInternalLifecycleOption(&session, options[0]) {
			t.Fatal("fixture did not produce reviewed lifecycle action")
		}
		setInternalLifecyclePending(&session, PendingInternalRescheduleConfirmation, options[0].ID)
		tool.actionErrors = []error{booking.ErrSchedulingAuthorityNotReady}
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes.", "lifecycle-cutoff", store.services, store.staff, &store.cfg)
		got, err := service.tryNeutralReschedule(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome == OutcomeBookingRescheduled || got.AppointmentID != "" || got.Handoff == nil || !errors.Is(tool.actionErrors[0], booking.ErrSchedulingAuthorityNotReady) {
			t.Fatalf("cutoff failure was exposed as confirmation: %#v", got)
		}
		if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "has been rescheduled") {
			t.Fatalf("cutoff failure used false confirmation wording: %q", store.lastTurn.AIMessage)
		}
	})

	t.Run("cancelled target is terminal and is never reoffered", func(t *testing.T) {
		store, tool, service, session, _, availability := lifecycleConversationFixture(t)
		options := internalLifecycleOptionsFromAvailability(availability.VerifiedSlots, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone))
		if len(options) != 1 || !prepareSelectedInternalLifecycleOption(&session, options[0]) {
			t.Fatal("fixture did not produce reviewed lifecycle action")
		}
		setInternalLifecyclePending(&session, PendingInternalRescheduleConfirmation, options[0].ID)
		tool.fakeBookingTool.candidates = nil
		tool.actionErrors = []error{booking.ErrOperationConflict}
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes.", "lifecycle-terminal-cancel", store.services, store.staff, &store.cfg)
		got, err := service.tryNeutralReschedule(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got.Handoff == nil || got.Outcome == OutcomeBookingRescheduled || got.AppointmentID != "" || tool.availabilityCalls != 0 ||
			strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "has been rescheduled") {
			t.Fatalf("terminal target was reoffered or falsely confirmed: %#v reply=%q", got, store.lastTurn.AIMessage)
		}
	})
}

func TestInternalLifecycleRejectsProviderOrPartialConfirmationEvidence(t *testing.T) {
	store, _, _, session, _, availability := lifecycleConversationFixture(t)
	options := internalLifecycleOptionsFromAvailability(availability.VerifiedSlots, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone))
	if len(options) != 1 || !prepareSelectedInternalLifecycleOption(&session, options[0]) {
		t.Fatal("fixture did not produce reviewed lifecycle action")
	}
	action, _, _ := reviewedInternalLifecycleAction(session)
	base := lifecycleConfirmedResult(scheduling.ActionRequest{OperationType: scheduling.OperationKindReschedule, Segments: action.Segments}, booking.StatusRescheduled, len(action.Segments), false)
	tests := []struct {
		name   string
		mutate func(*scheduling.ActionResult)
	}{
		{name: "provider evidence", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.ExternalAttemptID = "square-attempt"
		}},
		{name: "partial children", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.Children = result.ConfirmedAppointment.Children[:1]
		}},
		{name: "missing status", mutate: func(result *scheduling.ActionResult) { result.ConfirmedAppointment.AppointmentStatus = "" }},
		{name: "wrong status", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.AppointmentStatus = booking.StatusConfirmed
		}},
		{name: "wrong active child count", mutate: func(result *scheduling.ActionResult) { result.ConfirmedAppointment.ActiveChildCount++ }},
		{name: "wrong new version", mutate: func(result *scheduling.ActionResult) { result.AuthorityAppointmentVersion = 9 }},
		{name: "wrong root", mutate: func(result *scheduling.ActionResult) { result.ConfirmedAppointment.AppointmentID = "different-root" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyResult := *base
			copyConfirmed := *base.ConfirmedAppointment
			copyConfirmed.Children = append([]scheduling.ConfirmedAppointmentSegment(nil), base.ConfirmedAppointment.Children...)
			copyResult.ConfirmedAppointment = &copyConfirmed
			test.mutate(&copyResult)
			turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes.", "malformed-"+test.name, store.services, store.staff, &store.cfg)
			if applyNeutralActionResult(&turn, session, &copyResult, store.services, store.staff, &store.cfg) {
				t.Fatalf("malformed lifecycle evidence was accepted: %#v", copyResult)
			}
		})
	}
}
