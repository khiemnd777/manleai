package conversation

import (
	"context"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type fakeConversationActInterpreter struct {
	act     ConversationAct
	calls   int
	request ConversationActInterpretationRequest
}

func (f *fakeConversationActInterpreter) InterpretConversationAct(ctx context.Context, req ConversationActInterpretationRequest) (ConversationAct, error) {
	f.calls++
	f.request = req
	return f.act, nil
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
	service.SetConversationActInterpreter(interpreter)
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
	service.SetConversationActInterpreter(invalid)
	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Make it the secret menu service."})
	if err != nil {
		t.Fatalf("invalid structured act fallback: %v", err)
	}
	if session.ServiceID != "service_spa" {
		t.Fatalf("invalid catalog ID mutated service: %#v", session)
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

func TestFinalReviewAuthorizationDoesNotOverrideCorrectionInSameUtterance(t *testing.T) {
	services := []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	session := Session{
		Intent:          IntentBooking,
		ServiceID:       "service_gel",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}},
		DialogState:     DialogState{Version: DialogStateVersion, ReviewRequired: true, Phase: DialogPhaseReview},
	}
	act := deterministicConversationAct(session, "Yes, book it, but change to Spa Pedicure.", services, nil, nil)
	if act.Kind != ConversationActReplace || !sameStrings(act.TargetServiceIDs, []string{"service_spa"}) {
		t.Fatalf("correction was mistaken for review authorization: %#v", act)
	}
}

func TestDeterministicConversationActCoversNaturalServiceEditParaphrases(t *testing.T) {
	services := []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_spa", Name: "Spa Pedicure"},
	}
	session := Session{
		Intent:          IntentBooking,
		ServiceID:       "service_gel",
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "service_gel", StaffSelectionMode: booking.StaffSelectionAnyone}},
	}
	tests := []struct {
		message string
		kind    string
		target  string
	}{
		{message: "Not Gel Manicure, Spa Pedicure.", kind: ConversationActReplace, target: "service_spa"},
		{message: "Make that Spa Pedicure instead.", kind: ConversationActReplace, target: "service_spa"},
		{message: "I'd rather have Spa Pedicure.", kind: ConversationActReplace, target: "service_spa"},
		{message: "Take Gel Manicure off the appointment.", kind: ConversationActRemove},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			act := deterministicConversationAct(session, test.message, services, nil, nil)
			if act.Kind != test.kind {
				t.Fatalf("act = %#v, want kind %s", act, test.kind)
			}
			if test.target != "" && !sameStrings(act.TargetServiceIDs, []string{test.target}) {
				t.Fatalf("target IDs = %#v", act.TargetServiceIDs)
			}
		})
	}

	session.DialogState.LastMutation = &DraftMutation{Kind: ConversationActReplace}
	if act := deterministicConversationAct(session, "Scratch that, go back.", services, nil, nil); act.Kind != ConversationActUndo {
		t.Fatalf("undo act = %#v", act)
	}
}
