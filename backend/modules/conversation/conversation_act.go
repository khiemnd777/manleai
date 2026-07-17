package conversation

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type conversationDraftResult struct {
	Handled       bool
	Changed       bool
	Clarification bool
	Escalate      bool
	Reply         string
	ReplySource   string
	Act           ConversationAct
}

type reviewResponseKind string

const (
	reviewResponseAccept     reviewResponseKind = "accept"
	reviewResponseReject     reviewResponseKind = "reject"
	reviewResponseCorrection reviewResponseKind = "correction"
	reviewResponseAmbiguous  reviewResponseKind = "ambiguous"
)

type reviewResponseEvidence struct {
	HasDraftMutation       bool
	HasInformationQuestion bool
	RequestsOtherAction    bool
	HasPartyMutation       bool
}

func (s *Service) turnUnderstandingForMessage(ctx context.Context, session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption) TurnUnderstanding {
	plan := TurnPlan{
		Route:                 TurnRouteSemanticLane,
		ExpectedInput:         expectedInputForSession(session),
		DeterministicCoverage: TurnCoverageNone,
		SemanticServices:      append([]ServiceOption(nil), services...),
		SemanticStaff:         append([]StaffOption(nil), staff...),
	}
	return s.turnUnderstandingForPlan(ctx, session, message, services, aliases, categoryAliases, staff, plan)
}

func (s *Service) turnUnderstandingForPlan(ctx context.Context, session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption, plan TurnPlan) TurnUnderstanding {
	startedAt := time.Now()
	semanticServices := plan.SemanticServices
	semanticStaff := plan.SemanticStaff
	semanticContract := semanticContractForTurnPlan(session, plan)
	recognizableGuidanceActions := []string(nil)
	if semanticContract == TurnSemanticContractGuidance {
		recognizableGuidanceActions = append([]string(nil), plan.RecognizableGuidanceActions...)
	}
	finish := func(turn TurnUnderstanding) TurnUnderstanding {
		diagnostics := cloneStringMap(turn.InterpreterDiagnostics)
		if diagnostics == nil {
			diagnostics = map[string]string{}
		}
		diagnostics["semantic_contract"] = semanticContract
		diagnostics["duration_ms"] = strconv.FormatInt(time.Since(startedAt).Milliseconds(), 10)
		turn.InterpreterDiagnostics = diagnostics
		return turn
	}
	fallback := plan.Understanding
	if fallback.Source == "" {
		fallback.Source = "turn_kernel"
	}
	if fallback.Reason == "" {
		fallback.Reason = plan.Reason
	}
	if s.turnInterpreter == nil {
		fallback.InterpreterOutcome = "interpreter_absent"
		fallback = finish(fallback)
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathInterpreterAbsent, map[string]string{
			"turn_interpreter_outcome": fallback.InterpreterOutcome,
			"turn_semantic_contract":   semanticContract,
		})
		return fallback
	}
	// The caller/channel request owns the deadline. A nested fixed timeout used
	// to cancel valid model work before the simulator or phone turn itself had
	// expired, making identical semantic requests fail in both channels.
	interpreted, err := s.turnInterpreter.InterpretTurn(ctx, TurnInterpretationRequest{
		SalonID:                     session.SalonID,
		SessionID:                   session.ID,
		Channel:                     session.Channel,
		CustomerMessage:             sanitizedTurnInterpreterMessage(message, session),
		ExpectedInput:               plan.ExpectedInput,
		SemanticContract:            semanticContract,
		RecognizableGuidanceActions: recognizableGuidanceActions,
		SelectedServices:            conversationServiceRefs(selectedServiceOptions(session, services)),
		CatalogServices:             conversationServiceRefs(semanticServices),
		CatalogServiceAliases:       conversationServiceAliasRefs(aliases, semanticServices),
		CatalogCategories:           conversationCategoryRefs(semanticServices, categoryAliases),
		SelectedStaff:               conversationStaffRefs(selectedStaffOptions(session, staff)),
		CatalogStaff:                conversationStaffRefs(semanticStaff),
		Pending:                     clonePendingConversationAct(session.DialogState.Pending),
		CurrentBookingStage:         normalizedDialogState(session.DialogState).Phase,
		BookingAction:               bookingActionForSession(session),
		CurrentDraft:                conversationDraftRef(session),
		Consultation:                cloneDialogState(session.DialogState).Consultation,
	})
	fallback.ModelInvoked = true
	if err != nil {
		fallback.InterpreterOutcome = turnInterpreterErrorOutcome(err)
		fallback.InterpreterDiagnostics = turnInterpreterErrorDiagnostics(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fallback.InterpreterOutcome = TurnInterpreterOutcomeTimeout
		}
		fallback.Reason = "semantic_interpreter_" + fallback.InterpreterOutcome
		fallback = finish(fallback)
		attributes := map[string]string{
			"turn_interpreter_outcome": fallback.InterpreterOutcome,
			"turn_semantic_contract":   semanticContract,
			"turn_model_service_count": strconv.Itoa(len(semanticServices)),
			"turn_model_staff_count":   strconv.Itoa(len(semanticStaff)),
		}
		mergeStringAttributes(attributes, turnInterpreterDiagnosticAttributes(fallback.InterpreterDiagnostics))
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, attributes)
		return fallback
	}
	interpreted.ModelInvoked = true
	interpreted = normalizeActiveConsultationTurn(interpreted, session)
	interpreted = normalizeOperationalMutationGoal(interpreted, session)
	interpreted = normalizeExpectedAvailabilityQuestions(interpreted, plan.ExpectedInput)
	// A validated safety concern must not be discarded because an unrelated part
	// of the model reply is malformed or has lower confidence. The caller is
	// handed off before any other interpreted fields are consumed.
	if safety, ok := validateSafetyAssessment(interpreted.Safety); ok && safety.Concern {
		interpreted.Acts = nil
		interpreted.Questions = nil
		interpreted.Consultation = ConsultationNeedProfile{}
		interpreted.ConsultationMutations = nil
		interpreted.Safety = safety
		interpreted.Source = "structured_ai"
		interpreted.InterpreterOutcome = TurnInterpreterOutcomeAccepted
		interpreted = finish(interpreted)
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathStructuredAI, map[string]string{
			"turn_interpreter_outcome": interpreted.InterpreterOutcome,
			"turn_semantic_contract":   semanticContract,
		})
		return interpreted
	}
	validated, valid := validateTurnUnderstanding(interpreted, session, semanticServices, semanticStaff)
	if valid {
		validated, valid = validateGuidanceActionForPlan(validated, semanticContract, plan.RecognizableGuidanceActions)
	}
	if valid {
		validated = normalizeGuidanceConsultationTransitionFlags(validated, semanticContract)
	}
	replacementSourceRejected := false
	if valid && !semanticReplacementSourcesGrounded(validated, message, session, services, aliases, categoryAliases) {
		valid = false
		replacementSourceRejected = true
	}
	if valid {
		validated.Source = "structured_ai"
		validated.InterpreterOutcome = TurnInterpreterOutcomeAccepted
		if semanticContract == TurnSemanticContractGuidance && validated.GuidanceAction == GuidanceActionBook && validated.GuidancePartySize >= 2 {
			validated.Acts = append(validated.Acts, ConversationAct{
				Kind: ConversationActSet, Entity: ConversationEntityGuest, Count: validated.GuidancePartySize,
				Confidence: validated.Confidence, Reason: "guidance_party_size", Source: "structured_ai",
			})
		}
		for index := range validated.Acts {
			validated.Acts[index].Source = "structured_ai"
			if validated.Acts[index].Entity == "" {
				validated.Acts[index].Entity = defaultConversationActEntity(validated.Acts[index].Kind)
			}
		}
		if len(validated.Acts) == 0 && len(validated.Questions) == 0 && len(validated.ConsultationMutations) == 0 &&
			!validated.Safety.Concern && !meaningfulConsultationNeeds(validated.Consultation) &&
			strings.TrimSpace(validated.GuidanceAction) == "" &&
			(validated.Goal == "" || validated.Goal == "unknown") {
			fallback.InterpreterOutcome = "empty_understanding"
			fallback.Reason = "semantic_interpreter_empty_understanding"
			fallback = finish(fallback)
			recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, map[string]string{
				"turn_interpreter_outcome": fallback.InterpreterOutcome,
				"turn_semantic_contract":   semanticContract,
			})
			return fallback
		}
		catalogUnderstanding := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases)
		if shouldUseCatalogServiceEditFallback(session, message, catalogUnderstanding) {
			recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathStructuredAI, map[string]string{
				"turn_interpreter_outcome": "catalog_fallback",
				"turn_semantic_contract":   semanticContract,
			})
			return finish(TurnUnderstanding{
				Goal:                  validated.Goal,
				GuidanceAction:        validated.GuidanceAction,
				GuidanceCatalogMode:   validated.GuidanceCatalogMode,
				Confidence:            validated.Confidence,
				Reason:                "catalog_backed_service_edit_fallback",
				Consultation:          validated.Consultation,
				ConsultationMutations: append([]ConsultationNeedMutation(nil), validated.ConsultationMutations...),
				Safety:                validated.Safety,
				Source:                "catalog_fallback",
				ModelInvoked:          true,
				CatalogFallback:       true,
				InterpreterOutcome:    "catalog_fallback",
			})
		}
		validated = reconcileSemanticServiceTargets(validated, catalogUnderstanding)
		validated = reconcileDeterministicInformationQuestions(message, validated)
		validated = finish(validated)
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathStructuredAI, map[string]string{
			"turn_interpreter_outcome": validated.InterpreterOutcome,
			"turn_semantic_contract":   semanticContract,
		})
		return validated
	}
	if replacementSourceRejected {
		fallback.InterpreterOutcome = TurnInterpreterOutcomeSourceUngrounded
	} else {
		fallback.InterpreterOutcome = rejectedTurnUnderstandingOutcome(interpreted, semanticServices, semanticStaff)
	}
	fallback.Reason = "semantic_interpretation_" + fallback.InterpreterOutcome
	fallback = finish(fallback)
	recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, map[string]string{
		"turn_interpreter_outcome": fallback.InterpreterOutcome,
		"turn_semantic_contract":   semanticContract,
	})
	return fallback
}

