package training

import (
	"context"
	"errors"
	"time"
)

const (
	CategoryFAQ        = "faq"
	CategoryPolicy     = "policy"
	CategoryServices   = "services"
	CategoryHours      = "hours"
	CategoryHandoff    = "handoff"
	CategoryOperations = "operations"

	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusArchived = "archived"

	CorrectionStatusPending   = "pending"
	CorrectionStatusApplied   = "applied"
	CorrectionStatusDismissed = "dismissed"

	SourceOwner      = "owner"
	SourceCorrection = "correction"
)

var (
	ErrValidation = errors.New("training validation failed")
	ErrNotFound   = errors.New("training record not found")
)

type KnowledgeReader interface {
	ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error)
}

type KnowledgeSnippet struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Body     string `json:"body"`
}

type KnowledgeItem struct {
	ID        string    `json:"id"`
	SalonID   string    `json:"salon_id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type KnowledgeItemInput struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Body     string `json:"body"`
	Status   string `json:"status"`
}

type OwnerCorrection struct {
	ID                     string    `json:"id"`
	SalonID                string    `json:"salon_id"`
	CallSessionID          string    `json:"call_session_id,omitempty"`
	TranscriptMessageID    string    `json:"transcript_message_id,omitempty"`
	Correction             string    `json:"correction"`
	Status                 string    `json:"status"`
	AppliedKnowledgeItemID string    `json:"applied_knowledge_item_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type OwnerCorrectionInput struct {
	CallSessionID       string `json:"call_session_id"`
	TranscriptMessageID string `json:"transcript_message_id"`
	Correction          string `json:"correction"`
}

type EvaluateRequest struct {
	Message string `json:"message"`
}

type EvaluateResponse struct {
	Message                 string            `json:"message"`
	Reply                   string            `json:"reply"`
	MatchedKnowledge        *KnowledgeSnippet `json:"matched_knowledge,omitempty"`
	Outcome                 string            `json:"outcome"`
	BookingAction           string            `json:"booking_action"`
	POSConfirmationRequired bool              `json:"pos_confirmation_required"`
}
