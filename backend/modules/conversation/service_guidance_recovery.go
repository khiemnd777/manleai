package conversation

import (
	"context"
	"strings"
)

const (
	promptCallerGoalGuidanceRecovery = "caller_goal_guidance_recovery"
	promptServiceGuidanceRecovery    = "service_guidance_recovery"
	maxGuidanceRecoveryPrompts       = 3
)

type guidanceRecoveryChoice string

const (
	guidanceRecoveryUnknown       guidanceRecoveryChoice = ""
	guidanceRecoveryBook          guidanceRecoveryChoice = "book"
	guidanceRecoveryListServices  guidanceRecoveryChoice = "list_services"
	guidanceRecoveryConsult       guidanceRecoveryChoice = "consultation"
	guidanceRecoverySalonQuestion guidanceRecoveryChoice = "salon_question"
	guidanceRecoveryOwner         guidanceRecoveryChoice = "owner"
	guidanceRecoveryService       guidanceRecoveryChoice = "service_selected"
)

func isGuidanceRecoveryPrompt(promptKey string) bool {
	switch strings.TrimSpace(promptKey) {
	case promptCallerGoalGuidanceRecovery, promptServiceGuidanceRecovery:
		return true
	default:
		return false
	}
}

func guidanceRecoveryExpectedInput(promptKey string) string {
	if strings.TrimSpace(promptKey) == promptServiceGuidanceRecovery {
		return ExpectedInputService
	}
	return ExpectedInputCallerGoal
}

func semanticInterpretationUnavailable(turn TurnUnderstanding) bool {
	if !turn.ModelInvoked {
		return false
	}
	outcome := strings.TrimSpace(turn.InterpreterOutcome)
	return outcome != "" && outcome != TurnInterpreterOutcomeAccepted
}

func classifyGuidanceRecoveryChoice(message string, understanding serviceUnderstandingResult) guidanceRecoveryChoice {
	if understanding.Status == serviceUnderstandingStatusSelected && len(understanding.Candidates) == 1 {
		return guidanceRecoveryService
	}
	if shouldHandoff(message) || shouldComplaintHandoff(message) {
		return guidanceRecoveryOwner
	}
	if asksServiceMenu(message) {
		return guidanceRecoveryListServices
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return guidanceRecoveryUnknown
	}
	consultationSignal := containsAnyLoosePhrase(normalized, []string{
		"help me choose", "help choosing", "help choose", "choose for me", "which should i choose",
		"what should i choose", "what service should", "which service should", "recommend a service",
		"what do you recommend", "need a recommendation", "consult for me", "consultation", "advise me",
		"need advice", "guide me", "right treatment", "right service", "compare services",
	})
	if consultationSignal {
		return guidanceRecoveryConsult
	}
	if hasBookingVerbSignal(message) {
		return guidanceRecoveryBook
	}
	if containsAnyLoosePhrase(normalized, []string{
		"ask about the salon", "question about the salon", "salon question", "something about the salon",
		"general question", "ask a question",
	}) {
		return guidanceRecoverySalonQuestion
	}
	return guidanceRecoveryUnknown
}

func guidanceRecoveryPrompt(promptKey string, repeated bool) string {
	if promptKey == promptServiceGuidanceRecovery {
		if repeated {
			return "You can say 'list services' or 'help me choose.' Which would you prefer?"
		}
		return "No problem. Would you like me to list the bookable services, or help you choose?"
	}
	if repeated {
		return "I can help with booking, choosing a service, or a salon question. Which would you like?"
	}
	return "No problem. Are you looking to book, get help choosing a service, or ask about the salon?"
}

func serviceGuidanceMenuReply(services []ServiceOption) string {
	names := serviceCandidateNames(services, 8)
	if len(names) == 0 {
		return "I don't have a bookable service list right now. I can ask the owner to help you choose."
	}
	prefix := "Bookable services include "
	if len(services) > len(names) {
		prefix = "Some bookable services include "
	}
	return prefix + joinHumanList(names) + ". You can name one, or ask me to help you choose."
}

func setGuidanceRecoveryState(session *Session, promptKey string, promptCount int) {
	if session == nil {
		return
	}
	state := normalizedDialogState(session.DialogState)
	state.Phase = DialogPhaseClarifying
	state.Pending = nil
	state.ReviewAccepted = false
	state.ReviewedRevision = 0
	state.AuthorizedRevision = 0
	state.LastPromptKey = promptKey
	state.NoProgressCount = promptCount
	session.DialogState = state
}

func clearGuidanceRecoveryState(session *Session, phase string) {
	if session == nil || !isGuidanceRecoveryPrompt(session.DialogState.LastPromptKey) {
		return
	}
	session.DialogState = resetDialogProgress(session.DialogState, phase)
}