func normalizeGuidanceConsultationTransitionFlags(turn TurnUnderstanding, semanticContract string) TurnUnderstanding {
	if semanticContract != TurnSemanticContractGuidance || turn.GuidanceAction != GuidanceActionConsultation {
		return turn
	}
	// Initial guidance selects the consultation workflow; booking/complete are
	// state-transition signals owned by an already active consultation. Do not
	// let an auxiliary extraction boolean skip that workflow boundary.
	if turn.Consultation.BookingRequested || turn.Consultation.ConversationComplete {
		turn.InterpreterDiagnostics = mergeInterpreterDiagnostic(turn.InterpreterDiagnostics, "guidance_consultation_transition_flags_dropped", "1")
	}
	turn.Consultation.BookingRequested = false
	turn.Consultation.ConversationComplete = false
	return turn
}

func validateGuidanceActionForPlan(turn TurnUnderstanding, contract string, recognizableActions []string) (TurnUnderstanding, bool) {
	action := strings.TrimSpace(turn.GuidanceAction)
	catalogMode := strings.TrimSpace(turn.GuidanceCatalogMode)
	questionSubject := strings.TrimSpace(turn.GuidanceQuestionSubject)
	partySize := turn.GuidancePartySize
	turn.GuidanceAction = action
	turn.GuidanceCatalogMode = catalogMode
	turn.GuidanceQuestionSubject = questionSubject
	if contract != TurnSemanticContractGuidance {
		return turn, action == "" && catalogMode == "" && questionSubject == "" && partySize == 0
	}
	if action == "" {
		return turn, strings.TrimSpace(turn.Goal) == "unknown" && catalogMode == "" && questionSubject == "" && partySize == 0
	}
	if !containsString(recognizableActions, action) {
		return TurnUnderstanding{}, false
	}
	if action == GuidanceActionServiceCatalog {
		if catalogMode == "" {
			turn.GuidanceCatalogMode = ConversationQuestionModeList
		} else if !validInformationQuestionMode(catalogMode) {
			return TurnUnderstanding{}, false
		}
		if questionSubject == "" {
			turn.GuidanceQuestionSubject = ConversationQuestionCatalog
		} else if questionSubject != ConversationQuestionCatalog {
			return TurnUnderstanding{}, false
		}
	} else if catalogMode != "" {
		return TurnUnderstanding{}, false
	}
	if action == GuidanceActionSalonQuestion {
		switch questionSubject {
		case ConversationQuestionAvailability, ConversationQuestionPrice, ConversationQuestionHours, ConversationQuestionStaff, ConversationQuestionPolicy:
		default:
			return TurnUnderstanding{}, false
		}
	} else if action != GuidanceActionServiceCatalog && questionSubject != "" {
		return TurnUnderstanding{}, false
	}
	if partySize != 0 {
		if action != GuidanceActionBook || partySize < 2 || partySize > 20 {
			return TurnUnderstanding{}, false
		}
	}
	expectedGoal := GuidanceGoalForAction(action)
	return turn, expectedGoal != "unknown" && strings.TrimSpace(turn.Goal) == expectedGoal
}

func semanticContractForTurnPlan(session Session, plan TurnPlan) string {
	if plan.Route != TurnRouteSemanticLane || plan.Reason == "multiple_signals" ||
		(plan.DeterministicCoverage == TurnCoveragePartial && plan.Reason != "multi_service_semantic_context") {
		return TurnSemanticContractFull
	}
	if plan.ExpectedInput != ExpectedInputCallerGoal && plan.ExpectedInput != ExpectedInputService {
		return TurnSemanticContractFull
	}
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil || state.Phase == DialogPhaseReview || activePartyPlan(session.PartyPlan) {
		return TurnSemanticContractFull
	}
	if !guidanceRecoveryStateActive(state.Guidance) &&
		(strings.TrimSpace(session.Intent) == IntentBooking || serviceSelectionScopedToPending(session, plan.SemanticServices)) {
		return TurnSemanticContractFull
	}
	if strings.TrimSpace(session.ServiceID) != "" || len(session.BookingSegments) > 0 ||
		strings.TrimSpace(session.RequestedDate) != "" || session.RequestedStartTime != nil || len(session.OfferedSlots) > 0 ||
		strings.TrimSpace(session.CustomerName) != "" || strings.TrimSpace(session.StaffID) != "" {
		return TurnSemanticContractFull
	}
	return TurnSemanticContractGuidance
}

func turnInterpreterDiagnosticAttributes(diagnostics map[string]string) map[string]string {
	if len(diagnostics) == 0 {
		return nil
	}
	mapping := map[string]string{
		"provider": "turn_interpreter_provider", "failure_stage": "turn_interpreter_failure_stage",
		"semantic_contract": "turn_semantic_contract", "duration_ms": "turn_interpreter_ms",
		"http_status": "turn_interpreter_http_status", "http_status_class": "turn_interpreter_http_status_class",
		"request_id": "turn_interpreter_request_id", "error_type": "turn_interpreter_error_type",
		"error_code": "turn_interpreter_error_code", "error_param": "turn_interpreter_error_param",
		"schema_fingerprint": "turn_interpreter_schema_fingerprint", "circuit_open": "turn_interpreter_circuit_open",
		"consultation_profile_dropped":   "turn_consultation_profile_dropped",
		"consultation_mutations_dropped": "turn_consultation_mutations_dropped",
	}
	attributes := map[string]string{}
	for source, target := range mapping {
		if value := strings.TrimSpace(diagnostics[source]); value != "" {
			attributes[target] = value
		}
	}
	return attributes
}

func mergeStringAttributes(target map[string]string, values map[string]string) {
	for key, value := range values {
		target[key] = value
	}
}

func rejectedTurnUnderstandingOutcome(turn TurnUnderstanding, services []ServiceOption, staff []StaffOption) string {
	if turn.Confidence > 0 && turn.Confidence < 0.78 {
		return TurnInterpreterOutcomeLowConfidence
	}
	for _, act := range turn.Acts {
		if act.Confidence > 0 && act.Confidence < 0.78 {
			return TurnInterpreterOutcomeLowConfidence
		}
	}
	for _, question := range turn.Questions {
		if question.Confidence > 0 && question.Confidence < 0.78 {
			return TurnInterpreterOutcomeLowConfidence
		}
	}
	for _, mutation := range turn.ConsultationMutations {
		if mutation.Confidence > 0 && mutation.Confidence < 0.78 {
			return TurnInterpreterOutcomeLowConfidence
		}
	}
	if turn.Safety.Concern && turn.Safety.Confidence < 0.78 {
		return TurnInterpreterOutcomeLowConfidence
	}
	validServices := stringSet(serviceOptionIDs(services))
	validStaff := map[string]bool{}
	for _, option := range staff {
		validStaff[strings.TrimSpace(option.ID)] = true
	}
	for _, act := range turn.Acts {
		ids := append(append([]string(nil), act.SourceServiceIDs...), act.TargetServiceIDs...)
		for _, id := range ids {
			if act.Entity == ConversationEntityStaff {
				if !validStaff[strings.TrimSpace(id)] {
					return TurnInterpreterOutcomeCatalogRejected
				}
				continue
			}
			if (act.Entity == ConversationEntityService || isServiceMutationAct(act.Kind)) && !validServices[strings.TrimSpace(id)] {
				return TurnInterpreterOutcomeCatalogRejected
			}
		}
	}
	for _, question := range turn.Questions {
		for _, id := range question.ServiceIDs {
			if !validServices[strings.TrimSpace(id)] {
				return TurnInterpreterOutcomeCatalogRejected
			}
		}
		for _, id := range question.StaffIDs {
			if !validStaff[strings.TrimSpace(id)] {
				return TurnInterpreterOutcomeCatalogRejected
			}
		}
	}
	for _, id := range turn.Consultation.ComparedServiceIDs {
		if !validServices[strings.TrimSpace(id)] {
			return TurnInterpreterOutcomeCatalogRejected
		}
	}
	for _, mutation := range turn.ConsultationMutations {
		if mutation.Field != ConsultationNeedFieldComparedServiceIDs {
			continue
		}
		for _, id := range mutation.Values {
			if !validServices[strings.TrimSpace(id)] {
				return TurnInterpreterOutcomeCatalogRejected
			}
		}
	}
	return TurnInterpreterOutcomeSchemaInvalid
}

func reconcileDeterministicInformationQuestions(message string, turn TurnUnderstanding) TurnUnderstanding {
	if !asksAvailabilityQuestion(message) {
		return turn
	}
	acts := make([]ConversationAct, 0, len(turn.Acts))
	for _, act := range turn.Acts {
		if act.Kind != ConversationActSummarize {
			acts = append(acts, act)
		}
	}
	questions := make([]ConversationQuestion, 0, len(turn.Questions)+1)
	hasAvailability := false
	for _, question := range turn.Questions {
		switch question.Subject {
		case ConversationQuestionCurrentBooking:
			continue
		case ConversationQuestionAvailability:
			hasAvailability = true
		}
		questions = append(questions, question)
	}
	if !hasAvailability {
		questions = append(questions, ConversationQuestion{
			Subject:    ConversationQuestionAvailability,
			Confidence: 1,
			Reason:     "deterministic_availability_evidence",
		})
	}
	turn.Acts = acts
	turn.Questions = questions
	turn.Reason = "deterministic_availability_evidence"
	return turn
}

func shouldUseCatalogServiceEditFallback(session Session, message string, result serviceUnderstandingResult) bool {
	return result.Status == serviceUnderstandingStatusSelected &&
		shouldApplyBareServiceSwitch(session, message, result) &&
		hasServiceSwitchContext(session)
}

func reconcileSemanticServiceTargets(turn TurnUnderstanding, result serviceUnderstandingResult) TurnUnderstanding {
	if result.Status != serviceUnderstandingStatusAmbiguous || len(result.Candidates) < 2 || !isCategoryLevelServiceUnderstanding(result) {
		return turn
	}
	targetIDs := serviceOptionIDs(result.Candidates)
	candidateSet := stringSet(targetIDs)
	for index := range turn.Acts {
		act := &turn.Acts[index]
		if act.Entity != ConversationEntityService || (act.Kind != ConversationActAdd && act.Kind != ConversationActReplace) {
			continue
		}
		if len(act.TargetServiceIDs) == 0 || !allServiceIDsInSet(act.TargetServiceIDs, candidateSet) {
			continue
		}
		act.TargetServiceIDs = append([]string(nil), targetIDs...)
		act.TargetCategoryID = strings.TrimSpace(result.MatchedCategoryID)
		act.TargetCategoryName = strings.TrimSpace(result.MatchedCategoryName)
		act.Reason = "catalog_ambiguity_preserved"
	}
	return turn
}

