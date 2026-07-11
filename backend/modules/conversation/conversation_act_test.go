package conversation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type fakeConversationActInterpreter struct {
	act     ConversationAct
	turn    TurnUnderstanding
	calls   int
	request TurnInterpretationRequest
}

type scriptedTurnInterpreter struct {
	interpret func(req TurnInterpretationRequest) TurnUnderstanding
}

func (s *scriptedTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	return s.interpret(req), nil
}

func (f *fakeConversationActInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	f.calls++
	f.request = req
	if f.turn.Goal != "" || len(f.turn.Acts) > 0 || len(f.turn.Questions) > 0 {
		return f.turn, nil
	}
	return TurnUnderstanding{Goal: "book_appointment", Acts: []ConversationAct{f.act}, Confidence: f.act.Confidence, Source: "structured_ai"}, nil
}

func TestConversationActGoldenSwitchesServiceFamilyWithoutPendingLoop(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic_mani", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_gel_mani", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_classic_pedi", Name: "Classic Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
		{ID: "service_spa_pedi", Name: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel_mani"
	store.session.ServiceName = "Gel Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{
		{ServiceID: "service_gel_mani", StaffSelectionMode: booking.StaffSelectionAnyone},
		{ServiceID: "service_classic_mani", StaffSelectionMode: booking.StaffSelectionAnyone},
	}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&scriptedTurnInterpreter{interpret: func(req TurnInterpretationRequest) TurnUnderstanding {
		message := strings.ToLower(req.CustomerMessage)
		if strings.Contains(message, "how many") {
			return TurnUnderstanding{Goal: "information", Questions: []ConversationQuestion{{Subject: ConversationQuestionCurrentBooking, Confidence: 0.98}}, Confidence: 0.98}
		}
		targetIDs := []string{"service_classic_pedi", "service_spa_pedi"}
		targetCategoryName := "Pedicure"
		if strings.Contains(message, "spa pedicure") {
			targetIDs = []string{"service_spa_pedi"}
			targetCategoryName = ""
		}
		return TurnUnderstanding{Goal: "book_appointment", Confidence: 0.97, Acts: []ConversationAct{{
			Kind: ConversationActReplace, Entity: ConversationEntityService,
			SourceServiceIDs: []string{"service_gel_mani", "service_classic_mani"}, TargetServiceIDs: targetIDs,
			TargetCategoryID: "cat_pedi", TargetCategoryName: targetCategoryName, Scope: ConversationScopeAllMatching, Confidence: 0.97,
		}}}
	}})
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I think I will switch from manicure to pedicure.",
	})
	if err != nil {
		t.Fatalf("directional replacement: %v", err)
	}
	if got := selectedServiceIDs(*session); !sameStrings(got, []string{"service_gel_mani", "service_classic_mani"}) {
		t.Fatalf("services changed before target selection: %#v", got)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which pedicure") || strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which manicure") {
		t.Fatalf("wrong replacement target prompt: %s", store.lastTurn.AIMessage)
	}
	if session.DialogState.Pending == nil || !sameStrings(session.DialogState.Pending.SourceServiceIDs, []string{"service_gel_mani", "service_classic_mani"}) {
		t.Fatalf("pending source services = %#v", session.DialogState.Pending)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "How many manicure I book?",
	})
	if err != nil {
		t.Fatalf("current booking summary: %v", err)
	}
	for _, expected := range []string{"two services", "Gel Manicure", "Classic Manicure"} {
		if !strings.Contains(store.lastTurn.AIMessage, expected) {
			t.Fatalf("summary missing %q: %s", expected, store.lastTurn.AIMessage)
		}
	}
	if session.DialogState.Pending == nil || session.DialogState.Pending.TargetCategoryName != "Pedicure" {
		t.Fatalf("informational detour cleared pending target: %#v", session.DialogState)
	}

	_, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "I want to change to pedicure."})
	if err != nil {
		t.Fatalf("repeated category correction: %v", err)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Pedicure is a service category") || !strings.Contains(store.lastTurn.AIMessage, "Classic Pedicure") {
		t.Fatalf("repeat should explain the category and offer concrete targets: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Spa Pedicure."})
	if err != nil {
		t.Fatalf("concrete replacement target: %v", err)
	}
	if got := selectedServiceIDs(*session); !sameStrings(got, []string{"service_spa_pedi"}) {
		t.Fatalf("selected services = %#v, want Spa Pedicure only", got)
	}
	if session.DialogState.Pending != nil || session.DialogState.LastMutation == nil {
		t.Fatalf("resolved mutation state = %#v", session.DialogState)
	}
}

