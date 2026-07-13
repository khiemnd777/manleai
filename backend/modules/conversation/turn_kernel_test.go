package conversation

import (
	"context"
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
