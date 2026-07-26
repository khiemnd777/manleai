package conversation

import (
	"context"
	"strings"
)

const (
	AssistantActionAnswerQuestion    = "answer_question"
	AssistantActionAskClarification  = "ask_clarification"
	AssistantActionAskMissingField   = "ask_missing_field"
	AssistantActionCheckAvailability = "check_availability"
	AssistantActionOfferSlots        = "offer_slots"
	AssistantActionReadReview        = "read_final_review"
	AssistantActionAskReview         = "ask_review_authorization"
	AssistantActionExecuteBooking    = "execute_booking"
	AssistantActionHandoffOwner      = "handoff_owner"
	AssistantActionInformResult      = "inform_result"
)

type AssistantAction struct {
	Kind         string
	MissingField string
	Reason       string
}

func planNextConversationAction(session Session, missing string, cfg *RuntimeConfig) AssistantAction {
	if missing != "" {
		return AssistantAction{Kind: AssistantActionAskMissingField, MissingField: missing, Reason: "required_field_missing"}
	}
	if bookingActionForSession(session) != BookingActionBook {
		return AssistantAction{Kind: AssistantActionExecuteBooking, Reason: "appointment_action_ready"}
	}
	state := normalizedDialogState(session.DialogState)
	if !state.ReviewRequired {
		return AssistantAction{Kind: AssistantActionExecuteBooking, Reason: "review_not_required"}
	}
	expectedAuthority := selectedSchedulingAuthorityForReview(session, cfg)
	if reviewAuthorizationCurrentForPolicy(state, cfg, expectedAuthority) {
		return AssistantAction{Kind: AssistantActionExecuteBooking, Reason: "current_revision_authorized"}
	}
	if state.Phase == DialogPhaseReview && state.ReviewedRevision == state.DraftRevision && cfg != nil &&
		state.ReviewedBookingMode == cfg.BookingMode && strings.TrimSpace(state.SelectedSchedulingAuthority) == strings.TrimSpace(expectedAuthority) {
		return AssistantAction{Kind: AssistantActionAskReview, Reason: "current_revision_reviewed_authorization_missing"}
	}
	return AssistantAction{Kind: AssistantActionReadReview, Reason: "review_missing_or_stale"}
}

func resumeAfterInformationPrompt(session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (string, string, bool) {
	if session == nil {
		return "", "", false
	}
	missing := missingBookingField(*session)
	action := planNextConversationAction(*session, missing, cfg)
	switch action.Kind {
	case AssistantActionReadReview:
		state := normalizedDialogState(session.DialogState)
		state.Phase = DialogPhaseReview
		state.Pending = nil
		state.ReviewAccepted = false
		state.ReviewedRevision = state.DraftRevision
		state.AuthorizedRevision = 0
		state.NoProgressCount = 0
		state.LastPromptKey = "final_review"
		session.DialogState = state
		stampSchedulingReviewFence(session, cfg)
		return finalBookingReviewPrompt(*session, services, staff, cfg), "booking_review", true
	case AssistantActionAskReview:
		state := normalizedDialogState(session.DialogState)
		state.Phase = DialogPhaseReview
		state.Pending = nil
		state.ReviewAccepted = false
		state.AuthorizedRevision = 0
		state.LastPromptKey = "final_review_retry"
		session.DialogState = state
		return reviewAuthorizationPrompt(cfg, false), "booking_review", true
	default:
		return resumeBookingPrompt(*session, services, cfg), missing, false
	}
}

func (s *Service) continueAfterDraftReady(ctx context.Context, ownerUserID string, turn TurnRecord, before Session, next Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	nextAction := planNextConversationAction(next, missingBookingField(next), cfg)
	if nextAction.Kind == AssistantActionAskMissingField {
		missing := nextAction.MissingField
		turn.AIMessage = promptForMissingField(missing)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, before, next, missing, missing, "missing_field")
		return s.store.SaveTurn(ctx, turn)
	}
	if nextAction.Kind == AssistantActionAskReview {
		state := normalizedDialogState(next.DialogState)
		state.Phase = DialogPhaseReview
		state.Pending = nil
		state.ReviewAccepted = false
		state.AuthorizedRevision = 0
		state.LastPromptKey = "final_review_retry"
		next.DialogState = state
		stampSchedulingReviewFence(&next, cfg)
		syncTurnUpdate(&turn, next, services, staff, cfg)
		turn.AIMessage = "I didn't catch that. " + reviewAuthorizationPrompt(cfg, true)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, before, next, "booking_review", "booking_review", "final_booking_review_retry")
		return s.store.SaveTurn(ctx, turn)
	}
	if nextAction.Kind == AssistantActionReadReview {
		state := normalizedDialogState(next.DialogState)
		state.Phase = DialogPhaseReview
		state.Pending = nil
		state.ReviewAccepted = false
		state.ReviewedRevision = state.DraftRevision
		state.AuthorizedRevision = 0
		state.NoProgressCount = 0
		state.LastPromptKey = "final_review"
		next.DialogState = state
		stampSchedulingReviewFence(&next, cfg)
		syncTurnUpdate(&turn, next, services, staff, cfg)
		turn.AIMessage = finalBookingReviewPrompt(next, services, staff, cfg)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, before, next, "booking_review", "booking_review", "final_booking_review")
		return s.store.SaveTurn(ctx, turn)
	}
	if handled, updated, err := s.maybeAskCustomerSMSConsent(ctx, ownerUserID, turn, before, next, services, staff, cfg, knowledge); handled {
		return updated, err
	}
	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}
