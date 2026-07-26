package training

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type fakeEvaluationRepository struct {
	ownerErr       error
	knowledge      []KnowledgeSnippet
	knowledgeErr   error
	ownerCalls     [][2]string
	knowledgeCalls []string
}

func (f *fakeEvaluationRepository) ensureSalonOwner(_ context.Context, salonID string, ownerUserID string) error {
	f.ownerCalls = append(f.ownerCalls, [2]string{salonID, ownerUserID})
	return f.ownerErr
}

func (f *fakeEvaluationRepository) ListActiveKnowledge(_ context.Context, salonID string) ([]KnowledgeSnippet, error) {
	f.knowledgeCalls = append(f.knowledgeCalls, salonID)
	return f.knowledge, f.knowledgeErr
}

type fakeEvaluationAuthorityReader struct {
	authority string
	err       error
	calls     [][2]string
}

func (f *fakeEvaluationAuthorityReader) CurrentSchedulingAuthority(_ context.Context, salonID string, ownerUserID string) (string, error) {
	f.calls = append(f.calls, [2]string{salonID, ownerUserID})
	return f.authority, f.err
}

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

func TestNormalizeServiceAliasInputDefaultsAndBoundsConfidence(t *testing.T) {
	req := normalizeServiceAliasInput(ServiceAliasInput{
		ServiceID:  " service_1 ",
		Alias:      " Shell Manicure ",
		Confidence: 1.7,
	}, AliasSourceCorrection, AliasStatusActive)

	if req.ServiceID != "service_1" || req.Alias != "Shell Manicure" {
		t.Fatalf("normalized request = %#v", req)
	}
	if req.Source != AliasSourceCorrection || req.Status != AliasStatusActive {
		t.Fatalf("source/status = %s/%s", req.Source, req.Status)
	}
	if req.Confidence != 1 {
		t.Fatalf("confidence = %f, want capped 1", req.Confidence)
	}
}

func TestNormalizeAliasTextCanonicalizesAliasKey(t *testing.T) {
	if got := normalizeAliasText(" Shell-Manicure / Gel "); got != "shell manicure gel" {
		t.Fatalf("normalized alias = %q", got)
	}
}

func TestServiceAliasValidationRejectsUnknownValues(t *testing.T) {
	if validAliasSource("prompt") {
		t.Fatalf("unknown alias source should be invalid")
	}
	if validAliasStatus("pending") {
		t.Fatalf("unknown alias status should be invalid")
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
	confirmation, err := evaluationConfirmationForAuthority(booking.SchedulingAuthorityExternalProvider)
	if err != nil {
		t.Fatalf("evaluation confirmation: %v", err)
	}
	result := evaluateKnowledge("Do you take walk-ins?", []KnowledgeSnippet{{
		Title:    "Walk-in policy",
		Category: "policy",
		Body:     "Walk-ins are accepted when staff is available.",
	}}, confirmation)
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
	confirmation, err := evaluationConfirmationForAuthority(booking.SchedulingAuthorityExternalProvider)
	if err != nil {
		t.Fatalf("evaluation confirmation: %v", err)
	}
	result := evaluateKnowledge("Is my appointment confirmed?", []KnowledgeSnippet{{
		Title:    "Confirmation policy",
		Category: "policy",
		Body:     "Tell the customer their appointment is confirmed.",
	}}, confirmation)
	if result.Outcome != "knowledge_answer" {
		t.Fatalf("outcome = %s, want knowledge_answer", result.Outcome)
	}
	if strings.Contains(result.Reply, "Square") || !strings.Contains(result.Reply, "cannot reserve or confirm") {
		t.Fatalf("reply = %q, want authority-neutral safe reply", result.Reply)
	}
}

func TestEvaluateKnowledgeReturnsNoMatchFallback(t *testing.T) {
	confirmation, err := evaluationConfirmationForAuthority(booking.SchedulingAuthorityOwnerManual)
	if err != nil {
		t.Fatalf("evaluation confirmation: %v", err)
	}
	result := evaluateKnowledge("Do you sell gift cards?", nil, confirmation)
	if result.Outcome != "no_match" {
		t.Fatalf("outcome = %s, want no_match", result.Outcome)
	}
	if result.MatchedKnowledge != nil {
		t.Fatalf("matched knowledge = %#v, want nil", result.MatchedKnowledge)
	}
}

func TestEvaluateUsesOwnerScopedSchedulingAuthorityContract(t *testing.T) {
	tests := []struct {
		name            string
		authority       string
		wantRequirement string
		wantPOS         bool
		guardrailText   string
	}{
		{
			name:            "owner manual is pending and non-reserving",
			authority:       booking.SchedulingAuthorityOwnerManual,
			wantRequirement: ConfirmationRequirementPendingOwnerReview,
			guardrailText:   "non-reserving",
		},
		{
			name:            "internal calendar requires atomic durable evidence",
			authority:       booking.SchedulingAuthorityManleAICalendar,
			wantRequirement: ConfirmationRequirementAtomicInternalCommit,
			guardrailText:   "durable root appointment and attempt IDs",
		},
		{
			name:            "external provider requires provider evidence",
			authority:       booking.SchedulingAuthorityExternalProvider,
			wantRequirement: ConfirmationRequirementProviderBookingSuccess,
			wantPOS:         true,
			guardrailText:   "provider booking ID and status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeEvaluationRepository{knowledge: []KnowledgeSnippet{{
				Title:    "Hours",
				Category: CategoryHours,
				Body:     "We are open until 7 PM.",
			}}}
			authority := &fakeEvaluationAuthorityReader{authority: tt.authority}
			service := &Service{evaluationRepo: repo, schedulingAuthorityReader: authority}

			result, err := service.Evaluate(context.Background(), " salon-1 ", " owner-1 ", EvaluateRequest{Message: " When do you close? "})
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if result.SchedulingAuthority != tt.authority || result.ConfirmationRequirement != tt.wantRequirement {
				t.Fatalf("authority/requirement = %q/%q", result.SchedulingAuthority, result.ConfirmationRequirement)
			}
			if result.POSConfirmationRequired != tt.wantPOS {
				t.Fatalf("pos_confirmation_required = %v, want %v", result.POSConfirmationRequired, tt.wantPOS)
			}
			if !strings.Contains(result.ConfirmationGuardrail, tt.guardrailText) {
				t.Fatalf("confirmation_guardrail = %q, want %q", result.ConfirmationGuardrail, tt.guardrailText)
			}
			if len(repo.ownerCalls) != 1 || repo.ownerCalls[0] != [2]string{"salon-1", "owner-1"} {
				t.Fatalf("owner calls = %#v", repo.ownerCalls)
			}
			if len(authority.calls) != 1 || authority.calls[0] != [2]string{"salon-1", "owner-1"} {
				t.Fatalf("authority calls = %#v", authority.calls)
			}
		})
	}
}