func TestConversationActExplicitTargetEscapesStalePendingCandidates(t *testing.T) {
	services := []ServiceOption{
		{ID: "service_classic_mani", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_gel_mani", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_spa_pedi", Name: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
	}
	session := Session{
		ServiceID:       "service_classic_mani",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_classic_mani", StaffSelectionMode: booking.StaffSelectionAnyone}},
		DialogState: DialogState{
			Version: DialogStateVersion,
			Pending: &PendingConversationAct{
				Kind:             ConversationActReplace,
				SourceServiceIDs: []string{"service_classic_mani"},
				TargetServiceIDs: []string{"service_classic_mani", "service_gel_mani"},
				PromptKey:        "replace_target",
			},
		},
	}
	act := deterministicConversationAct(session, "Actually, Spa Pedicure.", services, nil, nil)
	if act.Kind != ConversationActReplace || !sameStrings(act.TargetServiceIDs, []string{"service_spa_pedi"}) {
		t.Fatalf("explicit out-of-set target act = %#v", act)
	}
}

func TestFinalReviewRequiresAuthorizationBeforePOSBooking(t *testing.T) {
	store := newFakeConversationStore()
	store.session.DialogState = DialogState{Version: DialogStateVersion, ReviewRequired: true, Phase: DialogPhaseOpen}
	bookingTool := &fakeBookingTool{attempt: &booking.BookingAttempt{
		ID:           "attempt_1",
		Status:       booking.StatusConfirmed,
		POSBookingID: "pos_booking_1",
		Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
	}}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("review turn: %v", err)
	}
	if bookingTool.calls != 0 || session.Outcome != OutcomeCollecting || session.DialogState.Phase != DialogPhaseReview {
		t.Fatalf("booking ran before review: calls=%d session=%#v", bookingTool.calls, session)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Let me review everything") || !strings.Contains(store.lastTurn.AIMessage, "Would you like me to book it") {
		t.Fatalf("review prompt = %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Yes, please book it."})
	if err != nil {
		t.Fatalf("booking authorization: %v", err)
	}
	if bookingTool.calls != 1 || session.Outcome != OutcomeBookingConfirmed || session.AppointmentID == "" {
		t.Fatalf("booking result: calls=%d session=%#v", bookingTool.calls, session)
	}
}

func TestStructuredAIConversationActIsCatalogValidatedBeforeMutation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	interpreter := &fakeConversationActInterpreter{act: ConversationAct{
		Kind:             ConversationActReplace,
		SourceServiceIDs: []string{"service_gel"},
		TargetServiceIDs: []string{"service_spa"},
		Scope:            ConversationScopeOne,
		Confidence:       0.91,
	}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Make it the relaxing one instead."})
	if err != nil {
		t.Fatalf("valid structured act: %v", err)
	}
	if interpreter.calls != 1 || session.ServiceID != "service_spa" {
		t.Fatalf("valid act was not applied: calls=%d session=%#v", interpreter.calls, session)
	}
	if got := store.lastTurn.CustomerMetadata["conversation_act_source"]; got != "structured_ai" {
		t.Fatalf("conversation act source = %#v", got)
	}

	store.session = *session
	invalid := &fakeConversationActInterpreter{act: ConversationAct{
		Kind:             ConversationActReplace,
		SourceServiceIDs: []string{"service_spa"},
		TargetServiceIDs: []string{"invented_service"},
		Confidence:       0.99,
	}}
	service.SetTurnInterpreter(invalid)
	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Make it the secret menu service."})
	if err != nil {
		t.Fatalf("invalid structured act fallback: %v", err)
	}
	if session.ServiceID != "service_spa" {
		t.Fatalf("invalid catalog ID mutated service: %#v", session)
	}
}