func normalizeActiveConsultationTurn(turn TurnUnderstanding, session Session) TurnUnderstanding {
	if !consultationStateActive(normalizedDialogState(session.DialogState).Consultation) {
		return turn
	}
	// Consultation ranking and recommendation belong to the backend's
	// owner-approved profiles. Model output is extraction-only while this state
	// is active, so no model-authored operation may reach the booking reducer.
	turn.Acts = nil
	return turn
}

func normalizeOperationalMutationGoal(turn TurnUnderstanding, session Session) TurnUnderstanding {
	if strings.TrimSpace(turn.Goal) != "consultation" || !turnHasMutations(turn) || meaningfulConsultationNeeds(turn.Consultation) || len(turn.ConsultationMutations) > 0 {
		return turn
	}
	// A validated draft mutation is operational evidence. A bare consultation
	// goal without consultation needs must not divert that mutation into the
	// recommendation workflow.
	turn.Goal = "unknown"
	if strings.TrimSpace(session.Intent) == IntentBooking || hasOperationalBookingProgress(session) {
		turn.Goal = "book_appointment"
	}
	turn.InterpreterDiagnostics = mergeInterpreterDiagnostic(turn.InterpreterDiagnostics, "bare_consultation_goal_overridden_by_operational_mutation", "1")
	return turn
}

func normalizeExpectedAvailabilityQuestions(turn TurnUnderstanding, expectedInput string) TurnUnderstanding {
	if expectedInput != ExpectedInputRequestedDate && expectedInput != ExpectedInputRequestedTime {
		return turn
	}
	turn.Questions = append([]ConversationQuestion(nil), turn.Questions...)
	for index := range turn.Questions {
		if _, ok := normalizedSlotTimePreference(turn.Questions[index].TimePreference); !ok {
			continue
		}
		turn.Questions[index].Subject = ConversationQuestionAvailability
	}
	return turn
}

func semanticReplacementSourcesGrounded(turn TurnUnderstanding, message string, session Session, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) bool {
	// A single-service draft can safely resolve an implied source from its one
	// current selection. A party plan cannot: every destructive mutation must
	// retain guest-specific, catalog-grounded source evidence.
	if !activePartyPlan(session.PartyPlan) {
		return true
	}
	hasReplacement := false
	for _, act := range turn.Acts {
		if act.Kind == ConversationActReplace && act.Entity == ConversationEntityService {
			hasReplacement = true
			break
		}
	}
	if !hasReplacement {
		return true
	}

	state := normalizedDialogState(session.DialogState)
	pendingSources := map[string]bool{}
	if state.Pending != nil && state.Pending.Kind == ConversationActReplace {
		pendingSources = stringSet(state.Pending.SourceServiceIDs)
	}
	selected := selectedServiceOptions(session, services)
	explicit := interpretServiceWithCategoryAliases(message, selected, aliases, categoryAliases)
	explicitSources := map[string]bool{}
	if explicit.Reason == serviceUnderstandingExact || explicit.Reason == serviceUnderstandingAlias {
		explicitSources = stringSet(serviceOptionIDs(explicit.Candidates))
	}

	for _, act := range turn.Acts {
		if act.Kind != ConversationActReplace || act.Entity != ConversationEntityService {
			continue
		}
		if len(act.SourceServiceIDs) == 0 {
			return false
		}
		for _, sourceID := range act.SourceServiceIDs {
			sourceID = strings.TrimSpace(sourceID)
			if !explicitSources[sourceID] && !pendingSources[sourceID] {
				return false
			}
		}
	}
	return true
}

func isCategoryLevelServiceUnderstanding(result serviceUnderstandingResult) bool {
	switch result.Reason {
	case serviceUnderstandingCategory, serviceUnderstandingCategoryAlias:
		return true
	default:
		return false
	}
}

func allServiceIDsInSet(ids []string, allowed map[string]bool) bool {
	for _, id := range ids {
		if !allowed[strings.TrimSpace(id)] {
			return false
		}
	}
	return true
}

func primaryConversationAct(understanding TurnUnderstanding) ConversationAct {
	if len(understanding.Acts) == 0 {
		return ConversationAct{Kind: ConversationActUnknown, Source: understanding.Source}
	}
	return understanding.Acts[0]
}

func sanitizedTurnInterpreterMessage(message string, session Session) string {
	message = phonePattern.ReplaceAllString(message, "[phone]")
	message = emailPattern.ReplaceAllString(message, "[email]")
	for _, name := range []string{strings.TrimSpace(session.CustomerName)} {
		if name == "" {
			continue
		}
		message = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(name)).ReplaceAllString(message, "[customer_name]")
	}
	return strings.TrimSpace(message)
}

func deterministicConversationAct(session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) ConversationAct {
	return deterministicConversationActForReviewResponse(
		session,
		message,
		services,
		aliases,
		categoryAliases,
		classifyReviewResponse(message, reviewResponseEvidence{}),
	)
}

func deterministicConversationActForReviewResponse(session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, reviewResponse reviewResponseKind) ConversationAct {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
	}
	state := normalizedDialogState(session.DialogState)
	if state.Phase == DialogPhaseReview && reviewResponse == reviewResponseAccept {
		return ConversationAct{Kind: ConversationActReview, Confidence: 1, Reason: "review_authorization", Source: "deterministic"}
	}
	if state.Pending != nil && !invalidServiceEditPending(session) {
		if act := conversationActFromPending(session, message, services, aliases, categoryAliases, *state.Pending); act.Kind != ConversationActUnknown {
			return act
		}
	}

	return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
}