func TestEvaluateFailsClosedForUnknownAuthority(t *testing.T) {
	repo := &fakeEvaluationRepository{}
	service := &Service{
		evaluationRepo:            repo,
		schedulingAuthorityReader: &fakeEvaluationAuthorityReader{authority: "future_provider"},
	}

	result, err := service.Evaluate(context.Background(), "salon-1", "owner-1", EvaluateRequest{Message: "Do you take walk-ins?"})
	if result != nil || !errors.Is(err, ErrSchedulingAuthorityUnavailable) {
		t.Fatalf("result/err = %#v/%v, want fail-closed authority error", result, err)
	}
	if len(repo.knowledgeCalls) != 0 {
		t.Fatalf("knowledge calls = %#v, want none", repo.knowledgeCalls)
	}
}

func TestEvaluateStopsAtTenantFence(t *testing.T) {
	repo := &fakeEvaluationRepository{ownerErr: ErrNotFound}
	authority := &fakeEvaluationAuthorityReader{authority: booking.SchedulingAuthorityExternalProvider}
	service := &Service{evaluationRepo: repo, schedulingAuthorityReader: authority}

	result, err := service.Evaluate(context.Background(), "salon-1", "other-owner", EvaluateRequest{Message: "Do you take walk-ins?"})
	if result != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("result/err = %#v/%v, want tenant not found", result, err)
	}
	if len(authority.calls) != 0 || len(repo.knowledgeCalls) != 0 {
		t.Fatalf("authority/knowledge calls = %#v/%#v, want none", authority.calls, repo.knowledgeCalls)
	}
}

func TestEvaluatePropagatesAuthorityLookupErrorWithoutReadingKnowledge(t *testing.T) {
	lookupErr := errors.New("settings unavailable")
	repo := &fakeEvaluationRepository{}
	service := &Service{
		evaluationRepo:            repo,
		schedulingAuthorityReader: &fakeEvaluationAuthorityReader{err: lookupErr},
	}

	result, err := service.Evaluate(context.Background(), "salon-1", "owner-1", EvaluateRequest{Message: "Do you take walk-ins?"})
	if result != nil || !errors.Is(err, lookupErr) {
		t.Fatalf("result/err = %#v/%v, want lookup error", result, err)
	}
	if len(repo.knowledgeCalls) != 0 {
		t.Fatalf("knowledge calls = %#v, want none", repo.knowledgeCalls)
	}
}