func TestSemanticInitialCategorySelectionPreservesCatalogAmbiguity(t *testing.T) {
	tests := []struct {
		name            string
		message         string
		services        []ServiceOption
		categoryAliases []ServiceCategoryAlias
		modelTargetID   string
		wantIDs         []string
		wantReply       string
	}{
		{
			name:    "category name",
			message: "I want to book a manicure service.",
			services: []ServiceOption{
				{ID: "service_classic_mani", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
				{ID: "service_gel_mani", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
			},
			modelTargetID: "service_classic_mani",
			wantIDs:       []string{"service_classic_mani", "service_gel_mani"},
			wantReply:     "Which manicure",
		},
		{
			name:    "different category alias wording",
			message: "Could I get something from the foot care menu?",
			services: []ServiceOption{
				{ID: "service_classic_pedi", Name: "Classic Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
				{ID: "service_spa_pedi", Name: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
			},
			categoryAliases: []ServiceCategoryAlias{{
				ID: "alias_foot_care", CategoryID: "cat_pedi", CategoryName: "Pedicure",
				Alias: "foot care", NormalizedAlias: "foot care", Source: "owner", Confidence: 0.95,
			}},
			modelTargetID: "service_classic_pedi",
			wantIDs:       []string{"service_classic_pedi", "service_spa_pedi"},
			wantReply:     "Which pedicure",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = test.services
			store.categoryAliases = test.categoryAliases
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
				Goal: "book_appointment", Confidence: 0.96,
				Acts: []ConversationAct{{
					Kind: ConversationActAdd, Entity: ConversationEntityService,
					TargetServiceIDs: []string{test.modelTargetID}, Confidence: 0.96,
				}},
			}})

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: test.message})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if session.ServiceID != "" || len(session.BookingSegments) != 0 {
				t.Fatalf("category request selected a concrete service: %#v", session)
			}
			if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
				t.Fatalf("category request called booking tools: availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
			}
			if session.DialogState.Pending == nil || !sameStrings(session.DialogState.Pending.TargetServiceIDs, test.wantIDs) {
				t.Fatalf("pending candidates = %#v, want %#v", session.DialogState.Pending, test.wantIDs)
			}
			if !strings.Contains(store.lastTurn.AIMessage, test.wantReply) {
				t.Fatalf("clarification reply = %q, want %q", store.lastTurn.AIMessage, test.wantReply)
			}
		})
	}
}

func TestSemanticSummaryCannotDiscardDeterministicDateCorrection(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-06-10"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID: "service_1", StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	store.session.OfferedSlots = []OfferedSlot{{
		StartTime: time.Date(2026, 6, 10, 17, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 10, 17, 45, 0, 0, time.UTC),
		StaffID:   "staff_1",
	}}
	bookingTool := &fakeBookingTool{availabilityResult: availabilityResultForStart(
		"service_1", "Classic Manicure", time.Date(2026, 6, 15, 17, 0, 0, 0, time.UTC),
	)}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "information", Confidence: 0.95,
		Questions: []ConversationQuestion{{Subject: ConversationQuestionCurrentBooking, Confidence: 0.95}},
	}})
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Next Monday."})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.RequestedDate != "2026-06-10" || session.DialogState.Pending == nil {
		t.Fatalf("date correction should wait for confirmation: %#v", session)
	}
	if bookingTool.availabilityCalls != 0 || !strings.Contains(store.lastTurn.AIMessage, "Monday") {
		t.Fatalf("date correction prompt = %q calls=%d", store.lastTurn.AIMessage, bookingTool.availabilityCalls)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Yes."})
	if err != nil {
		t.Fatalf("confirmation Message returned error: %v", err)
	}
	if session.RequestedDate != "2026-06-15" {
		t.Fatalf("confirmed requested date = %q, want 2026-06-15", session.RequestedDate)
	}
	if bookingTool.availabilityCalls != 1 || bookingTool.availabilityRequest.PreferredDate != "2026-06-15" {
		t.Fatalf("availability request = %#v calls=%d", bookingTool.availabilityRequest, bookingTool.availabilityCalls)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Wednesday") || !strings.Contains(store.lastTurn.AIMessage, "Monday") {
		t.Fatalf("date correction reply used stale day: %s", store.lastTurn.AIMessage)
	}
}

func TestSemanticBareServiceSwitchUsesCatalogConfirmationFlow(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_removal", Name: "Gel Removal", CategoryID: "cat_removal", CategoryName: "Removal"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_classic"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-06-15"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID: "service_classic", StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.97,
		Acts: []ConversationAct{{
			Kind: ConversationActReplace, Entity: ConversationEntityService,
			SourceServiceIDs: []string{"service_classic"}, TargetServiceIDs: []string{"service_removal"}, Confidence: 0.97,
		}},
	}})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Gel Removal."})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "service_classic" || bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("bare semantic switch mutated before confirmation: session=%#v availability=%d booking=%d", session, bookingTool.availabilityCalls, bookingTool.calls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "I have Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "switch to Gel Removal") {
		t.Fatalf("bare semantic switch reply = %q", store.lastTurn.AIMessage)
	}
	if got := store.lastTurn.CustomerMetadata["turn_understanding_reason"]; got != "catalog_backed_service_edit_fallback" {
		t.Fatalf("turn fallback reason = %#v", got)
	}
}

