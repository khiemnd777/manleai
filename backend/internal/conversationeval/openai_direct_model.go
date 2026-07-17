package conversationeval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
	"github.com/manleai/ai-receptionist/modules/voice_openai"
)

type StrictOpenAIConfigResolver interface {
	ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error)
}

type OpenAIDirectModel struct {
	resolver StrictOpenAIConfigResolver
	adapter  *voice_openai.Adapter
	replies  *voice.GuardedReplyGenerator
	client   *http.Client
	usageMu  sync.Mutex
	usage    ModelUsage
	callMu   sync.Mutex
}

func NewOpenAIDirectModel(resolver StrictOpenAIConfigResolver) *OpenAIDirectModel {
	adapter := voice_openai.NewAdapter(config.OpenAIVoiceConfig{})
	adapter.SetConfigResolver(resolver)
	model := &OpenAIDirectModel{
		resolver: resolver, adapter: adapter, replies: voice.NewGuardedReplyGenerator(adapter),
		client: &http.Client{Timeout: 30 * time.Second},
	}
	adapter.SetUsageObserver(func(_ string, usage voice_openai.Usage) {
		model.usageMu.Lock()
		model.usage.Add(ModelUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens})
		model.usageMu.Unlock()
	})
	return model
}

func (m *OpenAIDirectModel) Identity(ctx context.Context, salonID string) (string, error) {
	if m == nil || m.resolver == nil {
		return "", errors.New("strict OpenAI resolver is required")
	}
	cfg, enabled, err := m.resolver.ResolveOpenAIConfig(ctx, salonID)
	if err != nil {
		return "", err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ReplyModel) == "" || strings.TrimSpace(cfg.BaseURL) == "" {
		return "", voice.ErrProviderDisabled
	}
	// Include the credential only inside the one-way identity digest so resume
	// cannot mix results produced by two different stored configurations. The
	// key itself is never returned, logged, or persisted.
	sum := sha256.Sum256([]byte(strings.TrimSpace(cfg.BaseURL) + "\x00" + strings.TrimSpace(cfg.ReplyModel) + "\x00" + strings.TrimSpace(cfg.APIKey)))
	return fmt.Sprintf("%s:%x", strings.TrimSpace(cfg.ReplyModel), sum[:6]), nil
}

func (m *OpenAIDirectModel) InterpretTurn(ctx context.Context, salonID string, request voice.SemanticEvaluationRequest) (voice.TurnModelReply, ModelUsage, error) {
	return captureOpenAIUsage(m, func() (voice.TurnModelReply, error) {
		return m.adapter.InterpretTurn(ctx, voice.TurnModelRequest{
			SalonID: salonID, SessionID: "direct-evaluation:" + request.ScenarioID, Channel: request.Channel,
			CustomerMessage: request.CustomerMessage, ExpectedInput: request.ExpectedInput, SemanticContract: request.SemanticContract,
			RecognizableGuidanceActions: append([]string(nil), request.RecognizableGuidanceActions...),
			SelectedServices:            append([]conversation.ConversationServiceRef(nil), request.SelectedServices...),
			CatalogServices:             append([]conversation.ConversationServiceRef(nil), request.CatalogServices...),
			CatalogServiceAliases:       append([]conversation.ConversationServiceAliasRef(nil), request.CatalogServiceAliases...),
			CatalogCategories:           append([]conversation.ConversationCategoryRef(nil), request.CatalogCategories...),
			SelectedStaff:               append([]conversation.ConversationStaffRef(nil), request.SelectedStaff...),
			CatalogStaff:                append([]conversation.ConversationStaffRef(nil), request.CatalogStaff...),
			Pending:                     request.Pending, CurrentBookingStage: request.CurrentBookingStage, BookingAction: request.BookingAction,
			CurrentDraft: request.CurrentDraft, Consultation: request.Consultation,
		})
	})
}

func (m *OpenAIDirectModel) GenerateReply(ctx context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, ModelUsage, error) {
	return captureOpenAIUsage(m, func() (conversation.ReplyGenerationResult, error) {
		return m.replies.GenerateEvaluationReply(ctx, request)
	})
}

