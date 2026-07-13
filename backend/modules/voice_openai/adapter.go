package voice_openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type Adapter struct {
	cfg            config.OpenAIVoiceConfig
	configResolver OpenAIConfigResolver
	httpClient     *http.Client
}

type OpenAIConfigResolver interface {
	ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error)
}

func NewAdapter(cfg config.OpenAIVoiceConfig) *Adapter {
	return &Adapter{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Adapter) SetConfigResolver(resolver OpenAIConfigResolver) {
	a.configResolver = resolver
}

func (a *Adapter) Name() string {
	return voice.ProviderOpenAI
}

func (a *Adapter) Configured(ctx context.Context, salonID string) bool {
	cfg, enabled, err := a.configFor(ctx, salonID)
	return err == nil && enabled && strings.TrimSpace(cfg.APIKey) != ""
}

func (a *Adapter) ContentType() string {
	return "audio/mpeg"
}

func (a *Adapter) Transcribe(ctx context.Context, salonID string, req voice.SpeechToTextRequest) (string, error) {
	cfg, enabled, err := a.configFor(ctx, salonID)
	if err != nil {
		return "", err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.TranscriptionModel) == "" {
		return "", voice.ErrProviderDisabled
	}
	if len(req.Audio) == 0 {
		return "", voice.ErrValidation
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", strings.TrimSpace(cfg.TranscriptionModel)); err != nil {
		return "", err
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return "", err
		}
	}
	part, err := writer.CreateFormFile("file", filenameForContentType(req.ContentType))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(req.Audio); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(cfg, "/audio/transcriptions"), &body)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	a.authorize(httpReq, cfg)

	var res transcriptionResponse
	if err := a.do(httpReq, &res); err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}