func TestSemanticConcreteServiceReplyAcknowledgesOnce(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.97,
		Acts: []ConversationAct{{
			Kind: ConversationActAdd, Entity: ConversationEntityService,
			TargetServiceIDs: []string{"service_1"}, Confidence: 0.97,
		}},
	}})

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Please book Classic Manicure."})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if got, want := store.lastTurn.AIMessage, "Okay, I added Classic Manicure. What day would you like?"; got != want {
		t.Fatalf("concise service acknowledgement = %q, want %q", got, want)
	}
}

func TestConversationActStopsRepeatedClarificationWithSafeHandoff(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_classic_pedi", Name: "Classic Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
		{ID: "service_spa_pedi", Name: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.95,
		Acts: []ConversationAct{{
			Kind: ConversationActReplace, Entity: ConversationEntityService, SourceServiceIDs: []string{"service_gel"},
			TargetServiceIDs: []string{"service_classic_pedi", "service_spa_pedi"}, TargetCategoryID: "cat_pedi", TargetCategoryName: "Pedicure", Confidence: 0.95,
		}},
	}})
	service.now = fixedNow

	messages := []string{"Change to pedicure.", "Pedicure.", "I said pedicure.", "Pedicure, please."}
	var session *Session
	for _, message := range messages {
		var err error
		session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: message})
		if err != nil {
			t.Fatalf("message %q: %v", message, err)
		}
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session did not stop repeated clarification: %#v", session)
	}
	if session.Handoff == nil || session.Handoff.Reason != HandoffReasonServiceClarification {
		t.Fatalf("handoff = %#v", session.Handoff)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("handoff wording = %s", store.lastTurn.AIMessage)
	}
}

func TestConversationActClarifiesSameCategoryServiceGuestScope(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
		{ID: "service_classic", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&scriptedTurnInterpreter{interpret: func(req TurnInterpretationRequest) TurnUnderstanding {
		guestScope := ""
		if strings.Contains(strings.ToLower(req.CustomerMessage), "another person") {
			guestScope = ConversationGuestAnother
		}
		return TurnUnderstanding{Goal: "book_appointment", Confidence: 0.95, Acts: []ConversationAct{{
			Kind: ConversationActAdd, Entity: ConversationEntityService, TargetServiceIDs: []string{"service_classic"}, GuestScope: guestScope, Confidence: 0.95,
		}}}
	}})
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "I also to book Classic Manicure."})
	if err != nil {
		t.Fatalf("same-category add: %v", err)
	}
	if got := selectedServiceIDs(*session); !sameStrings(got, []string{"service_gel"}) {
		t.Fatalf("service added before guest scope was known: %#v", got)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "another service for you") || !strings.Contains(store.lastTurn.AIMessage, "another guest") {
		t.Fatalf("guest-scope prompt = %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "It is for another person."})
	if err != nil {
		t.Fatalf("another-guest response: %v", err)
	}
	if got := selectedServiceIDs(*session); !sameStrings(got, []string{"service_gel", "service_classic"}) {
		t.Fatalf("selected services = %#v", got)
	}
	if session.PartyPlan == nil || session.PartyPlan.PartySize != 2 || !partyPlanComplete(session.PartyPlan) {
		t.Fatalf("party plan = %#v", session.PartyPlan)
	}
}

func TestDeterministicLayerDoesNotInferFreeformServiceMutation(t *testing.T) {
	services := []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	session := Session{
		Intent:          IntentBooking,
		ServiceID:       "service_gel",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}},
	}
	for _, message := range []string{
		"Not Gel Manicure, Spa Pedicure.", "Make that Spa Pedicure instead.",
		"I'd rather have Spa Pedicure.", "Take Gel Manicure off the appointment.",
	} {
		if act := deterministicConversationAct(session, message, services, nil, nil); act.Kind != ConversationActUnknown {
			t.Fatalf("freeform mutation %q was inferred deterministically: %#v", message, act)
		}
	}

	session.DialogState.LastMutation = &DraftMutation{Kind: ConversationActReplace}
	if act := deterministicConversationAct(session, "Scratch that, go back.", services, nil, nil); act.Kind != ConversationActUnknown {
		t.Fatalf("freeform undo was inferred deterministically: %#v", act)
	}
}

