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

	AliasStatusActive   = "active"
	AliasStatusArchived = "archived"

	AliasSourceOwner      = "owner"
	AliasSourceCorrection = "correction"
	AliasSourceImport     = "import"
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
	ImportKey string    `json:"-"`
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
	AppliedServiceAliasID  string    `json:"applied_service_alias_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type OwnerCorrectionInput struct {
	CallSessionID       string `json:"call_session_id"`
	TranscriptMessageID string `json:"transcript_message_id"`
	Correction          string `json:"correction"`
}

type ServiceAlias struct {
	ID              string    `json:"id"`
	SalonID         string    `json:"salon_id"`
	ServiceID       string    `json:"service_id"`
	ServiceName     string    `json:"service_name"`
	Alias           string    `json:"alias"`
	NormalizedAlias string    `json:"normalized_alias"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	Confidence      float64   `json:"confidence"`
	CorrectionID    string    `json:"correction_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ServiceAliasInput struct {
	ServiceID  string  `json:"service_id"`
	Alias      string  `json:"alias"`
	Source     string  `json:"source"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
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
