package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestTurnKernelRoutesConsultationWithoutTreatingServicesAsParty(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	ctx := &AIAnswerContext{Services: []ServiceOption{
		{ID: "classic", Name: "Classic Manicure"},
		{ID: "gel", Name: "Gel Manicure"},
	}}
	plan := service.planTurn("Help me choose between Classic Manicure and Gel Manicure.", store.session, ctx, &store.cfg)
	if plan.PartySignal.IsParty || plan.Route == TurnRouteFastLane {
		t.Fatalf("plan = %#v, want consultation to remain answer/semantic routed", plan)
	}
}

func TestTurnKernelDoesNotTreatInboundCallerPhoneAsBookingProgress(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	session := Session{
		ID: "session_1", SalonID: "salon_1", Channel: ChannelPhone,
		CustomerPhone: "+13125550199", Status: StatusActive,
		DialogState: DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	if hasOperationalBookingProgress(session) {
		t.Fatal("inbound caller phone must not start booking progress")
	}
	plan := service.planTurn("Can you help me understand your options?", session, &AIAnswerContext{}, &RuntimeConfig{Timezone: "UTC"})
	if plan.ExpectedInput != ExpectedInputCallerGoal {
		t.Fatalf("expected input = %q, want %q", plan.ExpectedInput, ExpectedInputCallerGoal)
	}
}

func TestTurnKernelRoutesSimpleExpectedFieldsWithoutSemanticModel(t *testing.T) {
	services := []ServiceOption{{ID: "classic", Name: "Classic Manicure"}}
	staff := []StaffOption{{ID: "staff_1", Name: "Kim", AIBookable: true}}
	ctx := &AIAnswerContext{Services: services, Staff: staff, ActiveStaff: staff}
	base := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, Status: StatusActive,
		ServiceID: "classic", ServiceName: "Classic Manicure",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "classic"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.now = fixedNow

	datePlan := service.planTurn("Next Wednesday.", base, ctx, &RuntimeConfig{Timezone: "America/Chicago"})
	if datePlan.Route != TurnRouteFastLane || datePlan.ExpectedInput != ExpectedInputRequestedDate {
		t.Fatalf("date plan = %#v", datePlan)
	}

	withDate := cloneSessionForTurn(base)
	withDate.RequestedDate = "2026-06-10"
	staffPlan := service.planTurn("Anyone available is fine.", withDate, ctx, &RuntimeConfig{Timezone: "America/Chicago"})
	if staffPlan.Route == TurnRouteSemanticLane && staffPlan.ExpectedInput == ExpectedInputRequestedTime {
		// Staff is not the expected field yet, so the semantic lane must retain
		// the unexpected correction instead of silently consuming it.
		return
	}
	if staffPlan.Route == TurnRouteFastLane {
		t.Fatalf("unexpected staff answer should not bypass the pending time question: %#v", staffPlan)
	}
}

func TestTurnKernelKeepsCorrectionsAndMultiIntentOnSemanticLane(t *testing.T) {
	services := []ServiceOption{
		{ID: "gel", Name: "Gel Manicure"},
		{ID: "spa", Name: "Spa Pedicure"},
	}
	ctx := &AIAnswerContext{Services: services}
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, ServiceID: "gel", ServiceName: "Gel Manicure",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "gel"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.now = fixedNow
	for _, message := range []string{
		"Use Spa Pedicure instead.",
		"Move it to next Friday and use Spa Pedicure instead.",
		"Use the relaxing option; what does my appointment contain now?",
	} {
		plan := service.planTurn(message, session, ctx, &RuntimeConfig{Timezone: "America/Chicago"})
		if plan.Route != TurnRouteSemanticLane {
			t.Fatalf("message %q plan = %#v, want semantic lane", message, plan)
		}
	}
}

func TestTurnKernelSeparatesSemanticRouteReasons(t *testing.T) {
	services := []ServiceOption{{ID: "gel", Name: "Gel Manicure"}}
	staff := []StaffOption{{ID: "kim", Name: "Kim", AIBookable: true}}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.now = fixedNow

	multiple := service.planTurn(
		"Please book Gel Manicure with Kim.",
		Session{ID: "session_1", SalonID: "salon_1", DialogState: DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting}},
		&AIAnswerContext{Services: services, Staff: staff, ActiveStaff: staff},
		&RuntimeConfig{Timezone: "UTC"},
	)
	if multiple.Route != TurnRouteSemanticLane || multiple.Reason != "multiple_signals" {
		t.Fatalf("multiple-signal plan = %#v", multiple)
	}

	inProgress := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking,
		ServiceID: "gel", ServiceName: "Gel Manicure",
		StaffSelectionMode: booking.StaffSelectionAnyone,
		BookingSegments:    []booking.BookingSegmentRequest{{ServiceID: "gel", StaffSelectionMode: booking.StaffSelectionAnyone}},
		DialogState:        DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	conflict := service.planTurn(
		"What are your hours? My phone number is 312-555-0199.",
		inProgress,
		&AIAnswerContext{
			Services:      services,
			BusinessHours: []BusinessHourPeriod{{ID: "hours_thu", DayOfWeek: 4, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"}},
		},
		&RuntimeConfig{Timezone: "UTC"},
	)
	if conflict.Route != TurnRouteSemanticLane || conflict.Reason != "structured_answer_conflict" {
		t.Fatalf("structured-answer conflict plan = %#v", conflict)
	}
}

func TestTurnKernelDistinguishesCurrentDraftCountFromCatalogCount(t *testing.T) {
	services := []ServiceOption{
		{ID: "classic", Name: "Classic Manicure", CategoryName: "Manicure"},
		{ID: "gel", Name: "Gel Manicure", CategoryName: "Manicure"},
	}
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking,
		DialogState: DialogState{Version: DialogStateVersion, Phase: DialogPhaseClarifying, Pending: &PendingConversationAct{PromptKey: "add_target"}},
	}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	ctx := &AIAnswerContext{Services: services}

	catalog := service.planTurn("How many manicure services do you have?", session, ctx, &RuntimeConfig{Timezone: "UTC"})
	if catalog.Route != TurnRouteAnswerLane || len(catalog.Understanding.Questions) != 1 || catalog.Understanding.Questions[0].Subject != ConversationQuestionCatalog {
		t.Fatalf("catalog plan = %#v", catalog)
	}

	withDraft := cloneSessionForTurn(session)
	withDraft.ServiceID = "classic"
	withDraft.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "classic"}}
	current := service.planTurn("How many services did I book?", withDraft, ctx, &RuntimeConfig{Timezone: "UTC"})
	if current.Route != TurnRouteAnswerLane || len(current.Understanding.Questions) != 1 || current.Understanding.Questions[0].Subject != ConversationQuestionCurrentBooking {
		t.Fatalf("current draft plan = %#v", current)
	}
}

