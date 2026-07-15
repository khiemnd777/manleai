package conversation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

type providerFailureTurnInterpreter struct {
	calls   int
	outcome string
}

func (i *providerFailureTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	i.calls++
	outcome := i.outcome
	if outcome == "" {
		outcome = TurnInterpreterOutcomeProviderError
	}
	return TurnUnderstanding{}, NewTurnInterpreterErrorWithDiagnostics(outcome, errors.New("provider unavailable"), map[string]string{
		"provider": "openai", "failure_stage": "turn_interpretation_response", "http_status": "503",
		"http_status_class": "5xx", "request_id": "req_safe_123",
	})
}

func TestPhoneGuidanceRecoveryRespectsDisabledConsultationDuringProviderOutage(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.cfg.ConsultationEnabled = false
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{outcome: TurnInterpreterOutcomeProviderDisabled})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need advice choosing the right nail service.",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if session.Intent != IntentUnknown || consultationStateActive(session.DialogState.Consultation) {
		t.Fatalf("disabled consultation state = intent=%q dialog=%#v", session.Intent, session.DialogState)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "help choosing a service") {
		t.Fatalf("disabled consultation reply = %q", store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("disabled consultation called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPhoneGuidanceRecoveryEntersConsultationDuringSemanticProviderFailure(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+13125550199"
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeMaintain, ConsultationSystemNatural)},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeColorRefresh, ConsultationSystemGel)},
	}
	bookingTool := &fakeBookingTool{}
	interpreter := &providerFailureTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)
	service.SetReplyGenerator(&fakeReplyGenerator{message: "What result would you like from the service?"})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I'm new here and not sure where to begin.", EventKey: "turn_1",
	})
	if err != nil {
		t.Fatalf("first Message: %v", err)
	}
	if session.DialogState.LastPromptKey != promptCallerGoalGuidanceRecovery || session.DialogState.NoProgressCount != 1 {
		t.Fatalf("first recovery state = %#v", session.DialogState)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "help choosing a service") || strings.Contains(store.lastTurn.AIMessage, "Which service would you like?") {
		t.Fatalf("first recovery reply = %q", store.lastTurn.AIMessage)
	}
	if got := store.lastTurn.CustomerMetadata["turn_interpreter_failure_stage"]; got != "turn_interpretation_response" {
		t.Fatalf("failure stage metadata = %#v", got)
	}
	if got := store.lastTurn.CustomerMetadata["turn_interpreter_request_id"]; got != "req_safe_123" {
		t.Fatalf("request id metadata = %#v", got)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Help me choose", EventKey: "turn_2",
	})
	if err != nil {
		t.Fatalf("consultation Message: %v", err)
	}
	if session.Intent != IntentConsultation || session.DialogState.Phase != DialogPhaseConsultation || !consultationStateActive(session.DialogState.Consultation) {
		t.Fatalf("consultation state = intent=%q dialog=%#v", session.Intent, session.DialogState)
	}
	if store.lastTurn.AIMessage != "What result would you like from the service?" {
		t.Fatalf("consultation reply = %q", store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("guidance recovery called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPhoneGuidanceRecoveryDoesNotInferConsultationFromReportedWordingDuringProviderFailure(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What service should I choose?",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if session.Intent != IntentUnknown || consultationStateActive(session.DialogState.Consultation) || session.DialogState.LastPromptKey != promptCallerGoalGuidanceRecovery {
		t.Fatalf("provider failure should preserve semantic uncertainty: intent=%q dialog=%#v reply=%q", session.Intent, session.DialogState, store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("reported wording called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPhoneGuidanceRecoverySelectsCatalogServiceWithoutSemanticProvider(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.services = []ServiceOption{{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"}}
	bookingTool := &fakeBookingTool{}
	interpreter := &providerFailureTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "I'm undecided.", EventKey: "turn_1"}); err != nil {
		t.Fatalf("recovery Message: %v", err)
	}
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Gel Manicure, please.", EventKey: "turn_2"})
	if err != nil {
		t.Fatalf("service selection Message: %v", err)
	}
	if session.ServiceID != "gel_mani" || session.Intent != IntentBooking {
		t.Fatalf("selected service state = service=%q intent=%q", session.ServiceID, session.Intent)
	}
	if isGuidanceRecoveryPrompt(session.DialogState.LastPromptKey) || session.DialogState.NoProgressCount != 0 {
		t.Fatalf("recovery state was not cleared: %#v", session.DialogState)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("next booking prompt = %q", store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service collection called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPhoneGuidanceRecoveryDoesNotTreatBareYesAfterMenuAsServiceSelection(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure"},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	for index, message := range []string{"I'm undecided.", "List services", "Yes."} {
		if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message: message, EventKey: "menu_turn_" + strconv.Itoa(index+1),
		}); err != nil {
			t.Fatalf("Message %d: %v", index+1, err)
		}
	}
	session := store.session
	if session.ServiceID != "" || len(session.BookingSegments) != 0 {
		t.Fatalf("bare yes selected a service: service=%q segments=%#v", session.ServiceID, session.BookingSegments)
	}
	if session.DialogState.LastPromptKey != promptServiceGuidanceRecovery || session.DialogState.ProviderFailureCount != 1 || !strings.Contains(store.lastTurn.AIMessage, "help me choose") {
		t.Fatalf("bare yes recovery state=%#v reply=%q", session.DialogState, store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("menu recovery called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
}

func TestPhoneGuidanceRecoveryBoundsNoProgressAndHandsOffOnce(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.Intent = IntentBooking
	bookingTool := &fakeBookingTool{}
	interpreter := &providerFailureTurnInterpreter{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)

	messages := []string{
		"I need a little more time to decide.",
		"That still doesn't resolve it for me.",
	}
	var session *Session
	for index, message := range messages {
		var err error
		session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: message, EventKey: "recovery_turn_" + strconv.Itoa(index+1)})
		if err != nil {
			t.Fatalf("Message %d: %v", index+1, err)
		}
	}
	if session.Status != StatusHandoff || session.Handoff == nil || session.Handoff.Reason != HandoffReasonServiceClarification {
		t.Fatalf("bounded recovery result = status=%q handoff=%#v", session.Status, session.Handoff)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("handoff reply = %q", store.lastTurn.AIMessage)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("bounded recovery called booking tools: create=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	transcriptLength := len(session.Transcript)
	replayed, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: messages[1], EventKey: "recovery_turn_2"})
	if err != nil {
		t.Fatalf("handoff replay: %v", err)
	}
	if len(replayed.Transcript) != transcriptLength || replayed.Handoff == nil || replayed.Handoff.Reason != HandoffReasonServiceClarification {
		t.Fatalf("handoff replay was not idempotent: transcript=%d want=%d handoff=%#v", len(replayed.Transcript), transcriptLength, replayed.Handoff)
	}
}
