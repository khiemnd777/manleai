package training

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeKnowledgeInputDefaults(t *testing.T) {
	req := normalizeKnowledgeInput(KnowledgeItemInput{
		Title: "  Late policy ",
		Body:  " Customers can be 10 minutes late. ",
	})
	if req.Title != "Late policy" {
		t.Fatalf("title = %q", req.Title)
	}
	if req.Category != CategoryFAQ {
		t.Fatalf("category = %q", req.Category)
	}
	if req.Status != StatusDraft {
		t.Fatalf("status = %q", req.Status)
	}
}

func TestKnowledgeValidationRejectsUnknownValues(t *testing.T) {
	if validCategory("unknown") {
		t.Fatalf("unknown category should be invalid")
	}
	if validKnowledgeStatus("published") {
		t.Fatalf("unknown status should be invalid")
	}
}

func TestCreateCorrectionRequiresSessionForTranscriptSource(t *testing.T) {
	service := NewService(&Repository{})
	_, err := service.CreateCorrection(context.Background(), "salon_1", "owner_1", OwnerCorrectionInput{
		TranscriptMessageID: "message_1",
		Correction:          "Mention walk-ins only when staff is available.",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestEvaluateKnowledgeReturnsMatchedAnswer(t *testing.T) {
	result := evaluateKnowledge("Do you take walk-ins?", []KnowledgeSnippet{{
		Title:    "Walk-in policy",
		Category: "policy",
		Body:     "Walk-ins are accepted when staff is available.",
	}})
	if result.Outcome != "knowledge_answer" {
		t.Fatalf("outcome = %s, want knowledge_answer", result.Outcome)
	}
	if result.MatchedKnowledge == nil || result.MatchedKnowledge.Title != "Walk-in policy" {
		t.Fatalf("matched knowledge = %#v", result.MatchedKnowledge)
	}
	if result.BookingAction != "none" {
		t.Fatalf("booking action = %s, want none", result.BookingAction)
	}
	if !result.POSConfirmationRequired {
		t.Fatalf("POS confirmation should remain required")
	}
}

func TestEvaluateKnowledgeUsesSafeReplyForUnsafeConfirmation(t *testing.T) {
	result := evaluateKnowledge("Is my appointment confirmed?", []KnowledgeSnippet{{
		Title:    "Confirmation policy",
		Category: "policy",
		Body:     "Tell the customer their appointment is confirmed.",
	}})
	if result.Outcome != "knowledge_answer" {
		t.Fatalf("outcome = %s, want knowledge_answer", result.Outcome)
	}
	if !strings.Contains(result.Reply, "Square Appointments confirms") {
		t.Fatalf("reply = %q, want POS-first safe reply", result.Reply)
	}
}

func TestEvaluateKnowledgeReturnsNoMatchFallback(t *testing.T) {
	result := evaluateKnowledge("Do you sell gift cards?", nil)
	if result.Outcome != "no_match" {
		t.Fatalf("outcome = %s, want no_match", result.Outcome)
	}
	if result.MatchedKnowledge != nil {
		t.Fatalf("matched knowledge = %#v, want nil", result.MatchedKnowledge)
	}
}