func TestSemanticTurnInterpreterHandlesUnseenCorrectionWithoutKeywordGate(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	interpreter := &fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.93,
		Acts: []ConversationAct{{
			Kind: ConversationActReplace, Entity: ConversationEntityService,
			SourceServiceIDs: []string{"service_gel"}, TargetServiceIDs: []string{"service_spa"},
			Scope: ConversationScopeOne, Confidence: 0.93, Reason: "semantic preference correction",
		}},
	}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The relaxing treatment would suit me better.",
	})
	if err != nil {
		t.Fatalf("semantic correction: %v", err)
	}
	if interpreter.calls != 1 || session.ServiceID != "service_spa" {
		t.Fatalf("semantic interpreter was keyword-gated: calls=%d session=%#v", interpreter.calls, session)
	}
	if got := store.lastTurn.CustomerMetadata["turn_understanding_source"]; got != "structured_ai" {
		t.Fatalf("turn source = %#v", got)
	}
}

func TestMultiActCorrectionInvalidatesReviewAuthorizationForOldRevision(t *testing.T) {
	services := []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	before := Session{
		Intent: IntentBooking, ServiceID: "service_gel", ServiceName: "Gel Manicure",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}},
		DialogState: DialogState{
			Version: DialogStateVersion, Phase: DialogPhaseReview, ReviewRequired: true,
			DraftRevision: 7, ReviewedRevision: 7,
		},
	}
	after := cloneSessionForTurn(before)
	service := &Service{}
	result := service.applyTurnUnderstandingToDraft(&after, TurnUnderstanding{Acts: []ConversationAct{
		{Kind: ConversationActReview, Confidence: 0.99},
		{Kind: ConversationActReplace, Entity: ConversationEntityService, SourceServiceIDs: []string{"service_gel"}, TargetServiceIDs: []string{"service_spa"}, Confidence: 0.96},
	}}, services, nil)
	advanceDraftRevision(before, &after)

	if !result.Changed || after.ServiceID != "service_spa" {
		t.Fatalf("multi-act correction was not reduced: result=%#v session=%#v", result, after)
	}
	if after.DialogState.DraftRevision != 8 || after.DialogState.ReviewAccepted || after.DialogState.AuthorizedRevision != 0 {
		t.Fatalf("stale review remained authorized: %#v", after.DialogState)
	}
	if action := planNextConversationAction(after, ""); action.Kind != AssistantActionReadReview {
		t.Fatalf("next action = %#v, want fresh review", action)
	}
}

func TestStaffCorrectionInvalidatesAvailabilityAndReview(t *testing.T) {
	staff := []StaffOption{{ID: "staff_mai", Name: "Mai"}, {ID: "staff_anna", Name: "Anna"}}
	before := Session{
		Intent: IntentBooking, ServiceID: "service_1", StaffID: "staff_mai", StaffName: "Mai", StaffSelectionMode: booking.StaffSelectionSpecific,
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_1", StaffID: "staff_mai", StaffSelectionMode: booking.StaffSelectionSpecific}},
		OfferedSlots:    []OfferedSlot{{StaffID: "staff_mai"}},
		DialogState:     DialogState{Version: DialogStateVersion, Phase: DialogPhaseReview, ReviewRequired: true, ReviewAccepted: true, DraftRevision: 4, ReviewedRevision: 4, AuthorizedRevision: 4},
	}
	after := cloneSessionForTurn(before)
	service := &Service{}
	result := service.applyTurnUnderstandingToDraft(&after, TurnUnderstanding{Acts: []ConversationAct{{
		Kind: ConversationActSet, Entity: ConversationEntityStaff, TargetServiceIDs: []string{"staff_anna"}, Confidence: 0.95,
	}}}, nil, staff)
	advanceDraftRevision(before, &after)

	if !result.Changed || after.StaffID != "staff_anna" || len(after.OfferedSlots) != 0 {
		t.Fatalf("staff dependency invalidation failed: result=%#v session=%#v", result, after)
	}
	if after.DialogState.DraftRevision != 5 || reviewAuthorizationCurrent(after.DialogState) {
		t.Fatalf("staff correction kept stale review: %#v", after.DialogState)
	}
}

