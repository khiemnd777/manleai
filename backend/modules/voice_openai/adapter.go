package voice_openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type Adapter struct {
	cfg            config.OpenAIVoiceConfig
	configResolver OpenAIConfigResolver
	httpClient     *http.Client
	circuitMu      sync.Mutex
	turnCircuits   map[string]turnContractCircuit
}

type turnContractCircuit struct {
	configFingerprint string
	error             voice.ProviderRequestError
}

type OpenAIConfigResolver interface {
	ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error)
}

func NewAdapter(cfg config.OpenAIVoiceConfig) *Adapter {
	return &Adapter{
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		turnCircuits: map[string]turnContractCircuit{},
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
	if err := a.do(httpReq, &res, "transcription_response"); err != nil {
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
	instructions := []string{
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
	}
	if req.ReplyPolicy == conversation.ReplyPolicyConsultationQuestion {
		instructions = []string{
			"You are the AI phone receptionist for a US nail salon.",
			"Generate exactly one concise, natural spoken question from consultation_question.",
			"Ask only about consultation_question.field and use only its option values as facts.",
			"Do not recommend or name a service, invent suitability, price, policy, timing, or medical advice.",
			"Do not mention internal fields, profile revisions, IDs, providers, or structured data.",
			toneInstruction(req.AITone),
			"Return strict JSON with message, confidence, handoff, and reason.",
		}
	}
	payload := responseRequest{
		Model:        strings.TrimSpace(cfg.ReplyModel),
		Instructions: strings.Join(instructions, "\n"),
		Input:        modelInput(req),
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
	if err := a.do(httpReq, &res, "reply_response"); err != nil {
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
	return a.interpretTurn(ctx, req, false)
}

func (a *Adapter) CheckTurnContract(ctx context.Context, salonID string) (voice.TurnContractCheck, error) {
	req := voice.TurnModelRequest{
		SalonID:         strings.TrimSpace(salonID),
		Channel:         "semantic_contract_check",
		CustomerMessage: "Validate the structured turn contract without proposing any operation.",
		ExpectedInput:   "contract_validation",
	}
	reply, err := a.interpretTurn(ctx, req, true)
	_ = reply
	schemaFingerprint, fingerprintErr := structuredOutputSchemaFingerprint(turnUnderstandingSchema())
	if fingerprintErr != nil {
		return voice.TurnContractCheck{}, fingerprintErr
	}
	check := voice.TurnContractCheck{Provider: voice.ProviderOpenAI, SchemaFingerprint: schemaFingerprint}
	if err != nil {
		var providerErr *voice.ProviderRequestError
		if errors.As(err, &providerErr) {
			check.RequestID = strings.TrimSpace(providerErr.RequestID)
		}
		return check, err
	}
	return check, nil
}

func (a *Adapter) interpretTurn(ctx context.Context, req voice.TurnModelRequest, bypassCircuit bool) (voice.TurnModelReply, error) {
	cfg, enabled, err := a.configFor(ctx, req.SalonID)
	if err != nil {
		return voice.TurnModelReply{}, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ReplyModel) == "" {
		return voice.TurnModelReply{}, voice.ErrProviderDisabled
	}
	schema := turnUnderstandingSchema()
	if err := validateStructuredOutputSchema(schema); err != nil {
		return voice.TurnModelReply{}, fmt.Errorf("%w: %v", voice.ErrTurnModelInvalidOutput, err)
	}
	schemaFingerprint, err := structuredOutputSchemaFingerprint(schema)
	if err != nil {
		return voice.TurnModelReply{}, err
	}
	configFingerprint := turnContractConfigFingerprint(cfg, schemaFingerprint)
	if !bypassCircuit {
		if circuitErr := a.openTurnCircuitError(req.SalonID, configFingerprint); circuitErr != nil {
			return voice.TurnModelReply{}, circuitErr
		}
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
			"For a customer-name correction, emit set_field with entity=customer, subject=name, and value equal to the corrected name.",
			"For a request to use a different technician without naming one, emit set_field with entity=staff, subject=alternative, and no target ID; never choose a technician for the caller.",
			"Do not infer booking confirmation, availability, customer identity, prices, or policy.",
			"For consultation, extract only the caller's stated current nail system, desired outcome, length change, priorities, desired finishes, compared catalog service IDs, booking request, and whether the caller is done. Never recommend a service in model output.",
			"For every consultation preference stated or corrected in this turn, emit a field-level mutation. Use set for an initial value, replace for a correction, add or remove for list items, and clear only when the caller explicitly withdraws the field. Never treat a negated value as an addition.",
			"Classify safety.concern=true for pain, injury, infection, allergy, bleeding, swelling, adverse reaction, or a request for medical suitability or treatment advice. This safety classification applies regardless of the caller's booking or consultation goal.",
			"Use empty strings and arrays when consultation details are not present. Do not infer health suitability or treatment claims.",
			"Return strict JSON matching the schema.",
		}, "\n"),
		Input: turnModelInput(req),
		Text: responseTextFormat{Format: responseFormat{
			Type:   "json_schema",
			Name:   "turn_understanding",
			Schema: schema,
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
	if err := a.do(httpReq, &res, "turn_interpretation_response"); err != nil {
		var providerErr *voice.ProviderRequestError
		if errors.As(err, &providerErr) {
			providerErr.SchemaFingerprint = schemaFingerprint
			if isNonRetryableTurnContractFailure(providerErr) {
				a.openTurnCircuit(req.SalonID, configFingerprint, providerErr)
			}
		}
		return voice.TurnModelReply{}, err
	}
	a.clearTurnCircuit(req.SalonID)
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

func (a *Adapter) do(req *http.Request, output any, stage string) error {
	res, err := a.httpClient.Do(req)
	if err != nil {
		return &voice.ProviderRequestError{Provider: voice.ProviderOpenAI, Stage: stage, Err: err}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errorType, errorCode, errorParam := safeProviderErrorFields(res.Body)
		return &voice.ProviderRequestError{
			Provider: voice.ProviderOpenAI, Stage: stage, StatusCode: res.StatusCode,
			RequestID: firstNonEmptyHeader(res.Header, "x-request-id", "request-id"),
			ErrorType: errorType, ErrorCode: errorCode, ErrorParam: errorParam,
			Err: fmt.Errorf("openai request failed with status %d", res.StatusCode),
		}
	}
	if err := json.NewDecoder(res.Body).Decode(output); err != nil {
		return &voice.ProviderRequestError{
			Provider: voice.ProviderOpenAI, Stage: stage + "_decode",
			RequestID: firstNonEmptyHeader(res.Header, "x-request-id", "request-id"), Err: err,
		}
	}
	return nil
}

func safeProviderErrorFields(reader io.Reader) (string, string, string) {
	var envelope struct {
		Error struct {
			Type  string `json:"type"`
			Code  string `json:"code"`
			Param string `json:"param"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 64*1024))
	if err := decoder.Decode(&envelope); err != nil {
		return "", "", ""
	}
	return safeOpenAIErrorValue(envelope.Error.Type), safeOpenAIErrorValue(envelope.Error.Code), safeOpenAIErrorValue(envelope.Error.Param)
}

func safeOpenAIErrorValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-[]", r) {
			continue
		}
		return ""
	}
	return value
}

func structuredOutputSchemaFingerprint(schema map[string]any) (string, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:12]), nil
}

func turnContractConfigFingerprint(cfg config.OpenAIVoiceConfig, schemaFingerprint string) string {
	raw := strings.Join([]string{
		strings.TrimSpace(cfg.BaseURL),
		strings.TrimSpace(cfg.ReplyModel),
		strings.TrimSpace(cfg.APIKey),
		strings.TrimSpace(schemaFingerprint),
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("sha256:%x", sum[:12])
}

func isNonRetryableTurnContractFailure(err *voice.ProviderRequestError) bool {
	if err == nil || err.StatusCode < 400 || err.StatusCode >= 500 {
		return false
	}
	switch err.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return false
	default:
		return true
	}
}

func (a *Adapter) openTurnCircuit(salonID string, configFingerprint string, providerErr *voice.ProviderRequestError) {
	if a == nil || providerErr == nil {
		return
	}
	a.circuitMu.Lock()
	defer a.circuitMu.Unlock()
	if a.turnCircuits == nil {
		a.turnCircuits = map[string]turnContractCircuit{}
	}
	a.turnCircuits[strings.TrimSpace(salonID)] = turnContractCircuit{
		configFingerprint: configFingerprint,
		error: voice.ProviderRequestError{
			Provider: providerErr.Provider, Stage: providerErr.Stage, StatusCode: providerErr.StatusCode,
			RequestID: providerErr.RequestID, ErrorType: providerErr.ErrorType, ErrorCode: providerErr.ErrorCode,
			ErrorParam: providerErr.ErrorParam, SchemaFingerprint: providerErr.SchemaFingerprint,
		},
	}
}

func (a *Adapter) openTurnCircuitError(salonID string, configFingerprint string) error {
	if a == nil {
		return nil
	}
	a.circuitMu.Lock()
	defer a.circuitMu.Unlock()
	key := strings.TrimSpace(salonID)
	circuit, ok := a.turnCircuits[key]
	if !ok {
		return nil
	}
	if circuit.configFingerprint != configFingerprint {
		delete(a.turnCircuits, key)
		return nil
	}
	err := circuit.error
	err.CircuitOpen = true
	err.Err = errors.New("openai semantic contract circuit is open")
	return &err
}

func (a *Adapter) clearTurnCircuit(salonID string) {
	if a == nil {
		return
	}
	a.circuitMu.Lock()
	delete(a.turnCircuits, strings.TrimSpace(salonID))
	a.circuitMu.Unlock()
}

func validateStructuredOutputSchema(schema map[string]any) error {
	return validateStructuredOutputSchemaNode(schema, "$")
}

func validateStructuredOutputSchemaNode(schema map[string]any, path string) error {
	allowed := map[string]struct{}{
		"type": {}, "additionalProperties": {}, "properties": {}, "required": {},
		"enum": {}, "items": {}, "minimum": {}, "maximum": {},
	}
	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unsupported structured-output keyword %q at %s", key, path)
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		propertyNames := make([]string, 0, len(properties))
		for name := range properties {
			propertyNames = append(propertyNames, name)
		}
		sort.Strings(propertyNames)
		required, ok := schema["required"].([]string)
		if !ok || !sameStringSet(propertyNames, required) {
			return fmt.Errorf("object properties must all be required at %s", path)
		}
		for _, name := range propertyNames {
			child, ok := properties[name].(map[string]any)
			if !ok {
				return fmt.Errorf("property %q is not a schema object at %s", name, path)
			}
			if err := validateStructuredOutputSchemaNode(child, path+".properties."+name); err != nil {
				return err
			}
		}
	}
	if items, ok := schema["items"]; ok {
		child, ok := items.(map[string]any)
		if !ok {
			return fmt.Errorf("array items is not a schema object at %s", path)
		}
		if err := validateStructuredOutputSchemaNode(child, path+".items"); err != nil {
			return err
		}
	}
	return nil
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	want := make(map[string]int, len(left))
	for _, value := range left {
		want[value]++
	}
	for _, value := range right {
		want[value]--
	}
	for _, count := range want {
		if count != 0 {
			return false
		}
	}
	return true
}

func firstNonEmptyHeader(header http.Header, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	return ""
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
		"consultation_question":  req.ConsultationQuestion,
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
	consultationMutation := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"field": map[string]any{"type": "string", "enum": []string{
				conversation.ConsultationNeedFieldCurrentSystem, conversation.ConsultationNeedFieldDesiredOutcome,
				conversation.ConsultationNeedFieldLengthChange, conversation.ConsultationNeedFieldPriorities,
				conversation.ConsultationNeedFieldDesiredFinishes, conversation.ConsultationNeedFieldComparedServiceIDs,
			}},
			"operation": map[string]any{"type": "string", "enum": []string{
				conversation.ConsultationNeedOperationSet, conversation.ConsultationNeedOperationReplace,
				conversation.ConsultationNeedOperationAdd, conversation.ConsultationNeedOperationRemove,
				conversation.ConsultationNeedOperationClear,
			}},
			"values":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"field", "operation", "values", "confidence", "reason"},
	}
	consultation := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"current_system": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConsultationSystemNatural, conversation.ConsultationSystemRegularPolish,
				conversation.ConsultationSystemGel, conversation.ConsultationSystemDip, conversation.ConsultationSystemAcrylic,
				conversation.ConsultationSystemExtension, conversation.ConsultationSystemUnknown,
			}},
			"desired_outcome": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConsultationOutcomeMaintain, conversation.ConsultationOutcomeShorten,
				conversation.ConsultationOutcomeAddLength, conversation.ConsultationOutcomeAddStrength,
				conversation.ConsultationOutcomeRepair, conversation.ConsultationOutcomeRemoval,
				conversation.ConsultationOutcomeColorRefresh, conversation.ConsultationOutcomeCompare,
				conversation.ConsultationOutcomeUnknown,
			}},
			"length_change": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConsultationLengthKeep, conversation.ConsultationLengthShorten,
				conversation.ConsultationLengthAddLength, conversation.ConsultationLengthUnknown,
			}},
			"priorities": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{
				conversation.ConsultationPriorityDurability, conversation.ConsultationPriorityLowerMaintenance,
				conversation.ConsultationPriorityLowerCost, conversation.ConsultationPriorityShorterVisit,
			}}},
			"desired_finishes": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{
				conversation.ConsultationFinishNatural, conversation.ConsultationFinishRegularPolish,
				conversation.ConsultationFinishGelPolish, conversation.ConsultationFinishGlossy,
				conversation.ConsultationFinishMatte, conversation.ConsultationFinishNailArt,
			}}},
			"compared_service_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"booking_requested":     map[string]any{"type": "boolean"},
			"conversation_complete": map[string]any{"type": "boolean"},
			"confidence":            map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":                map[string]any{"type": "string"},
			"mutations":             map[string]any{"type": "array", "items": consultationMutation},
		},
		"required": []string{"current_system", "desired_outcome", "length_change", "priorities", "desired_finishes", "compared_service_ids", "booking_requested", "conversation_complete", "confidence", "reason", "mutations"},
	}
	safety := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"concern": map[string]any{"type": "boolean"},
			"category": map[string]any{"type": "string", "enum": []string{
				"", conversation.SafetyCategoryPain, conversation.SafetyCategoryInjury,
				conversation.SafetyCategoryInfection, conversation.SafetyCategoryAllergy,
				conversation.SafetyCategoryBleeding, conversation.SafetyCategorySwelling,
				conversation.SafetyCategoryMedicalSuitability, conversation.SafetyCategoryOtherHealth,
			}},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"concern", "category", "confidence", "reason"},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"goal": map[string]any{"type": "string", "enum": []string{
				"unknown", "book_appointment", "reschedule_appointment", "cancel_appointment", "consultation", "information", "human_handoff",
			}},
			"acts":         map[string]any{"type": "array", "items": act},
			"questions":    map[string]any{"type": "array", "items": question},
			"confidence":   map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":       map[string]any{"type": "string"},
			"consultation": consultation,
			"safety":       safety,
		},
		"required": []string{"goal", "acts", "questions", "confidence", "reason", "consultation", "safety"},
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
		"consultation":          req.Consultation,
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
