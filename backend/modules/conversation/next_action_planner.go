package conversation

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