func TestSemanticInterpreterFailureDoesNotGuessServiceMutation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_gel", Name: "Gel Manicure"}, {ID: "service_spa", Name: "Spa Pedicure"}}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	interpreter := &fakeConversationActInterpreter{}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Spa Pedicure."})
	if err != nil {
		t.Fatalf("semantic fallback: %v", err)
	}
	if session.ServiceID != "service_gel" || len(session.OfferedSlots) != 0 {
		t.Fatalf("failed interpretation guessed a mutation: %#v", session)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "add it, or replace") {
		t.Fatalf("safe clarification = %s", store.lastTurn.AIMessage)
	}
}

func TestDraftMutationHistorySupportsRepeatedUndo(t *testing.T) {
	services := []ServiceOption{{ID: "service_a", Name: "Service A"}, {ID: "service_b", Name: "Service B"}, {ID: "service_c", Name: "Service C"}}
	session := Session{ServiceID: "service_a", ServiceName: "Service A", BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_a"}}}
	service := &Service{}

	first := service.applyConversationActToDraft(&session, ConversationAct{Kind: ConversationActReplace, SourceServiceIDs: []string{"service_a"}, TargetServiceIDs: []string{"service_b"}}, services)
	second := service.applyConversationActToDraft(&session, ConversationAct{Kind: ConversationActReplace, SourceServiceIDs: []string{"service_b"}, TargetServiceIDs: []string{"service_c"}}, services)
	if !first.Changed || !second.Changed || session.ServiceID != "service_c" || len(session.DialogState.MutationHistory) != 2 {
		t.Fatalf("mutation history = %#v session=%#v", session.DialogState.MutationHistory, session)
	}
	service.applyConversationActToDraft(&session, ConversationAct{Kind: ConversationActUndo}, services)
	if session.ServiceID != "service_b" {
		t.Fatalf("first undo service = %s", session.ServiceID)
	}
	service.applyConversationActToDraft(&session, ConversationAct{Kind: ConversationActUndo}, services)
	if session.ServiceID != "service_a" || len(session.DialogState.MutationHistory) != 0 {
		t.Fatalf("second undo state = %#v", session)
	}
}

func TestTurnInterpreterInputRedactsKnownPII(t *testing.T) {
	session := Session{CustomerName: "Linh Tran"}
	got := sanitizedTurnInterpreterMessage("Linh Tran, use 312-555-0101 or linh@example.com.", session)
	if strings.Contains(got, "Linh Tran") || strings.Contains(got, "312") || strings.Contains(got, "linh@example.com") {
		t.Fatalf("PII was not redacted: %s", got)
	}
	for _, placeholder := range []string{"[customer_name]", "[phone]", "[email]"} {
		if !strings.Contains(got, placeholder) {
			t.Fatalf("missing %s in %s", placeholder, got)
		}
	}
}

func TestMultiIntentCorrectionAnswersCurrentDraftThenResumes(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_gel", Name: "Gel Manicure"}, {ID: "service_spa", Name: "Spa Pedicure"}}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}}
	interpreter := &fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.94,
		Acts:      []ConversationAct{{Kind: ConversationActReplace, Entity: ConversationEntityService, SourceServiceIDs: []string{"service_gel"}, TargetServiceIDs: []string{"service_spa"}, Confidence: 0.94}},
		Questions: []ConversationQuestion{{Subject: ConversationQuestionCurrentBooking, Confidence: 0.92}},
	}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(interpreter)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Use the relaxing one; what does my appointment contain now?"})
	if err != nil {
		t.Fatalf("multi-intent turn: %v", err)
	}
	if session.ServiceID != "service_spa" || !strings.Contains(store.lastTurn.AIMessage, "Spa Pedicure") || !strings.Contains(store.lastTurn.AIMessage, "What day") {
		t.Fatalf("multi-intent result session=%#v reply=%s", session, store.lastTurn.AIMessage)
	}
}

