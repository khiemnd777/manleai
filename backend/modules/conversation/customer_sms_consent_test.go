package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	customernotification "github.com/manleai/ai-receptionist/modules/customer_notification"
)

type fakeCustomerSMSConsentTool struct {
	consent       *customernotification.Consent
	statusErr     error
	requestErr    error
	responseErr   error
	requestCalls  int
	responseCalls int
	lastGranted   bool
}

func (f *fakeCustomerSMSConsentTool) ConsentStatus(context.Context, string, string) (*customernotification.Consent, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.consent, nil
}

func (f *fakeCustomerSMSConsentTool) RecordConsentRequested(context.Context, string, string, string, string) (*customernotification.Consent, bool, error) {
	f.requestCalls++
	if f.requestErr != nil {
		return nil, false, f.requestErr
	}
	f.consent = &customernotification.Consent{ID: "consent-1", Status: customernotification.ConsentPending, Version: 1}
	return f.consent, false, nil
}

func (f *fakeCustomerSMSConsentTool) RecordConversationConsent(_ context.Context, req customernotification.RecordConversationConsentRequest) (*customernotification.Consent, bool, error) {
	f.responseCalls++
	f.lastGranted = req.Granted
	if f.responseErr != nil {
		return nil, false, f.responseErr
	}
	status := customernotification.ConsentDeclined
	if req.Granted {
		status = customernotification.ConsentConsented
	}
	f.consent = &customernotification.Consent{ID: "consent-1", Status: status, Version: 2}
	return f.consent, false, nil
}

func smsAuthorizedOwnerManualSession(store *fakeConversationStore) Session {
	session := ownerManualReadySession(store)
	session.Channel = ChannelPhone
	state := normalizedDialogState(session.DialogState)
	state.ReviewRequired = true
	state.ReviewAccepted = true
	state.DraftRevision = 3
	state.ReviewedRevision = 3
	state.AuthorizedRevision = 3
	state.ReviewedBookingMode = "pending_approval"
	state.SelectedSchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	session.DialogState = state
	return session
}

func smsReadyConfig() *RuntimeConfig {
	return &RuntimeConfig{
		SalonName: "Lotus Nails", Timezone: "America/Chicago", AIEnabled: true,
		BookingMode: "pending_approval", SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		CustomerSMSEnabled: true, CustomerSMSQuietStart: "21:00", CustomerSMSQuietEnd: "08:00", CustomerSMSPolicyVersion: 1,
	}
}

func TestCustomerSMSConsentAskedAfterAuthorizationAndNaturalYesResumesOwnerRequest(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
	tool := newOwnerManualSchedulingTool("request-sms-1")
	service := NewService(store, tool)
	consentTool := &fakeCustomerSMSConsentTool{statusErr: customernotification.ErrNotFound}
	service.SetCustomerSMSConsentTool(consentTool)
	session := smsAuthorizedOwnerManualSession(store)
	store.session = session
	cfg := smsReadyConfig()
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes, book that.", "review-event", store.services, store.staff, cfg)

	handled, pending, err := service.maybeAskCustomerSMSConsent(context.Background(), "owner_1", turn, session, session, store.services, store.staff, cfg, nil)
	if err != nil || !handled || pending == nil {
		t.Fatalf("ask consent handled=%v pending=%#v err=%v", handled, pending, err)
	}
	if tool.actionCalls != 0 || consentTool.requestCalls != 1 || !strings.Contains(store.lastTurn.AIMessage, "ending in 0101") {
		t.Fatalf("pre-consent calls=%d request=%d reply=%q", tool.actionCalls, consentTool.requestCalls, store.lastTurn.AIMessage)
	}
	if pending.DialogState.CustomerSMSConsent == nil || pending.DialogState.CustomerSMSConsent.Status != conversationSMSAwaiting || !reviewAuthorizationCurrent(pending.DialogState) {
		t.Fatalf("pending consent must preserve review fence: %#v", pending.DialogState)
	}

	consentTool.statusErr = nil
	handled, completed, err := service.handlePendingCustomerSMSConsent(context.Background(), "owner_1", *pending,
		"Sure, please.", "sms-answer-event", store.services, store.staff, cfg, nil)
	if err != nil || !handled || completed == nil {
		t.Fatalf("answer consent handled=%v completed=%#v err=%v", handled, completed, err)
	}
	if !consentTool.lastGranted || consentTool.responseCalls != 1 || tool.actionCalls != 1 {
		t.Fatalf("consent/action calls granted=%v response=%d action=%d", consentTool.lastGranted, consentTool.responseCalls, tool.actionCalls)
	}
	if completed.Outcome != OutcomeOwnerReviewPending || !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "not a confirmed appointment") {
		t.Fatalf("owner request result=%#v reply=%q", completed, store.lastTurn.AIMessage)
	}
}

