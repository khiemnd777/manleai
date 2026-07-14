package conversation

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const consultationCandidateLimit = 3

type consultationRankedService struct {
	Service ServiceOption
	Score   int
	Reasons []string
}

func consultationStateActive(state *ConsultationState) bool {
	if state == nil {
		return false
	}
	switch strings.TrimSpace(state.Status) {
	case ConsultationStatusCollectingNeeds, ConsultationStatusComparing, ConsultationStatusAwaitingSelection, ConsultationStatusAwaitingBooking:
		return true
	default:
		return false
	}
}

func (s *Service) handleServiceConsultation(ctx context.Context, ownerUserID string, session Session, message, eventKey string, understanding serviceUnderstandingResult, turnUnderstanding TurnUnderstanding, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, *Session, error) {
	if shouldHandoff(message) || shouldComplaintHandoff(message) || shouldRouteCancel(session, message) || shouldRouteReschedule(session, message) || bookingActionForSession(session) != BookingActionBook || activePartyPlan(session.PartyPlan) {
		return false, nil, nil
	}

	dialog := normalizedDialogState(session.DialogState)
	active := consultationStateActive(dialog.Consultation)
	if !active && !isConsultationRequest(message, understanding, turnUnderstanding) && !isConsultationSafetyConcern(message) {
		return false, nil, nil
	}
	next := cloneSessionForTurn(session)
	dialog = normalizedDialogState(next.DialogState)
	consultation := initializeConsultationState(dialog, session)
	dialog.Phase = DialogPhaseConsultation
	dialog.Consultation = consultation
	next.DialogState = dialog
	next.Intent = IntentConsultation

	if isConsultationSafetyConcern(message) {
		consultation.Status = ConsultationStatusHandedOff
		consultation.ExitReason = HandoffReasonConsultationSafety
		dialog.Consultation = consultation
		next.DialogState = dialog
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		setConsultationMetadata(&turn, consultation, services)
		updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonConsultationSafety, "For pain, injury, infection, allergy, or another health concern, the owner should help you directly. I cannot give medical advice, and this is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	}
	if cfg != nil && !cfg.ConsultationEnabled {
		consultation.Status = ConsultationStatusCompleted
		consultation.ExitReason = "consultation_disabled"
		dialog.Phase = consultationResumePhase(*consultation, session)
		dialog.Consultation = consultation
		next.DialogState = dialog
		if hasOperationalBookingProgress(session) {
			next.Intent = IntentBooking
		} else {
			next.Intent = IntentUnknown
		}
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = "AI consultation is not enabled. I can help book a specific service, or I can ask the owner to help you choose."
		setConsultationMetadata(&turn, consultation, services)
		finalizeTurnMetadata(&turn, session, next, "", "", "consultation_disabled")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}

	if isConsultationCompletionUtterance(message) || turnUnderstanding.Consultation.ConversationComplete {
		consultation.Status = ConsultationStatusCompleted
		consultation.ExitReason = "caller_completed_without_booking"
		dialog.Phase = consultationResumePhase(*consultation, session)
		dialog.Consultation = consultation
		next.DialogState = dialog
		if hasOperationalBookingProgress(session) && !isGoodbyeUtterance(message) {
			consultation.ExitReason = "resumed_existing_booking"
			dialog.Consultation = consultation
			next.DialogState = dialog
			next.Intent = IntentBooking
			reply := resumeBookingPrompt(next, services, cfg)
			if reply == "" {
				reply = "Let's continue with your original booking details."
			}
			return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, reply, "consultation_completed_resume_booking", consultation, services, staff, cfg)
		}
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = "You're welcome. If you decide to book later, just let us know."
		turn.Update.Status = StatusCompleted
		turn.Update.Outcome = OutcomeConsultationCompleted
		turn.Update.EndSession = true
		setConsultationMetadata(&turn, consultation, services)
		finalizeTurnMetadata(&turn, session, next, "", "", "consultation_completed")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if consultation.Status == ConsultationStatusAwaitingBooking && consultationBookingDeclined(message) {
		consultation.Status = ConsultationStatusCompleted
		consultation.ExitReason = "caller_completed_without_booking"
		dialog.Phase = consultationResumePhase(*consultation, session)
		dialog.Consultation = consultation
		next.DialogState = dialog
		if hasOperationalBookingProgress(session) {
			consultation.ExitReason = "resumed_existing_booking"
			dialog.Consultation = consultation
			next.DialogState = dialog
			next.Intent = IntentBooking
			reply := resumeBookingPrompt(next, services, cfg)
			if reply == "" {
				reply = "No problem. Let's continue with your original booking details."
			}
			return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, reply, "consultation_booking_declined_resume_booking", consultation, services, staff, cfg)
		}
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = "No problem. Thanks for calling. You can ask us to book it whenever you're ready."
		turn.Update.Status = StatusCompleted
		turn.Update.Outcome = OutcomeConsultationCompleted
		turn.Update.EndSession = true
		setConsultationMetadata(&turn, consultation, services)
		finalizeTurnMetadata(&turn, session, next, "", "", "consultation_booking_declined")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if active && asksServiceMenu(message) {
		consultation.Status = ConsultationStatusCollectingNeeds
		consultation.CandidateServiceIDs = nil
		consultation.RecommendedServiceIDs = nil
		consultation.SelectedServiceID = ""
		consultation.LastAskedField = "service_comparison"
		consultation.NoProgressCount = 0
		dialog.Consultation = consultation
		next.DialogState = dialog
		names := serviceCandidateNames(services, 8)
		reply := "I don't have any bookable catalog services to list. I can ask the owner to help."
		if len(names) > 0 {
			prefix := "Bookable services include "
			if len(services) > len(names) {
				prefix = "Some bookable services include "
			}
			reply = prefix + joinHumanList(names) + ". Which ones would you like me to compare?"
		}
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, reply, "consultation_service_menu", consultation, services, staff, cfg)
	}

	selected := consultationSelectedService(message, understanding, turnUnderstanding, consultation, services)
	if active && hasOperationalBookingProgress(session) && !turnHasMutations(turnUnderstanding) &&
		containsAnyLoosePhrase(normalizeLooseText(message), []string{"continue my booking", "back to my booking", "original booking"}) {
		consultation.Status = ConsultationStatusCompleted
		consultation.ExitReason = "resumed_existing_booking"
		dialog.Phase = consultationResumePhase(*consultation, session)
		dialog.Consultation = consultation
		next.DialogState = dialog
		next.Intent = IntentBooking
		reply := resumeBookingPrompt(next, services, cfg)
		if reply == "" {
			reply = "Let's continue with your original booking details."
		}
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, reply, "consultation_resumed_booking", consultation, services, staff, cfg)
	}
	if consultation.Status == ConsultationStatusAwaitingBooking &&
		(isAffirmativeOnly(message) || hasBookingVerbSignal(message) || turnUnderstanding.Consultation.BookingRequested) {
		selected = serviceByID(services, firstNonEmpty(consultation.SelectedServiceID, onlyServiceID(consultation.RecommendedServiceIDs)))
		if selected == nil {
			consultation.Status = ConsultationStatusAwaitingSelection
			consultation.LastAskedField = "service_selection"
			dialog.Consultation = consultation
			next.DialogState = dialog
			return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, "Which service would you like: "+joinHumanList(serviceCandidateNames(pendingConsultationServices(next, services), consultationCandidateLimit))+"?", "consultation_ambiguous_affirmative", consultation, services, staff, cfg)
		}
		if !consultationAffirmationOnly(message) {
			applyExtraction(&next, message, services, nil, nil, staff, timezoneLocation(timezoneFromConfig(cfg)), s.now)
		}
		return s.startBookingFromConsultation(ctx, ownerUserID, session, next, message, eventKey, *selected, consultation, services, staff, cfg)
	}
	if selected != nil && (hasBookingVerbSignal(message) || turnUnderstanding.Consultation.BookingRequested) {
		// Let the normal booking reducer own compound turns so date, time, staff,
		// and customer fields in the same utterance are not lost.
		return false, nil, nil
	}
	if selected != nil && consultation.Status == ConsultationStatusAwaitingSelection {
		consultation.SelectedServiceID = selected.ID
		consultation.Status = ConsultationStatusAwaitingBooking
		consultation.LastAskedField = "booking_intent"
		dialog.Consultation = consultation
		next.DialogState = dialog
		reply := "Would you like help booking " + strings.TrimSpace(selected.Name) + "?"
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, reply, "consultation_selection_confirmed", consultation, services, staff, cfg)
	}

	beforeNeeds := consultation.Needs
	consultation.Needs = mergeConsultationNeeds(consultation.Needs, turnUnderstanding.Consultation, inferConsultationNeeds(message))
	candidates := consultationCandidates(message, understanding, services)
	if len(turnUnderstanding.Consultation.ComparedServiceIDs) > 0 {
		candidates = mergeServiceOptions(candidates, servicesByIDs(services, turnUnderstanding.Consultation.ComparedServiceIDs))
	}
	if len(candidates) > consultationCandidateLimit {
		candidates = candidates[:consultationCandidateLimit]
	}
	if len(candidates) > 0 {
		consultation.CandidateServiceIDs = serviceOptionIDs(candidates)
		consultation.Needs.ComparedServiceIDs = append([]string(nil), consultation.CandidateServiceIDs...)
	}
	if consultationNeedsEqual(beforeNeeds, consultation.Needs) && active {
		consultation.NoProgressCount++
	} else {
		consultation.NoProgressCount = 0
	}
	if consultation.NoProgressCount >= 3 {
		consultation.Status = ConsultationStatusHandedOff
		consultation.ExitReason = HandoffReasonConsultationUnresolved
		dialog.Consultation = consultation
		next.DialogState = dialog
		turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		setConsultationMetadata(&turn, consultation, services)
		updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonConsultationUnresolved, "I still don't have enough approved information to recommend a service safely. I'll ask the owner to help, and this is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	}

	comparisonRequested := len(consultation.CandidateServiceIDs) > 1 || consultation.Needs.DesiredOutcome == ConsultationOutcomeCompare
	if !comparisonRequested && consultation.Needs.DesiredOutcome == "" {
		consultation.Status = ConsultationStatusCollectingNeeds
		consultation.LastAskedField = "desired_outcome"
		dialog.Consultation = consultation
		next.DialogState = dialog
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, "What would you like to change: shorten, add length, add strength, remove the current product, or refresh the color?", "consultation_ask_desired_outcome", consultation, services, staff, cfg)
	}
	if !comparisonRequested && consultation.Needs.CurrentSystem == "" {
		consultation.Status = ConsultationStatusCollectingNeeds
		consultation.LastAskedField = "current_system"
		dialog.Consultation = consultation
		next.DialogState = dialog
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, "What is currently on your nails: natural nails, regular polish, gel, dip, acrylic, or extensions?", "consultation_ask_current_system", consultation, services, staff, cfg)
	}

	ranked := rankConsultationServices(services, consultation.Needs, consultation.CandidateServiceIDs)
	if len(ranked) == 0 && len(consultation.CandidateServiceIDs) > 0 {
		fallback := servicesByIDs(services, consultation.CandidateServiceIDs)
		if len(fallback) > 0 {
			consultation.Status = ConsultationStatusAwaitingSelection
			consultation.RecommendedServiceIDs = serviceOptionIDs(fallback)
			consultation.LastAskedField = "service_selection"
			dialog.Consultation = consultation
			next.DialogState = dialog
			return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, consultationComparisonReply(fallback), "consultation_approved_facts_comparison", consultation, services, staff, cfg)
		}
	}
	if len(ranked) == 0 {
		consultation.Status = ConsultationStatusCollectingNeeds
		consultation.LastAskedField = "owner_help"
		dialog.Consultation = consultation
		next.DialogState = dialog
		return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, "I don't have enough owner-approved service details to make a safe recommendation. I can ask the owner to help, or I can list the bookable services.", "consultation_no_approved_match", consultation, services, staff, cfg)
	}

	if len(ranked) > 2 {
		ranked = ranked[:2]
	}
	consultation.Status = ConsultationStatusComparing
	consultation.RecommendedServiceIDs = nil
	consultation.ProfileRevisions = map[string]int{}
	consultation.RecommendationReasons = map[string][]string{}
	for _, item := range ranked {
		consultation.RecommendedServiceIDs = append(consultation.RecommendedServiceIDs, item.Service.ID)
		consultation.RecommendationReasons[item.Service.ID] = append([]string(nil), item.Reasons...)
		if item.Service.ConsultationProfile != nil {
			consultation.ProfileRevisions[item.Service.ID] = item.Service.ConsultationProfile.Revision
		}
	}
	if len(ranked) == 1 {
		consultation.Status = ConsultationStatusAwaitingBooking
		consultation.SelectedServiceID = ranked[0].Service.ID
		consultation.LastAskedField = "booking_intent"
	} else {
		consultation.Status = ConsultationStatusAwaitingSelection
		consultation.LastAskedField = "service_selection"
	}
	dialog.Consultation = consultation
	next.DialogState = dialog
	return s.saveConsultationTurn(ctx, ownerUserID, session, next, message, eventKey, consultationRecommendationReply(ranked), "service_consultation", consultation, services, staff, cfg)
}