func (m *OpenAIDirectModel) GenerateConsultationQuestion(ctx context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, ModelUsage, error) {
	return captureOpenAIUsage(m, func() (conversation.ReplyGenerationResult, error) {
		return m.replies.GenerateConsultationQuestion(ctx, request)
	})
}

func captureOpenAIUsage[T any](m *OpenAIDirectModel, call func() (T, error)) (T, ModelUsage, error) {
	var zero T
	if m == nil {
		return zero, ModelUsage{}, errors.New("OpenAI direct model is nil")
	}
	m.callMu.Lock()
	defer m.callMu.Unlock()
	m.usageMu.Lock()
	m.usage = ModelUsage{}
	m.usageMu.Unlock()
	value, err := call()
	m.usageMu.Lock()
	usage := m.usage
	m.usage = ModelUsage{}
	m.usageMu.Unlock()
	return value, usage, err
}

func (m *OpenAIDirectModel) ReviewReplies(ctx context.Context, salonID string, input DirectReviewInput) (DirectReviewRound, ModelUsage, error) {
	var empty DirectReviewRound
	if m == nil || m.resolver == nil || m.client == nil {
		return empty, ModelUsage{}, errors.New("OpenAI direct reviewer is not configured")
	}
	cfg, enabled, err := m.resolver.ResolveOpenAIConfig(ctx, salonID)
	if err != nil {
		return empty, ModelUsage{}, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ReplyModel) == "" {
		return empty, ModelUsage{}, voice.ErrProviderDisabled
	}
	reviewInput, err := json.Marshal(input)
	if err != nil {
		return empty, ModelUsage{}, err
	}
	payload := map[string]any{
		"model": strings.TrimSpace(cfg.ReplyModel),
		"instructions": strings.Join([]string{
			"Review up to five retained AI receptionist outputs from a US nail salon evaluation.",
			"Judge final_reply against customer_message, backend_safe_reply, backend_evidence, structured recognition, errors, and tool attempts.",
			"Score naturalness, catalog grounding, one-question compliance, booking safety, and caller usefulness from 1 to 5.",
			"Set passed to true if and only if every score is at least 4; otherwise set it to false and explain the concrete failures.",
			"Use backend_evidence.booking_confirmed as the booking fact. A missing provider booking ID is expected and safe when booking_confirmed is false; never penalize the reply merely because no ID exists or because recognition says booking_requested.",
			"Fail booking safety only if final_reply claims confirmation while backend_evidence.booking_confirmed is false, or if backend_evidence itself reports confirmation without a provider booking ID.",
			"When backend_evidence.handoff_mode is owner_request, the allowed operation is an asynchronous owner request, not a live transfer. Penalize wording that promises an immediate connection or asks the caller to hold.",
			"Fail naturalness and caller usefulness when final_reply exposes internal IDs, protocol labels, metadata keys, or machine-facing guest references.",
			"Compare structured consultation recognition with customer_message. Fail catalog grounding and caller usefulness when recognition invents a current system, desired outcome, length change, priority, finish, booking request, completion signal, or positive mutation that the caller did not state.",
			"Compare backend state before and after. Penalize replies that silently change an explicitly requested service, staff member, date, or time and then move to the next question without acknowledging the change.",
			"When backend_evidence has a time preference, compare every offered_slot_local_minutes value with time_preference_direction and time_preference_minutes. Fail caller usefulness if any offered time violates before, after, or exact in the recorded time_preference_timezone.",
			"Penalize needlessly reciting a full weekly schedule when the caller asks a narrower hours question that can be answered from structured hours.",
			"Every finding must identify a concrete phrase from final_reply or a concrete backend_evidence/error field. Do not invent missing requirements.",
			"Do not excuse empty, robotic, misleading, provider-exposing, or operationally useless replies.",
			"Use only scenario IDs present in the input. Return strict JSON.",
		}, "\n"),
		"input": string(reviewInput),
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "receptionist_reply_review", "strict": true,
			"schema": directReviewSchema(),
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return empty, ModelUsage{}, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return empty, ModelUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	res, err := m.client.Do(req)
	if err != nil {
		return empty, ModelUsage{}, &voice.ProviderRequestError{Provider: voice.ProviderOpenAI, Stage: "evaluation_review", Err: err}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024))
		return empty, ModelUsage{}, &voice.ProviderRequestError{
			Provider: voice.ProviderOpenAI, Stage: "evaluation_review", StatusCode: res.StatusCode,
			RequestID: strings.TrimSpace(res.Header.Get("x-request-id")), Err: fmt.Errorf("OpenAI evaluation review failed with status %d", res.StatusCode),
		}
	}
	var response directOpenAIResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 4*1024*1024)).Decode(&response); err != nil {
		return empty, ModelUsage{}, &voice.ProviderRequestError{Provider: voice.ProviderOpenAI, Stage: "evaluation_review_decode", RequestID: strings.TrimSpace(res.Header.Get("x-request-id")), Err: err}
	}
	text := strings.TrimSpace(response.OutputText)
	if text == "" {
		text = response.firstText()
	}
	if text == "" {
		return empty, response.Usage, errors.New("OpenAI evaluation review returned no text")
	}
	if err := json.Unmarshal([]byte(text), &empty); err != nil {
		return DirectReviewRound{}, response.Usage, err
	}
	if err := validateDirectReview(input, empty); err != nil {
		return DirectReviewRound{}, response.Usage, err
	}
	return empty, response.Usage, nil
}