func TestTurnKernelRoutesOfferedSlotSelectionFast(t *testing.T) {
	start := time.Date(2026, 6, 10, 13, 0, 0, 0, time.UTC)
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking,
		ServiceID: "classic", ServiceName: "Classic Manicure", RequestedDate: "2026-06-10",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "classic", StaffSelectionMode: booking.StaffSelectionAnyone}},
		OfferedSlots: []OfferedSlot{{
			StartTime: start, EndTime: start.Add(30 * time.Minute), StaffID: "staff_1", StaffSelectionMode: booking.StaffSelectionAnyone,
			Segments: []OfferedSlotSegment{{ServiceID: "classic", StaffID: "staff_1", StaffSelectionMode: booking.StaffSelectionAnyone}},
		}},
		DialogState: DialogState{Version: DialogStateVersion, Phase: DialogPhaseAvailability},
	}
	ctx := &AIAnswerContext{Services: []ServiceOption{{ID: "classic", Name: "Classic Manicure"}}}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	plan := service.planTurn("The first one works for me.", session, ctx, &RuntimeConfig{Timezone: "UTC"})
	if plan.Route != TurnRouteFastLane || plan.Reason != "offered_slot_selection" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestTurnKernelPreservesCatalogScopeForUnseenServiceCorrection(t *testing.T) {
	services := []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "classic_pedi", Name: "Classic Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, ServiceID: "gel_mani", ServiceName: "Gel Manicure",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "gel_mani"}, {ServiceID: "classic_mani"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	interpreter := &fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.97,
		Acts: []ConversationAct{{
			Kind: ConversationActReplace, Entity: ConversationEntityService,
			SourceServiceIDs: []string{"gel_mani", "classic_mani"}, TargetServiceIDs: []string{"classic_pedi", "spa_pedi"},
			TargetCategoryID: "pedi", TargetCategoryName: "Pedicure", Scope: ConversationScopeAllMatching, Confidence: 0.97,
		}},
	}}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	ctx := &AIAnswerContext{Services: services}
	plan := service.planTurn("I think I will switch from manicure to pedicure.", session, ctx, &RuntimeConfig{Timezone: "UTC"})
	turn := service.turnUnderstandingForPlan(context.Background(), session, "I think I will switch from manicure to pedicure.", services, nil, nil, nil, plan)
	if turn.CatalogFallback || len(turn.Acts) != 1 {
		t.Fatalf("plan=%#v turn=%#v", plan, turn)
	}
}

