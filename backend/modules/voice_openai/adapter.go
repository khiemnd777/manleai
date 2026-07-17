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
	observerMu     sync.RWMutex
	usageObserver  func(stage string, usage Usage)
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type turnContractCircuit struct {
	error voice.ProviderRequestError
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

// SetUsageObserver exposes non-secret token counters for retained evaluation
// reports. Production behavior is unchanged when no observer is installed.
func (a *Adapter) SetUsageObserver(observer func(stage string, usage Usage)) {
	if a == nil {
		return
	}
	a.observerMu.Lock()
	a.usageObserver = observer
	a.observerMu.Unlock()
}

func (a *Adapter) observeUsage(stage string, usage Usage) {
	if a == nil {
		return
	}
	a.observerMu.RLock()
	observer := a.usageObserver
	a.observerMu.RUnlock()
	if observer != nil {
		observer(stage, usage)
	}
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
			"Sound like a receptionist beginning a helpful consultation, not a form validator: briefly acknowledge that you can help, then ask the practical question.",
			"Treat option values as semantic labels and express them in ordinary caller language; never read raw codes or underscores aloud.",
			"Do not use bureaucratic verification language or claim that the question guarantees the best service.",
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
	a.observeUsage("reply", res.Usage)
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
	contracts := []string{conversation.TurnSemanticContractFull, conversation.TurnSemanticContractGuidance}
	fingerprints := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		fingerprint, err := structuredOutputSchemaFingerprint(turnUnderstandingSchemaForContract(contract))
		if err != nil {
			return voice.TurnContractCheck{}, err
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	check := voice.TurnContractCheck{Provider: voice.ProviderOpenAI, SchemaFingerprint: turnContractSetFingerprint(fingerprints)}
	for _, contract := range contracts {
		req := voice.TurnModelRequest{
			SalonID:          strings.TrimSpace(salonID),
			Channel:          "semantic_contract_check",
			CustomerMessage:  "Validate the structured turn contract without proposing any operation.",
			ExpectedInput:    "contract_validation",
			SemanticContract: contract,
		}
		if contract == conversation.TurnSemanticContractGuidance {
			req.RecognizableGuidanceActions = conversation.GuidanceActionValues()
		}
		_, err := a.interpretTurn(ctx, req, true)
		if err == nil {
			continue
		}
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
	contract := normalizedTurnSemanticContract(req.SemanticContract)
	schema := turnUnderstandingSchemaForContract(contract, turnModelCatalogServiceIDs(req.CatalogServices)...)
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
		Model:        strings.TrimSpace(cfg.ReplyModel),
		Instructions: strings.Join(turnUnderstandingInstructions(contract), "\n"),
		Input:        turnModelInput(req),
		Text: responseTextFormat{Format: responseFormat{
			Type:   "json_schema",
			Name:   turnUnderstandingSchemaName(contract),
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
	a.observeUsage("turn_interpretation", res.Usage)
	a.clearTurnCircuit(req.SalonID, configFingerprint)
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
	if err := normalizeTurnModelTimePreferences(&parsed); err != nil {
		return voice.TurnModelReply{}, fmt.Errorf("%w: %v", voice.ErrTurnModelInvalidOutput, err)
	}
	parsed.Consultation = normalizeConsultationUnknownValues(parsed.Consultation)
	if contract == conversation.TurnSemanticContractGuidance {
		parsed = normalizeGuidanceModelReply(parsed)
	}
	parsed.Diagnostics = map[string]string{"schema_fingerprint": schemaFingerprint}
	return parsed, nil
}

func normalizeGuidanceModelReply(reply voice.TurnModelReply) voice.TurnModelReply {
	reply.Goal = conversation.GuidanceGoalForAction(reply.GuidanceAction)
	// Guidance chooses the workflow. These flags are meaningful only after a
	// consultation is active and must not become a second transition authority.
	reply.Consultation.BookingRequested = false
	reply.Consultation.ConversationComplete = false
	switch strings.TrimSpace(reply.GuidanceAction) {
	case conversation.GuidanceActionServiceCatalog:
		reply.GuidanceQuestionSubject = conversation.ConversationQuestionCatalog
	case conversation.GuidanceActionSalonQuestion:
		reply.GuidanceCatalogMode = ""
	default:
		reply.GuidanceCatalogMode = ""
		reply.GuidanceQuestionSubject = ""
	}
	return reply
}

func normalizeConsultationUnknownValues(reply voice.ConsultationModelReply) voice.ConsultationModelReply {
	if strings.TrimSpace(reply.CurrentSystem) == conversation.ConsultationSystemUnknown {
		reply.CurrentSystem = ""
	}
	if strings.TrimSpace(reply.DesiredOutcome) == conversation.ConsultationOutcomeUnknown {
		reply.DesiredOutcome = ""
	}
	if strings.TrimSpace(reply.LengthChange) == conversation.ConsultationLengthUnknown {
		reply.LengthChange = ""
	}
	return reply
}

func normalizeTurnModelTimePreferences(reply *voice.TurnModelReply) error {
	if reply == nil {
		return nil
	}
	for index := range reply.Questions {
		preference := &reply.Questions[index].TimePreference
		direction := strings.TrimSpace(preference.Direction)
		if direction == "" {
			if preference.Hour != -1 || preference.Minute != -1 {
				return fmt.Errorf("question %d has time components without a direction", index)
			}
			preference.Minutes = -1
			continue
		}
		switch direction {
		case conversation.TimePreferenceBefore, conversation.TimePreferenceAfter, conversation.TimePreferenceExact:
		default:
			return fmt.Errorf("question %d has unsupported time direction", index)
		}
		if preference.Hour < 0 || preference.Hour > 23 || preference.Minute < 0 || preference.Minute > 59 {
			return fmt.Errorf("question %d has invalid local clock components", index)
		}
		preference.Minutes = preference.Hour*60 + preference.Minute
	}
	return nil
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

func turnContractSetFingerprint(schemaFingerprints []string) string {
	raw := strings.Join(schemaFingerprints, "\x00")
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
	a.turnCircuits[turnCircuitKey(salonID, configFingerprint)] = turnContractCircuit{
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
	key := turnCircuitKey(salonID, configFingerprint)
	circuit, ok := a.turnCircuits[key]
	if !ok {
		return nil
	}
	err := circuit.error
	err.CircuitOpen = true
	err.Err = errors.New("openai semantic contract circuit is open")
	return &err
}

func (a *Adapter) clearTurnCircuit(salonID string, configFingerprint string) {
	if a == nil {
		return
	}
	a.circuitMu.Lock()
	delete(a.turnCircuits, turnCircuitKey(salonID, configFingerprint))
	a.circuitMu.Unlock()
}

func turnCircuitKey(salonID string, configFingerprint string) string {
	return strings.TrimSpace(salonID) + "\x00" + strings.TrimSpace(configFingerprint)
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

func turnUnderstandingSchema(catalogServiceIDs ...string) map[string]any {
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
			"mode": map[string]any{"type": "string", "enum": []string{
				"", conversation.ConversationQuestionModeList, conversation.ConversationQuestionModeCount,
				conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionModeDetails,
				conversation.ConversationQuestionModeCompare,
			}},
			"service_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"staff_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"time_preference": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"direction": map[string]any{"type": "string", "enum": []string{"", conversation.TimePreferenceBefore, conversation.TimePreferenceAfter, conversation.TimePreferenceExact}},
					"hour":      map[string]any{"type": "integer", "minimum": -1, "maximum": 23},
					"minute":    map[string]any{"type": "integer", "minimum": -1, "maximum": 59},
				},
				"required": []string{"direction", "hour", "minute"},
			},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"subject", "mode", "service_ids", "staff_ids", "time_preference", "confidence", "reason"},
	}
	consultationMutation := consultationMutationSchema(catalogServiceIDs)
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

func consultationMutationSchema(catalogServiceIDs []string) map[string]any {
	values := []string{
		conversation.ConsultationSystemNatural, conversation.ConsultationSystemRegularPolish,
		conversation.ConsultationSystemGel, conversation.ConsultationSystemDip,
		conversation.ConsultationSystemAcrylic, conversation.ConsultationSystemExtension,
		conversation.ConsultationOutcomeMaintain, conversation.ConsultationOutcomeShorten,
		conversation.ConsultationOutcomeAddLength, conversation.ConsultationOutcomeAddStrength,
		conversation.ConsultationOutcomeRepair, conversation.ConsultationOutcomeRemoval,
		conversation.ConsultationOutcomeColorRefresh, conversation.ConsultationOutcomeCompare,
		conversation.ConsultationLengthKeep, conversation.ConsultationLengthShorten,
		conversation.ConsultationLengthAddLength, conversation.ConsultationPriorityDurability,
		conversation.ConsultationPriorityLowerMaintenance, conversation.ConsultationPriorityLowerCost,
		conversation.ConsultationPriorityShorterVisit, conversation.ConsultationFinishNatural,
		conversation.ConsultationFinishRegularPolish, conversation.ConsultationFinishGelPolish,
		conversation.ConsultationFinishGlossy, conversation.ConsultationFinishMatte,
		conversation.ConsultationFinishNailArt,
	}
	values = append(values, catalogServiceIDs...)
	return map[string]any{
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
			"values":     map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": uniqueNonEmptyStrings(values)}},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string"},
		},
		"required": []string{"field", "operation", "values", "confidence", "reason"},
	}
}

func turnModelCatalogServiceIDs(services []conversation.ConversationServiceRef) []string {
	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, service.ServiceID)
	}
	return uniqueNonEmptyStrings(ids)
}

func uniqueNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func normalizedTurnSemanticContract(contract string) string {
	if strings.TrimSpace(contract) == conversation.TurnSemanticContractGuidance {
		return conversation.TurnSemanticContractGuidance
	}
	return conversation.TurnSemanticContractFull
}

func turnUnderstandingSchemaForContract(contract string, catalogServiceIDs ...string) map[string]any {
	schema := turnUnderstandingSchema(catalogServiceIDs...)
	if normalizedTurnSemanticContract(contract) != conversation.TurnSemanticContractGuidance {
		return schema
	}
	properties := schema["properties"].(map[string]any)
	delete(properties, "goal")
	delete(properties, "acts")
	delete(properties, "questions")
	properties["guidance_action"] = map[string]any{"type": "string", "enum": append([]string{""}, conversation.GuidanceActionValues()...)}
	properties["guidance_catalog_mode"] = map[string]any{"type": "string", "enum": []string{
		"", conversation.ConversationQuestionModeList, conversation.ConversationQuestionModeCount,
		conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionModeDetails,
		conversation.ConversationQuestionModeCompare,
	}}
	properties["guidance_question_subject"] = map[string]any{"type": "string", "enum": []string{
		"", conversation.ConversationQuestionCatalog, conversation.ConversationQuestionAvailability,
		conversation.ConversationQuestionPrice, conversation.ConversationQuestionHours,
		conversation.ConversationQuestionStaff, conversation.ConversationQuestionPolicy,
	}}
	properties["guidance_party_size"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 20}
	schema["required"] = []string{"guidance_action", "guidance_catalog_mode", "guidance_question_subject", "guidance_party_size", "confidence", "reason", "consultation", "safety"}
	return schema
}

func turnUnderstandingSchemaName(contract string) string {
	if normalizedTurnSemanticContract(contract) == conversation.TurnSemanticContractGuidance {
		return "guidance_turn_understanding"
	}
	return "turn_understanding"
}

func turnUnderstandingInstructions(contract string) []string {
	shared := []string{
		"Interpret one caller turn for a US nail salon receptionist.",
		"The caller's explicit actionable request, question, correction, or cancellation owns the interpretation. Politeness, greetings, filler, and background context do not override it; only a later explicit change or withdrawal supersedes an earlier request.",
		"expected_input describes the state-owned field or decision currently awaiting an answer; use it as context, not as a closed vocabulary.",
		"Use only service and category IDs present in catalog_services.",
		"Do not infer booking confirmation, availability, customer identity, prices, or policy.",
		"For consultation, extract only the caller's stated current nail system, desired outcome, length change, priorities, desired finishes, compared catalog service IDs, booking request, and whether the caller is done. Never recommend a service in model output.",
		"For every consultation preference stated or corrected in this turn, emit a field-level mutation. Use set for an initial value, replace for a correction, add or remove for list items, and clear only when the caller explicitly withdraws the field. Every positive set, replace, or add value must also appear in the matching same-turn consultation snapshot field; never create a mutation from uncertainty, a question, or a guessed preference. Never treat a negated value as an addition. Unknown means absent: never emit unknown as a mutation value, and never emit a no-op mutation.",
		"Classify safety.concern=true for pain, injury, infection, allergy, bleeding, swelling, adverse reaction, or a request for medical suitability or treatment advice. This safety classification applies regardless of the caller's booking or consultation goal.",
		"Use empty strings and arrays when consultation details are not present. Set consultation booking_requested only for an explicit request to schedule or book after consultation; wanting a nail service, asking for advice, or entering consultation is not a booking request. Set conversation_complete only when the caller explicitly ends consultation. Do not infer health suitability or treatment claims.",
		"Return strict JSON matching the schema.",
	}
	if normalizedTurnSemanticContract(contract) == conversation.TurnSemanticContractGuidance {
		return append([]string{
			"The dialog is awaiting the caller's goal or the kind of service guidance they want.",
			"Choose guidance_action only from recognizable_guidance_actions in the input. These values describe meanings you must recognize; they do not claim the salon can fulfill every workflow. Use an empty guidance_action only when none matches the caller's meaning.",
			"Use book for an explicit new appointment request, service_catalog for a request to hear or view available services, consultation for needs-based help choosing, salon_question for an operational salon question, name_service when the caller names or says they can name the desired service, reschedule for moving an existing appointment, cancel for cancelling an existing appointment, and human_handoff for an explicit request for a person.",
			"When the caller identifies a concrete desired service or category from the supplied catalog, choose name_service even if the same sentence asks to book it. Choose book only when the caller wants to start a new booking without identifying the desired catalog service or category.",
			"Choose consultation when the caller wants a nail service but has not identified a concrete catalog service and is asking for help, direction, a suggestion, or a way to determine what fits. This includes a broad stated desire for a nail service when no booking or scheduling request is present. Choose book for an explicit appointment or scheduling request without a named service. Choose service_catalog only when the caller asks to hear, view, count, check, describe, or compare catalog offerings without asking for needs-based help.",
			"A request to check, describe, or compare named catalog services is service_catalog even when the caller is gathering information before deciding. A question about whether the salon offers a service, appointment type, price, staff, hours, policy, or opening is information, not an implicit booking command.",
			"Set guidance_party_size to the explicit total party size only for a new booking covering two or more people; otherwise set it to zero. Never infer a party size from service quantity.",
			"When guidance_action is service_catalog, set guidance_catalog_mode to list, count, existence, details, or compare from the caller's requested operation. Otherwise use an empty guidance_catalog_mode.",
			"When guidance_action is service_catalog, set guidance_question_subject=catalog. When it is salon_question, set guidance_question_subject to availability, price, hours, staff, or policy. Otherwise use an empty guidance_question_subject.",
			"Return only the typed guidance action, extraction-only consultation details, confidence, reason, and safety assessment; never create a separate goal, booking operations, or questions.",
		}, shared...)
	}
	return append([]string{
		"Return structured operations and questions only; never write a customer-facing reply.",
		"When current_booking_stage is consultation, set goal=consultation unless the caller explicitly requests cancellation, rescheduling, or human handoff; keep acts empty and return only consultation extraction/mutations plus any structured information questions. Never select, add, replace, or remove a service from consultation needs.",
		"A turn may contain multiple operations and questions. Preserve their spoken order.",
		"Use only staff IDs present in catalog_staff.",
		"Distinguish the salon catalog from the caller's current booking draft.",
		"For replacements, preserve source (what is being replaced) separately from target (the new service).",
		"Choose an operation from the caller's stated change, never from current_draft contents. An additive or inclusive request is add_service even when the target shares a category with an existing service or the guest already has another service. Use replace_service only when the caller explicitly requests replacement in this turn. A replace_service source_id must identify a current service the caller explicitly referred to as the source being changed; never invent replacement wording or source_ids from the draft merely because that draft or guest already has another service.",
		"For entity=staff, source_ids and target_ids may contain only IDs from catalog_staff and must never contain service IDs. When the caller asks for any different technician without naming one, use subject=alternative with empty source_ids and target_ids.",
		"Use guest_scope=another_guest only before a structured party plan exists and the caller explicitly assigns a service to another person.",
		"For an existing party plan, set guest_ref to an exact current_draft.party_groups guest_ref for every service mutation and leave guest_scope empty; guest_ref is authoritative and guest groups must never be flattened.",
		"A pending clarification is context, not a restriction: a clearly different new target may supersede it.",
		"For an initial concrete service selection, emit add_service with entity=service and the catalog target ID.",
		"When the caller supplies or revises an appointment date or time, emit set_field with entity=date_time and subject=requested_date or requested_time. Never encode a requested appointment date or time as a current_booking question.",
		"When expected_input is requested_date or requested_time and the caller supplies a scheduling constraint, acts must contain the corresponding set_field date_time operation, or questions must contain an availability time_preference when the caller is asking rather than selecting. A booking goal or consultation.booking_requested flag never substitutes for that structured scheduling signal; do not return both acts and questions empty.",
		"Represent questions about the current draft as questions with subject=current_booking.",
		"A request to list, count, check, describe, or compare services already in current_draft is always a current_booking question with the matching mode and current draft service_ids. Never turn a current-booking comparison into goal=consultation or consultation compared_service_ids.",
		"For every information question, set mode to list, count, existence, details, or compare when that operation applies; otherwise use an empty mode.",
		"For availability constraints, set time_preference.direction to before, after, or exact and use salon-local 24-hour clock components in time_preference.hour and time_preference.minute. For example, 1:30 PM is hour 13 and minute 30. Do not calculate minutes after midnight. Use direction empty with hour -1 and minute -1 when no time constraint is present.",
		"A final-review acceptance may coexist with a correction; include both and never suppress the correction.",
		"Use set_field or clear_field for staff, date/time, guest, or customer-field corrections.",
		"For a customer-name correction, emit set_field with entity=customer, subject=name, and value equal to the corrected name.",
		"For a request to use a different technician without naming one, emit set_field with entity=staff, subject=alternative, and no target ID; never choose a technician for the caller.",
	}, shared...)
}

func turnModelInput(req voice.TurnModelRequest) string {
	raw, _ := json.Marshal(map[string]any{
		"channel":                       req.Channel,
		"customer_message":              req.CustomerMessage,
		"expected_input":                req.ExpectedInput,
		"semantic_contract":             normalizedTurnSemanticContract(req.SemanticContract),
		"recognizable_guidance_actions": append([]string(nil), req.RecognizableGuidanceActions...),
		"selected_services":             turnModelServiceRefs(req.SelectedServices),
		"catalog_services":              turnModelServiceRefs(req.CatalogServices),
		"catalog_service_aliases":       req.CatalogServiceAliases,
		"catalog_categories":            req.CatalogCategories,
		"selected_staff":                req.SelectedStaff,
		"catalog_staff":                 req.CatalogStaff,
		"pending":                       req.Pending,
		"current_booking_stage":         req.CurrentBookingStage,
		"booking_action":                req.BookingAction,
		"current_draft":                 turnModelDraftRef(req.CurrentDraft),
		"consultation":                  req.Consultation,
	})
	return string(raw)
}

func turnModelDraftRef(draft conversation.ConversationDraftRef) conversation.ConversationDraftRef {
	copyDraft := draft
	copyDraft.ServiceIDs = append([]string(nil), draft.ServiceIDs...)
	copyDraft.PartyGroups = make([]conversation.ConversationPartyGroupRef, 0, len(draft.PartyGroups))
	for _, group := range draft.PartyGroups {
		// Guest identity and count are needed for assignment. Existing per-guest
		// services stay backend-owned so the model cannot infer a destructive
		// replacement source from draft layout rather than caller evidence.
		copyDraft.PartyGroups = append(copyDraft.PartyGroups, conversation.ConversationPartyGroupRef{
			GuestRef: strings.TrimSpace(group.GuestRef), Count: group.Count,
		})
	}
	return copyDraft
}

func turnModelServiceRefs(refs []conversation.ConversationServiceRef) []conversation.ConversationServiceRef {
	out := make([]conversation.ConversationServiceRef, 0, len(refs))
	for _, ref := range refs {
		ref.ConsultationProfile = nil
		out = append(out, ref)
	}
	return out
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
	Usage      Usage            `json:"usage"`
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