func (a *Adapter) GenerateReply(ctx context.Context, req voice.ModelRequest) (voice.ModelReply, error) {
	cfg, enabled, err := a.configFor(ctx, req.SalonID)
	if err != nil {
		return voice.ModelReply{}, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ReplyModel) == "" {
		return voice.ModelReply{}, voice.ErrProviderDisabled
	}
	payload := responseRequest{
		Model: strings.TrimSpace(cfg.ReplyModel),
		Instructions: strings.Join([]string{
			"You are the AI phone receptionist for a US nail salon.",
			"Rewrite only the safe_reply into a concise spoken response.",
			"Ask at most one question.",
			"Do not ask for booking fields listed in known_booking_fields.",
			"If next_required_field is set, keep the response focused on that field.",
			"Keep every selected_service_names item that appears in safe_reply; do not swap, omit, or add booking services.",
			"Do not invent prices or policies.",
			"Use knowledge_context only when it is relevant to the customer's question.",
			toneInstruction(req.AITone),
			"Do not add casual openers like Hey, Hi there, Awesome choice, or Great choice.",
			"Do not say an appointment is confirmed unless booking_confirmed is true.",
			"Do not mention POS providers, Square, Square Appointments, POS, or provider names in customer-facing replies.",
			"For human requests, complaints, refunds, payment disputes, low confidence, or complex group bookings, route to the owner.",
			"Return strict JSON with message, confidence, handoff, and reason.",
		}, "\n"),
		Input: modelInput(req),
		Text: responseTextFormat{
			Format: responseFormat{
				Type: "json_schema",
				Name: "voice_reply",
				Schema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"message":    map[string]any{"type": "string"},
						"confidence": map[string]any{"type": "number"},
						"handoff":    map[string]any{"type": "boolean"},
						"reason":     map[string]any{"type": "string"},
					},
					"required": []string{"message", "confidence", "handoff", "reason"},
				},
				Strict: true,
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return voice.ModelReply{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(cfg, "/responses"), bytes.NewReader(raw))
	if err != nil {
		return voice.ModelReply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	a.authorize(httpReq, cfg)

	var res responseResponse
	if err := a.do(httpReq, &res); err != nil {
		return voice.ModelReply{}, err
	}
	text := strings.TrimSpace(res.OutputText)
	if text == "" {
		text = strings.TrimSpace(res.firstText())
	}
	if text == "" {
		return voice.ModelReply{}, errors.New("openai response has no text")
	}
	var parsed voice.ModelReply
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		parsed = voice.ModelReply{Message: text, Confidence: 0.5}
	}
	parsed.Message = strings.TrimSpace(parsed.Message)
	parsed.Reason = strings.TrimSpace(parsed.Reason)
	return parsed, nil
}

func (a *Adapter) InterpretTurn(ctx context.Context, req voice.TurnModelRequest) (voice.TurnModelReply, error) {
	cfg, enabled, err := a.configFor(ctx, req.SalonID)
	if err != nil {
		return voice.TurnModelReply{}, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ReplyModel) == "" {
		return voice.TurnModelReply{}, voice.ErrProviderDisabled
	}
	payload := responseRequest{
		Model: strings.TrimSpace(cfg.ReplyModel),
		Instructions: strings.Join([]string{
			"Interpret one caller turn for a US nail salon receptionist.",
			"expected_input describes the state-owned field or decision currently awaiting an answer; use it as context, not as a closed vocabulary.",
			"Return structured operations and questions only; never write a customer-facing reply.",
			"A turn may contain multiple operations and questions. Preserve their spoken order.",
			"Use only service and category IDs present in catalog_services.",
			"Use only staff IDs present in catalog_staff.",
			"Distinguish the salon catalog from the caller's current booking draft.",
			"For replacements, preserve source (what is being replaced) separately from target (the new service).",
			"Use guest_scope=another_guest only when the caller explicitly assigns the added service to another person.",
			"For an existing party plan, set guest_ref to an exact current_draft.party_groups guest_ref for every service mutation; never flatten guest groups.",
			"A pending clarification is context, not a restriction: a clearly different new target may supersede it.",
			"For an initial concrete service selection, emit add_service with entity=service and the catalog target ID.",
			"Represent questions about the current draft as questions with subject=current_booking.",
			"For availability constraints, set time_preference.direction to before, after, or exact and time_preference.minutes to salon-local minutes after midnight. Use direction empty and minutes -1 when no time constraint is present.",
			"A final-review acceptance may coexist with a correction; include both and never suppress the correction.",
			"Use set_field or clear_field for staff, date/time, guest, or customer-field corrections.",
			"Do not infer booking confirmation, availability, customer identity, prices, or policy.",
			"Return strict JSON matching the schema.",
		}, "\n"),
		Input: turnModelInput(req),
		Text: responseTextFormat{Format: responseFormat{
			Type:   "json_schema",
			Name:   "turn_understanding",
			Schema: turnUnderstandingSchema(),
			Strict: true,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return voice.TurnModelReply{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(cfg, "/responses"), bytes.NewReader(raw))
	if err != nil {
		return voice.TurnModelReply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	a.authorize(httpReq, cfg)

	var res responseResponse
	if err := a.do(httpReq, &res); err != nil {
		return voice.TurnModelReply{}, err
	}
	text := strings.TrimSpace(res.OutputText)
	if text == "" {
		text = strings.TrimSpace(res.firstText())
	}
	if text == "" {
		return voice.TurnModelReply{}, fmt.Errorf("%w: openai response has no text", voice.ErrTurnModelEmptyOutput)
	}
	var parsed voice.TurnModelReply
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return voice.TurnModelReply{}, fmt.Errorf("%w: %v", voice.ErrTurnModelInvalidOutput, err)
	}
	return parsed, nil
}

func (a *Adapter) Synthesize(ctx context.Context, salonID string, text string, requestedVoice string) ([]byte, error) {
	cfg, enabled, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.SpeechModel) == "" {
		return nil, voice.ErrProviderDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, voice.ErrValidation
	}
	voiceName := strings.TrimSpace(requestedVoice)
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.SpeechVoice)
	}
	if voiceName == "" {
		return nil, voice.ErrValidation
	}
	payload := map[string]any{
		"model":           strings.TrimSpace(cfg.SpeechModel),
		"voice":           voiceName,
		"input":           text,
		"response_format": "mp3",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(cfg, "/audio/speech"), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	a.authorize(req, cfg)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("openai speech failed with status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 10*1024*1024))
}

func (a *Adapter) do(req *http.Request, output any) error {
	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("openai request failed with status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(output)
}

func (a *Adapter) authorize(req *http.Request, cfg config.OpenAIVoiceConfig) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
}

func (a *Adapter) url(cfg config.OpenAIVoiceConfig, path string) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return base + path
}

func (a *Adapter) configFor(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	if a.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return a.cfg, true, nil
	}
	return a.configResolver.ResolveOpenAIConfig(ctx, salonID)
}

func modelInput(req voice.ModelRequest) string {
	raw, _ := json.Marshal(map[string]any{
		"salon_id":               req.SalonID,
		"session_id":             req.SessionID,
		"channel":                req.Channel,
		"intent":                 req.Intent,
		"outcome":                req.Outcome,
		"customer_message":       req.CustomerMessage,
		"safe_reply":             req.SafeReply,
		"salon_name":             req.SalonName,
		"ai_tone":                normalizedAITone(req.AITone),
		"booking_confirmed":      req.BookingConfirmed,
		"fallback_or_handoff":    req.FallbackOrHandoff,
		"missing_booking_field":  req.MissingBookingField,
		"known_booking_fields":   req.KnownBookingFields,
		"next_required_field":    req.NextRequiredField,
		"selected_service_names": req.SelectedServiceNames,
		"summary":                req.Summary,
		"knowledge_context":      req.KnowledgeContext,
		"reply_policy":           req.ReplyPolicy,
	})
	return string(raw)
}

func turnUnderstandingSchema() map[string]any {
	act := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"kind": map[string]any{"type": "string", "enum": []string{
				conversation.ConversationActUnknown, conversation.ConversationActAdd, conversation.ConversationActReplace,
				conversation.ConversationActRemove, conversation.ConversationActUndo, conversation.ConversationActSummarize,
				conversation.ConversationActReview, conversation.ConversationActSet, conversation.ConversationActClear,
			}},
			"entity": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConversationEntityService, conversation.ConversationEntityStaff,
				conversation.ConversationEntityDateTime, conversation.ConversationEntityGuest, conversation.ConversationEntityCustomer,
			}},
			"source_ids":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"target_ids":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"source_category_id":   map[string]any{"type": "string"},
			"source_category_name": map[string]any{"type": "string"},
			"target_category_id":   map[string]any{"type": "string"},
			"target_category_name": map[string]any{"type": "string"},
			"scope": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConversationScopeOne, conversation.ConversationScopeAllMatching, conversation.ConversationScopeAll,
			}},
			"guest_scope": map[string]any{"type": "string", "enum": []string{"", conversation.ConversationGuestCaller, conversation.ConversationGuestAnother}},
			"guest_ref":   map[string]any{"type": "string"},
			"subject":     map[string]any{"type": "string"},
			"value":       map[string]any{"type": "string"},
			"count":       map[string]any{"type": "integer", "minimum": 0, "maximum": 20},
			"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":      map[string]any{"type": "string"},
		},
		"required": []string{
			"kind", "entity", "source_ids", "target_ids", "source_category_id", "source_category_name",
			"target_category_id", "target_category_name", "scope", "guest_scope", "guest_ref", "subject", "value", "count", "confidence", "reason",
		},
	}
	question := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"subject": map[string]any{"type": "string", "enum": []string{
				conversation.ConversationQuestionCurrentBooking, conversation.ConversationQuestionCatalog,
				conversation.ConversationQuestionAvailability, conversation.ConversationQuestionPrice,
				conversation.ConversationQuestionHours, conversation.ConversationQuestionStaff, conversation.ConversationQuestionPolicy,
			}},
			"service_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"staff_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"time_preference": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"direction": map[string]any{"type": "string", "enum": []string{"", conversation.TimePreferenceBefore, conversation.TimePreferenceAfter, conversation.TimePreferenceExact}},
					"minutes":   map[string]any{"type": "integer", "minimum": -1, "maximum": 1439},
				},
				"required": []string{"direction", "minutes"},
			},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"subject", "service_ids", "staff_ids", "time_preference", "confidence", "reason"},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"goal": map[string]any{"type": "string", "enum": []string{
				"unknown", "book_appointment", "reschedule_appointment", "cancel_appointment", "consultation", "information", "human_handoff",
			}},
			"acts":       map[string]any{"type": "array", "items": act},
			"questions":  map[string]any{"type": "array", "items": question},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"goal", "acts", "questions", "confidence", "reason"},
	}
}