func conversationActFromPending(session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, pending PendingConversationAct) ConversationAct {
	full := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases)
	switch pending.PromptKey {
	case PendingPartyServiceGuest:
		guestRef := partyGuestRefFromMessage(session.PartyPlan, message, pending.TargetServiceIDs, services)
		if guestRef == "" {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		return ConversationAct{
			Kind:       ConversationActSet,
			Entity:     ConversationEntityGuest,
			GuestRef:   guestRef,
			Confidence: 0.98,
			Reason:     "pending_party_service_guest",
			Source:     "deterministic",
		}
	case PendingPartyServiceOperation:
		kind := partyServiceOperationForMessage(message)
		if kind == ConversationActUnknown || strings.TrimSpace(pending.GuestRef) == "" {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		return ConversationAct{
			Kind:             kind,
			Entity:           ConversationEntityService,
			SourceServiceIDs: append([]string(nil), pending.SourceServiceIDs...),
			TargetServiceIDs: append([]string(nil), pending.TargetServiceIDs...),
			Scope:            ConversationScopeOne,
			GuestRef:         pending.GuestRef,
			Confidence:       0.98,
			Reason:           "pending_party_service_operation",
			Source:           "deterministic",
		}
	case PendingPartyServiceSource:
		candidates := servicesByIDs(services, pending.SourceServiceIDs)
		chosen := newServiceCatalogIndex(candidates, nil, nil).InterpretPending(message)
		if chosen.Status != serviceUnderstandingStatusSelected || len(chosen.Candidates) != 1 || strings.TrimSpace(pending.GuestRef) == "" {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		return ConversationAct{
			Kind:             ConversationActReplace,
			Entity:           ConversationEntityService,
			SourceServiceIDs: []string{chosen.Candidates[0].ID},
			TargetServiceIDs: append([]string(nil), pending.TargetServiceIDs...),
			Scope:            ConversationScopeOne,
			GuestRef:         pending.GuestRef,
			Confidence:       0.98,
			Reason:           "pending_party_service_source",
			Source:           "deterministic",
		}
	case "semantic_add_or_replace":
		if !hasSelectedServiceDraft(session) {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		kind := ConversationActUnknown
		switch {
		case hasServiceAddSignal(message), isPendingServiceAddDecision(message):
			kind = ConversationActAdd
		case hasServiceReplaceSignal(message), isPendingServiceReplaceDecision(message):
			kind = ConversationActReplace
		default:
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		targetIDs := append([]string(nil), pending.TargetServiceIDs...)
		if full.Status == serviceUnderstandingStatusSelected && len(full.Candidates) > 0 {
			targetIDs = serviceOptionIDs(full.Candidates)
		}
		return ConversationAct{
			Kind:             kind,
			SourceServiceIDs: append([]string(nil), pending.SourceServiceIDs...),
			TargetServiceIDs: targetIDs,
			Scope:            ConversationScopeOne,
			Confidence:       0.96,
			Reason:           "pending_add_or_replace_response",
			Source:           "deterministic",
		}
	case "replace_target", "add_target":
		chosen := full
		pendingServices := servicesByIDs(services, pending.TargetServiceIDs)
		if len(pendingServices) > 0 {
			limited := newServiceCatalogIndex(pendingServices, nil, nil).InterpretPending(message)
			if limited.Status != serviceUnderstandingStatusUnknown && !hasExplicitFullCatalogServiceEvidence(full) {
				chosen = limited
			}
		}
		if chosen.Status == serviceUnderstandingStatusUnknown {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		kind := pending.Kind
		act := ConversationAct{
			Kind:               kind,
			SourceServiceIDs:   append([]string(nil), pending.SourceServiceIDs...),
			SourceCategoryID:   pending.SourceCategoryID,
			SourceCategoryName: pending.SourceCategoryName,
			Scope:              pending.Scope,
			Confidence:         chosen.Confidence,
			Reason:             "pending_target_response",
			Source:             "deterministic",
		}
		applyUnderstandingToAct(&act, chosen, false, session)
		return act
	case "replace_source", "remove_source":
		selected := selectedServiceOptions(session, services)
		result := interpretServiceWithCategoryAliases(message, selected, nil, nil)
		if result.Status != serviceUnderstandingStatusSelected || len(result.Candidates) != 1 {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		return ConversationAct{
			Kind:               pending.Kind,
			SourceServiceIDs:   []string{result.Candidates[0].ID},
			TargetServiceIDs:   append([]string(nil), pending.TargetServiceIDs...),
			TargetCategoryID:   pending.TargetCategoryID,
			TargetCategoryName: pending.TargetCategoryName,
			Scope:              ConversationScopeOne,
			Confidence:         result.Confidence,
			Reason:             "pending_source_response",
			Source:             "deterministic",
		}
	case "same_category_add_scope":
		guestScope := guestScopeForMessage(message)
		if guestScope == "" {
			return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
		}
		return ConversationAct{
			Kind:             ConversationActAdd,
			SourceServiceIDs: append([]string(nil), pending.SourceServiceIDs...),
			TargetServiceIDs: append([]string(nil), pending.TargetServiceIDs...),
			GuestScope:       guestScope,
			Confidence:       0.94,
			Reason:           "pending_same_category_guest_scope",
			Source:           "deterministic",
		}
	}
	return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
}

func hasExplicitFullCatalogServiceEvidence(result serviceUnderstandingResult) bool {
	switch result.Reason {
	case serviceUnderstandingExact, serviceUnderstandingAlias, serviceUnderstandingCategory, serviceUnderstandingCategoryAlias:
		return true
	default:
		return false
	}
}

func applyUnderstandingToAct(act *ConversationAct, result serviceUnderstandingResult, source bool, session Session) {
	if act == nil || result.Status == serviceUnderstandingStatusUnknown {
		return
	}
	ids := serviceOptionIDs(result.Candidates)
	if source {
		ids = intersectServiceIDs(ids, selectedServiceIDs(session))
		act.SourceServiceIDs = ids
		act.SourceCategoryID = strings.TrimSpace(result.MatchedCategoryID)
		act.SourceCategoryName = strings.TrimSpace(result.MatchedCategoryName)
		return
	}
	act.TargetServiceIDs = ids
	act.TargetCategoryID = strings.TrimSpace(result.MatchedCategoryID)
	act.TargetCategoryName = strings.TrimSpace(result.MatchedCategoryName)
}

func (s *Service) applyConversationActToDraft(session *Session, act ConversationAct, services []ServiceOption) conversationDraftResult {
	result := conversationDraftResult{Act: act}
	if session == nil || act.Kind == ConversationActUnknown {
		return result
	}
	state := normalizedDialogState(session.DialogState)
	result.Handled = true
	switch act.Kind {
	case ConversationActSummarize:
		result.Reply = currentBookingSummaryReply(*session, services, act.TargetServiceIDs...)
		if state.Pending != nil {
			result.Reply += " " + pendingConversationPrompt(*session, services, state, false)
		}
		result.ReplySource = "current_booking_summary"
		return result
	case ConversationActReview:
		state.ReviewAccepted = state.Phase == DialogPhaseReview && state.ReviewedRevision == state.DraftRevision
		if state.ReviewAccepted {
			state.AuthorizedRevision = state.DraftRevision
		} else {
			state.AuthorizedRevision = 0
		}
		state.Pending = nil
		state.NoProgressCount = 0
		state.LastActKind = act.Kind
		session.DialogState = state
		result.ReplySource = "review_accepted"
		return result
	case ConversationActUndo:
		if len(state.MutationHistory) == 0 && state.LastMutation == nil {
			result.Reply = "There is no recent service change to undo. " + currentBookingSummaryReply(*session, services)
			result.ReplySource = "undo_unavailable"
			return result
		}
		mutation := state.LastMutation
		if len(state.MutationHistory) > 0 {
			last := cloneDraftMutation(state.MutationHistory[len(state.MutationHistory)-1])
			mutation = &last
			state.MutationHistory = state.MutationHistory[:len(state.MutationHistory)-1]
		}
		session.ServiceID = mutation.BeforeServiceID
		session.ServiceName = mutation.BeforeServiceName
		session.BookingSegments = append([]booking.BookingSegmentRequest(nil), mutation.BeforeSegments...)
		session.OfferedSlots = nil
		state = resetDialogProgress(state, DialogPhaseDrafting)
		state.LastMutation = nil
		if len(state.MutationHistory) > 0 {
			last := cloneDraftMutation(state.MutationHistory[len(state.MutationHistory)-1])
			state.LastMutation = &last
		}
		state.LastActKind = act.Kind
		session.DialogState = state
		result.Changed = true
		result.Reply = "Okay, I restored your previous service selection. " + currentBookingSummaryReply(*session, services)
		result.ReplySource = "service_edit_undo"
		return result
	}

	beforeID := session.ServiceID
	beforeName := session.ServiceName
	beforeSegments := append([]booking.BookingSegmentRequest(nil), session.BookingSegments...)
	beforeIDs := selectedServiceIDs(*session)

	switch act.Kind {
	case ConversationActAdd:
		targets := servicesByIDs(services, act.TargetServiceIDs)
		if len(targets) != 1 {
			return setConversationClarification(session, act, services, "add_target")
		}
		sameCategorySources := selectedSameCategoryServiceIDs(*session, targets[0], services)
		if len(sameCategorySources) > 0 && act.GuestScope == "" {
			act.SourceServiceIDs = sameCategorySources
			return setConversationClarification(session, act, services, "same_category_add_scope")
		}
		if act.GuestScope == ConversationGuestAnother && len(beforeIDs) != 1 {
			return conversationDraftResult{
				Handled:       true,
				Clarification: true,
				Escalate:      true,
				Reply:         "I need the owner to help assign those services to the right guests without changing your current selection. This is not a confirmed appointment.",
				ReplySource:   "multi_service_guest_scope_handoff",
				Act:           act,
			}
		}
		result.Changed = addServiceSelection(session, targets)
		if result.Changed && act.GuestScope == ConversationGuestAnother && len(beforeIDs) == 1 {
			session.PartyPlan = &PartyPlan{
				PartySize:       2,
				ParseSource:     "conversation_act_guest_scope",
				ParseConfidence: act.Confidence,
				Groups: []PartyPlanGroup{
					{Label: "caller", Count: 1, ResolvedServiceIDs: []string{beforeIDs[0]}, Source: "current_booking"},
					{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{targets[0].ID}, Source: "conversation_act"},
				},
			}
		}
	case ConversationActReplace:
		targets := servicesByIDs(services, act.TargetServiceIDs)
		if len(targets) != 1 {
			return setConversationClarification(session, act, services, "replace_target")
		}
		sourceIDs := intersectServiceIDs(act.SourceServiceIDs, beforeIDs)
		if len(sourceIDs) == 0 {
			if len(beforeIDs) == 1 {
				sourceIDs = append(sourceIDs, beforeIDs[0])
			} else {
				act.TargetServiceIDs = []string{targets[0].ID}
				return setConversationClarification(session, act, services, "replace_source")
			}
		}
		result.Changed = replaceSelectedServices(session, sourceIDs, targets[0], services)
	case ConversationActRemove:
		sourceIDs := intersectServiceIDs(act.SourceServiceIDs, beforeIDs)
		if len(sourceIDs) == 0 {
			if len(beforeIDs) == 1 {
				sourceIDs = append(sourceIDs, beforeIDs[0])
			} else {
				return setConversationClarification(session, act, services, "remove_source")
			}
		}
		result.Changed = removeSelectedServices(session, sourceIDs, services)
	}

	if !result.Changed {
		result.Reply = currentBookingSummaryReply(*session, services)
		result.ReplySource = "service_edit_no_change"
		return result
	}
	state = resetDialogProgress(state, DialogPhaseDrafting)
	state.LastActKind = act.Kind
	mutation := DraftMutation{
		Kind:              act.Kind,
		BeforeServiceID:   beforeID,
		BeforeServiceName: beforeName,
		BeforeServiceIDs:  beforeIDs,
		BeforeSegments:    beforeSegments,
		AfterServiceIDs:   selectedServiceIDs(*session),
		AfterSegments:     append([]booking.BookingSegmentRequest(nil), session.BookingSegments...),
	}
	state.MutationHistory = append(state.MutationHistory, cloneDraftMutation(mutation))
	if len(state.MutationHistory) > 5 {
		state.MutationHistory = append([]DraftMutation(nil), state.MutationHistory[len(state.MutationHistory)-5:]...)
	}
	state.LastMutation = &mutation
	session.DialogState = state
	result.ReplySource = "conversation_act_reducer"
	return result
}

func setConversationClarification(session *Session, act ConversationAct, services []ServiceOption, promptKey string) conversationDraftResult {
	state := normalizedDialogState(session.DialogState)
	previousKey := state.LastPromptKey
	if previousKey == promptKey {
		state.NoProgressCount++
	} else {
		state.NoProgressCount = 0
	}
	state.Phase = DialogPhaseClarifying
	state.ReviewAccepted = false
	state.LastPromptKey = promptKey
	state.LastActKind = act.Kind
	state.Pending = &PendingConversationAct{
		Kind:               act.Kind,
		SourceServiceIDs:   append([]string(nil), act.SourceServiceIDs...),
		TargetServiceIDs:   append([]string(nil), act.TargetServiceIDs...),
		SourceCategoryID:   act.SourceCategoryID,
		SourceCategoryName: act.SourceCategoryName,
		TargetCategoryID:   act.TargetCategoryID,
		TargetCategoryName: act.TargetCategoryName,
		Scope:              act.Scope,
		GuestScope:         act.GuestScope,
		PromptKey:          promptKey,
	}
	session.DialogState = state
	escalate := state.NoProgressCount >= 3
	reply := pendingConversationPrompt(*session, services, state, state.NoProgressCount > 0)
	if escalate {
		reply = "I’m sorry, I’m still not certain which service change you want. I’ll send this to the owner so they can help without changing your current selection. This is not a confirmed appointment."
	}
	return conversationDraftResult{
		Handled:       true,
		Clarification: true,
		Escalate:      escalate,
		Reply:         reply,
		ReplySource:   "conversation_act_clarification",
		Act:           act,
	}
}

func pendingConversationPrompt(session Session, services []ServiceOption, state DialogState, explain bool) string {
	if state.Pending == nil {
		return "What would you like to change?"
	}
	pending := state.Pending
	switch pending.PromptKey {
	case PendingPartyServiceTarget:
		candidates := servicesByIDs(services, pending.TargetServiceIDs)
		return "Which specific service should I use for the group change: " + joinChoiceList(serviceCandidateNames(candidates, 6)) + "?"
	case PendingPartyServiceGuest:
		return partyServiceGuestPrompt(session.PartyPlan, pending.TargetServiceIDs, services)
	case PendingPartyServiceOperation:
		return partyServiceOperationPrompt(session.PartyPlan, *pending, services)
	case PendingPartyServiceSource:
		return partyServiceSourcePrompt(session.PartyPlan, *pending, services)
	case "replace_target", "add_target":
		candidates := servicesByIDs(services, pending.TargetServiceIDs)
		prefix := ""
		if explain && pending.TargetCategoryName != "" {
			prefix = pending.TargetCategoryName + " is a service category, so I need one specific option. "
		}
		return prefix + serviceEditTargetPrompt(session, candidates, services, pending.PromptKey == "add_target")
	case "replace_source":
		return serviceEditReplaceSourcePrompt(session, services)
	case "remove_source":
		return "Which service would you like me to remove: " + joinChoiceList(selectedServiceNames(session, services)) + "?"
	case "same_category_add_scope":
		target := "that service"
		if len(pending.TargetServiceIDs) > 0 {
			target = serviceName(pending.TargetServiceIDs[0], services, target)
		}
		current := strings.TrimSpace(serviceSummary(session, services))
		return "You already have " + current + ". Is " + target + " another service for you, or is it for another guest?"
	default:
		return "What would you like to change?"
	}
}

func currentBookingSummaryReply(session Session, services []ServiceOption, filterServiceIDs ...string) string {
	filter := stringSet(filterServiceIDs)
	filtered := cloneSessionForTurn(session)
	if len(filter) > 0 {
		filtered.BookingSegments = nil
		for _, segment := range session.BookingSegments {
			if filter[strings.TrimSpace(segment.ServiceID)] {
				filtered.BookingSegments = append(filtered.BookingSegments, segment)
			}
		}
	}
	if len(filtered.BookingSegments) == 0 {
		if len(filter) > 0 {
			return "You do not currently have any of those services in your appointment draft."
		}
		return "You do not have a service selected yet."
	}
	count := len(filtered.BookingSegments)
	label := "services"
	if count == 1 {
		label = "service"
	}
	details := selectedServiceCountSummary(filtered, services)
	if details == "" {
		details = joinHumanList(selectedServiceNames(filtered, services))
	}
	return "You currently have " + countWord(count) + " " + label + ": " + details + "."
}

func replaceSelectedServices(session *Session, sourceIDs []string, target ServiceOption, services []ServiceOption) bool {
	if session == nil || len(sourceIDs) == 0 || strings.TrimSpace(target.ID) == "" {
		return false
	}
	remove := stringSet(sourceIDs)
	before := selectedServiceIDs(*session)
	segments := append([]booking.BookingSegmentRequest(nil), session.BookingSegments...)
	if len(segments) == 0 && session.ServiceID != "" {
		segments = bookingSegmentsFromServices(servicesByIDs(services, []string{session.ServiceID}), *session)
	}
	updated := make([]booking.BookingSegmentRequest, 0, len(segments))
	inserted := false
	for _, segment := range segments {
		if remove[strings.TrimSpace(segment.ServiceID)] {
			if !inserted {
				replacement := segment
				replacement.ServiceID = target.ID
				updated = append(updated, replacement)
				inserted = true
			}
			continue
		}
		updated = append(updated, segment)
	}
	if !inserted {
		return false
	}
	session.BookingSegments = dedupeBookingSegments(updated)
	session.ServiceID = session.BookingSegments[0].ServiceID
	session.ServiceName = serviceName(session.ServiceID, services, target.Name)
	session.OfferedSlots = nil
	session.PartyPlan = nil
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func removeSelectedServices(session *Session, sourceIDs []string, services []ServiceOption) bool {
	if session == nil || len(sourceIDs) == 0 {
		return false
	}
	remove := stringSet(sourceIDs)
	before := selectedServiceIDs(*session)
	segments := make([]booking.BookingSegmentRequest, 0, len(session.BookingSegments))
	for _, segment := range session.BookingSegments {
		if !remove[strings.TrimSpace(segment.ServiceID)] {
			segments = append(segments, segment)
		}
	}
	session.BookingSegments = segments
	if len(segments) == 0 {
		session.ServiceID = ""
		session.ServiceName = ""
	} else {
		session.ServiceID = segments[0].ServiceID
		session.ServiceName = serviceName(session.ServiceID, services, "")
	}
	session.OfferedSlots = nil
	session.PartyPlan = nil
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func dedupeBookingSegments(segments []booking.BookingSegmentRequest) []booking.BookingSegmentRequest {
	seen := map[string]bool{}
	out := make([]booking.BookingSegmentRequest, 0, len(segments))
	for _, segment := range segments {
		id := strings.TrimSpace(segment.ServiceID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, segment)
	}
	return out
}

func guestScopeForMessage(message string) string {
	normalized := normalizeLooseText(message)
	for _, phrase := range []string{"another guest", "another person", "someone else", "my friend", "my daughter", "my mom", "my mother", "my sister", "my partner", "my wife", "my husband"} {
		if containsLoosePhrase(normalized, phrase) || strings.Contains(normalized, phrase) {
			return ConversationGuestAnother
		}
	}
	for _, phrase := range []string{"for me", "same person", "both for me", "just me", "for myself"} {
		if containsLoosePhrase(normalized, phrase) || strings.Contains(normalized, phrase) {
			return ConversationGuestCaller
		}
	}
	return ""
}

func selectedSameCategoryServiceIDs(session Session, target ServiceOption, services []ServiceOption) []string {
	categoryID := strings.TrimSpace(target.CategoryID)
	categoryName := normalizeServiceText(target.CategoryName)
	if categoryID == "" && categoryName == "" {
		return nil
	}
	out := make([]string, 0)
	for _, selected := range selectedServiceOptions(session, services) {
		if categoryID != "" && strings.TrimSpace(selected.CategoryID) == categoryID {
			out = append(out, selected.ID)
			continue
		}
		if categoryName != "" && normalizeServiceText(selected.CategoryName) == categoryName {
			out = append(out, selected.ID)
		}
	}
	return uniqueStrings(out)
}

func isReviewAuthorization(message string) bool {
	return classifyReviewResponse(message, reviewResponseEvidence{}) == reviewResponseAccept
}

func classifyReviewResponse(message string, evidence reviewResponseEvidence) reviewResponseKind {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return reviewResponseAmbiguous
	}
	if evidence.HasDraftMutation || evidence.RequestsOtherAction || evidence.HasPartyMutation || hasReviewCorrectionEvidence(normalized) {
		return reviewResponseCorrection
	}
	if hasReviewRejectionEvidence(normalized) {
		return reviewResponseReject
	}
	if evidence.HasInformationQuestion || hasReviewInformationQuestion(message, normalized) {
		return reviewResponseAmbiguous
	}
	if hasNaturalReviewApproval(normalized) {
		return reviewResponseAccept
	}
	return reviewResponseAmbiguous
}

func classifyStateScopedReviewResponse(message string) reviewResponseKind {
	if isExactAffirmativeResponse(message) {
		return reviewResponseAccept
	}
	if isNegativeOnly(message) {
		return reviewResponseReject
	}
	return reviewResponseAmbiguous
}

func hasReviewCorrectionEvidence(normalized string) bool {
	for _, token := range strings.Fields(normalized) {
		switch token {
		case "but", "actually", "instead", "wait", "change", "switch", "replace", "remove", "add", "another", "different", "cancel", "reschedule", "move":
			return true
		}
	}
	return false
}

func hasReviewRejectionEvidence(normalized string) bool {
	for _, token := range strings.Fields(normalized) {
		switch token {
		case "no", "nope", "nah", "not", "never", "stop", "cannot":
			return true
		}
	}
	return containsAnyLoosePhrase(normalized, []string{"do not", "don t", "can t", "will not", "won t"})
}

func hasReviewInformationQuestion(message string, normalized string) bool {
	hasQuestionSignal := strings.Contains(message, "?") || containsAnyLoosePhrase(normalized, []string{
		"what", "which", "when", "where", "why", "how", "who",
		"can you tell", "could you tell", "i have a question", "price", "how much",
	})
	if !hasQuestionSignal {
		return false
	}
	return !isExplicitReviewBookingRequestQuestion(normalized)
}

func isExplicitReviewBookingRequestQuestion(normalized string) bool {
	return containsAnyLoosePhrase(normalized, []string{
		"can you book it", "can you book this", "can you book the appointment",
		"could you book it", "could you book this", "could you book the appointment",
		"would you book it", "would you book this", "will you book it", "will you book this",
		"can you schedule it", "could you schedule it", "can you confirm it", "could you confirm it",
	})
}

func hasNaturalReviewApproval(normalized string) bool {
	if normalized == "i would" || normalized == "i would like that" || normalized == "i do" ||
		normalized == "please do" || normalized == "do it" || normalized == "sounds good" ||
		normalized == "that works" || normalized == "everything looks good" {
		return true
	}
	remainder, hasPositivePrefix := trimReviewPositivePrefix(normalized)
	if hasPositivePrefix {
		if remainder == "" || remainder == "please" || remainder == "of course" || remainder == "i would" ||
			remainder == "i would like that" || remainder == "i do" || remainder == "sounds good" || remainder == "that works" {
			return true
		}
		return hasReviewBookingDirective(remainder)
	}
	return hasReviewBookingDirective(normalized)
}

func trimReviewPositivePrefix(normalized string) (string, bool) {
	for _, prefix := range []string{"of course", "absolutely", "certainly", "yes", "yeah", "yep", "sure", "okay", "ok", "correct", "right"} {
		if normalized == prefix {
			return "", true
		}
		if strings.HasPrefix(normalized, prefix+" ") {
			return strings.TrimSpace(strings.TrimPrefix(normalized, prefix)), true
		}
	}
	return normalized, false
}

func hasReviewBookingDirective(normalized string) bool {
	return containsAnyLoosePhrase(normalized, []string{
		"book it", "book this", "book that", "book the appointment",
		"confirm it", "confirm this", "confirm that", "confirm the appointment",
		"make the appointment", "schedule it", "schedule this", "schedule that", "schedule the appointment",
		"go ahead", "please proceed", "proceed", "please do", "do it", "do so",
	})
}

func selectedServiceOptions(session Session, services []ServiceOption) []ServiceOption {
	return servicesByIDs(services, selectedServiceIDs(session))
}

func conversationServiceRefs(services []ServiceOption) []ConversationServiceRef {
	out := make([]ConversationServiceRef, 0, len(services))
	for _, service := range services {
		ref := ConversationServiceRef{ServiceID: service.ID, ServiceName: service.Name, CategoryID: service.CategoryID, CategoryName: service.CategoryName}
		out = append(out, ref)
	}
	return out
}

func conversationServiceAliasRefs(aliases []ServiceAlias, services []ServiceOption) []ConversationServiceAliasRef {
	allowed := stringSet(serviceOptionIDs(services))
	out := make([]ConversationServiceAliasRef, 0, len(aliases))
	seen := map[string]bool{}
	for _, alias := range aliases {
		serviceID := strings.TrimSpace(alias.ServiceID)
		phrase := strings.TrimSpace(alias.Alias)
		key := serviceID + "\x00" + normalizeServiceText(phrase)
		if !allowed[serviceID] || phrase == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ConversationServiceAliasRef{ServiceID: serviceID, Alias: phrase})
	}
	return out
}

func conversationCategoryRefs(services []ServiceOption, aliases []ServiceCategoryAlias) []ConversationCategoryRef {
	out := make([]ConversationCategoryRef, 0)
	index := map[string]int{}
	for _, service := range services {
		categoryID := strings.TrimSpace(service.CategoryID)
		if categoryID == "" {
			continue
		}
		position, ok := index[categoryID]
		if !ok {
			position = len(out)
			index[categoryID] = position
			out = append(out, ConversationCategoryRef{
				CategoryID: categoryID, CategoryName: strings.TrimSpace(service.CategoryName),
			})
		}
		out[position].ServiceIDs = uniqueStrings(append(out[position].ServiceIDs, strings.TrimSpace(service.ID)))
	}
	for _, alias := range aliases {
		position, ok := index[strings.TrimSpace(alias.CategoryID)]
		phrase := strings.TrimSpace(alias.Alias)
		if !ok || phrase == "" {
			continue
		}
		out[position].Aliases = uniqueStrings(append(out[position].Aliases, phrase))
	}
	return out
}

func selectedStaffOptions(session Session, staff []StaffOption) []StaffOption {
	staffID := strings.TrimSpace(session.StaffID)
	if staffID == "" {
		return nil
	}
	for _, option := range staff {
		if strings.TrimSpace(option.ID) == staffID {
			return []StaffOption{option}
		}
	}
	return nil
}

func conversationStaffRefs(staff []StaffOption) []ConversationStaffRef {
	out := make([]ConversationStaffRef, 0, len(staff))
	for _, option := range staff {
		out = append(out, ConversationStaffRef{StaffID: option.ID, StaffName: option.Name})
	}
	return out
}

func conversationDraftRef(session Session) ConversationDraftRef {
	state := normalizedDialogState(session.DialogState)
	requestedStart := ""
	if session.RequestedStartTime != nil {
		requestedStart = session.RequestedStartTime.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	partySize := 0
	partyGroups := []ConversationPartyGroupRef(nil)
	if session.PartyPlan != nil {
		partySize = session.PartyPlan.PartySize
		for _, group := range session.PartyPlan.Groups {
			partyGroups = append(partyGroups, ConversationPartyGroupRef{
				GuestRef: strings.TrimSpace(group.Label), Count: group.Count,
				ServiceIDs: append([]string(nil), group.ResolvedServiceIDs...),
			})
		}
	}
	return ConversationDraftRef{
		ServiceIDs:        selectedServiceIDs(session),
		StaffID:           strings.TrimSpace(session.StaffID),
		RequestedDate:     strings.TrimSpace(session.RequestedDate),
		RequestedStartISO: requestedStart,
		PartySize:         partySize,
		PartyGroups:       partyGroups,
		HasCustomerName:   strings.TrimSpace(session.CustomerName) != "",
		HasCustomerPhone:  strings.TrimSpace(session.CustomerPhone) != "",
		DraftRevision:     state.DraftRevision,
	}
}

func clonePendingConversationAct(pending *PendingConversationAct) *PendingConversationAct {
	if pending == nil {
		return nil
	}
	cloned := *pending
	cloned.SourceServiceIDs = append([]string(nil), pending.SourceServiceIDs...)
	cloned.TargetServiceIDs = append([]string(nil), pending.TargetServiceIDs...)
	return &cloned
}

func validateInterpretedConversationAct(act ConversationAct, session Session, services []ServiceOption) (ConversationAct, bool) {
	allowed := map[string]bool{
		ConversationActAdd: true, ConversationActReplace: true, ConversationActRemove: true,
		ConversationActUndo: true, ConversationActSummarize: true, ConversationActReview: true,
		ConversationActSet: true, ConversationActClear: true,
	}
	if !allowed[act.Kind] || act.Confidence < 0.78 {
		return ConversationAct{}, false
	}
	if act.Entity == "" {
		act.Entity = defaultConversationActEntity(act.Kind)
	}
	if act.Kind == ConversationActSet && act.Entity == ConversationEntityStaff && strings.TrimSpace(act.Subject) == "alternative" {
		// The alternative-staff protocol intentionally carries no selection.
		// The reducer derives choices from the current salon staff catalog, so
		// model-authored source/category/guest decoration is non-authoritative.
		act.SourceServiceIDs = nil
		act.SourceCategoryID = ""
		act.SourceCategoryName = ""
		act.TargetCategoryID = ""
		act.TargetCategoryName = ""
		act.Scope = ""
		act.GuestScope = ""
		act.GuestRef = ""
		act.Value = ""
		act.Count = 0
	}
	if isServiceMutationAct(act.Kind) && act.Entity != ConversationEntityService {
		return ConversationAct{}, false
	}
	if act.Kind == ConversationActSet || act.Kind == ConversationActClear {
		allowedEntity := map[string]bool{
			ConversationEntityStaff: true, ConversationEntityDateTime: true,
			ConversationEntityGuest: true, ConversationEntityCustomer: true,
		}
		if !allowedEntity[act.Entity] {
			return ConversationAct{}, false
		}
		if act.Entity == ConversationEntityCustomer {
			subject := strings.TrimSpace(act.Subject)
			if subject != "name" && subject != "phone" && subject != "email" {
				return ConversationAct{}, false
			}
			if act.Kind == ConversationActSet && strings.TrimSpace(act.Value) == "" {
				return ConversationAct{}, false
			}
		}
		if act.Entity == ConversationEntityStaff && strings.TrimSpace(act.Subject) == "alternative" && len(act.TargetServiceIDs) != 0 {
			return ConversationAct{}, false
		}
	}
	validCatalog := stringSet(serviceOptionIDs(services))
	validCategories := map[string]bool{}
	for _, service := range services {
		if categoryID := strings.TrimSpace(service.CategoryID); categoryID != "" {
			validCategories[categoryID] = true
		}
	}
	if act.Entity == ConversationEntityService || isServiceMutationAct(act.Kind) {
		for _, id := range append(append([]string(nil), act.SourceServiceIDs...), act.TargetServiceIDs...) {
			if !validCatalog[strings.TrimSpace(id)] {
				return ConversationAct{}, false
			}
		}
		for _, categoryID := range []string{act.SourceCategoryID, act.TargetCategoryID} {
			categoryID = strings.TrimSpace(categoryID)
			if categoryID != "" && !validCategories[categoryID] {
				return ConversationAct{}, false
			}
		}
	}
	if act.Kind == ConversationActReview && normalizedDialogState(session.DialogState).Phase != DialogPhaseReview {
		return ConversationAct{}, false
	}
	if act.GuestScope != "" && act.GuestScope != ConversationGuestCaller && act.GuestScope != ConversationGuestAnother {
		return ConversationAct{}, false
	}
	if act.Count < 0 || act.Count > 20 {
		return ConversationAct{}, false
	}
	return act, true
}

func validateTurnUnderstanding(turn TurnUnderstanding, session Session, services []ServiceOption, staff []StaffOption) (TurnUnderstanding, bool) {
	if turn.Confidence > 0 && turn.Confidence < 0.78 {
		return TurnUnderstanding{}, false
	}
	allowedGoals := map[string]bool{
		"": true, "unknown": true, "book_appointment": true, "reschedule_appointment": true,
		"cancel_appointment": true, "consultation": true, "information": true, "human_handoff": true,
	}
	if !allowedGoals[strings.TrimSpace(turn.Goal)] {
		return TurnUnderstanding{}, false
	}
	validatedActs := make([]ConversationAct, 0, len(turn.Acts))
	validStaff := map[string]bool{}
	for _, option := range staff {
		if id := strings.TrimSpace(option.ID); id != "" {
			validStaff[id] = true
		}
	}
	for _, act := range turn.Acts {
		if act.Kind == ConversationActUnknown {
			continue
		}
		validated, ok := validateInterpretedConversationAct(act, session, services)
		if !ok {
			return TurnUnderstanding{}, false
		}
		if activePartyPlan(session.PartyPlan) && isServiceMutationAct(validated.Kind) {
			if !partyGuestRefExists(session.PartyPlan, validated.GuestRef) {
				return TurnUnderstanding{}, false
			}
			// The exact, state-owned guest reference is authoritative once a
			// structured party plan exists. Remove the redundant broad scope so
			// contradictory model labels cannot affect downstream interpretation.
			validated.GuestScope = ""
		}
		for _, staffID := range append(append([]string(nil), validated.SourceServiceIDs...), validated.TargetServiceIDs...) {
			if validated.Entity == ConversationEntityStaff && !validStaff[strings.TrimSpace(staffID)] {
				return TurnUnderstanding{}, false
			}
		}
		validatedActs = append(validatedActs, validated)
	}
	validServices := stringSet(serviceOptionIDs(services))
	allowedQuestions := map[string]bool{
		ConversationQuestionCurrentBooking: true,
		ConversationQuestionCatalog:        true,
		ConversationQuestionAvailability:   true,
		ConversationQuestionPrice:          true,
		ConversationQuestionHours:          true,
		ConversationQuestionStaff:          true,
		ConversationQuestionPolicy:         true,
	}
	validatedQuestions := make([]ConversationQuestion, 0, len(turn.Questions))
	for _, question := range turn.Questions {
		question.Subject = strings.TrimSpace(question.Subject)
		question.Mode = strings.TrimSpace(question.Mode)
		if !allowedQuestions[question.Subject] || question.Confidence < 0.78 {
			return TurnUnderstanding{}, false
		}
		if question.Mode != "" && !validInformationQuestionMode(question.Mode) {
			return TurnUnderstanding{}, false
		}
		for _, serviceID := range question.ServiceIDs {
			if !validServices[strings.TrimSpace(serviceID)] {
				return TurnUnderstanding{}, false
			}
		}
		for _, staffID := range question.StaffIDs {
			if !validStaff[strings.TrimSpace(staffID)] {
				return TurnUnderstanding{}, false
			}
		}
		if question.TimePreference != nil {
			if question.Subject != ConversationQuestionAvailability {
				return TurnUnderstanding{}, false
			}
			if _, ok := normalizedSlotTimePreference(question.TimePreference); !ok {
				return TurnUnderstanding{}, false
			}
		}
		validatedQuestions = append(validatedQuestions, question)
	}
	consultation, consultationValid := validateConsultationNeedProfile(turn.Consultation, validServices)
	if !consultationValid {
		// Consultation extraction is auxiliary to the primary act/question/guidance
		// contract. A malformed or low-confidence snapshot must not erase a valid
		// booking action, information question, or guidance decision.
		consultation = ConsultationNeedProfile{}
		turn.InterpreterDiagnostics = mergeInterpreterDiagnostic(turn.InterpreterDiagnostics, "consultation_profile_dropped", "1")
	}
	currentConsultationNeeds := ConsultationNeedProfile{}
	if state := normalizedDialogState(session.DialogState); consultationStateActive(state.Consultation) {
		currentConsultationNeeds = state.Consultation.Needs
	}
	mutations, droppedMutations := validateConsultationNeedMutationsPartial(turn.ConsultationMutations, currentConsultationNeeds, validServices)
	if droppedMutations > 0 {
		turn.InterpreterDiagnostics = mergeInterpreterDiagnostic(turn.InterpreterDiagnostics, "consultation_mutations_dropped", strconv.Itoa(droppedMutations))
	}
	safety, ok := validateSafetyAssessment(turn.Safety)
	if !ok {
		return TurnUnderstanding{}, false
	}
	turn.Acts = validatedActs
	turn.Questions = validatedQuestions
	turn.Consultation = consultation
	turn.ConsultationMutations = mutations
	turn.Safety = safety
	if len(turn.Acts) == 0 && len(turn.Questions) == 0 {
		return turn, true
	}
	return turn, true
}

func validInformationQuestionMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case ConversationQuestionModeList, ConversationQuestionModeCount, ConversationQuestionModeExistence,
		ConversationQuestionModeDetails, ConversationQuestionModeCompare:
		return true
	default:
		return false
	}
}

func validateConsultationNeedProfile(profile ConsultationNeedProfile, validServices map[string]bool) (ConsultationNeedProfile, bool) {
	profile.CurrentSystem = strings.TrimSpace(profile.CurrentSystem)
	profile.DesiredOutcome = strings.TrimSpace(profile.DesiredOutcome)
	profile.LengthChange = strings.TrimSpace(profile.LengthChange)
	profile.Reason = strings.TrimSpace(profile.Reason)
	// Model-level "unknown" means the caller did not supply the field. It is
	// never persisted as a consultation preference and must not make an
	// otherwise empty auxiliary snapshot meaningful.
	if profile.CurrentSystem == ConsultationSystemUnknown {
		profile.CurrentSystem = ""
	}
	if profile.DesiredOutcome == ConsultationOutcomeUnknown {
		profile.DesiredOutcome = ""
	}
	if profile.LengthChange == ConsultationLengthUnknown {
		profile.LengthChange = ""
	}
	if !allowedConsultationValue(profile.CurrentSystem, "", ConsultationSystemNatural, ConsultationSystemRegularPolish, ConsultationSystemGel, ConsultationSystemDip, ConsultationSystemAcrylic, ConsultationSystemExtension, ConsultationSystemUnknown) {
		return ConsultationNeedProfile{}, false
	}
	if !allowedConsultationValue(profile.DesiredOutcome, "", ConsultationOutcomeMaintain, ConsultationOutcomeShorten, ConsultationOutcomeAddLength, ConsultationOutcomeAddStrength, ConsultationOutcomeRepair, ConsultationOutcomeRemoval, ConsultationOutcomeColorRefresh, ConsultationOutcomeCompare, ConsultationOutcomeUnknown) {
		return ConsultationNeedProfile{}, false
	}
	if !allowedConsultationValue(profile.LengthChange, "", ConsultationLengthKeep, ConsultationLengthShorten, ConsultationLengthAddLength, ConsultationLengthUnknown) {
		return ConsultationNeedProfile{}, false
	}
	priorities := make([]string, 0, len(profile.Priorities))
	seenPriorities := map[string]bool{}
	for _, priority := range profile.Priorities {
		priority = strings.TrimSpace(priority)
		if !allowedConsultationValue(priority, ConsultationPriorityDurability, ConsultationPriorityLowerMaintenance, ConsultationPriorityLowerCost, ConsultationPriorityShorterVisit) {
			return ConsultationNeedProfile{}, false
		}
		if !seenPriorities[priority] {
			seenPriorities[priority] = true
			priorities = append(priorities, priority)
		}
	}
	finishes := make([]string, 0, len(profile.DesiredFinishes))
	seenFinishes := map[string]bool{}
	for _, finish := range profile.DesiredFinishes {
		finish = strings.TrimSpace(finish)
		if !allowedConsultationValue(finish, ConsultationFinishNatural, ConsultationFinishRegularPolish, ConsultationFinishGelPolish, ConsultationFinishGlossy, ConsultationFinishMatte, ConsultationFinishNailArt) {
			return ConsultationNeedProfile{}, false
		}
		if !seenFinishes[finish] {
			seenFinishes[finish] = true
			finishes = append(finishes, finish)
		}
	}
	compared := make([]string, 0, len(profile.ComparedServiceIDs))
	seenServices := map[string]bool{}
	for _, id := range profile.ComparedServiceIDs {
		id = strings.TrimSpace(id)
		if id == "" || !validServices[id] {
			return ConsultationNeedProfile{}, false
		}
		if !seenServices[id] {
			seenServices[id] = true
			compared = append(compared, id)
		}
	}
	meaningful := profile.CurrentSystem != "" || profile.DesiredOutcome != "" || profile.LengthChange != "" || len(priorities) > 0 || len(finishes) > 0 || len(compared) > 0 || profile.BookingRequested || profile.ConversationComplete
	if meaningful && profile.Confidence < 0.78 {
		return ConsultationNeedProfile{}, false
	}
	profile.Priorities = priorities
	profile.DesiredFinishes = finishes
	profile.ComparedServiceIDs = compared
	return profile, true
}

func allowedConsultationValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateConsultationNeedMutations(mutations []ConsultationNeedMutation, current ConsultationNeedProfile, validServices map[string]bool) ([]ConsultationNeedMutation, bool) {
	if len(mutations) > 16 {
		return nil, false
	}
	validated := make([]ConsultationNeedMutation, 0, len(mutations))
	for _, mutation := range mutations {
		mutation.Field = strings.TrimSpace(mutation.Field)
		mutation.Operation = strings.TrimSpace(mutation.Operation)
		mutation.Reason = strings.TrimSpace(mutation.Reason)
		if mutation.Confidence < 0.78 || !allowedConsultationValue(mutation.Operation,
			ConsultationNeedOperationSet, ConsultationNeedOperationReplace, ConsultationNeedOperationAdd,
			ConsultationNeedOperationRemove, ConsultationNeedOperationClear) {
			return nil, false
		}

		isScalar := mutation.Field == ConsultationNeedFieldCurrentSystem || mutation.Field == ConsultationNeedFieldDesiredOutcome || mutation.Field == ConsultationNeedFieldLengthChange
		isList := mutation.Field == ConsultationNeedFieldPriorities || mutation.Field == ConsultationNeedFieldDesiredFinishes || mutation.Field == ConsultationNeedFieldComparedServiceIDs
		if !isScalar && !isList {
			return nil, false
		}
		if mutation.Operation == ConsultationNeedOperationClear {
			if len(mutation.Values) != 0 {
				return nil, false
			}
			if !consultationNeedMutationValidForState(current, mutation) {
				return nil, false
			}
			validated = append(validated, mutation)
			applyConsultationNeedMutation(&current, mutation)
			continue
		}
		if isScalar && mutation.Operation != ConsultationNeedOperationSet && mutation.Operation != ConsultationNeedOperationReplace {
			return nil, false
		}
		if len(mutation.Values) == 0 || (isScalar && len(mutation.Values) != 1) {
			return nil, false
		}
		values := make([]string, 0, len(mutation.Values))
		seen := map[string]bool{}
		for _, value := range mutation.Values {
			value = strings.TrimSpace(value)
			if value == "" || !validConsultationMutationValue(mutation.Field, value, validServices) {
				return nil, false
			}
			if seen[value] {
				continue
			}
			seen[value] = true
			values = append(values, value)
		}
		mutation.Values = values
		if !consultationNeedMutationValidForState(current, mutation) {
			return nil, false
		}
		validated = append(validated, mutation)
		applyConsultationNeedMutation(&current, mutation)
	}
	return validated, true
}

// validateConsultationNeedMutationsPartial preserves every state-valid,
// catalog-grounded mutation in spoken order while dropping malformed and no-op
// auxiliary mutations. Primary turn meaning is validated independently by
// validateTurnUnderstanding.
func validateConsultationNeedMutationsPartial(mutations []ConsultationNeedMutation, current ConsultationNeedProfile, validServices map[string]bool) ([]ConsultationNeedMutation, int) {
	if len(mutations) > 16 {
		return nil, len(mutations)
	}
	validated := make([]ConsultationNeedMutation, 0, len(mutations))
	dropped := 0
	for _, mutation := range mutations {
		candidate, ok := validateConsultationNeedMutations([]ConsultationNeedMutation{mutation}, current, validServices)
		if !ok || len(candidate) != 1 {
			dropped++
			continue
		}
		validated = append(validated, candidate[0])
		applyConsultationNeedMutation(&current, candidate[0])
	}
	return validated, dropped
}

func mergeInterpreterDiagnostic(diagnostics map[string]string, key string, value string) map[string]string {
	if diagnostics == nil {
		diagnostics = map[string]string{}
	}
	diagnostics[strings.TrimSpace(key)] = strings.TrimSpace(value)
	return diagnostics
}

func consultationNeedMutationValidForState(current ConsultationNeedProfile, mutation ConsultationNeedMutation) bool {
	if mutation.Operation == ConsultationNeedOperationClear {
		return consultationNeedFieldHasValue(current, mutation.Field)
	}
	if mutation.Field == ConsultationNeedFieldCurrentSystem || mutation.Field == ConsultationNeedFieldDesiredOutcome || mutation.Field == ConsultationNeedFieldLengthChange {
		currentValue := consultationScalarNeedValue(current, mutation.Field)
		if len(mutation.Values) != 1 {
			return false
		}
		switch mutation.Operation {
		case ConsultationNeedOperationSet:
			return currentValue == ""
		case ConsultationNeedOperationReplace:
			return currentValue != "" && currentValue != mutation.Values[0]
		default:
			return false
		}
	}

	currentValues := consultationListNeedValues(current, mutation.Field)
	switch mutation.Operation {
	case ConsultationNeedOperationSet:
		return len(currentValues) == 0
	case ConsultationNeedOperationReplace:
		return len(currentValues) > 0 && !sameConsultationValueSet(currentValues, mutation.Values)
	case ConsultationNeedOperationAdd:
		present := stringSet(currentValues)
		for _, value := range mutation.Values {
			if !present[value] {
				return true
			}
		}
		return false
	case ConsultationNeedOperationRemove:
		present := stringSet(currentValues)
		for _, value := range mutation.Values {
			if !present[value] {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func consultationNeedFieldHasValue(current ConsultationNeedProfile, field string) bool {
	if field == ConsultationNeedFieldCurrentSystem || field == ConsultationNeedFieldDesiredOutcome || field == ConsultationNeedFieldLengthChange {
		return consultationScalarNeedValue(current, field) != ""
	}
	return len(consultationListNeedValues(current, field)) > 0
}

func consultationScalarNeedValue(current ConsultationNeedProfile, field string) string {
	switch field {
	case ConsultationNeedFieldCurrentSystem:
		return strings.TrimSpace(current.CurrentSystem)
	case ConsultationNeedFieldDesiredOutcome:
		return strings.TrimSpace(current.DesiredOutcome)
	case ConsultationNeedFieldLengthChange:
		return strings.TrimSpace(current.LengthChange)
	default:
		return ""
	}
}

func consultationListNeedValues(current ConsultationNeedProfile, field string) []string {
	switch field {
	case ConsultationNeedFieldPriorities:
		return current.Priorities
	case ConsultationNeedFieldDesiredFinishes:
		return current.DesiredFinishes
	case ConsultationNeedFieldComparedServiceIDs:
		return current.ComparedServiceIDs
	default:
		return nil
	}
}

func sameConsultationValueSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	rightSet := stringSet(right)
	for _, value := range left {
		if !rightSet[strings.TrimSpace(value)] {
			return false
		}
	}
	return true
}

func validConsultationMutationValue(field string, value string, validServices map[string]bool) bool {
	switch field {
	case ConsultationNeedFieldCurrentSystem:
		return allowedConsultationValue(value, ConsultationSystemNatural, ConsultationSystemRegularPolish, ConsultationSystemGel, ConsultationSystemDip, ConsultationSystemAcrylic, ConsultationSystemExtension)
	case ConsultationNeedFieldDesiredOutcome:
		return allowedConsultationValue(value, ConsultationOutcomeMaintain, ConsultationOutcomeShorten, ConsultationOutcomeAddLength, ConsultationOutcomeAddStrength, ConsultationOutcomeRepair, ConsultationOutcomeRemoval, ConsultationOutcomeColorRefresh, ConsultationOutcomeCompare)
	case ConsultationNeedFieldLengthChange:
		return allowedConsultationValue(value, ConsultationLengthKeep, ConsultationLengthShorten, ConsultationLengthAddLength)
	case ConsultationNeedFieldPriorities:
		return allowedConsultationValue(value, ConsultationPriorityDurability, ConsultationPriorityLowerMaintenance, ConsultationPriorityLowerCost, ConsultationPriorityShorterVisit)
	case ConsultationNeedFieldDesiredFinishes:
		return allowedConsultationValue(value, ConsultationFinishNatural, ConsultationFinishRegularPolish, ConsultationFinishGelPolish, ConsultationFinishGlossy, ConsultationFinishMatte, ConsultationFinishNailArt)
	case ConsultationNeedFieldComparedServiceIDs:
		return validServices[value]
	default:
		return false
	}
}

func validateSafetyAssessment(safety SafetyAssessment) (SafetyAssessment, bool) {
	safety.Category = strings.TrimSpace(safety.Category)
	safety.Reason = strings.TrimSpace(safety.Reason)
	if !safety.Concern {
		safety.Category = ""
		return safety, true
	}
	if safety.Confidence < 0.78 || !allowedConsultationValue(safety.Category,
		SafetyCategoryPain, SafetyCategoryInjury, SafetyCategoryInfection, SafetyCategoryAllergy,
		SafetyCategoryBleeding, SafetyCategorySwelling, SafetyCategoryMedicalSuitability, SafetyCategoryOtherHealth) {
		return SafetyAssessment{}, false
	}
	return safety, true
}

func partyGuestRefExists(plan *PartyPlan, guestRef string) bool {
	guestRef = strings.TrimSpace(guestRef)
	if plan == nil || guestRef == "" {
		return false
	}
	for _, group := range plan.Groups {
		if strings.EqualFold(strings.TrimSpace(group.Label), guestRef) {
			return true
		}
	}
	return false
}

func defaultConversationActEntity(kind string) string {
	if isServiceMutationAct(kind) {
		return ConversationEntityService
	}
	return ""
}

func isServiceMutationAct(kind string) bool {
	switch kind {
	case ConversationActAdd, ConversationActReplace, ConversationActRemove, ConversationActUndo:
		return true
	default:
		return false
	}
}

func intersectServiceIDs(left []string, right []string) []string {
	rightSet := stringSet(right)
	out := make([]string, 0, len(left))
	for _, id := range left {
		id = strings.TrimSpace(id)
		if id != "" && rightSet[id] {
			out = append(out, id)
		}
	}
	return uniqueStrings(out)
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = true
		}
	}
	return out
}

func serviceOptionIDs(services []ServiceOption) []string {
	out := make([]string, 0, len(services))
	for _, service := range services {
		if id := strings.TrimSpace(service.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func applyConversationActMetadata(turn *TurnRecord, act ConversationAct, result conversationDraftResult) {
	if turn == nil || act.Kind == ConversationActUnknown {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"conversation_act_kind":                 act.Kind,
		"conversation_act_source":               act.Source,
		"conversation_act_reason":               act.Reason,
		"conversation_act_confidence":           act.Confidence,
		"conversation_act_scope":                act.Scope,
		"conversation_act_guest_scope":          act.GuestScope,
		"conversation_act_guest_ref":            act.GuestRef,
		"conversation_act_subject":              act.Subject,
		"conversation_act_source_service_ids":   append([]string(nil), act.SourceServiceIDs...),
		"conversation_act_target_service_ids":   append([]string(nil), act.TargetServiceIDs...),
		"conversation_act_source_category_id":   act.SourceCategoryID,
		"conversation_act_source_category_name": act.SourceCategoryName,
		"conversation_act_target_category_id":   act.TargetCategoryID,
		"conversation_act_target_category_name": act.TargetCategoryName,
	})
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"dialog_state_version":       turn.Update.DialogState.Version,
		"dialog_phase":               turn.Update.DialogState.Phase,
		"dialog_no_progress_count":   turn.Update.DialogState.NoProgressCount,
		"dialog_review_accepted":     turn.Update.DialogState.ReviewAccepted,
		"conversation_act_changed":   result.Changed,
		"conversation_act_handled":   result.Handled,
		"conversation_act_clarified": result.Clarification,
		"conversation_act_escalated": result.Escalate,
	})
}

func prependConversationMutationAcknowledgement(turn *TurnRecord, result conversationDraftResult, session Session, services []ServiceOption) {
	if turn == nil || strings.TrimSpace(turn.AIMessage) == "" {
		return
	}
	staffChanged := strings.TrimSpace(turn.Session.StaffID) != strings.TrimSpace(turn.Update.StaffID)
	if !result.Changed && !staffChanged {
		return
	}
	acknowledgement := ""
	if staffChanged || result.Act.Entity == ConversationEntityStaff {
		if strings.TrimSpace(turn.Update.StaffID) == "" {
			acknowledgement = "Okay, I'll use any available technician."
		} else if name := strings.TrimSpace(session.StaffName); name != "" {
			acknowledgement = "Okay, I changed the technician to " + name + "."
		}
		if acknowledgement != "" {
			turn.AIMessage = acknowledgement + " " + turn.AIMessage
			return
		}
	}
	summary := strings.TrimSpace(serviceSummary(session, services))
	if activePartyPlan(session.PartyPlan) && strings.TrimSpace(result.Act.GuestRef) != "" {
		target := partyServiceTargetName(result.Act.TargetServiceIDs, services)
		scope := partyGroupSpokenScope(session.PartyPlan, result.Act.GuestRef)
		switch result.Act.Kind {
		case ConversationActAdd:
			acknowledgement = "Okay, I added " + target + " for " + scope + "."
		case ConversationActReplace:
			acknowledgement = "Okay, I changed the service for " + scope + " to " + target + "."
		}
		if acknowledgement != "" {
			turn.AIMessage = acknowledgement + " " + turn.AIMessage
			return
		}
	}
	switch result.Act.Kind {
	case ConversationActAdd:
		if summary != "" {
			acknowledgement = "Okay, I added " + summary + "."
		} else {
			acknowledgement = "Okay, I added that service."
		}
	case ConversationActReplace:
		if summary != "" {
			acknowledgement = "Okay, I changed it to " + summary + "."
		} else {
			acknowledgement = "Okay, I changed your service selection."
		}
	case ConversationActRemove:
		acknowledgement = "Okay, I removed that service."
	case ConversationActUndo:
		acknowledgement = "Okay, I restored your previous service selection."
	}
	if acknowledgement == "" {
		return
	}
	if summary != "" && result.Act.Kind != ConversationActAdd && result.Act.Kind != ConversationActReplace {
		acknowledgement += " You now have " + summary + "."
	}
	turn.AIMessage = acknowledgement + " " + turn.AIMessage
}

func finalBookingReviewPrompt(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	parts := make([]string, 0, 5)
	if service := serviceSummary(session, services); service != "" {
		parts = append(parts, service)
	}
	if session.RequestedStartTime != nil {
		loc := timezoneLocation(timezoneFromConfig(cfg))
		parts = append(parts, session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM"))
	}
	if sessionUsesAnyone(session) {
		parts = append(parts, "any available technician")
	} else if member := sessionAssignedStaffLabel(session, staff); member != "" {
		parts = append(parts, "with "+member)
	}
	if name := strings.TrimSpace(session.CustomerName); name != "" {
		parts = append(parts, "for "+name)
	}
	if phone := strings.TrimSpace(session.CustomerPhone); len(phone) >= 4 {
		parts = append(parts, "phone ending in "+phone[len(phone)-4:])
	}
	if len(parts) == 0 {
		return "Before I book it, would you like me to proceed with the appointment details we reviewed?"
	}
	return "Let me review everything: " + strings.Join(parts, ", ") + ". Would you like me to book it?"
}
