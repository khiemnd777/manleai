package conversation

import "context"

const (
	AssistantActionAnswerQuestion    = "answer_question"
	AssistantActionAskClarification  = "ask_clarification"
	AssistantActionAskMissingField   = "ask_missing_field"
	AssistantActionCheckAvailability = "check_availability"
	AssistantActionOfferSlots        = "offer_slots"
	AssistantActionReadReview        = "read_final_review"
	AssistantActionExecuteBooking    = "execute_booking"
	AssistantActionHandoffOwner      = "handoff_owner"
	AssistantActionInformResult      = "inform_result"
)

type AssistantAction struct {
	Kind         string
	MissingField string
	Reason       string
}

func planNextConversationAction(session Session, missing string) AssistantAction {
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
	if reviewAuthorizationCurrent(state) {
		return AssistantAction{Kind: AssistantActionExecuteBooking, Reason: "current_revision_authorized"}
	}
	return AssistantAction{Kind: AssistantActionReadReview, Reason: "review_missing_or_stale"}
}

func (s *Service) continueAfterDraftReady(ctx context.Context, ownerUserID string, turn TurnRecord, before Session, next Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	nextAction := planNextConversationAction(next, missingBookingField(next))
	if nextAction.Kind == AssistantActionAskMissingField {
		missing := nextAction.MissingField
		turn.AIMessage = promptForMissingField(missing)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, before, next, missing, missing, "missing_field")
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
		syncTurnUpdate(&turn, next, services, staff, cfg)
		turn.AIMessage = finalBookingReviewPrompt(next, services, staff, cfg)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, before, next, "booking_review", "booking_review", "final_booking_review")
		return s.store.SaveTurn(ctx, turn)
	}
	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}
