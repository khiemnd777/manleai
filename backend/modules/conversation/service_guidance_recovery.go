package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	legacyPromptCallerGoalGuidanceRecovery = "caller_goal_guidance_recovery"
	legacyPromptServiceGuidanceRecovery    = "service_guidance_recovery"

	GuidanceRecoveryStageCallerGoal = "caller_goal"
	GuidanceRecoveryStageService    = "service"

	guidanceRecoveryActionBook          = GuidanceActionBook
	guidanceRecoveryActionCatalog       = GuidanceActionServiceCatalog
	guidanceRecoveryActionConsultation  = GuidanceActionConsultation
	guidanceRecoveryActionSalonQuestion = GuidanceActionSalonQuestion
	guidanceRecoveryActionNameService   = GuidanceActionNameService
	guidanceRecoveryActionHumanHandoff  = GuidanceActionHumanHandoff
	guidanceRecoveryActionReschedule    = GuidanceActionReschedule
	guidanceRecoveryActionCancel        = GuidanceActionCancel

	maxGuidanceNoProgress       = 3
	maxGuidanceProviderFailures = 2
)

type guidanceRecoveryEvidenceKind string

const (
	guidanceEvidenceNone             guidanceRecoveryEvidenceKind = ""
	guidanceEvidenceServiceSelected  guidanceRecoveryEvidenceKind = "service_selected"
	guidanceEvidenceCategoryScoped   guidanceRecoveryEvidenceKind = "category_scoped"
	guidanceEvidenceCatalogMenu      guidanceRecoveryEvidenceKind = "catalog_menu"
	guidanceEvidenceConsultation     guidanceRecoveryEvidenceKind = "consultation"
	guidanceEvidenceBooking          guidanceRecoveryEvidenceKind = "booking"
	guidanceEvidenceInformation      guidanceRecoveryEvidenceKind = "information_question"
	guidanceEvidenceNameService      guidanceRecoveryEvidenceKind = "name_service"
	guidanceEvidenceOwner            guidanceRecoveryEvidenceKind = "owner"
	guidanceEvidenceProviderFailure  guidanceRecoveryEvidenceKind = "provider_failure"
	guidanceEvidenceCallerNoProgress guidanceRecoveryEvidenceKind = "caller_no_progress"
)

type guidanceRecoveryEvidence struct {
	Kind            guidanceRecoveryEvidenceKind
	ProviderOutcome string
}

type guidanceRecoveryTransition struct {
	State         *GuidanceRecoveryState
	HandoffReason string
}

func isLegacyGuidanceRecoveryPrompt(promptKey string) bool {
	switch strings.TrimSpace(promptKey) {
	case legacyPromptCallerGoalGuidanceRecovery, legacyPromptServiceGuidanceRecovery:
		return true
	default:
		return false
	}
}

func legacyGuidanceRecoveryStage(promptKey string) string {
	if strings.TrimSpace(promptKey) == legacyPromptServiceGuidanceRecovery {
		return GuidanceRecoveryStageService
	}
	return GuidanceRecoveryStageCallerGoal
}

func guidanceRecoveryStateActive(state *GuidanceRecoveryState) bool {
	if state == nil {
		return false
	}
	switch strings.TrimSpace(state.Stage) {
	case GuidanceRecoveryStageCallerGoal, GuidanceRecoveryStageService:
		return true
	default:
		return false
	}
}

func guidanceRecoveryExpectedInput(state *GuidanceRecoveryState) string {
	if state != nil && strings.TrimSpace(state.Stage) == GuidanceRecoveryStageService {
		return ExpectedInputService
	}
	return ExpectedInputCallerGoal
}