func consultationAffirmationOnly(message string) bool {
	switch normalizeLooseText(message) {
	case "yes", "yeah", "yep", "ok", "okay", "sure", "yes please", "please do", "book it", "yes book it":
		return true
	default:
		return false
	}
}

func consultationBookingDeclined(message string) bool {
	if isNegativeOnly(message) {
		return true
	}
	normalized := normalizeLooseText(message)
	if normalized == "no thanks" || containsLoosePhrase(normalized, "not today") {
		return true
	}
	declines := containsLoosePhrase(normalized, "do not want") || containsLoosePhrase(normalized, "dont want") || containsLoosePhrase(normalized, "not looking to")
	return declines && hasBookingVerbSignal(normalized)
}

func isConsultationCompletionUtterance(message string) bool {
	if isGoodbyeUtterance(message) {
		return true
	}
	normalized := normalizeLooseText(message)
	return strings.HasSuffix(normalized, " goodbye") ||
		containsAnyLoosePhrase(normalized, []string{"no thanks that answers it", "that answers my question", "that is all", "thats all"})
}

func initializeConsultationState(dialog DialogState, session Session) *ConsultationState {
	if dialog.Consultation != nil && consultationStateActive(dialog.Consultation) {
		copy := *dialog.Consultation
		return &copy
	}
	resume := dialog.Phase
	if resume == "" || resume == DialogPhaseConsultation {
		resume = DialogPhaseOpen
	}
	if hasOperationalBookingProgress(session) && resume == DialogPhaseOpen {
		resume = DialogPhaseDrafting
	}
	return &ConsultationState{
		Status:                ConsultationStatusCollectingNeeds,
		ResumePhase:           resume,
		ProfileRevisions:      map[string]int{},
		RecommendationReasons: map[string][]string{},
	}
}

