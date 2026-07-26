package conversation

import (
	"context"
	"strings"
)

const (
	fuzzyServiceConfirmationPartyInitial    = "party_initial"
	fuzzyServiceConfirmationPartyCorrection = "party_correction"
)

func fuzzyServiceCandidate(result serviceUnderstandingResult) (ServiceOption, bool) {
	if result.Status != serviceUnderstandingStatusSelected || result.Reason != serviceUnderstandingFuzzyService || len(result.Candidates) != 1 {
		return ServiceOption{}, false
	}
	candidate := result.Candidates[0]
	if strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.Name) == "" {
		return ServiceOption{}, false
	}
	return candidate, true
}

func pendingFuzzyServiceConfirmation(session Session, services []ServiceOption) (PendingConversationAct, ServiceOption, bool) {
	state := normalizedDialogState(session.DialogState)
	if state.Pending == nil || state.Pending.PromptKey != PendingFuzzyServiceConfirmation || state.Pending.Entity != ConversationEntityService ||
		state.Pending.Subject != serviceUnderstandingFuzzyService || len(state.Pending.TargetServiceIDs) != 1 {
		return PendingConversationAct{}, ServiceOption{}, false
	}
	candidate := serviceByID(services, strings.TrimSpace(state.Pending.TargetServiceIDs[0]))
	if candidate == nil || strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.Name) == "" {
		return PendingConversationAct{}, ServiceOption{}, false
	}
	return *clonePendingConversationAct(state.Pending), *candidate, true
}