func turnModelInput(req voice.TurnModelRequest) string {
	raw, _ := json.Marshal(map[string]any{
		"channel":               req.Channel,
		"customer_message":      req.CustomerMessage,
		"expected_input":        req.ExpectedInput,
		"selected_services":     req.SelectedServices,
		"catalog_services":      req.CatalogServices,
		"selected_staff":        req.SelectedStaff,
		"catalog_staff":         req.CatalogStaff,
		"pending":               req.Pending,
		"current_booking_stage": req.CurrentBookingStage,
		"booking_action":        req.BookingAction,
		"current_draft":         req.CurrentDraft,
	})
	return string(raw)
}

func toneInstruction(tone string) string {
	switch normalizedAITone(tone) {
	case "natural_human":
		return "Use a natural, human spoken tone with light contractions and no scripted or robotic phrasing."
	case "friendly_young":
		return "Use a friendly, upbeat, younger-sounding tone while staying respectful, concise, and professional."
	case "concise_calm":
		return "Use a calm, concise tone with fewer filler words and direct next-step wording."
	default:
		return "Use a warm professional salon receptionist tone."
	}
}

func normalizedAITone(tone string) string {
	switch strings.TrimSpace(tone) {
	case "natural_human", "friendly_young", "concise_calm":
		return strings.TrimSpace(tone)
	default:
		return "professional_warm"
	}
}

func filenameForContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(contentType, "mpeg"), strings.Contains(contentType, "mp3"):
		return "speech.mp3"
	case strings.Contains(contentType, "webm"):
		return "speech.webm"
	default:
		return "speech.wav"
	}
}

type transcriptionResponse struct {
	Text string `json:"text"`
}

type responseRequest struct {
	Model        string             `json:"model"`
	Instructions string             `json:"instructions"`
	Input        string             `json:"input"`
	Text         responseTextFormat `json:"text"`
}

type responseTextFormat struct {
	Format responseFormat `json:"format"`
}

type responseFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type responseResponse struct {
	OutputText string           `json:"output_text"`
	Output     []responseOutput `json:"output"`
}

type responseOutput struct {
	Content []responseContent `json:"content"`
}

type responseContent struct {
	Text string `json:"text"`
}

func (r responseResponse) firstText() string {
	for _, output := range r.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				return content.Text
			}
		}
	}
	return ""
}
