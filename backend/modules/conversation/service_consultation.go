package conversation

import (
	"context"
	"fmt"
	"strings"
)

const consultationCandidateLimit = 3

func (s *Service) handleServiceConsultation(ctx context.Context, ownerUserID string, session Session, message, eventKey string, understanding serviceUnderstandingResult, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, *Session, error) {
	if shouldHandoff(message) || shouldComplaintHandoff(message) || shouldRouteCancel(session, message) || shouldRouteReschedule(session, message) || bookingActionForSession(session) != BookingActionBook || activePartyPlan(session.PartyPlan) {
		return false, nil, nil
	}
	if isConsultationSafetyConcern(message) {
		turn := newTurnRecord(session.SalonID, ownerUserID, session, session, message, eventKey, services, staff, cfg)
		updated, err := s.saveHandoffTurn(ctx, turn, session, HandoffReasonConsultationSafety, "For pain, injury, infection, allergy, or another health concern, the owner should help you directly. I cannot give medical advice, and this is not a confirmed appointment.", services, staff, cfg)
		return true, updated, err
	}

	pending := pendingConsultationServices(session, services)
	if len(pending) > 1 && isAffirmativeOnly(message) {
		turn := newTurnRecord(session.SalonID, ownerUserID, session, session, message, eventKey, services, staff, cfg)
		turn.AIMessage = "Which service would you like: " + joinHumanList(serviceCandidateNames(pending, consultationCandidateLimit)) + "?"
		setPendingConsultationMetadata(&turn, pending)
		finalizeTurnMetadata(&turn, session, session, "service", "service", "consultation_ambiguous_affirmative")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}

	if !isConsultationRequest(message, understanding, pending) {
		return false, nil, nil
	}
	candidates := consultationCandidates(message, understanding, services)
	if len(candidates) == 0 {
		candidates = pending
	}
	if len(candidates) == 0 {
		turn := newTurnRecord(session.SalonID, ownerUserID, session, session, message, eventKey, services, staff, cfg)
		turn.AIMessage = "I can compare bookable services using the salon's approved details. Which services are you considering?"
		finalizeTurnMetadata(&turn, session, session, "service", "service", "consultation_needs_candidates")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if len(candidates) > consultationCandidateLimit {
		candidates = candidates[:consultationCandidateLimit]
	}
	next := cloneSessionForTurn(session)
	next.Intent = IntentConsultation
	turn := newTurnRecord(session.SalonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	turn.AIMessage = consultationReply(candidates)
	setPendingConsultationMetadata(&turn, candidates)
	finalizeTurnMetadata(&turn, session, next, "service", "service", "service_consultation")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func isConsultationRequest(message string, understanding serviceUnderstandingResult, pending []ServiceOption) bool {
	normalized := normalizeLooseText(message)
	signals := []string{
		"help me choose", "not sure which", "which one should", "what do you recommend", "recommend a service",
		"compare", "difference between", "what is the difference", "which is better", "tell me about",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return len(pending) > 0 || understanding.Status != serviceUnderstandingStatusUnknown || mentionsGeneralServiceCatalog(normalized)
		}
	}
	return false
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

func consultationReply(candidates []ServiceOption) string {
	parts := make([]string, 0, len(candidates))
	for _, service := range candidates {
		facts := make([]string, 0, 4)
		if category := strings.TrimSpace(service.CategoryName); category != "" {
			facts = append(facts, category)
		}
		description := strings.TrimSpace(service.AIDescription)
		if description == "" {
			description = strings.TrimSpace(service.Description)
		}
		if description != "" {
			facts = append(facts, truncateRunes(description, 320))
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

func consultationPrice(service ServiceOption) string {
	if display := strings.TrimSpace(service.PriceDisplay); display != "" {
		return display
	}
	if service.PriceFrom > 0 {
		return fmt.Sprintf("from $%.2f", service.PriceFrom)
	}
	return ""
}

func setPendingConsultationMetadata(turn *TurnRecord, candidates []ServiceOption) {
	ids := make([]string, 0, len(candidates))
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, strings.TrimSpace(candidate.ID))
		names = append(names, strings.TrimSpace(candidate.Name))
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_consultation_candidate_ids": ids,
		"pending_consultation_candidates":    names,
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
