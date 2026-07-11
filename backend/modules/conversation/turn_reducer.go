package conversation

import (
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func (s *Service) applyTurnUnderstandingToDraft(session *Session, turn TurnUnderstanding, services []ServiceOption, staff []StaffOption) conversationDraftResult {
	aggregate := conversationDraftResult{}
	for _, act := range turn.Acts {
		var result conversationDraftResult
		switch act.Entity {
		case ConversationEntityStaff:
			result = applyStaffConversationAct(session, act, staff)
		case ConversationEntityDateTime:
			result = applyDateTimeConversationAct(session, act)
		case ConversationEntityCustomer:
			result = applyCustomerConversationAct(session, act)
		case ConversationEntityGuest:
			result = applyGuestConversationAct(session, act)
		default:
			if session != nil && activePartyPlan(session.PartyPlan) && isServiceMutationAct(act.Kind) {
				result = applyPartyServiceConversationAct(session, act, services)
			} else {
				result = s.applyConversationActToDraft(session, act, services)
			}
		}
		aggregate = mergeConversationDraftResults(aggregate, result)
		if result.Clarification || result.Escalate {
			return aggregate
		}
	}
	for _, question := range turn.Questions {
		if question.Subject != ConversationQuestionCurrentBooking {
			continue
		}
		aggregate.Handled = true
		aggregate.Reply = currentBookingSummaryReply(*session, services, question.ServiceIDs...)
		state := normalizedDialogState(session.DialogState)
		if state.Pending != nil {
			aggregate.Reply += " " + pendingConversationPrompt(*session, services, state, false)
		}
		aggregate.ReplySource = "current_booking_summary"
	}
	return aggregate
}

func mergeConversationDraftResults(left conversationDraftResult, right conversationDraftResult) conversationDraftResult {
	left.Handled = left.Handled || right.Handled
	left.Changed = left.Changed || right.Changed
	left.Clarification = left.Clarification || right.Clarification
	left.Escalate = left.Escalate || right.Escalate
	if strings.TrimSpace(right.Reply) != "" {
		if strings.TrimSpace(left.Reply) != "" {
			left.Reply += " "
		}
		left.Reply += strings.TrimSpace(right.Reply)
	}
	if right.ReplySource != "" {
		left.ReplySource = right.ReplySource
	}
	if right.Act.Kind != "" {
		left.Act = right.Act
	}
	return left
}

func turnHasMutations(turn TurnUnderstanding) bool {
	for _, act := range turn.Acts {
		if act.Kind != ConversationActUnknown && act.Kind != ConversationActSummarize && act.Kind != ConversationActReview {
			return true
		}
	}
	return false
}

func turnIsStandaloneDraftSummary(turn TurnUnderstanding) bool {
	if turnHasMutations(turn) {
		return false
	}
	for _, act := range turn.Acts {
		if act.Kind == ConversationActSummarize {
			return true
		}
	}
	for _, question := range turn.Questions {
		if question.Subject == ConversationQuestionCurrentBooking {
			return true
		}
	}
	return false
}

func firstDeferredInformationQuestion(turn TurnUnderstanding) (ConversationQuestion, bool) {
	for _, question := range turn.Questions {
		switch question.Subject {
		case ConversationQuestionAvailability:
			continue
		default:
			return question, true
		}
	}
	return ConversationQuestion{}, false
}

func turnGoalIs(turn TurnUnderstanding, goal string) bool {
	return strings.TrimSpace(turn.Goal) == goal
}

func intentForTurnGoal(turn TurnUnderstanding, fallback string) string {
	switch strings.TrimSpace(turn.Goal) {
	case "book_appointment", "reschedule_appointment", "cancel_appointment":
		return IntentBooking
	case "consultation":
		return IntentConsultation
	case "human_handoff":
		return IntentHandoff
	default:
		return fallback
	}
}

func resumeBookingPrompt(session Session, services []ServiceOption, cfg *RuntimeConfig) string {
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil {
		return pendingConversationPrompt(session, services, state, false)
	}
	missing := missingBookingField(session)
	if missing == "" {
		return ""
	}
	if contextual := promptForMissingFieldWithServiceContext(missing, session, services, cfg); contextual != "" {
		return contextual
	}
	return promptForMissingField(missing)
}

func answerWithoutGenericBookingOffer(reply string) string {
	reply = strings.TrimSpace(reply)
	return strings.TrimSpace(strings.TrimSuffix(reply, "Would you like help with an appointment?"))
}

func semanticServiceEditFallback(session *Session, turn TurnUnderstanding, understanding serviceUnderstandingResult, services []ServiceOption) conversationDraftResult {
	if session == nil || !turn.ModelInvoked || turn.CatalogFallback || len(turn.Acts) > 0 || len(turn.Questions) > 0 || !hasBookingProgress(*session) {
		return conversationDraftResult{}
	}
	if goal := strings.TrimSpace(turn.Goal); goal != "" && goal != "unknown" && goal != "book_appointment" {
		return conversationDraftResult{}
	}
	state := normalizedDialogState(session.DialogState)
	candidates := understanding.Candidates
	if state.Pending != nil && state.Pending.PromptKey == "semantic_add_or_replace" {
		candidates = servicesByIDs(services, state.Pending.TargetServiceIDs)
	}
	if len(candidates) == 0 || (state.Pending == nil && sameServiceSelection(*session, candidates)) {
		return conversationDraftResult{}
	}
	promptKey := "semantic_add_or_replace"
	if state.LastPromptKey == promptKey {
		state.NoProgressCount++
	} else {
		state.NoProgressCount = 0
	}
	state.Phase = DialogPhaseClarifying
	state.ReviewAccepted = false
	state.ReviewedRevision = 0
	state.AuthorizedRevision = 0
	state.LastPromptKey = promptKey
	state.Pending = &PendingConversationAct{TargetServiceIDs: serviceOptionIDs(candidates), PromptKey: promptKey}
	session.DialogState = state
	if state.NoProgressCount >= 3 {
		return conversationDraftResult{
			Handled: true, Clarification: true, Escalate: true, ReplySource: "semantic_service_edit_handoff",
			Reply: "I’m sorry, I still can’t determine the service change safely. I’ll send this to the owner without changing your current appointment draft. This is not a confirmed appointment.",
		}
	}
	target := joinHumanList(serviceCandidateNames(candidates, len(candidates)))
	return conversationDraftResult{
		Handled: true, Clarification: true, ReplySource: "semantic_service_edit_safe_clarification",
		Reply: "I heard " + target + ", but I couldn’t safely tell how you want to change the draft. Would you like to add it, or replace one of your current services?",
	}
}

func applyTurnUnderstandingMetadata(turnRecord *TurnRecord, understanding TurnUnderstanding, result conversationDraftResult) {
	if turnRecord == nil {
		return
	}
	acts := make([]map[string]any, 0, len(understanding.Acts))
	for _, act := range understanding.Acts {
		acts = append(acts, map[string]any{
			"kind": act.Kind, "entity": act.Entity, "source_service_ids": append([]string(nil), act.SourceServiceIDs...),
			"target_service_ids": append([]string(nil), act.TargetServiceIDs...), "scope": act.Scope,
			"guest_scope": act.GuestScope, "guest_ref": act.GuestRef, "subject": act.Subject, "value_present": strings.TrimSpace(act.Value) != "", "count": act.Count,
			"confidence": act.Confidence, "reason": act.Reason,
		})
	}
	questions := make([]map[string]any, 0, len(understanding.Questions))
	for _, question := range understanding.Questions {
		questions = append(questions, map[string]any{
			"subject": question.Subject, "service_ids": append([]string(nil), question.ServiceIDs...),
			"staff_ids": append([]string(nil), question.StaffIDs...), "confidence": question.Confidence, "reason": question.Reason,
		})
	}
	turnRecord.CustomerMetadata = mergeMetadata(turnRecord.CustomerMetadata, map[string]any{
		"turn_understanding_source":        understanding.Source,
		"turn_understanding_goal":          understanding.Goal,
		"turn_understanding_confidence":    understanding.Confidence,
		"turn_understanding_reason":        understanding.Reason,
		"turn_understanding_model_invoked": understanding.ModelInvoked,
		"turn_understanding_acts":          acts,
		"turn_understanding_questions":     questions,
	})
	turnRecord.AIMetadata = mergeMetadata(turnRecord.AIMetadata, map[string]any{
		"draft_revision":      turnRecord.Update.DialogState.DraftRevision,
		"reviewed_revision":   turnRecord.Update.DialogState.ReviewedRevision,
		"authorized_revision": turnRecord.Update.DialogState.AuthorizedRevision,
		"turn_changed":        result.Changed,
		"turn_clarified":      result.Clarification,
	})
}

func applyStaffConversationAct(session *Session, act ConversationAct, staff []StaffOption) conversationDraftResult {
	result := conversationDraftResult{Handled: true, Act: act, ReplySource: "staff_draft_reducer"}
	if session == nil {
		return result
	}
	beforeID := strings.TrimSpace(session.StaffID)
	if act.Kind == ConversationActClear {
		session.StaffID = ""
		session.StaffName = ""
		session.StaffSelectionMode = booking.StaffSelectionAnyone
		clearBookingSegmentsStaffSelection(session)
	} else if act.Kind == ConversationActSet && len(act.TargetServiceIDs) == 1 {
		for _, option := range staff {
			if strings.TrimSpace(option.ID) != strings.TrimSpace(act.TargetServiceIDs[0]) {
				continue
			}
			session.StaffID = option.ID
			session.StaffName = option.Name
			session.StaffSelectionMode = booking.StaffSelectionSpecific
			applySpecificStaffToBookingSegments(session, option)
			break
		}
	}
	result.Changed = beforeID != strings.TrimSpace(session.StaffID)
	if result.Changed {
		session.OfferedSlots = nil
	}
	return result
}

func applyDateTimeConversationAct(session *Session, act ConversationAct) conversationDraftResult {
	result := conversationDraftResult{Handled: true, Act: act, ReplySource: "date_time_draft_reducer"}
	if session == nil || act.Kind != ConversationActClear {
		return result
	}
	beforeDate := session.RequestedDate
	beforeTime := session.RequestedStartTime
	switch strings.TrimSpace(act.Subject) {
	case "date":
		session.RequestedDate = ""
		session.RequestedStartTime = nil
	default:
		session.RequestedStartTime = nil
	}
	result.Changed = beforeDate != session.RequestedDate || beforeTime != session.RequestedStartTime
	if result.Changed {
		session.OfferedSlots = nil
	}
	return result
}

func applyCustomerConversationAct(session *Session, act ConversationAct) conversationDraftResult {
	result := conversationDraftResult{Handled: true, Act: act, ReplySource: "customer_draft_reducer"}
	if session == nil || act.Kind != ConversationActClear {
		return result
	}
	switch strings.TrimSpace(act.Subject) {
	case "name":
		result.Changed = session.CustomerName != ""
		session.CustomerName = ""
	case "phone":
		result.Changed = session.CustomerPhone != ""
		session.CustomerPhone = ""
	case "email":
		result.Changed = session.CustomerEmail != ""
		session.CustomerEmail = ""
	}
	return result
}

func applyGuestConversationAct(session *Session, act ConversationAct) conversationDraftResult {
	result := conversationDraftResult{Handled: true, Act: act, ReplySource: "party_plan_semantic_context"}
	if session == nil || act.Kind != ConversationActSet || act.Count <= 1 {
		return result
	}
	beforeSize := 0
	if session.PartyPlan != nil {
		beforeSize = session.PartyPlan.PartySize
	}
	if session.PartyPlan == nil {
		session.PartyPlan = &PartyPlan{ParseSource: "semantic_turn", ParseConfidence: act.Confidence}
	}
	session.PartyPlan.PartySize = act.Count
	if len(session.PartyPlan.Groups) == 0 {
		session.PartyPlan.ClarifyReason = "guest_services_required"
	}
	result.Changed = beforeSize != act.Count
	if result.Changed {
		session.OfferedSlots = nil
	}
	return result
}

func applyPartyServiceConversationAct(session *Session, act ConversationAct, services []ServiceOption) conversationDraftResult {
	result := conversationDraftResult{Handled: true, Act: act, ReplySource: "party_service_draft_reducer"}
	if session == nil || session.PartyPlan == nil || strings.TrimSpace(act.GuestRef) == "" {
		result.Clarification = true
		result.Reply = "Which guest should receive that service change?"
		return result
	}
	plan := clonePartyPlan(session.PartyPlan)
	if !partyGuestRefExists(plan, act.GuestRef) {
		assigned := 0
		for _, group := range plan.Groups {
			assigned += group.Count
		}
		count := act.Count
		if count <= 0 {
			count = 1
		}
		if assigned+count > plan.PartySize {
			result.Clarification = true
			result.Reply = "How many guests does that service apply to?"
			return result
		}
		plan.Groups = append(plan.Groups, PartyPlanGroup{Label: strings.TrimSpace(act.GuestRef), Count: count, Source: "semantic_turn"})
	}
	for index := range plan.Groups {
		group := &plan.Groups[index]
		if !strings.EqualFold(strings.TrimSpace(group.Label), strings.TrimSpace(act.GuestRef)) {
			continue
		}
		before := append([]string(nil), group.ResolvedServiceIDs...)
		switch act.Kind {
		case ConversationActAdd:
			targets := append([]string(nil), act.TargetServiceIDs...)
			if len(group.ResolvedServiceIDs) == 0 && len(targets) == 1 && group.Count > 1 {
				for len(targets) < group.Count {
					targets = append(targets, targets[0])
				}
			}
			group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, targets...)
		case ConversationActReplace:
			if len(act.TargetServiceIDs) != 1 {
				result.Clarification = true
				result.Reply = "Which specific service should " + group.Label + " receive?"
				return result
			}
			remove := stringSet(act.SourceServiceIDs)
			updated := make([]string, 0, len(group.ResolvedServiceIDs)+1)
			replaced := false
			for _, serviceID := range group.ResolvedServiceIDs {
				if remove[serviceID] || (len(remove) == 0 && !replaced) {
					if !replaced {
						updated = append(updated, act.TargetServiceIDs[0])
						replaced = true
					}
					continue
				}
				updated = append(updated, serviceID)
			}
			if !replaced {
				result.Clarification = true
				result.Reply = "Which current service for " + group.Label + " should I replace?"
				return result
			}
			group.ResolvedServiceIDs = updated
		case ConversationActRemove:
			remove := stringSet(act.SourceServiceIDs)
			updated := make([]string, 0, len(group.ResolvedServiceIDs))
			for _, serviceID := range group.ResolvedServiceIDs {
				if !remove[serviceID] {
					updated = append(updated, serviceID)
				}
			}
			group.ResolvedServiceIDs = updated
		}
		group.CandidateServiceIDs = nil
		result.Changed = !sameStringSlices(before, group.ResolvedServiceIDs)
		break
	}
	if !result.Changed {
		return result
	}
	plan.ParseSource = "semantic_turn"
	plan.ParseConfidence = act.Confidence
	plan.ClarifyReason = ""
	session.PartyPlan = plan
	session.OfferedSlots = nil
	if partyPlanComplete(plan) {
		applyPartyBookingPlan(session, partyBookingPlan{PartySize: plan.PartySize, Segments: partyPlanSegments(plan, *session)})
		session.ServiceName = serviceName(session.ServiceID, services, "")
	} else {
		session.BookingSegments = nil
		session.ServiceID = ""
		session.ServiceName = ""
	}
	return result
}