// deriveGuidanceRecoveryEvidence consumes typed routing, semantic, and catalog
// evidence only. Raw caller wording is intentionally absent: natural language
// meaning belongs to TurnUnderstanding, while service identity belongs to the
// catalog interpreter.
func deriveGuidanceRecoveryEvidence(plan TurnPlan, turn TurnUnderstanding, understanding serviceUnderstandingResult) guidanceRecoveryEvidence {
	outcome := strings.TrimSpace(turn.InterpreterOutcome)
	if (plan.Route == TurnRouteActionLane && plan.Reason == "owner_handoff") || turnGoalIs(turn, "human_handoff") || turn.GuidanceAction == GuidanceActionHumanHandoff {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceOwner, ProviderOutcome: outcome}
	}
	switch strings.TrimSpace(turn.GuidanceAction) {
	case GuidanceActionBook:
		if turn.GuidancePartySize >= 2 {
			return guidanceRecoveryEvidence{}
		}
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceBooking, ProviderOutcome: outcome}
	case GuidanceActionServiceCatalog:
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceCatalogMenu, ProviderOutcome: outcome}
	case GuidanceActionConsultation:
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceConsultation, ProviderOutcome: outcome}
	case GuidanceActionSalonQuestion:
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceInformation, ProviderOutcome: outcome}
	case GuidanceActionNameService:
		if understanding.Status == serviceUnderstandingStatusSelected && len(understanding.Candidates) == 1 {
			return guidanceRecoveryEvidence{Kind: guidanceEvidenceServiceSelected, ProviderOutcome: outcome}
		}
		if understanding.Status == serviceUnderstandingStatusAmbiguous && len(understanding.Candidates) > 0 {
			return guidanceRecoveryEvidence{Kind: guidanceEvidenceCategoryScoped, ProviderOutcome: outcome}
		}
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceNameService, ProviderOutcome: outcome}
	case GuidanceActionReschedule, GuidanceActionCancel:
		return guidanceRecoveryEvidence{}
	}
	if question, ok := firstDeferredInformationQuestion(turn); ok {
		if question.Subject == ConversationQuestionCatalog {
			if strings.TrimSpace(turn.Reason) == "service_menu" || strings.TrimSpace(question.Reason) == "service_menu" {
				return guidanceRecoveryEvidence{Kind: guidanceEvidenceCatalogMenu, ProviderOutcome: outcome}
			}
			return guidanceRecoveryEvidence{Kind: guidanceEvidenceInformation, ProviderOutcome: outcome}
		}
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceInformation, ProviderOutcome: outcome}
	}
	if turnGoalIs(turn, "consultation") || meaningfulConsultationNeeds(turn.Consultation) || len(turn.ConsultationMutations) > 0 {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceConsultation, ProviderOutcome: outcome}
	}
	if plan.Route == TurnRouteAnswerLane {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceInformation, ProviderOutcome: outcome}
	}
	if understanding.Status == serviceUnderstandingStatusSelected && len(understanding.Candidates) == 1 {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceServiceSelected, ProviderOutcome: outcome}
	}
	if understanding.Status == serviceUnderstandingStatusAmbiguous && len(understanding.Candidates) > 0 && isCategoryLevelServiceUnderstanding(understanding) {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceCategoryScoped, ProviderOutcome: outcome}
	}
	if len(turn.Acts) > 0 || plan.PartySignal.IsParty {
		return guidanceRecoveryEvidence{}
	}
	if turnGoalIs(turn, "book_appointment") {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceBooking, ProviderOutcome: outcome}
	}
	if turnGoalIs(turn, "reschedule_appointment") || turnGoalIs(turn, "cancel_appointment") {
		return guidanceRecoveryEvidence{}
	}
	if guidanceProviderFailureOutcome(outcome) {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceProviderFailure, ProviderOutcome: outcome}
	}
	if turn.ModelInvoked {
		return guidanceRecoveryEvidence{Kind: guidanceEvidenceCallerNoProgress, ProviderOutcome: outcome}
	}
	return guidanceRecoveryEvidence{}
}

func guidanceProviderFailureOutcome(outcome string) bool {
	switch strings.TrimSpace(outcome) {
	case TurnInterpreterOutcomeProviderDisabled, TurnInterpreterOutcomeProviderError,
		TurnInterpreterOutcomeTimeout, TurnInterpreterOutcomeEmptyOutput, TurnInterpreterOutcomeSchemaInvalid:
		return true
	default:
		return false
	}
}

