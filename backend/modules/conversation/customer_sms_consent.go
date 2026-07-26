package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	customernotification "github.com/manleai/ai-receptionist/modules/customer_notification"
)

const (
	conversationSMSAwaiting  = "awaiting_response"
	conversationSMSConsented = "consented"
	conversationSMSDeclined  = "declined"
	conversationSMSOptedOut  = "opted_out"
	conversationSMSSkipped   = "skipped"
)

type CustomerSMSConsentTool interface {
	ConsentStatus(context.Context, string, string) (*customernotification.Consent, error)
	RecordConsentRequested(context.Context, string, string, string, string) (*customernotification.Consent, bool, error)
	RecordConversationConsent(context.Context, customernotification.RecordConversationConsentRequest) (*customernotification.Consent, bool, error)
}

func (s *Service) maybeAskCustomerSMSConsent(
	ctx context.Context,
	ownerUserID string,
	turn TurnRecord,
	before Session,
	next Session,
	services []ServiceOption,
	staff []StaffOption,
	cfg *RuntimeConfig,
	knowledge []KnowledgeSnippet,
) (bool, *Session, error) {
	if s == nil || s.customerSMS == nil || cfg == nil || !cfg.CustomerSMSEnabled ||
		strings.TrimSpace(cfg.CustomerSMSQuietStart) == "" || strings.TrimSpace(cfg.CustomerSMSQuietEnd) == "" ||
		next.Channel != ChannelPhone || strings.TrimSpace(next.CustomerPhone) == "" ||
		!reviewAuthorizationCurrentForPolicy(normalizedDialogState(next.DialogState), cfg, selectedSchedulingAuthorityForReview(next, cfg)) {
		return false, nil, nil
	}
	destination := customernotification.NormalizeDestination(next.CustomerPhone)
	if destination == "" {
		return false, nil, nil
	}
	current, err := s.customerSMS.ConsentStatus(ctx, next.SalonID, destination)
	if err == nil {
		state := normalizedDialogState(next.DialogState)
		state.CustomerSMSConsent = conversationSMSState(current.Status, destination, state.DraftRevision, current.Version, "")
		next.DialogState = state
		if current.Status == customernotification.ConsentConsented || current.Status == customernotification.ConsentDeclined || current.Status == customernotification.ConsentOptedOut {
			return false, nil, nil
		}
	} else if !errors.Is(err, customernotification.ErrNotFound) {
		// Consent infrastructure cannot be allowed to block the already reviewed
		// scheduling operation. No consent is inferred and no SMS row can enqueue.
		state := normalizedDialogState(next.DialogState)
		state.CustomerSMSConsent = conversationSMSState(conversationSMSSkipped, destination, state.DraftRevision, 0, "")
		next.DialogState = state
		return false, nil, nil
	}
	state := normalizedDialogState(next.DialogState)
	eventKey := customerSMSRequestEventKey(next.ID, state.DraftRevision, destination)
	consent, _, err := s.customerSMS.RecordConsentRequested(ctx, next.SalonID, next.ID, destination, eventKey)
	if err != nil {
		state.CustomerSMSConsent = conversationSMSState(conversationSMSSkipped, destination, state.DraftRevision, 0, "")
		next.DialogState = state
		return false, nil, nil
	}
	state.CustomerSMSConsent = conversationSMSState(conversationSMSAwaiting, destination, state.DraftRevision, consent.Version, eventKey)
	next.DialogState = state
	syncTurnUpdate(&turn, next, services, staff, cfg)
	turn.AIMessage = "Would you like appointment updates by text at the number ending in " + lastFour(destination) + "? Please say yes or no."
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	finalizeTurnMetadata(&turn, before, next, ExpectedInputCustomerSMSConsent, ExpectedInputCustomerSMSConsent, "customer_sms_consent_requested")
	updated, saveErr := s.store.SaveTurn(ctx, turn)
	return true, updated, saveErr
}

func (s *Service) handlePendingCustomerSMSConsent(
	ctx context.Context,
	ownerUserID string,
	session Session,
	message, eventKey string,
	services []ServiceOption,
	staff []StaffOption,
	cfg *RuntimeConfig,
	knowledge []KnowledgeSnippet,
) (bool, *Session, error) {
	state := normalizedDialogState(session.DialogState)
	pending := state.CustomerSMSConsent
	if pending == nil || pending.Status != conversationSMSAwaiting {
		return false, nil, nil
	}
	destination := customernotification.NormalizeDestination(session.CustomerPhone)
	if destination == "" || pending.DestinationHash != hashDestination(destination) ||
		pending.DraftRevision != state.DraftRevision || session.Channel != ChannelPhone {
		state.CustomerSMSConsent = nil
		session.DialogState = state
		return false, nil, nil
	}
	next := cloneSessionForTurn(session)
	nextState := normalizedDialogState(next.DialogState)
	status := conversationSMSSkipped
	var consentVersion int
	granted := false
	answered := false
	if isExactAffirmativeResponse(message) {
		granted, answered = true, true
	} else if isNegativeOnly(message) {
		answered = true
	}
	if answered && s.customerSMS != nil {
		consent, _, err := s.customerSMS.RecordConversationConsent(ctx, customernotification.RecordConversationConsentRequest{
			SalonID: next.SalonID, CallSessionID: next.ID, Destination: destination,
			Granted: granted, EventKey: "sms-consent-response:" + next.ID + ":" + strings.TrimSpace(eventKey),
			EvidenceReference: strings.TrimSpace(eventKey),
		})
		if err == nil {
			consentVersion = consent.Version
			if consent.Status == customernotification.ConsentConsented {
				status = conversationSMSConsented
			} else if consent.Status == customernotification.ConsentOptedOut {
				status = conversationSMSOptedOut
			} else {
				status = conversationSMSDeclined
			}
		} else if errors.Is(err, customernotification.ErrConflict) {
			status = conversationSMSOptedOut
		}
	}
	nextState.CustomerSMSConsent = conversationSMSState(status, destination, nextState.DraftRevision, consentVersion, pending.RequestEventKey)
	nextState.CustomerSMSConsent.LastResponseEventKey = strings.TrimSpace(eventKey)
	next.DialogState = nextState
	turn := newTurnRecord(next.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"customer_sms_consent_result":            status,
		"customer_sms_consent_explicit_response": answered,
	})
	// A decline, unclear response, or consent dependency failure never blocks
	// the already authorized booking. The outbox trigger independently requires
	// an active consent row, so skipped responses cannot produce SMS.
	updated, err := s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
	return true, updated, err
}

func conversationSMSState(status, destination string, draftRevision, consentVersion int, requestEventKey string) *CustomerSMSConsentState {
	return &CustomerSMSConsentState{
		Status: status, DestinationHash: hashDestination(destination), DestinationMasked: "••••" + lastFour(destination),
		DraftRevision: draftRevision, ConsentVersion: consentVersion, RequestEventKey: requestEventKey,
	}
}

func customerSMSRequestEventKey(sessionID string, draftRevision int, destination string) string {
	return "sms-consent-request:" + strings.TrimSpace(sessionID) + ":" + hashDestination(destination) + ":v" + strings.TrimSpace(intString(draftRevision))
}

func hashDestination(destination string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(destination)))
	return hex.EncodeToString(sum[:])
}

func lastFour(destination string) string {
	if len(destination) < 4 {
		return ""
	}
	return destination[len(destination)-4:]
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	buffer := [20]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = digits[value%10]
		value /= 10
	}
	return string(buffer[position:])
}