func TestCustomerSMSConsentDeclineAndDependencyFailureNeverBlockBooking(t *testing.T) {
	for _, test := range []struct {
		name        string
		answer      string
		responseErr error
	}{
		{name: "explicit decline", answer: "No"},
		{name: "dependency failure", answer: "Yes", responseErr: errors.New("consent store unavailable")},
		{name: "unclear response", answer: "I am not sure about texts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.answerContextFence.SchedulingAuthority = booking.SchedulingAuthorityOwnerManual
			tool := newOwnerManualSchedulingTool("request-" + strings.ReplaceAll(test.name, " ", "-"))
			service := NewService(store, tool)
			consentTool := &fakeCustomerSMSConsentTool{responseErr: test.responseErr}
			service.SetCustomerSMSConsentTool(consentTool)
			session := smsAuthorizedOwnerManualSession(store)
			state := normalizedDialogState(session.DialogState)
			state.CustomerSMSConsent = conversationSMSState(conversationSMSAwaiting, "+13125550101", state.DraftRevision, 1, "request-event")
			session.DialogState = state
			store.session = session
			handled, result, err := service.handlePendingCustomerSMSConsent(context.Background(), "owner_1", session,
				test.answer, "answer-event-"+test.name, store.services, store.staff, smsReadyConfig(), nil)
			if err != nil || !handled || result == nil || tool.actionCalls != 1 {
				t.Fatalf("result=%#v handled=%v action=%d err=%v", result, handled, tool.actionCalls, err)
			}
			if result.DialogState.CustomerSMSConsent.Status == conversationSMSConsented && test.name != "dependency failure" {
				t.Fatalf("non-yes response inferred consent: %#v", result.DialogState.CustomerSMSConsent)
			}
		})
	}
}

func TestCustomerSMSConsentWrongStateYesDoesNothing(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, newOwnerManualSchedulingTool("unused"))
	consentTool := &fakeCustomerSMSConsentTool{}
	service.SetCustomerSMSConsentTool(consentTool)
	session := smsAuthorizedOwnerManualSession(store)
	handled, result, err := service.handlePendingCustomerSMSConsent(context.Background(), "owner_1", session,
		"Yes", "wrong-state", store.services, store.staff, smsReadyConfig(), nil)
	if err != nil || handled || result != nil || consentTool.responseCalls != 0 {
		t.Fatalf("wrong-state yes handled=%v result=%#v calls=%d err=%v", handled, result, consentTool.responseCalls, err)
	}
}

func TestCustomerSMSConsentSimulatorAndDisabledPolicySkip(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, newOwnerManualSchedulingTool("unused"))
	consentTool := &fakeCustomerSMSConsentTool{statusErr: customernotification.ErrNotFound}
	service.SetCustomerSMSConsentTool(consentTool)
	session := smsAuthorizedOwnerManualSession(store)
	session.Channel = ChannelSimulator
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "yes", "event", store.services, store.staff, smsReadyConfig())
	if handled, _, err := service.maybeAskCustomerSMSConsent(context.Background(), "owner_1", turn, session, session, store.services, store.staff, smsReadyConfig(), nil); err != nil || handled {
		t.Fatalf("simulator ask handled=%v err=%v", handled, err)
	}
	session.Channel = ChannelPhone
	cfg := smsReadyConfig()
	cfg.CustomerSMSEnabled = false
	if handled, _, err := service.maybeAskCustomerSMSConsent(context.Background(), "owner_1", turn, session, session, store.services, store.staff, cfg, nil); err != nil || handled {
		t.Fatalf("disabled ask handled=%v err=%v", handled, err)
	}
	if consentTool.requestCalls != 0 {
		t.Fatalf("consent request calls=%d", consentTool.requestCalls)
	}
}

var _ = time.Time{}