type contextBlockingTurnInterpreter struct{}

func (contextBlockingTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	<-ctx.Done()
	return TurnUnderstanding{}, ctx.Err()
}

type immediateTimeoutTurnInterpreter struct {
	calls int
}

func (i *immediateTimeoutTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	i.calls++
	return TurnUnderstanding{}, context.DeadlineExceeded
}

type deadlineCapturingTurnInterpreter struct {
	deadline  time.Time
	remaining time.Duration
}

func (i *deadlineCapturingTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	i.deadline, _ = ctx.Deadline()
	i.remaining = time.Until(i.deadline)
	return TurnUnderstanding{}, nil
}

func TestTurnKernelAppliesSemanticTimeoutBudget(t *testing.T) {
	interpreter := &deadlineCapturingTurnInterpreter{}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	session := Session{ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, DialogState: DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting}}
	services := []ServiceOption{{ID: "gel", Name: "Gel Manicure"}}
	plan := TurnPlan{
		Route: TurnRouteSemanticLane, ExpectedInput: ExpectedInputService, DeterministicCoverage: TurnCoverageNone,
		SemanticServices: services,
	}
	service.turnUnderstandingForPlan(context.Background(), session, "I need some help choosing.", services, nil, nil, nil, plan)
	if interpreter.deadline.IsZero() || interpreter.remaining <= semanticTurnTimeout-250*time.Millisecond || interpreter.remaining > semanticTurnTimeout {
		t.Fatalf("semantic deadline budget = %s, want approximately %s", interpreter.remaining, semanticTurnTimeout)
	}
}

