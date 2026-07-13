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

const semanticTurnTimeout = 2500 * time.Millisecond

type conversationDraftResult struct {
	Handled       bool
	Changed       bool
	Clarification bool
	Escalate      bool
	Reply         string
	ReplySource   string
	Act           ConversationAct
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
	fallback := plan.Understanding
	if fallback.Source == "" {
		fallback.Source = "turn_kernel"
	}
	if fallback.Reason == "" {
		fallback.Reason = plan.Reason
	}
	if s.turnInterpreter == nil {
		fallback.InterpreterOutcome = "interpreter_absent"
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathInterpreterAbsent, map[string]string{
			"turn_interpreter_outcome": fallback.InterpreterOutcome,
		})
		return fallback
	}
	semanticServices := plan.SemanticServices
	semanticStaff := plan.SemanticStaff
	interpretCtx, cancel := context.WithTimeout(ctx, semanticTurnTimeout)
	defer cancel()
	interpreted, err := s.turnInterpreter.InterpretTurn(interpretCtx, TurnInterpretationRequest{
		SalonID:             session.SalonID,
		SessionID:           session.ID,
		Channel:             session.Channel,
		CustomerMessage:     sanitizedTurnInterpreterMessage(message, session),
		ExpectedInput:       plan.ExpectedInput,
		SelectedServices:    conversationServiceRefs(selectedServiceOptions(session, services)),
		CatalogServices:     conversationServiceRefs(semanticServices),
		SelectedStaff:       conversationStaffRefs(selectedStaffOptions(session, staff)),
		CatalogStaff:        conversationStaffRefs(semanticStaff),
		Pending:             clonePendingConversationAct(session.DialogState.Pending),
		CurrentBookingStage: normalizedDialogState(session.DialogState).Phase,
		BookingAction:       bookingActionForSession(session),
		CurrentDraft:        conversationDraftRef(session),
	})
	fallback.ModelInvoked = true
	if err != nil {
		fallback.InterpreterOutcome = turnInterpreterErrorOutcome(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(interpretCtx.Err(), context.DeadlineExceeded) {
			fallback.InterpreterOutcome = TurnInterpreterOutcomeTimeout
		}
		fallback.Reason = "semantic_interpreter_" + fallback.InterpreterOutcome
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, map[string]string{
			"turn_interpreter_outcome": fallback.InterpreterOutcome,
			"turn_model_service_count": strconv.Itoa(len(semanticServices)),
			"turn_model_staff_count":   strconv.Itoa(len(semanticStaff)),
		})
		return fallback
	}
	interpreted.ModelInvoked = true
	if validated, ok := validateTurnUnderstanding(interpreted, session, semanticServices, semanticStaff); ok {
		validated.Source = "structured_ai"
		validated.InterpreterOutcome = TurnInterpreterOutcomeAccepted
		for index := range validated.Acts {
			validated.Acts[index].Source = "structured_ai"
			if validated.Acts[index].Entity == "" {
				validated.Acts[index].Entity = defaultConversationActEntity(validated.Acts[index].Kind)
			}
		}
		if len(validated.Acts) == 0 && len(validated.Questions) == 0 && (validated.Goal == "" || validated.Goal == "unknown") {
			fallback.InterpreterOutcome = "empty_understanding"
			fallback.Reason = "semantic_interpreter_empty_understanding"
			recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, map[string]string{
				"turn_interpreter_outcome": fallback.InterpreterOutcome,
			})
			return fallback
		}
		catalogUnderstanding := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases)
		if shouldUseCatalogServiceEditFallback(session, message, catalogUnderstanding) {
			recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathStructuredAI, map[string]string{
				"turn_interpreter_outcome": "catalog_fallback",
			})
			return TurnUnderstanding{
				Goal:               validated.Goal,
				Confidence:         validated.Confidence,
				Reason:             "catalog_backed_service_edit_fallback",
				Source:             "catalog_fallback",
				ModelInvoked:       true,
				CatalogFallback:    true,
				InterpreterOutcome: "catalog_fallback",
			}
		}
		validated = reconcileSemanticServiceTargets(validated, catalogUnderstanding)
		validated = reconcileDeterministicInformationQuestions(message, validated)
		recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathStructuredAI, map[string]string{
			"turn_interpreter_outcome": validated.InterpreterOutcome,
		})
		return validated
	}
	fallback.InterpreterOutcome = rejectedTurnUnderstandingOutcome(interpreted, semanticServices, semanticStaff)
	fallback.Reason = "semantic_interpretation_" + fallback.InterpreterOutcome
	recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, startedAt, TurnTimingPathProviderFallback, map[string]string{
		"turn_interpreter_outcome": fallback.InterpreterOutcome,
	})
	return fallback
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
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
	}
	state := normalizedDialogState(session.DialogState)
	if state.Phase == DialogPhaseReview && isReviewAuthorization(message) {
		return ConversationAct{Kind: ConversationActReview, Confidence: 1, Reason: "review_authorization", Source: "deterministic"}
	}
	if state.Pending != nil {
		if act := conversationActFromPending(session, message, services, aliases, categoryAliases, *state.Pending); act.Kind != ConversationActUnknown {
			return act
		}
	}

	return ConversationAct{Kind: ConversationActUnknown, Source: "deterministic"}
}