func directReviewSchema() map[string]any {
	scoreProperties := map[string]any{}
	for _, field := range []string{"naturalness", "catalog_grounding", "one_question_rule", "booking_safety", "caller_usefulness"} {
		scoreProperties[field] = map[string]any{"type": "integer", "minimum": 1, "maximum": 5}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"round":        map[string]any{"type": "integer", "minimum": 1},
			"scenario_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"passed":       map[string]any{"type": "boolean"},
			"scores": map[string]any{
				"type": "object", "additionalProperties": false, "properties": scoreProperties,
				"required": []string{"naturalness", "catalog_grounding", "one_question_rule", "booking_safety", "caller_usefulness"},
			},
			"findings": map[string]any{
				"type": "array", "items": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"scenario_id": map[string]any{"type": "string"}, "dimension": map[string]any{"type": "string"},
						"problem": map[string]any{"type": "string"}, "recommendation": map[string]any{"type": "string"},
					},
					"required": []string{"scenario_id", "dimension", "problem", "recommendation"},
				},
			},
			"summary": map[string]any{"type": "string"},
		},
		"required": []string{"round", "scenario_ids", "passed", "scores", "findings", "summary"},
	}
}

func validateDirectReview(input DirectReviewInput, review DirectReviewRound) error {
	allowed := map[string]bool{}
	for _, result := range input.Results {
		allowed[result.ScenarioID] = true
	}
	if review.Round != input.Round || len(review.ScenarioIDs) != len(input.Results) || strings.TrimSpace(review.Summary) == "" {
		return errors.New("review output does not cover the assigned round")
	}
	seen := map[string]bool{}
	for _, scenarioID := range review.ScenarioIDs {
		if !allowed[scenarioID] || seen[scenarioID] {
			return errors.New("review output contains an unknown or duplicate scenario ID")
		}
		seen[scenarioID] = true
	}
	for _, finding := range review.Findings {
		if !allowed[finding.ScenarioID] {
			return errors.New("review finding references an unknown scenario ID")
		}
	}
	for _, score := range []int{review.Scores.Naturalness, review.Scores.CatalogGrounding, review.Scores.OneQuestionRule, review.Scores.BookingSafety, review.Scores.CallerUsefulness} {
		if score < 1 || score > 5 {
			return errors.New("review score is outside 1..5")
		}
	}
	if review.Passed != directReviewPassed(review.Scores) {
		return errors.New("review passed flag is inconsistent with the minimum score gate")
	}
	return nil
}

func directReviewPassed(scores DirectReviewScores) bool {
	return scores.Naturalness >= 4 && scores.CatalogGrounding >= 4 && scores.OneQuestionRule >= 4 &&
		scores.BookingSafety >= 4 && scores.CallerUsefulness >= 4
}

type directOpenAIResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage ModelUsage `json:"usage"`
}

func (r directOpenAIResponse) firstText() string {
	for _, output := range r.Output {
		for _, content := range output.Content {
			if text := strings.TrimSpace(content.Text); text != "" {
				return text
			}
		}
	}
	return ""
}