// reduceGuidanceRecoveryState is the sole transition owner for guidance
// recovery counters. Provider failures never advance caller no-progress, and
// accepted catalog/workflow progress resets both counters.
func reduceGuidanceRecoveryState(current *GuidanceRecoveryState, initialStage string, offeredActions []string, evidence guidanceRecoveryEvidence) guidanceRecoveryTransition {
	next := &GuidanceRecoveryState{Stage: strings.TrimSpace(initialStage)}
	if current != nil {
		*next = *current
		next.OfferedActions = append([]string(nil), current.OfferedActions...)
	}
	if next.Stage == "" {
		next.Stage = GuidanceRecoveryStageCallerGoal
	}
	next.OfferedActions = append([]string(nil), offeredActions...)
	next.ProgressFingerprint = ""

	switch evidence.Kind {
	case guidanceEvidenceCatalogMenu, guidanceEvidenceConsultation, guidanceEvidenceBooking, guidanceEvidenceCategoryScoped, guidanceEvidenceNameService:
		next.Stage = GuidanceRecoveryStageService
		next.NoProgressCount = 0
		next.ProviderFailureCount = 0
		next.LastProviderOutcome = strings.TrimSpace(evidence.ProviderOutcome)
	case guidanceEvidenceInformation:
		next.NoProgressCount = 0
		next.ProviderFailureCount = 0
		next.LastProviderOutcome = strings.TrimSpace(evidence.ProviderOutcome)
	case guidanceEvidenceProviderFailure:
		next.ProviderFailureCount++
		next.LastProviderOutcome = strings.TrimSpace(evidence.ProviderOutcome)
		if next.ProviderFailureCount >= maxGuidanceProviderFailures {
			return guidanceRecoveryTransition{State: next, HandoffReason: HandoffReasonGuidanceProviderUnavailable}
		}
	case guidanceEvidenceCallerNoProgress:
		next.NoProgressCount++
		next.ProviderFailureCount = 0
		next.LastProviderOutcome = strings.TrimSpace(evidence.ProviderOutcome)
		if next.NoProgressCount >= maxGuidanceNoProgress {
			return guidanceRecoveryTransition{State: next, HandoffReason: HandoffReasonServiceClarification}
		}
	}
	return guidanceRecoveryTransition{State: next}
}

func guidanceRecoveryOfferedActions(stage string, services []ServiceOption, cfg *RuntimeConfig) []string {
	actions := []string{}
	if strings.TrimSpace(stage) == GuidanceRecoveryStageCallerGoal {
		actions = append(actions, guidanceRecoveryActionBook)
	}
	if len(services) > 0 {
		actions = append(actions, guidanceRecoveryActionCatalog)
		if strings.TrimSpace(stage) == GuidanceRecoveryStageService {
			actions = append(actions, guidanceRecoveryActionNameService)
		}
	}
	if consultationGuidanceAvailable(services, cfg) {
		actions = append(actions, guidanceRecoveryActionConsultation)
	}
	if strings.TrimSpace(stage) == GuidanceRecoveryStageCallerGoal {
		actions = append(actions, guidanceRecoveryActionSalonQuestion)
	}
	actions = append(actions, guidanceRecoveryActionHumanHandoff)
	return actions
}

func consultationGuidanceAvailable(services []ServiceOption, cfg *RuntimeConfig) bool {
	if cfg == nil || !cfg.ConsultationEnabled {
		return false
	}
	for _, service := range services {
		if consultationProfileReadyForRecommendation(service.ConsultationProfile) {
			return true
		}
	}
	return false
}

func guidanceRecoveryPrompt(state GuidanceRecoveryState, _ bool) string {
	choices := make([]string, 0, len(state.OfferedActions))
	for _, action := range state.OfferedActions {
		if label := guidanceActionSpokenLabel(action); label != "" {
			choices = append(choices, label)
		}
	}
	if len(choices) == 0 {
		return "What would you like help with?"
	}
	return "Would you like to " + joinHumanList(choices) + "?"
}