func conversationActFromPending(session Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, pending PendingConversationAct) ConversationAct {
	full := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases)
	switch pending.PromptKey {
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
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	tokens := strings.Fields(normalized)
	for _, token := range tokens {
		switch token {
		case "no", "not", "but", "actually", "instead", "wait", "change", "switch", "replace", "remove", "add", "another", "different", "cancel", "reschedule":
			return false
		}
	}
	allowed := map[string]bool{
		"yes": true, "yeah": true, "yep": true, "sure": true, "okay": true, "ok": true,
		"correct": true, "everything": true, "looks": true, "good": true, "right": true,
		"please": true, "just": true, "go": true, "ahead": true, "and": true, "do": true, "so": true,
		"book": true, "confirm": true, "make": true, "schedule": true,
		"it": true, "this": true, "that": true, "the": true, "appointment": true, "booking": true,
		"for": true, "me": true, "us": true, "now": true, "is": true,
	}
	hasAuthorization := false
	for _, token := range tokens {
		if !allowed[token] {
			return false
		}
		switch token {
		case "yes", "yeah", "yep", "sure", "okay", "ok", "correct", "book", "confirm", "make", "schedule", "good", "right":
			hasAuthorization = true
		}
	}
	if containsLoosePhrase(normalized, "go ahead") {
		hasAuthorization = true
	}
	return hasAuthorization
}

func selectedServiceOptions(session Session, services []ServiceOption) []ServiceOption {
	return servicesByIDs(services, selectedServiceIDs(session))
}

func conversationServiceRefs(services []ServiceOption) []ConversationServiceRef {
	out := make([]ConversationServiceRef, 0, len(services))
	for _, service := range services {
		out = append(out, ConversationServiceRef{ServiceID: service.ID, ServiceName: service.Name, CategoryID: service.CategoryID, CategoryName: service.CategoryName})
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
		if activePartyPlan(session.PartyPlan) && isServiceMutationAct(validated.Kind) && !partyGuestRefExists(session.PartyPlan, validated.GuestRef) {
			return TurnUnderstanding{}, false
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
		if !allowedQuestions[question.Subject] || question.Confidence < 0.78 {
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
		validatedQuestions = append(validatedQuestions, question)
	}
	turn.Acts = validatedActs
	turn.Questions = validatedQuestions
	if len(turn.Acts) == 0 && len(turn.Questions) == 0 {
		return turn, true
	}
	return turn, true
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
	if turn == nil || !result.Changed || strings.TrimSpace(turn.AIMessage) == "" {
		return
	}
	acknowledgement := ""
	summary := strings.TrimSpace(serviceSummary(session, services))
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