func (s *Service) startFuzzyServiceConfirmation(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	session Session,
	message string,
	eventKey string,
	understanding serviceUnderstandingResult,
	turnUnderstanding TurnUnderstanding,
	partySignal partySignal,
	services []ServiceOption,
	aliases []ServiceAlias,
	categoryAliases []ServiceCategoryAlias,
	staff []StaffOption,
	cfg *RuntimeConfig,
) (bool, *Session, error) {
	candidate, ok := fuzzyServiceCandidate(understanding)
	if !ok || selectedServiceContains(session, candidate.ID) {
		return false, nil, nil
	}
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil {
		turn := newTurnRecord(salonID, ownerUserID, session, session, message, eventKey, services, staff, cfg)
		applyServiceUnderstandingMetadata(&turn, understanding)
		turn.AIMessage = pendingConversationPrompt(session, services, state, false)
		if state.Pending.PromptKey == PendingCustomerNameConfirmation {
			turn.AIMessage = customerNameConfirmationPrompt(strings.TrimSpace(state.Pending.Value))
		}
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, session, session, expectedInputForSession(session), expectedInputForSession(session), "pending_state_precedes_fuzzy_service")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}

	next := cloneSessionForTurn(session)
	applyExtraction(&next, message, services, aliases, categoryAliases, staff, timezoneLocation(timezoneFromConfig(cfg)), s.now)
	next.Intent = IntentBooking
	kind := ConversationActUnknown
	scope := ""
	sourceIDs := selectedServiceIDs(session)
	if partySignal.IsParty {
		if plan, planOK := partyPlanFromSignal(partySignal, next); planOK {
			for index := range plan.Groups {
				group := &plan.Groups[index]
				if group.Source == partyIntentSourceSelectedService && sameStringSlices(nonEmptyStrings(group.CandidateServiceIDs), []string{candidate.ID}) {
					group.ResolvedServiceIDs = nil
				}
			}
			next.PartyPlan = plan
			scope = fuzzyServiceConfirmationPartyInitial
		}
	} else if activePartyPlan(session.PartyPlan) {
		scope = fuzzyServiceConfirmationPartyCorrection
	} else if len(sourceIDs) == 0 {
		kind = ConversationActSet
	} else {
		for _, act := range turnUnderstanding.Acts {
			if act.Entity != ConversationEntityService || !stringSet(act.TargetServiceIDs)[strings.TrimSpace(candidate.ID)] {
				continue
			}
			switch act.Kind {
			case ConversationActAdd, ConversationActReplace, ConversationActSet:
				kind = act.Kind
			}
			break
		}
	}
	state = normalizedDialogState(next.DialogState)
	state.Phase = DialogPhaseClarifying
	state.Pending = &PendingConversationAct{
		Kind: kind, Entity: ConversationEntityService,
		SourceServiceIDs: append([]string(nil), sourceIDs...), TargetServiceIDs: []string{candidate.ID},
		Scope: scope, Subject: serviceUnderstandingFuzzyService, Value: strings.TrimSpace(understanding.MatchedToken),
		PromptKey: PendingFuzzyServiceConfirmation,
	}
	state.LastPromptKey = PendingFuzzyServiceConfirmation
	state.ReviewAccepted = false
	state.AuthorizedRevision = 0
	next.DialogState = state
	turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	applyServiceUnderstandingMetadata(&turn, understanding)
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_confirmation_required":     true,
		"service_confirmation_candidate_id": candidate.ID,
		"service_confirmation_provenance":   understanding.Reason,
		"service_confirmation_token":        strings.TrimSpace(understanding.MatchedToken),
	})
	turn.AIMessage = fuzzyServiceConfirmationPrompt(candidate)
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	finalizeTurnMetadata(&turn, session, next, ExpectedInputServiceConfirmation, ExpectedInputServiceConfirmation, "fuzzy_service_confirmation_required")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func (s *Service) handlePendingFuzzyServiceConfirmation(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	session Session,
	message string,
	eventKey string,
	understanding serviceUnderstandingResult,
	services []ServiceOption,
	aliases []ServiceAlias,
	categoryAliases []ServiceCategoryAlias,
	staff []StaffOption,
	cfg *RuntimeConfig,
	knowledge []KnowledgeSnippet,
) (bool, *Session, error) {
	pending, candidate, ok := pendingFuzzyServiceConfirmation(session, services)
	if !ok {
		return false, nil, nil
	}
	confirmationSource := ""
	confirmedCandidate := candidate
	switch {
	case isFuzzyServiceConfirmationAffirmative(message):
		confirmationSource = "state_scoped_affirmative"
	case understanding.Status == serviceUnderstandingStatusSelected && len(understanding.Candidates) == 1 &&
		(understanding.Reason == serviceUnderstandingExact || understanding.Reason == serviceUnderstandingAlias):
		confirmedCandidate = understanding.Candidates[0]
		confirmationSource = understanding.Reason
	case isNegativeOnly(message):
		next := cloneSessionForTurn(session)
		clearPendingFuzzyServiceConfirmation(&next)
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
			"service_confirmation_result":       "rejected",
			"service_confirmation_candidate_id": candidate.ID,
		})
		turn.AIMessage = "Thanks for clarifying. " + serviceMenuReply(services)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, session, next, ExpectedInputService, ExpectedInputService, "fuzzy_service_confirmation_rejected")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	default:
		turn := newTurnRecord(salonID, ownerUserID, session, session, message, eventKey, services, staff, cfg)
		turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
			"service_confirmation_result":       "unresolved",
			"service_confirmation_candidate_id": candidate.ID,
		})
		turn.AIMessage = fuzzyServiceConfirmationPrompt(candidate)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, session, session, ExpectedInputServiceConfirmation, ExpectedInputServiceConfirmation, "fuzzy_service_confirmation_unresolved")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}

	if serviceByID(services, confirmedCandidate.ID) == nil {
		return s.saveInvalidFuzzyServiceConfirmation(ctx, salonID, ownerUserID, session, message, eventKey, services, staff, cfg)
	}
	if !sameStringSlices(selectedServiceIDs(session), pending.SourceServiceIDs) {
		return s.saveInvalidFuzzyServiceConfirmation(ctx, salonID, ownerUserID, session, message, eventKey, services, staff, cfg)
	}

	next := cloneSessionForTurn(session)
	clearPendingFuzzyServiceConfirmation(&next)
	changed := false
	switch pending.Scope {
	case fuzzyServiceConfirmationPartyInitial:
		changed = confirmFuzzyPartyPlanCandidate(&next, confirmedCandidate)
	case fuzzyServiceConfirmationPartyCorrection:
		state := normalizedDialogState(next.DialogState)
		state.Phase = DialogPhaseClarifying
		state.Pending = &PendingConversationAct{
			Kind: ConversationActReplace, Entity: ConversationEntityService,
			TargetServiceIDs: []string{confirmedCandidate.ID}, PromptKey: PendingPartyServiceGuest,
		}
		state.LastPromptKey = PendingPartyServiceGuest
		next.DialogState = state
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = pendingConversationPrompt(next, services, state, false)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, session, next, "party_service_correction", "party_service_correction", "fuzzy_party_service_confirmed")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	default:
		switch pending.Kind {
		case ConversationActAdd:
			changed = addServiceSelection(&next, []ServiceOption{confirmedCandidate})
		case ConversationActReplace:
			changed = applyServiceSelection(&next, []ServiceOption{confirmedCandidate})
		case ConversationActSet:
			if len(pending.SourceServiceIDs) == 0 {
				changed = applyServiceSelection(&next, []ServiceOption{confirmedCandidate})
			}
		}
		if !changed && !selectedServiceContains(next, confirmedCandidate.ID) {
			state := normalizedDialogState(next.DialogState)
			state.Phase = DialogPhaseClarifying
			state.Pending = &PendingConversationAct{
				Kind: ConversationActUnknown, Entity: ConversationEntityService,
				SourceServiceIDs: selectedServiceIDs(next), TargetServiceIDs: []string{confirmedCandidate.ID},
				PromptKey: "semantic_add_or_replace",
			}
			state.LastPromptKey = "semantic_add_or_replace"
			next.DialogState = state
			turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
			turn.AIMessage = serviceEditClarificationPrompt(next, []ServiceOption{confirmedCandidate}, services)
			turn.ReplyPolicy = ReplyPolicyOperationalFact
			finalizeTurnMetadata(&turn, session, next, ExpectedInputPendingServiceOperation, ExpectedInputPendingServiceOperation, "fuzzy_service_identity_confirmed_operation_pending")
			updated, err := s.store.SaveTurn(ctx, turn)
			return true, updated, err
		}
	}

	if !changed && !selectedServiceContains(next, confirmedCandidate.ID) {
		return s.saveInvalidFuzzyServiceConfirmation(ctx, salonID, ownerUserID, session, message, eventKey, services, staff, cfg)
	}
	invalidateCarriedAvailabilityProof(session, &next)
	advanceDraftRevision(session, &next)
	clearGuidanceRecoveryState(&next, DialogPhaseDrafting)
	turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_confirmation_result":       "accepted",
		"service_confirmation_source":       confirmationSource,
		"service_confirmation_candidate_id": confirmedCandidate.ID,
		"service_confirmation_provenance":   pending.Subject,
		"service_confirmation_token":        pending.Value,
	})
	return s.continueAfterFuzzyServiceConfirmation(ctx, ownerUserID, turn, session, next, services, staff, cfg, knowledge)
}