func guidanceProviderFailurePrompt(state GuidanceRecoveryState) string {
	choices := make([]string, 0, len(state.OfferedActions))
	for _, action := range state.OfferedActions {
		if label := guidanceActionSpokenLabel(action); label != "" {
			choices = append(choices, label)
		}
	}
	if len(choices) == 0 {
		return "I'm having trouble checking the salon's service guidance. I can ask the owner to help."
	}
	return "I'm having trouble checking the salon's service guidance. Would you like to " + joinHumanList(choices) + "?"
}

type guidanceActionPresentation struct {
	SpokenLabel string
}

// guidanceActionPresentations owns presentation copy for stable protocol
// actions. Runtime availability still comes exclusively from OfferedActions;
// adding copy here cannot make an unavailable workflow selectable.
var guidanceActionPresentations = map[string]guidanceActionPresentation{
	GuidanceActionBook: {
		SpokenLabel: "book an appointment",
	},
	GuidanceActionServiceCatalog: {
		SpokenLabel: "hear the bookable service menu",
	},
	GuidanceActionConsultation: {
		SpokenLabel: "get help choosing a service",
	},
	GuidanceActionSalonQuestion: {
		SpokenLabel: "ask a question about the salon",
	},
	GuidanceActionNameService: {
		SpokenLabel: "name a service you already know",
	},
	GuidanceActionHumanHandoff: {
		SpokenLabel: "have the owner help",
	},
	GuidanceActionReschedule: {
		SpokenLabel: "reschedule an existing appointment",
	},
	GuidanceActionCancel: {
		SpokenLabel: "cancel an existing appointment",
	},
}

func guidanceActionSpokenLabel(action string) string {
	return guidanceActionPresentations[strings.TrimSpace(action)].SpokenLabel
}

func serviceGuidanceMenuReply(services []ServiceOption, consultationAvailable bool) string {
	names := serviceCandidateNames(services, 8)
	if len(names) == 0 {
		return "I don't have a bookable service list right now. I can ask the owner to help you choose."
	}
	prefix := "Bookable services include "
	if len(services) > len(names) {
		prefix = "Some bookable services include "
	}
	reply := prefix + joinHumanList(names) + ". You can name one"
	if consultationAvailable {
		return reply + ", or tell me what you want for your nails and I can help narrow it down."
	}
	return reply + "."
}

func applyGuidanceRecoveryState(session *Session, state *GuidanceRecoveryState) {
	if session == nil || state == nil {
		return
	}
	dialog := normalizedDialogState(session.DialogState)
	dialog.Phase = DialogPhaseClarifying
	dialog.Pending = nil
	dialog.ReviewAccepted = false
	dialog.ReviewedRevision = 0
	dialog.AuthorizedRevision = 0
	dialog.ReviewedBookingMode = ""
	dialog.SelectedSchedulingAuthority = ""
	dialog.LastPromptKey = ""
	dialog.NoProgressCount = 0
	dialog.ProviderFailureCount = 0
	dialog.ProgressFingerprint = ""
	cloned := *state
	cloned.OfferedActions = append([]string(nil), state.OfferedActions...)
	dialog.Guidance = &cloned
	session.DialogState = dialog
	dialog.Guidance.ProgressFingerprint = guidanceProgressFingerprint(*session, *dialog.Guidance)
	session.DialogState = dialog
}

func guidanceProgressFingerprint(session Session, guidance GuidanceRecoveryState) string {
	payload := struct {
		Stage            string
		OfferedActions   []string
		Intent           string
		SelectedServices []string
		DraftRevision    int
		Pending          *PendingConversationAct
		Phase            string
	}{
		Stage: strings.TrimSpace(guidance.Stage), OfferedActions: append([]string(nil), guidance.OfferedActions...),
		Intent: strings.TrimSpace(session.Intent), SelectedServices: selectedServiceIDs(session),
		DraftRevision: session.DialogState.DraftRevision, Pending: clonePendingConversationAct(session.DialogState.Pending),
		Phase: strings.TrimSpace(session.DialogState.Phase),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:12])
}