func TestTurnKernelBoundsSemanticLatencyAndPreservesDraftFallback(t *testing.T) {
	services := []ServiceOption{{ID: "gel", Name: "Gel Manicure"}, {ID: "spa", Name: "Spa Pedicure"}}
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, ServiceID: "gel",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "gel"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.SetTurnInterpreter(contextBlockingTurnInterpreter{})
	plan := service.planTurn("The premium treatment would fit better.", session, &AIAnswerContext{Services: services}, &RuntimeConfig{Timezone: "UTC"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	turn := service.turnUnderstandingForPlan(ctx, session, "The premium treatment would fit better.", services, nil, nil, nil, plan)
	if !turn.ModelInvoked || turn.InterpreterOutcome != TurnInterpreterOutcomeTimeout || len(turn.Acts) != 0 {
		t.Fatalf("turn = %#v", turn)
	}
}

func TestTurnKernelScopesSemanticCatalogToRelevantDomain(t *testing.T) {
	services := []ServiceOption{
		{ID: "gel", Name: "Gel Manicure"},
		{ID: "spa", Name: "Spa Pedicure"},
		{ID: "dip", Name: "Dip Powder"},
	}
	staff := []StaffOption{{ID: "kim", Name: "Kim", AIBookable: true}, {ID: "mai", Name: "Mai", AIBookable: true}}
	session := Session{
		ID: "session_1", SalonID: "salon_1", Intent: IntentBooking, ServiceID: "gel",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "gel"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseDrafting},
	}
	service := NewService(newFakeConversationStore(), &fakeBookingTool{})
	service.now = fixedNow
	plan := service.planTurn("Next Friday, and please use Kim.", session, &AIAnswerContext{Services: services, Staff: staff, ActiveStaff: staff}, &RuntimeConfig{Timezone: "UTC"})
	if plan.Route != TurnRouteSemanticLane || len(plan.SemanticServices) != 1 || plan.SemanticServices[0].ID != "gel" || len(plan.SemanticStaff) != len(staff) {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestMessageFastLaneSkipsSemanticForUnseenExpectedDateWording(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_1", StaffSelectionMode: booking.StaffSelectionAnyone}}
	interpreter := &fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "information", Confidence: 0.99,
		Questions: []ConversationQuestion{{Subject: ConversationQuestionPolicy, Confidence: 0.99}},
	}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow
	var timings []TurnTiming

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "This coming Thursday would be best.",
		TimingRecorder: func(timing TurnTiming) {
			timings = append(timings, timing)
		},
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if interpreter.calls != 0 || session.RequestedDate != "2026-06-11" {
		t.Fatalf("interpreter calls=%d session=%#v", interpreter.calls, session)
	}
	found := false
	for _, timing := range timings {
		if timing.Stage == TurnTimingStageTurnRouter && timing.Result == TurnRouteFastLane && timing.Attributes["turn_expected_input"] == ExpectedInputRequestedDate {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing fast-lane diagnostics: %#v", timings)
	}
}

func TestPhoneBookingCategoryProviderFailureUsesBoundedRecoveryThenExactCatalogService(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+13125550199"
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.services = []ServiceOption{
		{ID: "express_mani", Name: "Express Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"},
	}
	store.knowledge = []KnowledgeSnippet{{ID: "knowledge_1", Title: "Manicure options", Body: "We provide several manicure treatments."}}
	bookingTool := &fakeBookingTool{}
	interpreter := &immediateTimeoutTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I'd like a manicure appointment.",
	})
	if err != nil {
		t.Fatalf("category Message: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("category booking semantic interpreter calls = %d, want 1", interpreter.calls)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 || session.Handoff != nil {
		t.Fatalf("category booking state = service=%q segments=%#v handoff=%#v", session.ServiceID, session.BookingSegments, session.Handoff)
	}
	if session.DialogState.Pending != nil && session.DialogState.Pending.PromptKey == "semantic_add_or_replace" {
		t.Fatalf("initial category booking created service-edit pending state: %#v", session.DialogState)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "book") || !strings.Contains(store.lastTurn.AIMessage, "help choosing") {
		t.Fatalf("provider failure recovery = %q", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Gel Manicure, please.",
	})
	if err != nil {
		t.Fatalf("exact service Message: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("exact catalog service added semantic interpreter calls: total=%d, want 1", interpreter.calls)
	}
	if session.ServiceID != "gel_mani" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "gel_mani" {
		t.Fatalf("selected service state = service=%q segments=%#v", session.ServiceID, session.BookingSegments)
	}
	if session.DialogState.Pending != nil || session.Handoff != nil {
		t.Fatalf("selected service retained invalid state: dialog=%#v handoff=%#v", session.DialogState, session.Handoff)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("next prompt = %q, want requested date", store.lastTurn.AIMessage)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("service collection called booking tools: availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
}

func TestPhoneBookingRepairsInvalidSemanticServiceEditPending(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+19047504572"
	store.session.Intent = IntentBooking
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.session.DialogState = DialogState{
		Version: DialogStateVersion, Phase: DialogPhaseClarifying, NoProgressCount: 2, LastPromptKey: "semantic_add_or_replace",
		Pending: &PendingConversationAct{PromptKey: "semantic_add_or_replace", TargetServiceIDs: []string{"classic_pedi", "spa_pedi"}},
	}
	store.services = []ServiceOption{
		{ID: "classic_pedi", Name: "Classic Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}
	bookingTool := &fakeBookingTool{}
	interpreter := &immediateTimeoutTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Classic Pedicure."})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if interpreter.calls != 0 {
		t.Fatalf("invalid pending repair invoked semantic interpreter %d times", interpreter.calls)
	}
	if session.ServiceID != "classic_pedi" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "classic_pedi" {
		t.Fatalf("repaired service state = service=%q segments=%#v", session.ServiceID, session.BookingSegments)
	}
	if session.DialogState.Pending != nil || session.DialogState.NoProgressCount != 0 || session.Handoff != nil {
		t.Fatalf("invalid pending was not cleared: dialog=%#v handoff=%#v", session.DialogState, session.Handoff)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("next prompt = %q, want requested date", store.lastTurn.AIMessage)
	}
}

func TestPhoneBookingSemanticTimeoutFallsBackToCatalogAndCapturedFields(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+13125550199"
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.services = []ServiceOption{{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"}}
	store.staff = []StaffOption{{ID: "kim", Name: "Kim", AIBookable: true}}
	store.activeStaff = append([]StaffOption(nil), store.staff...)
	bookingTool := &fakeBookingTool{}
	interpreter := &immediateTimeoutTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Please book Gel Manicure with Kim.",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("semantic interpreter calls = %d, want 1", interpreter.calls)
	}
	if session.ServiceID != "gel_mani" || session.StaffID != "kim" || len(session.BookingSegments) != 1 {
		t.Fatalf("timeout fallback state = service=%q staff=%q segments=%#v", session.ServiceID, session.StaffID, session.BookingSegments)
	}
	if session.DialogState.Pending != nil || session.Handoff != nil {
		t.Fatalf("timeout fallback created invalid pending/handoff: dialog=%#v handoff=%#v", session.DialogState, session.Handoff)
	}
	if got := store.lastTurn.CustomerMetadata["turn_interpreter_outcome"]; got != TurnInterpreterOutcomeTimeout {
		t.Fatalf("turn_interpreter_outcome = %#v", got)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("next prompt = %q, want requested date", store.lastTurn.AIMessage)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("timeout fallback called booking tools: availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
}

func TestSemanticAddOrReplacePendingHandlesReplaceOnlyDeterministically(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "classic_mani"
	store.session.ServiceName = "Classic Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "classic_mani", StaffSelectionMode: booking.StaffSelectionAnyone}}
	store.session.DialogState = DialogState{
		Version: DialogStateVersion, Phase: DialogPhaseClarifying, LastPromptKey: "semantic_add_or_replace",
		Pending: &PendingConversationAct{PromptKey: "semantic_add_or_replace", TargetServiceIDs: []string{"spa_pedi"}},
	}
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}
	interpreter := &immediateTimeoutTurnInterpreter{}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Spa Pedicure only."})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if interpreter.calls != 0 {
		t.Fatalf("pending replace invoked semantic interpreter %d times", interpreter.calls)
	}
	if session.ServiceID != "spa_pedi" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "spa_pedi" {
		t.Fatalf("replaced service state = service=%q segments=%#v", session.ServiceID, session.BookingSegments)
	}
	if session.DialogState.Pending != nil || session.DialogState.NoProgressCount != 0 {
		t.Fatalf("pending replace did not reset dialog state: %#v", session.DialogState)
	}
}

func TestPartyCorrectionTimeoutCollectsGuestThenOperationWithoutLosingReviewState(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.Intent = IntentBooking
	store.session.ServiceID = "gel_mani"
	store.session.ServiceName = "Gel Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{
		{ServiceID: "gel_mani", StaffSelectionMode: booking.StaffSelectionAnyone},
		{ServiceID: "classic_pedi", StaffSelectionMode: booking.StaffSelectionAnyone},
	}
	store.session.PartyPlan = &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
		{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"gel_mani"}},
		{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"classic_pedi"}},
	}}
	store.session.OfferedSlots = []OfferedSlot{{StartTime: testStartTime(), EndTime: testStartTime().Add(90 * time.Minute)}}
	store.session.DialogState = DialogState{
		Version: DialogStateVersion, Phase: DialogPhaseReview, DraftRevision: 4,
		ReviewRequired: true, ReviewAccepted: true, ReviewedRevision: 4, AuthorizedRevision: 4,
	}
	store.services = []ServiceOption{
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "classic_pedi", Name: "Classic Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}
	interpreter := &immediateTimeoutTurnInterpreter{}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Spa Pedicure, please."})
	if err != nil {
		t.Fatalf("target turn: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("semantic calls after target = %d, want 1", interpreter.calls)
	}
	if pending := session.DialogState.Pending; pending == nil || pending.PromptKey != PendingPartyServiceGuest || !sameStrings(pending.TargetServiceIDs, []string{"spa_pedi"}) || pending.GuestRef != "" {
		t.Fatalf("guest pending = %#v", pending)
	}
	if !session.DialogState.ReviewAccepted || session.DialogState.AuthorizedRevision != 4 || len(session.OfferedSlots) != 1 {
		t.Fatalf("unresolved correction cleared review/slots: state=%#v slots=%#v", session.DialogState, session.OfferedSlots)
	}
	if reply := strings.ToLower(store.lastTurn.AIMessage); !strings.Contains(reply, "who should get spa pedicure") || !strings.Contains(reply, "guest 2") {
		t.Fatalf("guest prompt = %q", store.lastTurn.AIMessage)
	}

	service.SetTurnInterpreter(nil)
	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Book it now."})
	if err != nil {
		t.Fatalf("unresolved booking turn: %v", err)
	}
	if bookingTool.calls != 0 || session.DialogState.Pending == nil || session.DialogState.Pending.PromptKey != PendingPartyServiceGuest {
		t.Fatalf("unresolved correction reached booking: calls=%d state=%#v", bookingTool.calls, session.DialogState)
	}
	if !session.DialogState.ReviewAccepted || session.DialogState.AuthorizedRevision != 4 || len(session.OfferedSlots) != 1 {
		t.Fatalf("unresolved booking turn cleared review/slots: state=%#v slots=%#v", session.DialogState, session.OfferedSlots)
	}
	if reply := strings.ToLower(store.lastTurn.AIMessage); !strings.Contains(reply, "who should get spa pedicure") {
		t.Fatalf("unresolved booking did not resume correction: %q", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "My friend, guest 2."})
	if err != nil {
		t.Fatalf("guest turn: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("guest reply invoked semantic interpreter; calls=%d", interpreter.calls)
	}
	if pending := session.DialogState.Pending; pending == nil || pending.PromptKey != PendingPartyServiceOperation || pending.GuestRef != "guest 2" || !sameStrings(pending.SourceServiceIDs, []string{"classic_pedi"}) || !sameStrings(pending.TargetServiceIDs, []string{"spa_pedi"}) {
		t.Fatalf("operation pending = %#v", pending)
	}
	if !session.DialogState.ReviewAccepted || session.DialogState.AuthorizedRevision != 4 || len(session.OfferedSlots) != 1 {
		t.Fatalf("guest selection cleared review/slots before mutation: state=%#v slots=%#v", session.DialogState, session.OfferedSlots)
	}
	if reply := strings.ToLower(store.lastTurn.AIMessage); !strings.Contains(reply, "for guest 2") || !strings.Contains(reply, "add spa pedicure") || !strings.Contains(reply, "replace classic pedicure") {
		t.Fatalf("operation prompt = %q", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Switch it instead."})
	if err != nil {
		t.Fatalf("operation turn: %v", err)
	}
	if interpreter.calls != 1 {
		t.Fatalf("operation reply invoked semantic interpreter; calls=%d", interpreter.calls)
	}
	if got := session.PartyPlan.Groups[0].ResolvedServiceIDs; !sameStrings(got, []string{"gel_mani"}) {
		t.Fatalf("caller group changed = %#v", got)
	}
	if got := session.PartyPlan.Groups[1].ResolvedServiceIDs; !sameStrings(got, []string{"spa_pedi"}) {
		t.Fatalf("guest 2 group = %#v, want Spa Pedicure", got)
	}
	if !sameStrings(selectedServiceIDs(*session), []string{"gel_mani", "spa_pedi"}) {
		t.Fatalf("booking segments = %#v", selectedServiceIDs(*session))
	}
	if session.DialogState.Pending != nil || session.DialogState.ReviewAccepted || session.DialogState.AuthorizedRevision != 0 || len(session.OfferedSlots) != 0 {
		t.Fatalf("resolved correction state = %#v slots=%#v", session.DialogState, session.OfferedSlots)
	}
	if reply := strings.ToLower(store.lastTurn.AIMessage); !strings.Contains(reply, "changed the service for guest 2 to spa pedicure") {
		t.Fatalf("scoped acknowledgement = %q", store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("correction called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPartyCorrectionAddIsScopedAndIdempotent(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "gel_mani"
	store.session.ServiceName = "Gel Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "gel_mani"}, {ServiceID: "classic_pedi"}}
	store.session.PartyPlan = &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
		{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"gel_mani"}},
		{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"classic_pedi"}},
	}}
	store.services = []ServiceOption{
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "classic_pedi", Name: "Classic Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}
	interpreter := &immediateTimeoutTurnInterpreter{}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	for attempt := 1; attempt <= 2; attempt++ {
		session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Could you add Spa Pedicure for guest 2?"})
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if got := session.PartyPlan.Groups[0].ResolvedServiceIDs; !sameStrings(got, []string{"gel_mani"}) {
			t.Fatalf("attempt %d caller group = %#v", attempt, got)
		}
		if got := session.PartyPlan.Groups[1].ResolvedServiceIDs; !sameStrings(got, []string{"classic_pedi", "spa_pedi"}) {
			t.Fatalf("attempt %d guest group = %#v", attempt, got)
		}
		if !partyPlanComplete(session.PartyPlan) || !sameStrings(selectedServiceIDs(*session), []string{"gel_mani", "classic_pedi", "spa_pedi"}) {
			t.Fatalf("attempt %d party draft = plan %#v segments %#v", attempt, session.PartyPlan, selectedServiceIDs(*session))
		}
	}
}