func (s *Service) continueAfterFuzzyServiceConfirmation(ctx context.Context, ownerUserID string, turn TurnRecord, before Session, next Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (bool, *Session, error) {
	if activePartyPlan(next.PartyPlan) && !partyPlanComplete(next.PartyPlan) {
		turn.AIMessage = partyPlanClarificationPrompt(next, next.PartyPlan, services, cfg)
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, before, next, ExpectedInputService, ExpectedInputService, "fuzzy_party_service_confirmation_incomplete")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if shouldCheckAvailabilityForRequestedTime(before, next, false) {
		available, _, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			updated, saveErr := s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
			return true, updated, saveErr
		}
		if !available {
			turn.ReplyPolicy = ReplyPolicyOperationalFact
			finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "availability_after_fuzzy_service_confirmation")
			updated, saveErr := s.store.SaveTurn(ctx, turn)
			return true, updated, saveErr
		}
	}
	if missingBookingField(next) == "requested_time" && strings.TrimSpace(next.RequestedDate) != "" && strings.TrimSpace(next.ServiceID) != "" {
		if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, next.RequestedDate, false, cfg); err != nil {
			updated, saveErr := s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
			return true, updated, saveErr
		}
		turn.ReplyPolicy = ReplyPolicyOperationalFact
		finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "availability_after_fuzzy_service_confirmation")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	updated, err := s.continueAfterDraftReady(ctx, ownerUserID, turn, before, next, services, staff, cfg, knowledge)
	return true, updated, err
}

