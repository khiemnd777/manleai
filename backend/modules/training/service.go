package training

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListKnowledge(ctx context.Context, salonID string, ownerUserID string) ([]KnowledgeItem, error) {
	return s.repo.ListKnowledge(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID))
}

func (s *Service) CreateKnowledge(ctx context.Context, salonID string, ownerUserID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	req = normalizeKnowledgeInput(req)
	if req.Title == "" || req.Body == "" || !validCategory(req.Category) || !validKnowledgeStatus(req.Status) {
		return nil, ErrValidation
	}
	return s.repo.CreateKnowledge(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), req)
}

func (s *Service) UpdateKnowledge(ctx context.Context, salonID string, ownerUserID string, itemID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	req = normalizeKnowledgeInput(req)
	if strings.TrimSpace(itemID) == "" || req.Title == "" || req.Body == "" || !validCategory(req.Category) || !validKnowledgeStatus(req.Status) {
		return nil, ErrValidation
	}
	return s.repo.UpdateKnowledge(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(itemID), req)
}

func (s *Service) DeleteKnowledge(ctx context.Context, salonID string, ownerUserID string, itemID string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ErrValidation
	}
	return s.repo.DeleteKnowledge(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), itemID)
}

func (s *Service) ListCorrections(ctx context.Context, salonID string, ownerUserID string) ([]OwnerCorrection, error) {
	return s.repo.ListCorrections(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID))
}

func (s *Service) CreateCorrection(ctx context.Context, salonID string, ownerUserID string, req OwnerCorrectionInput) (*OwnerCorrection, error) {
	req.CallSessionID = strings.TrimSpace(req.CallSessionID)
	req.TranscriptMessageID = strings.TrimSpace(req.TranscriptMessageID)
	req.Correction = strings.TrimSpace(req.Correction)
	if req.Correction == "" {
		return nil, ErrValidation
	}
	if req.TranscriptMessageID != "" && req.CallSessionID == "" {
		return nil, ErrValidation
	}
	return s.repo.CreateCorrection(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), req)
}

func (s *Service) ApplyCorrection(ctx context.Context, salonID string, ownerUserID string, correctionID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	correctionID = strings.TrimSpace(correctionID)
	req = normalizeKnowledgeInput(req)
	if correctionID == "" || req.Title == "" || req.Body == "" || !validCategory(req.Category) || !validKnowledgeStatus(req.Status) {
		return nil, ErrValidation
	}
	return s.repo.ApplyCorrection(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), correctionID, req)
}

func (s *Service) DismissCorrection(ctx context.Context, salonID string, ownerUserID string, correctionID string) (*OwnerCorrection, error) {
	correctionID = strings.TrimSpace(correctionID)
	if correctionID == "" {
		return nil, ErrValidation
	}
	return s.repo.UpdateCorrectionStatus(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), correctionID, CorrectionStatusDismissed)
}

func (s *Service) Evaluate(ctx context.Context, salonID string, ownerUserID string, req EvaluateRequest) (*EvaluateResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	message := strings.TrimSpace(req.Message)
	if salonID == "" || ownerUserID == "" || message == "" {
		return nil, ErrValidation
	}
	if err := s.repo.ensureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	knowledge, err := s.repo.ListActiveKnowledge(ctx, salonID)
	if err != nil {
		return nil, err
	}
	return evaluateKnowledge(message, knowledge), nil
}

func evaluateKnowledge(message string, knowledge []KnowledgeSnippet) *EvaluateResponse {
	match := bestKnowledgeMatch(message, knowledge)
	if match == nil || strings.TrimSpace(match.Body) == "" {
		return &EvaluateResponse{
			Message:                 message,
			Reply:                   "I do not have active salon knowledge for that yet. The owner can add a knowledge item or review this after a call.",
			Outcome:                 "no_match",
			BookingAction:           "none",
			POSConfirmationRequired: true,
		}
	}
	reply := truncateWords(match.Body, 34) + " Would you like help with an appointment?"
	if hasUnsafeKnowledgeConfirmation(match.Body) {
		reply = "I can share salon policies, but I cannot confirm appointments unless Square Appointments confirms the booking. Would you like help with an appointment?"
	}
	return &EvaluateResponse{
		Message:                 message,
		Reply:                   reply,
		MatchedKnowledge:        match,
		Outcome:                 "knowledge_answer",
		BookingAction:           "none",
		POSConfirmationRequired: true,
	}
}

func normalizeKnowledgeInput(req KnowledgeItemInput) KnowledgeItemInput {
	req.Title = strings.TrimSpace(req.Title)
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	req.Body = strings.TrimSpace(req.Body)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Category == "" {
		req.Category = CategoryFAQ
	}
	if req.Status == "" {
		req.Status = StatusDraft
	}
	return req
}

func validCategory(category string) bool {
	switch category {
	case CategoryFAQ, CategoryPolicy, CategoryServices, CategoryHours, CategoryHandoff, CategoryOperations:
		return true
	default:
		return false
	}
}

func validKnowledgeStatus(status string) bool {
	switch status {
	case StatusDraft, StatusActive, StatusArchived:
		return true
	default:
		return false
	}
}

func bestKnowledgeMatch(message string, knowledge []KnowledgeSnippet) *KnowledgeSnippet {
	lower := strings.ToLower(message)
	bestScore := 0
	var best *KnowledgeSnippet
	for i := range knowledge {
		item := knowledge[i]
		score := 0
		for _, token := range append(significantWords(item.Title), significantWords(item.Category)...) {
			if strings.Contains(lower, token) {
				score += 2
			}
		}
		for _, token := range significantWords(item.Body) {
			if strings.Contains(lower, token) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = &item
		}
	}
	if bestScore == 0 {
		return nil
	}
	return best
}

func significantWords(value string) []string {
	parts := strings.Fields(strings.ToLower(value))
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, " ,.;:/")
		if len(part) >= 4 {
			words = append(words, part)
		}
	}
	return words
}

func truncateWords(value string, maxWords int) string {
	words := strings.Fields(strings.TrimSpace(value))
	if len(words) <= maxWords {
		return strings.TrimSpace(value)
	}
	return strings.Join(words[:maxWords], " ") + "..."
}

func hasUnsafeKnowledgeConfirmation(value string) bool {
	lower := strings.ToLower(value)
	unsafeAlways := []string{
		"you are booked",
		"you're booked",
		"appointment is set",
		"all set for",
		"see you at",
	}
	for _, phrase := range unsafeAlways {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !strings.Contains(lower, "confirmed") {
		return false
	}
	for _, phrase := range []string{"not confirmed", "not a confirmed", "cannot confirm", "could not confirm", "not yet confirmed"} {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	return true
}

func formatKnowledgeContext(knowledge []KnowledgeSnippet) string {
	if len(knowledge) == 0 {
		return ""
	}
	lines := make([]string, 0, len(knowledge))
	for _, item := range knowledge {
		title := strings.TrimSpace(item.Title)
		body := truncateWords(item.Body, 40)
		if title == "" || body == "" {
			continue
		}
		category := strings.TrimSpace(item.Category)
		if category == "" {
			category = "knowledge"
		}
		lines = append(lines, fmt.Sprintf("%s: %s - %s", category, title, body))
	}
	return strings.Join(lines, "\n")
}