func TestSemanticTurnCorpusHasNoKeywordEntryGate(t *testing.T) {
	utterances := []string{
		"The relaxing treatment would suit me better.", "On second thought, the longer option feels right.",
		"Could mine be the spa version?", "I meant the other treatment.", "That first choice is not what I need.",
		"Please put me down for the gentler one.", "The option with polish is the one I wanted.",
		"Actually the shorter appointment works for me.", "I would prefer what my sister usually gets.",
		"Use the deluxe treatment for my appointment.", "The basic one is enough after all.",
		"I changed my mind about what I am getting.", "Could the second treatment be mine instead?",
		"The service I mentioned earlier should be different.", "I need the option that lasts longer.",
		"Please correct the treatment on my appointment.", "The other catalog choice is a better fit.",
		"Mine should be the service with the soak.", "I would be happier with the premium treatment.",
		"The simpler appointment is what I need.", "Could you use the first catalog option for me?",
		"The service for me should be the deluxe one.", "I no longer want the treatment we discussed.",
		"Please use the alternative my friend recommended.", "The appointment should have the shorter service.",
		"What I need is the longer-lasting treatment.", "Use the gentler catalog option on my draft.",
		"The premium choice belongs on my appointment.", "I intended to get the basic treatment.",
		"Please correct mine to the second option you listed.",
	}
	total := 0
	for catalogIndex := 0; catalogIndex < 10; catalogIndex++ {
		currentID := fmt.Sprintf("service_current_%d", catalogIndex)
		targetID := fmt.Sprintf("service_target_%d", catalogIndex)
		services := []ServiceOption{{ID: currentID, Name: fmt.Sprintf("Current Treatment %d", catalogIndex)}, {ID: targetID, Name: fmt.Sprintf("Target Treatment %d", catalogIndex)}}
		for phraseIndex, utterance := range utterances {
			interpreter := &fakeConversationActInterpreter{turn: TurnUnderstanding{
				Goal: "book_appointment", Confidence: 0.91,
				Acts: []ConversationAct{{Kind: ConversationActReplace, Entity: ConversationEntityService, SourceServiceIDs: []string{currentID}, TargetServiceIDs: []string{targetID}, Confidence: 0.91}},
			}}
			service := NewService(newFakeConversationStore(), &fakeBookingTool{})
			service.SetTurnInterpreter(interpreter)
			session := Session{ID: fmt.Sprintf("session_%d_%d", catalogIndex, phraseIndex), SalonID: "salon_1", Intent: IntentBooking, ServiceID: currentID, BookingSegments: []booking.BookingSegmentRequest{{ServiceID: currentID}}}
			turn := service.turnUnderstandingForMessage(context.Background(), session, utterance, services, nil, nil, nil)
			if interpreter.calls != 1 || len(turn.Acts) != 1 || !sameStrings(turn.Acts[0].TargetServiceIDs, []string{targetID}) {
				t.Fatalf("catalog=%d phrase=%q turn=%#v calls=%d", catalogIndex, utterance, turn, interpreter.calls)
			}
			total++
		}
	}
	if total != 300 {
		t.Fatalf("semantic corpus size = %d, want 300", total)
	}
}

func TestSemanticGoalsRouteRescheduleAndCancelWithoutKeywordSignals(t *testing.T) {
	tests := []struct {
		name    string
		goal    string
		message string
		want    string
	}{
		{name: "reschedule", goal: "reschedule_appointment", message: "The time on my existing visit no longer works for me.", want: BookingActionReschedule},
		{name: "cancel", goal: "cancel_appointment", message: "That existing visit will not be needed after all.", want: BookingActionCancel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.session.CustomerPhone = "+13125550101"
			bookingTool := &fakeBookingTool{candidates: []booking.AppointmentActionRef{testRescheduleAppointment()}}
			service := NewService(store, bookingTool)
			service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{Goal: test.goal, Confidence: 0.96}})
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: test.message})
			if err != nil {
				t.Fatalf("semantic goal: %v", err)
			}
			if session.BookingAction != test.want || bookingTool.candidateCalls != 1 {
				t.Fatalf("semantic goal did not route: session=%#v calls=%d", session, bookingTool.candidateCalls)
			}
		})
	}
}

func TestSemanticGuestCountStartsPartyPlanWithoutPhraseParserDependency(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", Confidence: 0.96,
		Acts: []ConversationAct{{Kind: ConversationActSet, Entity: ConversationEntityGuest, Count: 3, Confidence: 0.96}},
	}})
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "The appointment covers a trio."})
	if err != nil {
		t.Fatalf("semantic party count: %v", err)
	}
	if session.PartyPlan == nil || session.PartyPlan.PartySize != 3 || session.PartyPlan.ParseSource != "semantic_turn" {
		t.Fatalf("party plan = %#v", session.PartyPlan)
	}
	if session.DialogState.DraftRevision <= 1 {
		t.Fatalf("party correction did not advance draft revision: %#v", session.DialogState)
	}
}