func consultationResumePhase(consultation ConsultationState, session Session) string {
	if phase := strings.TrimSpace(consultation.ResumePhase); phase != "" && phase != DialogPhaseConsultation {
		return phase
	}
	if hasOperationalBookingProgress(session) {
		return DialogPhaseDrafting
	}
	return DialogPhaseOpen
}

func closeConsultationForWorkflow(session *Session, reason string, handedOff bool) {
	if session == nil {
		return
	}
	dialog := normalizedDialogState(session.DialogState)
	if !consultationStateActive(dialog.Consultation) {
		return
	}
	consultation := *dialog.Consultation
	consultation.Status = ConsultationStatusCompleted
	if handedOff {
		consultation.Status = ConsultationStatusHandedOff
	}
	consultation.ExitReason = strings.TrimSpace(reason)
	dialog.Phase = consultationResumePhase(consultation, *session)
	dialog.Consultation = &consultation
	session.DialogState = dialog
}

func (s *Service) startBookingFromConsultation(ctx context.Context, ownerUserID string, before Session, next Session, message, eventKey string, selected ServiceOption, consultation *ConsultationState, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, *Session, error) {
	applyServiceSelection(&next, []ServiceOption{selected})
	next.Intent = IntentBooking
	consultation.Status = ConsultationStatusCompleted
	consultation.SelectedServiceID = selected.ID
	consultation.ExitReason = "caller_requested_booking"
	dialog := normalizedDialogState(next.DialogState)
	dialog.Phase = consultationResumePhase(*consultation, next)
	dialog.Consultation = consultation
	next.DialogState = dialog
	advanceDraftRevision(before, &next)
	turn := newTurnRecord(before.SalonID, ownerUserID, before, next, message, eventKey, services, staff, cfg)
	setConsultationMetadata(&turn, consultation, services)
	if cfg != nil && !cfg.AIEnabled {
		updated, err := s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can send this request to the owner, but this is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	}
	missing := missingBookingField(next)
	if missing == "requested_time" && strings.TrimSpace(next.RequestedDate) != "" {
		if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, next.RequestedDate, false, cfg); err != nil {
			updated, saveErr := s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
			return true, updated, saveErr
		}
		setConsultationMetadata(&turn, consultation, services)
		finalizeTurnMetadata(&turn, before, next, missing, missing, "consultation_to_booking_availability")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if missing != "" {
		turn.AIMessage = resumeBookingPrompt(next, services, cfg)
		finalizeTurnMetadata(&turn, before, next, missing, missing, "consultation_to_booking")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	state := normalizedDialogState(next.DialogState)
	state.Phase = DialogPhaseReview
	state.ReviewedRevision = state.DraftRevision
	state.ReviewAccepted = false
	state.AuthorizedRevision = 0
	state.LastPromptKey = "final_review"
	next.DialogState = state
	syncTurnUpdate(&turn, next, services, staff, cfg)
	turn.AIMessage = finalBookingReviewPrompt(next, services, staff, cfg)
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	finalizeTurnMetadata(&turn, before, next, "booking_review", "booking_review", "consultation_to_booking_review")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func (s *Service) saveConsultationTurn(ctx context.Context, ownerUserID string, before Session, next Session, message, eventKey, reply, source string, consultation *ConsultationState, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, *Session, error) {
	turn := newTurnRecord(before.SalonID, ownerUserID, before, next, message, eventKey, services, staff, cfg)
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	turn.AIMessage = reply
	setConsultationMetadata(&turn, consultation, services)
	finalizeTurnMetadata(&turn, before, next, "consultation", consultation.LastAskedField, source)
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func isConsultationRequest(message string, understanding serviceUnderstandingResult, turn TurnUnderstanding) bool {
	if strings.TrimSpace(turn.Goal) == "consultation" || meaningfulConsultationNeeds(turn.Consultation) {
		return true
	}
	normalized := normalizeLooseText(message)
	signals := []string{
		"help me choose", "not sure which", "which one should", "what do you recommend", "recommend a service",
		"compare", "difference between", "what is the difference", "which is better", "tell me about",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return understanding.Status != serviceUnderstandingStatusUnknown || mentionsGeneralServiceCatalog(normalized)
		}
	}
	return false
}

func meaningfulConsultationNeeds(needs ConsultationNeedProfile) bool {
	return needs.CurrentSystem != "" || needs.DesiredOutcome != "" || needs.LengthChange != "" || len(needs.Priorities) > 0 || len(needs.DesiredFinishes) > 0 || len(needs.ComparedServiceIDs) > 0 || needs.BookingRequested || needs.ConversationComplete
}

func isConsultationSafetyConcern(message string) bool {
	normalized := normalizeLooseText(message)
	health := []string{"pain", "painful", "hurt", "hurts", "injury", "injured", "infection", "infected", "fungus", "fungal", "allergy", "allergic", "bleeding", "swollen", "swelling"}
	question := []string{"what should", "which service", "can you treat", "can i get", "is it safe", "recommend", "help"}
	return containsAnyLoosePhrase(normalized, health) && containsAnyLoosePhrase(normalized, question)
}

func containsAnyLoosePhrase(normalized string, phrases []string) bool {
	for _, phrase := range phrases {
		if containsLoosePhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func consultationCandidates(message string, understanding serviceUnderstandingResult, services []ServiceOption) []ServiceOption {
	normalized := normalizeLooseText(message)
	out := make([]ServiceOption, 0, consultationCandidateLimit)
	seen := map[string]bool{}
	add := func(service ServiceOption) {
		id := strings.TrimSpace(service.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, service)
		}
	}
	for _, service := range services {
		if name := normalizeLooseText(service.Name); name != "" && containsLoosePhrase(normalized, name) {
			add(service)
		}
	}
	for _, candidate := range understanding.Candidates {
		add(candidate)
	}
	if understanding.Selected != nil {
		add(*understanding.Selected)
	}
	return out
}

func consultationSelectedService(message string, understanding serviceUnderstandingResult, turn TurnUnderstanding, state *ConsultationState, services []ServiceOption) *ServiceOption {
	if understanding.Selected != nil {
		for _, allowed := range append(append([]string(nil), state.CandidateServiceIDs...), state.RecommendedServiceIDs...) {
			if allowed == understanding.Selected.ID {
				selected := *understanding.Selected
				return &selected
			}
		}
	}
	for _, id := range turn.Consultation.ComparedServiceIDs {
		if service := serviceByID(services, id); service != nil && containsLoosePhrase(normalizeLooseText(message), normalizeLooseText(service.Name)) {
			return service
		}
	}
	return nil
}

func serviceByID(services []ServiceOption, id string) *ServiceOption {
	id = strings.TrimSpace(id)
	for _, service := range services {
		if strings.TrimSpace(service.ID) == id && id != "" {
			copy := service
			return &copy
		}
	}
	return nil
}

func onlyServiceID(ids []string) string {
	if len(ids) == 1 {
		return strings.TrimSpace(ids[0])
	}
	return ""
}

func mergeConsultationNeeds(current, semantic, fallback ConsultationNeedProfile) ConsultationNeedProfile {
	merged := current
	for _, source := range []ConsultationNeedProfile{fallback, semantic} {
		if source.CurrentSystem != "" && source.CurrentSystem != ConsultationSystemUnknown {
			merged.CurrentSystem = source.CurrentSystem
		}
		if source.DesiredOutcome != "" && source.DesiredOutcome != ConsultationOutcomeUnknown {
			merged.DesiredOutcome = source.DesiredOutcome
		}
		if source.LengthChange != "" && source.LengthChange != ConsultationLengthUnknown {
			merged.LengthChange = source.LengthChange
		}
		merged.Priorities = appendUniqueStrings(merged.Priorities, source.Priorities...)
		merged.DesiredFinishes = appendUniqueStrings(merged.DesiredFinishes, source.DesiredFinishes...)
		merged.ComparedServiceIDs = appendUniqueStrings(merged.ComparedServiceIDs, source.ComparedServiceIDs...)
		merged.BookingRequested = merged.BookingRequested || source.BookingRequested
		merged.ConversationComplete = merged.ConversationComplete || source.ConversationComplete
		if source.Confidence > merged.Confidence {
			merged.Confidence = source.Confidence
		}
		if strings.TrimSpace(source.Reason) != "" {
			merged.Reason = source.Reason
		}
	}
	return merged
}

func appendUniqueStrings(current []string, values ...string) []string {
	seen := map[string]bool{}
	for _, value := range current {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
		}
	}
	out := append([]string(nil), current...)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func consultationNeedsEqual(left, right ConsultationNeedProfile) bool {
	return left.CurrentSystem == right.CurrentSystem && left.DesiredOutcome == right.DesiredOutcome && left.LengthChange == right.LengthChange && sameStringSlices(left.Priorities, right.Priorities) && sameStringSlices(left.DesiredFinishes, right.DesiredFinishes) && sameStringSlices(left.ComparedServiceIDs, right.ComparedServiceIDs) && left.BookingRequested == right.BookingRequested && left.ConversationComplete == right.ConversationComplete
}

func inferConsultationNeeds(message string) ConsultationNeedProfile {
	normalized := normalizeLooseText(message)
	needs := ConsultationNeedProfile{Confidence: 1, Reason: "state_scoped_deterministic_consultation_fallback"}
	for _, signal := range []struct {
		value   string
		phrases []string
	}{
		{ConsultationSystemRegularPolish, []string{"regular polish", "normal polish"}},
		{ConsultationSystemGel, []string{"gel polish", "gel nails", "gel"}},
		{ConsultationSystemDip, []string{"dip powder", "dip nails", "dip"}},
		{ConsultationSystemAcrylic, []string{"acrylic"}},
		{ConsultationSystemExtension, []string{"extensions", "extension", "tips"}},
		{ConsultationSystemNatural, []string{"natural nails", "nothing on", "bare nails"}},
	} {
		if containsAnyLoosePhrase(normalized, signal.phrases) {
			needs.CurrentSystem = signal.value
			break
		}
	}
	for _, signal := range []struct {
		value   string
		phrases []string
	}{
		{ConsultationOutcomeShorten, []string{"too long", "shorter", "shorten", "cut down"}},
		{ConsultationOutcomeAddLength, []string{"add length", "longer", "extensions"}},
		{ConsultationOutcomeAddStrength, []string{"stronger", "add strength", "weak nails"}},
		{ConsultationOutcomeRemoval, []string{"remove", "take off", "soak off"}},
		{ConsultationOutcomeColorRefresh, []string{"new color", "change color", "refresh color"}},
		{ConsultationOutcomeRepair, []string{"repair", "broken nail", "fix a nail"}},
		{ConsultationOutcomeMaintain, []string{"maintain", "fill", "refill"}},
		{ConsultationOutcomeCompare, []string{"compare", "difference between", "which is better"}},
	} {
		if containsAnyLoosePhrase(normalized, signal.phrases) {
			needs.DesiredOutcome = signal.value
			break
		}
	}
	if containsAnyLoosePhrase(normalized, []string{"shorter", "shorten", "too long", "cut down"}) {
		needs.LengthChange = ConsultationLengthShorten
	} else if containsAnyLoosePhrase(normalized, []string{"longer", "add length"}) {
		needs.LengthChange = ConsultationLengthAddLength
	}
	for _, signal := range []struct {
		value   string
		phrases []string
	}{
		{ConsultationPriorityDurability, []string{"last longer", "durable", "stronger"}},
		{ConsultationPriorityLowerMaintenance, []string{"low maintenance", "less maintenance", "easy upkeep"}},
		{ConsultationPriorityLowerCost, []string{"cheaper", "lower cost", "budget"}},
		{ConsultationPriorityShorterVisit, []string{"quick", "shorter visit", "less time"}},
	} {
		if containsAnyLoosePhrase(normalized, signal.phrases) {
			needs.Priorities = append(needs.Priorities, signal.value)
		}
	}
	for _, signal := range []struct {
		value   string
		phrases []string
	}{
		{ConsultationFinishRegularPolish, []string{"want regular polish", "with regular polish"}},
		{ConsultationFinishGelPolish, []string{"want gel polish", "with gel polish", "gel color"}},
		{ConsultationFinishGlossy, []string{"glossy finish", "want glossy"}},
		{ConsultationFinishMatte, []string{"matte finish", "want matte"}},
		{ConsultationFinishNailArt, []string{"nail art", "want a design", "want designs"}},
		{ConsultationFinishNatural, []string{"natural finish", "natural look"}},
	} {
		if containsAnyLoosePhrase(normalized, signal.phrases) {
			needs.DesiredFinishes = append(needs.DesiredFinishes, signal.value)
		}
	}
	return needs
}

func rankConsultationServices(services []ServiceOption, needs ConsultationNeedProfile, candidateIDs []string) []consultationRankedService {
	allowed := map[string]bool{}
	for _, id := range candidateIDs {
		allowed[strings.TrimSpace(id)] = true
	}
	ranked := make([]consultationRankedService, 0, len(services))
	for _, service := range services {
		if len(allowed) > 0 && !allowed[service.ID] {
			continue
		}
		profile := service.ConsultationProfile
		if profile == nil || profile.Status != ConsultationProfileStatusReady {
			continue
		}
		if needs.DesiredOutcome != "" && needs.DesiredOutcome != ConsultationOutcomeCompare && !containsString(profile.RecommendedOutcomes, needs.DesiredOutcome) {
			continue
		}
		if needs.CurrentSystem != "" && !containsString(profile.CompatibleCurrentSystems, needs.CurrentSystem) {
			continue
		}
		if needs.LengthChange != "" && !containsString(profile.LengthCapabilities, needs.LengthChange) {
			continue
		}
		if len(needs.DesiredFinishes) > 0 && !containsAnyString(profile.FinishOptions, needs.DesiredFinishes) {
			continue
		}
		item := consultationRankedService{Service: service}
		if needs.DesiredOutcome != "" && needs.DesiredOutcome != ConsultationOutcomeCompare && containsString(profile.RecommendedOutcomes, needs.DesiredOutcome) {
			item.Score += 4
			item.Reasons = append(item.Reasons, consultationReasonLabel(needs.DesiredOutcome))
		}
		if needs.CurrentSystem != "" && containsString(profile.CompatibleCurrentSystems, needs.CurrentSystem) {
			item.Score += 3
			item.Reasons = append(item.Reasons, "works with what is currently on your nails")
		}
		if needs.LengthChange != "" && containsString(profile.LengthCapabilities, needs.LengthChange) {
			item.Score += 3
			item.Reasons = append(item.Reasons, consultationReasonLabel(needs.LengthChange))
		}
		for _, priority := range needs.Priorities {
			if containsString(profile.PriorityTags, priority) {
				item.Score += 2
				item.Reasons = append(item.Reasons, consultationReasonLabel(priority))
			}
		}
		for _, finish := range needs.DesiredFinishes {
			if containsString(profile.FinishOptions, finish) {
				item.Score += 2
				item.Reasons = append(item.Reasons, consultationReasonLabel(finish))
			}
		}
		if len(allowed) > 0 && item.Score == 0 {
			item.Score = 1
			item.Reasons = append(item.Reasons, "matches the services you asked to compare")
		}
		if item.Score > 0 {
			item.Reasons = appendUniqueStrings(nil, item.Reasons...)
			ranked = append(ranked, item)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return strings.ToLower(ranked[i].Service.Name) < strings.ToLower(ranked[j].Service.Name)
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func containsAnyString(values []string, targets []string) bool {
	for _, target := range targets {
		if containsString(values, target) {
			return true
		}
	}
	return false
}

func consultationReasonLabel(value string) string {
	switch value {
	case ConsultationOutcomeShorten:
		return "supports shortening the current length"
	case ConsultationOutcomeAddLength:
		return "supports adding length"
	case ConsultationOutcomeAddStrength:
		return "supports added strength"
	case ConsultationOutcomeRemoval:
		return "supports removal of the current product"
	case ConsultationOutcomeColorRefresh:
		return "supports a color refresh"
	case ConsultationOutcomeRepair:
		return "supports repair"
	case ConsultationOutcomeMaintain:
		return "supports maintenance of the current set"
	case ConsultationPriorityDurability:
		return "fits a durability priority"
	case ConsultationPriorityLowerMaintenance:
		return "fits a lower-maintenance priority"
	case ConsultationPriorityLowerCost:
		return "fits a lower-cost priority"
	case ConsultationPriorityShorterVisit:
		return "fits a shorter-visit priority"
	case ConsultationFinishNatural:
		return "supports a natural finish"
	case ConsultationFinishRegularPolish:
		return "supports regular polish"
	case ConsultationFinishGelPolish:
		return "supports gel polish"
	case ConsultationFinishGlossy:
		return "supports a glossy finish"
	case ConsultationFinishMatte:
		return "supports a matte finish"
	case ConsultationFinishNailArt:
		return "supports nail art"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}

func consultationRecommendationReply(ranked []consultationRankedService) string {
	parts := make([]string, 0, len(ranked))
	for _, item := range ranked {
		detail := consultationApprovedSummary(item.Service)
		if profile := item.Service.ConsultationProfile; profile != nil && containsString(item.Reasons, consultationReasonLabel(ConsultationPriorityLowerMaintenance)) {
			if note := strings.TrimSpace(profile.MaintenanceNote); note != "" {
				if detail != "" {
					detail = strings.TrimSuffix(detail, ".") + ". Upkeep: " + note
				} else {
					detail = "Upkeep: " + note
				}
			}
		}
		if detail != "" {
			parts = append(parts, strings.TrimSpace(item.Service.Name)+": "+strings.TrimSuffix(detail, ".")+".")
			continue
		}
		parts = append(parts, strings.TrimSpace(item.Service.Name)+" is a fit because it "+strings.TrimSuffix(strings.Join(item.Reasons, " and "), ".")+".")
	}
	if len(ranked) == 1 {
		return strings.Join(parts, " ") + " Would you like help booking it?"
	}
	return strings.Join(parts, " ") + " Which one sounds better for you?"
}

func consultationComparisonReply(candidates []ServiceOption) string {
	parts := make([]string, 0, len(candidates))
	for _, service := range candidates {
		facts := make([]string, 0, 4)
		if summary := consultationApprovedSummary(service); summary != "" {
			facts = append(facts, summary)
		}
		if service.DurationMinutes > 0 {
			facts = append(facts, fmt.Sprintf("%d minutes", service.DurationMinutes))
		}
		if price := consultationPrice(service); price != "" {
			facts = append(facts, price)
		}
		if len(facts) == 0 {
			facts = append(facts, "the salon has not added consultation details yet")
		}
		parts = append(parts, strings.TrimSpace(service.Name)+": "+strings.Join(facts, ", ")+".")
	}
	return strings.Join(parts, " ") + " Which service would you like?"
}

func consultationApprovedSummary(service ServiceOption) string {
	if service.ConsultationProfile != nil {
		if summary := strings.TrimSpace(service.ConsultationProfile.OwnerApprovedSummary); summary != "" {
			return truncateRunes(summary, 320)
		}
	}
	if description := strings.TrimSpace(service.AIDescription); description != "" {
		return truncateRunes(description, 320)
	}
	return truncateRunes(strings.TrimSpace(service.Description), 320)
}

func consultationPrice(service ServiceOption) string {
	if display := strings.TrimSpace(service.PriceDisplay); display != "" {
		return display
	}
	if service.PriceFrom > 0 {
		return fmt.Sprintf("from $%.2f", service.PriceFrom)
	}
	return ""
}

func setConsultationMetadata(turn *TurnRecord, state *ConsultationState, services []ServiceOption) {
	if turn == nil || state == nil {
		return
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"consultation_status":                  state.Status,
		"consultation_candidate_ids":           append([]string(nil), state.CandidateServiceIDs...),
		"consultation_recommended_service_ids": append([]string(nil), state.RecommendedServiceIDs...),
		"consultation_selected_service_id":     state.SelectedServiceID,
		"consultation_profile_revisions":       cloneStringIntMap(state.ProfileRevisions),
		"consultation_recommendation_reasons":  cloneStringSliceMap(state.RecommendationReasons),
		"pending_consultation_candidate_ids":   append([]string(nil), state.RecommendedServiceIDs...),
		"pending_consultation_candidates":      serviceCandidateNames(servicesByIDs(services, state.RecommendedServiceIDs), consultationCandidateLimit),
	})
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"consultation_current_system":   state.Needs.CurrentSystem,
		"consultation_desired_outcome":  state.Needs.DesiredOutcome,
		"consultation_length_change":    state.Needs.LengthChange,
		"consultation_priorities":       append([]string(nil), state.Needs.Priorities...),
		"consultation_desired_finishes": append([]string(nil), state.Needs.DesiredFinishes...),
	})
}

func clearPendingConsultationMetadata(turn *TurnRecord, reason string) {
	if turn == nil {
		return
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_consultation_cleared": true,
		"consultation_clear_reason":    strings.TrimSpace(reason),
	})
}

func pendingConsultationServices(session Session, services []ServiceOption) []ServiceOption {
	state := normalizedDialogState(session.DialogState)
	if state.Consultation != nil {
		if consultationStateActive(state.Consultation) {
			ids := state.Consultation.RecommendedServiceIDs
			if len(ids) == 0 {
				ids = state.Consultation.CandidateServiceIDs
			}
			return servicesByIDs(services, ids)
		}
		return nil
	}
	for index := len(session.Transcript) - 1; index >= 0; index-- {
		message := session.Transcript[index]
		if message.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(message.Metadata, "pending_consultation_cleared") {
			return nil
		}
		ids := metadataStringSlice(message.Metadata, "pending_consultation_candidate_ids")
		if len(ids) > 0 {
			return servicesByIDs(services, ids)
		}
	}
	return nil
}