func confirmFuzzyPartyPlanCandidate(session *Session, candidate ServiceOption) bool {
	if session == nil || session.PartyPlan == nil {
		return false
	}
	plan := clonePartyPlan(session.PartyPlan)
	changed := false
	for index := range plan.Groups {
		group := &plan.Groups[index]
		if len(nonEmptyStrings(group.ResolvedServiceIDs)) > 0 || !sameStringSlices(nonEmptyStrings(group.CandidateServiceIDs), []string{candidate.ID}) || group.Count <= 0 {
			continue
		}
		group.ResolvedServiceIDs = repeatedString(candidate.ID, group.Count)
		changed = true
	}
	if !changed {
		return false
	}
	session.PartyPlan = plan
	if partyPlanComplete(plan) {
		applyPartyBookingPlan(session, partyBookingPlan{PartySize: plan.PartySize, Segments: partyPlanSegments(plan, *session)})
		session.ServiceName = serviceName(session.ServiceID, []ServiceOption{candidate}, candidate.Name)
	}
	return true
}

func clearPendingFuzzyServiceConfirmation(session *Session) {
	if session == nil {
		return
	}
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil && state.Pending.PromptKey == PendingFuzzyServiceConfirmation {
		state.Pending = nil
		state.LastPromptKey = ""
		state.Phase = DialogPhaseDrafting
	}
	session.DialogState = state
}

func fuzzyServiceConfirmationPrompt(candidate ServiceOption) string {
	return "Did you mean " + strings.TrimSpace(candidate.Name) + "?"
}

// isFuzzyServiceConfirmationAffirmative is active only while the persisted
// dialog state asks about one concrete catalog candidate. It is a bounded
// confirmation grammar, not a general caller-intent classifier.
func isFuzzyServiceConfirmationAffirmative(message string) bool {
	if isExactAffirmativeResponse(message) {
		return true
	}
	tokens := strings.Fields(normalizeLooseText(message))
	if len(tokens) == 0 || len(tokens) > 7 {
		return false
	}
	allowed := map[string]bool{
		"yes": true, "yeah": true, "yep": true, "ok": true, "okay": true,
		"sure": true, "correct": true, "right": true, "please": true,
		"that": true, "it": true, "is": true, "s": true, "the": true, "service": true,
	}
	hasAffirmation := false
	for _, token := range tokens {
		if !allowed[token] {
			return false
		}
		switch token {
		case "yes", "yeah", "yep", "ok", "okay", "sure", "correct", "right":
			hasAffirmation = true
		}
	}
	return hasAffirmation
}

func (s *Service) saveInvalidFuzzyServiceConfirmation(ctx context.Context, salonID string, ownerUserID string, session Session, message string, eventKey string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, *Session, error) {
	next := cloneSessionForTurn(session)
	clearPendingFuzzyServiceConfirmation(&next)
	turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.AIMessage = "I couldn't verify that service against the current catalog. Which service would you like?"
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	finalizeTurnMetadata(&turn, session, next, ExpectedInputService, ExpectedInputService, "fuzzy_service_confirmation_invalidated")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}