func clearGuidanceRecoveryState(session *Session, phase string) {
	if session == nil || !guidanceRecoveryStateActive(normalizedDialogState(session.DialogState).Guidance) {
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
	answerCtx *AIAnswerContext,
	services []ServiceOption,
	staff []StaffOption,
	cfg *RuntimeConfig,
) (bool, *Session, error) {
	state := normalizedDialogState(session.DialogState)
	active := guidanceRecoveryStateActive(state.Guidance)
	expectedInput := plan.ExpectedInput
	if active {
		expectedInput = guidanceRecoveryExpectedInput(state.Guidance)
	}
	evidence := deriveGuidanceRecoveryEvidence(plan, turnUnderstanding, serviceUnderstanding)
	if evidence.Kind == guidanceEvidenceNone || evidence.Kind == guidanceEvidenceServiceSelected {
		return false, nil, nil
	}
	if !active && expectedInput != ExpectedInputCallerGoal && expectedInput != ExpectedInputService {
		return false, nil, nil
	}
	if !active {
		switch evidence.Kind {
		case guidanceEvidenceProviderFailure, guidanceEvidenceCallerNoProgress:
		default:
			if strings.TrimSpace(turnUnderstanding.GuidanceAction) == "" {
				return false, nil, nil
			}
		}
	}

	guidanceCapability := resolveServiceGuidanceCapability(services, cfg)
	if evidence.Kind == guidanceEvidenceConsultation && guidanceCapability.RecommendationReady {
		return s.handleServiceConsultation(ctx, ownerUserID, session, message, eventKey, serviceUnderstanding, turnUnderstanding, services, staff, cfg)
	}

	next := cloneSessionForTurn(session)
	initialStage := GuidanceRecoveryStageCallerGoal
	if expectedInput == ExpectedInputService {
		initialStage = GuidanceRecoveryStageService
	}
	actionStage := initialStage
	if evidence.Kind == guidanceEvidenceCatalogMenu || evidence.Kind == guidanceEvidenceConsultation || evidence.Kind == guidanceEvidenceBooking || evidence.Kind == guidanceEvidenceCategoryScoped || evidence.Kind == guidanceEvidenceNameService {
		actionStage = GuidanceRecoveryStageService
	}
	offeredActions := guidanceRecoveryOfferedActions(actionStage, services, cfg)
	transition := reduceGuidanceRecoveryState(state.Guidance, initialStage, offeredActions, evidence)
	applyGuidanceRecoveryState(&next, transition.State)

	if evidence.Kind == guidanceEvidenceOwner {
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{"guidance_recovery_action": string(evidence.Kind)})
		updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll ask the owner to help directly. This is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	}

	if transition.HandoffReason != "" {
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
		reply := "I'm still not sure what kind of service help you need. I'll ask the owner to help, and this is not a confirmed appointment."
		if transition.HandoffReason == HandoffReasonGuidanceProviderUnavailable {
			reply = "I'm having trouble checking the salon's service guidance, so I won't guess. I'll ask the owner to help, and this is not a confirmed appointment."
		}
		updated, err := s.saveHandoffTurn(ctx, turn, next, transition.HandoffReason, reply, services, staff, cfg)
		return true, updated, err
	}

	turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	applyTurnPlanMetadata(&turn, plan)
	applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
	if evidence.Kind == guidanceEvidenceConsultation {
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, serviceGuidanceCapabilityMetadata(guidanceCapability))
	}

	switch evidence.Kind {
	case guidanceEvidenceConsultation:
		turn.AIMessage = serviceGuidanceCapabilityReply(guidanceCapability, services)
	case guidanceEvidenceCatalogMenu:
		applyGuidanceRecoveryState(&next, transition.State)
		question := ConversationQuestion{Subject: ConversationQuestionCatalog, Mode: turnUnderstanding.GuidanceCatalogMode, Confidence: turnUnderstanding.Confidence}
		route := routeStructuredServiceQuestion(question, session, serviceUnderstanding, answerCtx)
		turn = newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
		turn.AIMessage = route.Reply
		if question.Mode == "" || question.Mode == ConversationQuestionModeList {
			turn.AIMessage = serviceGuidanceMenuReply(services, consultationGuidanceAvailable(services, cfg))
		} else if question.Mode != ConversationQuestionModeExistence {
			followUp := guidanceCatalogFollowUp(session, services, cfg)
			// A structured detail/comparison answer may already end with the
			// useful next question. Do not append a second competing question.
			if !strings.Contains(turn.AIMessage, "?") {
				turn.AIMessage = strings.TrimSpace(turn.AIMessage + " " + followUp)
			}
		}
		applyAnswerRouteMetadata(&turn, route, answerCtx)
	case guidanceEvidenceBooking:
		next.Intent = IntentBooking
		applyGuidanceRecoveryState(&next, transition.State)
		turn = newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
		// The semantic action already resolved the caller's goal. Continue the
		// booking workflow by asking for its next missing field instead of
		// presenting the caller-goal menu again.
		turn.AIMessage = "Which bookable service would you like?"
	case guidanceEvidenceNameService:
		next.Intent = IntentBooking
		applyGuidanceRecoveryState(&next, transition.State)
		turn = newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
		turn.AIMessage = "Which bookable service would you like?"
	case guidanceEvidenceCategoryScoped:
		next.Intent = IntentBooking
		applyGuidanceRecoveryState(&next, transition.State)
		turn = newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, plan)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, guidanceRecoveryMetadata(evidence.Kind, next.DialogState.Guidance))
		turn.AIMessage = serviceClarificationPrompt(next, serviceUnderstanding, cfg)
		setPendingServiceCandidateMetadata(&turn, serviceUnderstanding)
	case guidanceEvidenceInformation:
		question := ConversationQuestion{Subject: turnUnderstanding.GuidanceQuestionSubject, Mode: turnUnderstanding.GuidanceCatalogMode, Confidence: turnUnderstanding.Confidence}
		route := routeStructuredQuestionAnswer(message, question, session, serviceUnderstanding, answerCtx, cfg, s.now)
		if !route.Handled {
			return false, nil, nil
		}
		// Answer the operational question that was actually asked. A generic
		// guidance menu here makes a correct hours/price/policy answer sound like
		// the system failed to understand the caller.
		turn.AIMessage = strings.TrimSpace(answerWithoutGenericBookingOffer(route.Reply))
		applyAnswerRouteMetadata(&turn, route, answerCtx)
	case guidanceEvidenceProviderFailure:
		turn.AIMessage = guidanceProviderFailurePrompt(*next.DialogState.Guidance)
	default:
		turn.AIMessage = guidanceRecoveryPrompt(*next.DialogState.Guidance, next.DialogState.Guidance.NoProgressCount > 1)
	}
	expected := guidanceRecoveryExpectedInput(next.DialogState.Guidance)
	finalizeTurnMetadata(&turn, session, next, expected, expected, "guidance_recovery")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func guidanceCatalogFollowUp(session Session, services []ServiceOption, cfg *RuntimeConfig) string {
	if pending := pendingServiceCandidateServices(session, services); len(pending) > 0 {
		label := serviceCatalogQuestionLabel(serviceUnderstandingResult{}, pending)
		return "For your appointment, which " + label + " service would you like?"
	}
	if hasOperationalBookingProgress(session) {
		return resumeBookingPrompt(session, services, cfg)
	}
	return "Would you like to book one of those, or get help choosing?"
}

func guidanceRecoveryMetadata(action guidanceRecoveryEvidenceKind, state *GuidanceRecoveryState) map[string]any {
	metadata := map[string]any{"guidance_recovery_action": string(action)}
	if state == nil {
		return metadata
	}
	metadata["guidance_recovery_stage"] = state.Stage
	metadata["guidance_recovery_no_progress_count"] = state.NoProgressCount
	metadata["guidance_recovery_provider_failure_count"] = state.ProviderFailureCount
	metadata["guidance_recovery_progress_fingerprint"] = state.ProgressFingerprint
	metadata["guidance_recovery_offered_actions"] = append([]string(nil), state.OfferedActions...)
	return metadata
}