func TestInformationalInterruptionAnswersThenResumesBookingQuestion(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service_1", StaffSelectionMode: booking.StaffSelectionAnyone}}
	store.businessHours = []BusinessHourPeriod{{ID: "hours_mon", DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00", Source: "imported", Provider: "square"}}
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: TurnUnderstanding{
		Goal: "information", Confidence: 0.96,
		Questions: []ConversationQuestion{{Subject: ConversationQuestionHours, Confidence: 0.96}},
	}})
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "What are your salon hours?"})
	if err != nil {
		t.Fatalf("informational interruption: %v", err)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Monday 9:00 AM to 7:00 PM") || !strings.Contains(store.lastTurn.AIMessage, "What day") {
		t.Fatalf("answer did not resume booking: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Would you like help with an appointment") {
		t.Fatalf("reply asked two booking questions: %s", store.lastTurn.AIMessage)
	}
	if store.session.ServiceID != "service_1" {
		t.Fatalf("informational interruption changed draft: %#v", store.session)
	}
}

func TestSemanticPartyServiceCorrectionPreservesGuestGroups(t *testing.T) {
	services := []ServiceOption{{ID: "service_gel", Name: "Gel Manicure"}, {ID: "service_classic", Name: "Classic Manicure"}, {ID: "service_spa", Name: "Spa Pedicure"}}
	session := Session{
		Intent: IntentBooking, ServiceID: "service_gel", ServiceName: "Gel Manicure",
		PartyPlan: &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
			{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"service_gel"}},
			{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"service_classic"}},
		}},
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_gel"}, {ServiceID: "service_classic"}},
	}
	turn := TurnUnderstanding{Goal: "book_appointment", Confidence: 0.96, Acts: []ConversationAct{{
		Kind: ConversationActReplace, Entity: ConversationEntityService, GuestRef: "guest 2",
		SourceServiceIDs: []string{"service_classic"}, TargetServiceIDs: []string{"service_spa"}, Confidence: 0.96,
	}}}
	validated, ok := validateTurnUnderstanding(turn, session, services, nil)
	if !ok {
		t.Fatal("catalog-backed guest correction was rejected")
	}
	service := &Service{}
	result := service.applyTurnUnderstandingToDraft(&session, validated, services, nil)
	if !result.Changed || !sameStrings(session.PartyPlan.Groups[0].ResolvedServiceIDs, []string{"service_gel"}) || !sameStrings(session.PartyPlan.Groups[1].ResolvedServiceIDs, []string{"service_spa"}) {
		t.Fatalf("party groups were flattened or not corrected: result=%#v plan=%#v", result, session.PartyPlan)
	}
	if got := selectedServiceIDs(session); !sameStrings(got, []string{"service_gel", "service_spa"}) {
		t.Fatalf("party segments = %#v", got)
	}

	turn.Acts[0].GuestRef = ""
	if _, ok := validateTurnUnderstanding(turn, session, services, nil); ok {
		t.Fatal("party service correction without guest_ref was accepted")
	}
}

func TestSemanticMultiActBuildsInitialPartyGroupsWithoutFlattening(t *testing.T) {
	services := []ServiceOption{{ID: "service_gel", Name: "Gel Manicure"}, {ID: "service_spa", Name: "Spa Pedicure"}}
	session := Session{Intent: IntentBooking}
	turn := TurnUnderstanding{Goal: "book_appointment", Confidence: 0.96, Acts: []ConversationAct{
		{Kind: ConversationActSet, Entity: ConversationEntityGuest, Count: 3, Confidence: 0.96},
		{Kind: ConversationActAdd, Entity: ConversationEntityService, GuestRef: "caller", Count: 1, TargetServiceIDs: []string{"service_gel"}, Confidence: 0.96},
		{Kind: ConversationActAdd, Entity: ConversationEntityService, GuestRef: "other guests", Count: 2, TargetServiceIDs: []string{"service_spa"}, Confidence: 0.96},
	}}
	validated, ok := validateTurnUnderstanding(turn, session, services, nil)
	if !ok {
		t.Fatal("initial semantic party turn was rejected")
	}
	service := &Service{}
	result := service.applyTurnUnderstandingToDraft(&session, validated, services, nil)
	if !result.Changed || session.PartyPlan == nil || !partyPlanComplete(session.PartyPlan) || len(session.PartyPlan.Groups) != 2 {
		t.Fatalf("party plan was not constructed: result=%#v plan=%#v", result, session.PartyPlan)
	}
	if got := selectedServiceIDs(session); !sameStrings(got, []string{"service_gel", "service_spa", "service_spa"}) {
		t.Fatalf("party segments = %#v", got)
	}
}