func (s *Service) handleGuidanceRecovery(
	ctx context.Context,
	ownerUserID string,
	session Session,
	message string,
	eventKey string,
	plan TurnPlan,
	turnUnderstanding TurnUnderstanding,
	serviceUnderstanding serviceUnderstandingResult,
	services []ServiceOption,
	staff []StaffOption,
	cfg *RuntimeConfig,
) (bool, *Session, error) {
	state := normalizedDialogState(session.DialogState)
	active := isGuidanceRecoveryPrompt(state.LastPromptKey)
	expectedInput := plan.ExpectedInput
	if active {
		expectedInput = guidanceRecoveryExpectedInput(state.LastPromptKey)
	}
	choice := classifyGuidanceRecoveryChoice(message, serviceUnderstanding)
	if choice == guidanceRecoveryService {
		return false, nil, nil
	}

	if !active {
		if !semanticInterpretationUnavailable(turnUnderstanding) || (expectedInput != ExpectedInputCallerGoal && expectedInput != ExpectedInputService) {
			return false, nil, nil
		}
		if choice == guidanceRecoveryConsult {
			consultationTurn := turnUnderstanding
			consultationTurn.Goal = "consultation"
			consultationTurn.Source = "deterministic_recovery"
			consultationTurn.Reason = "explicit_service_guidance_request"
			return s.handleServiceConsultation(ctx, ownerUserID, session, message, eventKey, serviceUnderstanding, consultationTurn, services, staff, cfg)
		}
	}

	next := cloneSessionForTurn(session)
	action := choice
	switch choice {
	case guidanceRecoveryConsult:
		consultationTurn := turnUnderstanding
		consultationTurn.Goal = "consultation"
		consultationTurn.Source = "deterministic_recovery"
		consultationTurn.Reason = "service_guidance_recovery_choice"
		return s.handleServiceConsultation(ctx, ownerUserID, session, message, eventKey, serviceUnderstanding, consultationTurn, services, staff, cfg)
	case guidanceRecoveryOwner:
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{"guidance_recovery_action": string(choice)})
		updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll ask the owner to help directly. This is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	case guidanceRecoveryListServices:
		next.Intent = IntentBooking
		setGuidanceRecoveryState(&next, promptServiceGuidanceRecovery, 1)
	case guidanceRecoveryBook:
		next.Intent = IntentBooking
		setGuidanceRecoveryState(&next, promptServiceGuidanceRecovery, 1)
	case guidanceRecoverySalonQuestion:
		next.Intent = IntentUnknown
		clearGuidanceRecoveryState(&next, DialogPhaseOpen)
	default:
		if active && (!semanticInterpretationUnavailable(turnUnderstanding) || plan.Route != TurnRouteSemanticLane) {
			return false, nil, nil
		}
		promptKey := promptCallerGoalGuidanceRecovery
		if expectedInput == ExpectedInputService {
			promptKey = promptServiceGuidanceRecovery
			next.Intent = IntentBooking
		}
		promptCount := 1
		if active && state.LastPromptKey == promptKey {
			promptCount = state.NoProgressCount + 1
		}
		setGuidanceRecoveryState(&next, promptKey, promptCount)
		if promptCount >= maxGuidanceRecoveryPrompts {
			turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
			applyTurnPlanMetadata(&turn, plan)
			applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
			turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
				"guidance_recovery_action": "bounded_handoff", "guidance_recovery_prompt_count": promptCount,
			})
			updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonServiceClarification, "I'm still not sure what help you need. I'll ask the owner to help, and this is not a confirmed appointment.", services, staff, cfg)
			return true, updated, err
		}
		action = guidanceRecoveryUnknown
	}

	turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	applyTurnPlanMetadata(&turn, plan)
	applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"guidance_recovery_action": string(action), "guidance_recovery_prompt_count": next.DialogState.NoProgressCount,
	})
	switch choice {
	case guidanceRecoveryListServices:
		turn.AIMessage = serviceGuidanceMenuReply(services)
	case guidanceRecoveryBook:
		turn.AIMessage = "Sure. Which service would you like to book?"
	case guidanceRecoverySalonQuestion:
		turn.AIMessage = "What would you like to know about the salon?"
	default:
		turn.AIMessage = guidanceRecoveryPrompt(next.DialogState.LastPromptKey, next.DialogState.NoProgressCount > 1)
	}
	finalizeTurnMetadata(&turn, session, next, guidanceRecoveryExpectedInput(next.DialogState.LastPromptKey), guidanceRecoveryExpectedInput(next.DialogState.LastPromptKey), "guidance_recovery")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}
