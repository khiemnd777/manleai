package voice_openai

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestStreamSpeechEmitsTwilioAudioBeforeProviderResponseCompletes(t *testing.T) {
	firstHalf, secondHalf := testPCM16Parts(1400, 700)
	release := make(chan struct{})
	pipeReader, pipeWriter := io.Pipe()

	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey:      "test-key",
		BaseURL:     "https://openai.test/v1",
		SpeechModel: "tts-1",
		SpeechVoice: "alloy",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode speech request: %v", err)
		}
		if payload["response_format"] != "pcm" {
			t.Fatalf("response_format = %#v, want pcm", payload["response_format"])
		}
		go func() {
			_, _ = pipeWriter.Write(firstHalf)
			<-release
			_, _ = pipeWriter.Write(secondHalf)
			_ = pipeWriter.Close()
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"audio/pcm"},
				"X-Request-Id": []string{"req_provider_1"},
			},
			Body: pipeReader,
		}, nil
	})}
	firstChunk := make(chan voice.SpeechChunk, 1)
	done := make(chan voice.SpeechStreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := adapter.StreamSpeech(context.Background(), "salon_1", voice.SpeechStreamRequest{
			RequestID: "reply_1",
			Text:      "Your appointment request is ready.",
			Voice:     "alloy",
		}, func(chunk voice.SpeechChunk) error {
			select {
			case firstChunk <- chunk:
			default:
			}
			return nil
		})
		if err != nil {
			errs <- err
			return
		}
		done <- result
	}()

	select {
	case chunk := <-firstChunk:
		if len(chunk.Audio) != twilioFrameBytes || chunk.Sequence != 0 {
			t.Fatalf("first chunk = sequence %d bytes %d", chunk.Sequence, len(chunk.Audio))
		}
	case err := <-errs:
		t.Fatalf("StreamSpeech before provider completion: %v", err)
	case <-time.After(time.Second):
		t.Fatal("first audio chunk was not emitted before provider completion")
	}
	select {
	case <-done:
		t.Fatal("speech stream completed before the provider response was released")
	default:
	}
	close(release)
	select {
	case result := <-done:
		if result.ProviderRequestID != "req_provider_1" || result.Encoding != "audio/x-mulaw" || result.SampleRate != 8000 || result.ChunkCount < 2 {
			t.Fatalf("stream result = %#v", result)
		}
	case err := <-errs:
		t.Fatalf("StreamSpeech: %v", err)
	case <-time.After(time.Second):
		t.Fatal("speech stream did not complete")
	}
}

func testPCM16Parts(sampleCount int, splitSamples int) ([]byte, []byte) {
	pcm := make([]byte, sampleCount*2)
	for i := 0; i < sampleCount; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16((i%200)-100)*100))
	}
	splitBytes := splitSamples * 2
	return pcm[:splitBytes], pcm[splitBytes:]
}

func TestInterpretTurnUsesStrictCatalogBoundMultiActSchema(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey:     "test-key",
		BaseURL:    "https://openai.test/v1",
		ReplyModel: "gpt-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		instructions, _ := req["instructions"].(string)
		if !strings.Contains(instructions, "Use only service and category IDs present in catalog_services") || !strings.Contains(instructions, "pending clarification is context, not a restriction") || !strings.Contains(instructions, "Never recommend a service in model output") || !strings.Contains(instructions, "safety classification applies regardless") || !strings.Contains(instructions, "guest_ref is authoritative") || !strings.Contains(instructions, "never invent replacement wording or source_ids") || !strings.Contains(instructions, "must never contain service IDs") || !strings.Contains(instructions, "keep acts empty") || !strings.Contains(instructions, "Never turn a current-booking comparison") || !strings.Contains(instructions, "salon-local 24-hour clock components") || !strings.Contains(instructions, "1:30 PM is hour 13 and minute 30") || !strings.Contains(instructions, "must also appear in the matching same-turn consultation snapshot") || !strings.Contains(instructions, "wanting a nail service, asking for advice, or entering consultation is not a booking request") {
			t.Fatalf("conversation act instructions = %s", instructions)
		}
		textConfig, _ := req["text"].(map[string]any)
		format, _ := textConfig["format"].(map[string]any)
		schema, _ := format["schema"].(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		consultationSchema, ok := properties["consultation"].(map[string]any)
		if !ok {
			t.Fatalf("structured schema is missing consultation: %#v", schema)
		}
		consultationProperties, _ := consultationSchema["properties"].(map[string]any)
		if _, ok := consultationProperties["desired_finishes"]; !ok {
			t.Fatalf("consultation schema is missing desired_finishes: %#v", consultationSchema)
		}
		if _, ok := consultationProperties["mutations"]; !ok {
			t.Fatalf("consultation schema is missing mutation semantics: %#v", consultationSchema)
		}
		if _, ok := properties["safety"]; !ok {
			t.Fatalf("structured schema is missing global safety assessment: %#v", schema)
		}
		input, _ := req["input"].(string)
		var modelInput map[string]any
		if err := json.Unmarshal([]byte(input), &modelInput); err != nil {
			t.Fatalf("decode model input: %v", err)
		}
		if modelInput["customer_message"] != "Make that a spa pedicure instead." {
			t.Fatalf("customer_message = %#v", modelInput["customer_message"])
		}
		if modelInput["expected_input"] != conversation.ExpectedInputService {
			t.Fatalf("expected_input = %#v", modelInput["expected_input"])
		}
		catalog, ok := modelInput["catalog_services"].([]any)
		if !ok || len(catalog) != 1 {
			t.Fatalf("catalog_services = %#v", modelInput["catalog_services"])
		}
		catalogService, _ := catalog[0].(map[string]any)
		if profile := catalogService["consultation_profile"]; profile != nil {
			t.Fatalf("consultation profile leaked into extraction-only model input: %#v", profile)
		}
		body, _ := json.Marshal(map[string]any{
			"output_text": `{"goal":"book_appointment","acts":[{"kind":"replace_service","entity":"service","source_ids":["service_gel"],"target_ids":["service_spa"],"source_category_id":"","source_category_name":"","target_category_id":"cat_pedi","target_category_name":"Pedicure","scope":"one","guest_scope":"","guest_ref":"","subject":"","value":"","count":0,"confidence":0.95,"reason":"explicit replacement"}],"questions":[{"subject":"availability","service_ids":["service_spa"],"staff_ids":[],"time_preference":{"direction":"","hour":-1,"minute":-1},"confidence":0.92,"reason":"caller asked about availability"}],"confidence":0.95,"reason":"correction plus question","consultation":{"current_system":"","desired_outcome":"","length_change":"","priorities":[],"desired_finishes":[],"compared_service_ids":[],"booking_requested":false,"conversation_complete":false,"confidence":0,"reason":"","mutations":[]},"safety":{"concern":false,"category":"","confidence":0,"reason":""}}`,
		})
		return jsonResponse(body), nil
	})}

	reply, err := adapter.InterpretTurn(context.Background(), voice.TurnModelRequest{
		SalonID:         "salon_1",
		CustomerMessage: "Make that a spa pedicure instead.",
		ExpectedInput:   conversation.ExpectedInputService,
		SelectedServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_gel", ServiceName: "Gel Manicure",
		}},
		CatalogServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_spa", ServiceName: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure",
			ConsultationProfile: &conversation.ConversationConsultationProfileRef{
				Status: conversation.ConsultationProfileStatusReady, RecommendedOutcomes: []string{conversation.ConsultationOutcomeMaintain},
				CompatibleCurrentSystems: []string{conversation.ConsultationSystemNatural}, PriorityTags: []string{conversation.ConsultationPriorityLowerMaintenance},
				FinishOptions: []string{conversation.ConsultationFinishGlossy}, OwnerApprovedSummary: "A lower-maintenance pedicure with a glossy finish.", Revision: 7,
			},
		}},
	})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	if len(reply.Acts) != 1 || reply.Acts[0].Kind != conversation.ConversationActReplace || reply.Acts[0].TargetIDs[0] != "service_spa" || len(reply.Questions) != 1 || reply.Questions[0].TimePreference.Minutes != -1 || reply.Confidence != 0.95 {
		t.Fatalf("reply = %#v", reply)
	}
}

func TestNormalizeTurnModelTimePreferencesComputesCanonicalMinutesFromClockParts(t *testing.T) {
	reply := voice.TurnModelReply{Questions: []voice.QuestionModelReply{{
		TimePreference: voice.TimePreferenceModelReply{
			Direction: conversation.TimePreferenceAfter, Hour: 13, Minute: 30,
		},
	}}}
	if err := normalizeTurnModelTimePreferences(&reply); err != nil {
		t.Fatalf("normalize time preference: %v", err)
	}
	if reply.Questions[0].TimePreference.Minutes != 13*60+30 {
		t.Fatalf("time preference = %#v", reply.Questions[0].TimePreference)
	}
}

func TestNormalizeTurnModelTimePreferencesRejectsInconsistentNoConstraint(t *testing.T) {
	reply := voice.TurnModelReply{Questions: []voice.QuestionModelReply{{
		TimePreference: voice.TimePreferenceModelReply{Hour: 13, Minute: 0},
	}}}
	if err := normalizeTurnModelTimePreferences(&reply); err == nil {
		t.Fatalf("inconsistent no-constraint preference accepted: %#v", reply.Questions[0].TimePreference)
	}
}

func TestInterpretTurnUsesCompactGuidanceContract(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		instructions, _ := req["instructions"].(string)
		if !strings.Contains(instructions, "awaiting the caller's goal") || !strings.Contains(instructions, "never create a separate goal") ||
			!strings.Contains(instructions, "concrete desired service or category from the supplied catalog") ||
			!strings.Contains(instructions, "broad stated desire for a nail service") ||
			!strings.Contains(instructions, "do not claim the salon can fulfill every workflow") {
			t.Fatalf("guidance instructions = %s", instructions)
		}
		textConfig, _ := req["text"].(map[string]any)
		format, _ := textConfig["format"].(map[string]any)
		if format["name"] != "guidance_turn_understanding" {
			t.Fatalf("schema name = %#v", format["name"])
		}
		schema, _ := format["schema"].(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		if _, exists := properties["acts"]; exists {
			t.Fatalf("guidance schema retained acts: %#v", properties)
		}
		if _, exists := properties["goal"]; exists {
			t.Fatalf("guidance schema retained duplicate goal: %#v", properties)
		}
		if _, exists := properties["questions"]; exists {
			t.Fatalf("guidance schema retained questions: %#v", properties)
		}
		if _, exists := properties["guidance_action"]; !exists {
			t.Fatalf("guidance schema lost typed action: %#v", properties)
		}
		if _, exists := properties["guidance_party_size"]; !exists {
			t.Fatalf("guidance schema lost typed party size: %#v", properties)
		}
		actionSchema, _ := properties["guidance_action"].(map[string]any)
		actionEnum := actionSchema["enum"]
		if !testEnumContains(actionEnum, conversation.GuidanceActionReschedule) || !testEnumContains(actionEnum, conversation.GuidanceActionCancel) {
			t.Fatalf("guidance schema lost appointment-management actions: %#v", actionEnum)
		}
		if _, exists := properties["consultation"]; !exists {
			t.Fatalf("guidance schema lost consultation: %#v", properties)
		}
		if _, exists := properties["safety"]; !exists {
			t.Fatalf("guidance schema lost safety: %#v", properties)
		}
		input, _ := req["input"].(string)
		var modelInput map[string]any
		if err := json.Unmarshal([]byte(input), &modelInput); err != nil {
			t.Fatalf("decode model input: %v", err)
		}
		allowed, _ := modelInput["recognizable_guidance_actions"].([]any)
		if modelInput["semantic_contract"] != conversation.TurnSemanticContractGuidance || modelInput["customer_message"] != "I'm undecided about which treatment fits me." || len(allowed) != 2 || allowed[1] != conversation.GuidanceActionConsultation {
			t.Fatalf("guidance model input = %#v", modelInput)
		}
		body, _ := json.Marshal(map[string]any{"output_text": validGuidanceTurnOutput()})
		return jsonResponse(body), nil
	})}

	reply, err := adapter.InterpretTurn(context.Background(), voice.TurnModelRequest{
		SalonID: "salon_1", CustomerMessage: "I'm undecided about which treatment fits me.",
		ExpectedInput: conversation.ExpectedInputCallerGoal, SemanticContract: conversation.TurnSemanticContractGuidance,
		RecognizableGuidanceActions: []string{conversation.GuidanceActionBook, conversation.GuidanceActionConsultation},
	})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	if reply.Goal != "consultation" || reply.GuidanceAction != conversation.GuidanceActionConsultation || reply.Consultation.DesiredOutcome != conversation.ConsultationOutcomeMaintain || len(reply.Acts) != 0 || len(reply.Questions) != 0 || reply.Diagnostics["schema_fingerprint"] == "" {
		t.Fatalf("guidance reply = %#v", reply)
	}
}

func TestTurnModelDraftRefKeepsPartyOwnershipWithoutPerGuestServiceHints(t *testing.T) {
	draft := conversation.ConversationDraftRef{
		ServiceIDs: []string{"svc_one", "svc_two"}, PartySize: 2,
		PartyGroups: []conversation.ConversationPartyGroupRef{
			{GuestRef: "guest_caller", Count: 1, ServiceIDs: []string{"svc_one"}},
			{GuestRef: "guest_2", Count: 1, ServiceIDs: []string{"svc_two"}},
		},
	}
	modelDraft := turnModelDraftRef(draft)
	if len(modelDraft.ServiceIDs) != 2 || len(modelDraft.PartyGroups) != 2 || modelDraft.PartyGroups[1].GuestRef != "guest_2" {
		t.Fatalf("model draft ownership = %#v", modelDraft)
	}
	for _, group := range modelDraft.PartyGroups {
		if len(group.ServiceIDs) != 0 {
			t.Fatalf("per-guest service hints leaked: %#v", modelDraft.PartyGroups)
		}
	}
	if len(draft.PartyGroups[1].ServiceIDs) != 1 {
		t.Fatalf("source draft mutated: %#v", draft)
	}
}

func TestNormalizeGuidanceModelReplyDerivesProtocolOwnedFields(t *testing.T) {
	book := normalizeGuidanceModelReply(voice.TurnModelReply{
		GuidanceAction: conversation.GuidanceActionBook, GuidanceCatalogMode: conversation.ConversationQuestionModeList,
		GuidanceQuestionSubject: conversation.ConversationQuestionAvailability,
		Consultation: voice.ConsultationModelReply{
			BookingRequested: true, ConversationComplete: true,
		},
	})
	if book.Goal != "book_appointment" || book.GuidanceCatalogMode != "" || book.GuidanceQuestionSubject != "" || book.Consultation.BookingRequested || book.Consultation.ConversationComplete {
		t.Fatalf("normalized book reply = %#v", book)
	}
	catalog := normalizeGuidanceModelReply(voice.TurnModelReply{
		GuidanceAction: conversation.GuidanceActionServiceCatalog, GuidanceCatalogMode: conversation.ConversationQuestionModeCompare,
		GuidanceQuestionSubject: conversation.ConversationQuestionStaff,
	})
	if catalog.Goal != "information" || catalog.GuidanceCatalogMode != conversation.ConversationQuestionModeCompare ||
		catalog.GuidanceQuestionSubject != conversation.ConversationQuestionCatalog {
		t.Fatalf("normalized catalog reply = %#v", catalog)
	}
	salon := normalizeGuidanceModelReply(voice.TurnModelReply{
		GuidanceAction: conversation.GuidanceActionSalonQuestion, GuidanceCatalogMode: conversation.ConversationQuestionModeList,
		GuidanceQuestionSubject: conversation.ConversationQuestionHours,
	})
	if salon.Goal != "information" || salon.GuidanceCatalogMode != "" || salon.GuidanceQuestionSubject != conversation.ConversationQuestionHours {
		t.Fatalf("normalized salon reply = %#v", salon)
	}
}

func TestNormalizeConsultationUnknownValuesTreatsProtocolUnknownAsAbsence(t *testing.T) {
	normalized := normalizeConsultationUnknownValues(voice.ConsultationModelReply{
		CurrentSystem: conversation.ConsultationSystemUnknown, DesiredOutcome: conversation.ConsultationOutcomeUnknown,
		LengthChange: conversation.ConsultationLengthUnknown,
	})
	if normalized.CurrentSystem != "" || normalized.DesiredOutcome != "" || normalized.LengthChange != "" {
		t.Fatalf("normalized consultation = %#v", normalized)
	}
}

func TestInterpretTurnReturnsPIIFreeProviderDiagnosticsWithoutResponseBody(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"X-Request-Id": []string{"req_provider_safe_1"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"invalid_json_schema","param":"text.format.schema","message":"customer transcript must stay private"}}`)),
		}, nil
	})}

	_, err := adapter.InterpretTurn(context.Background(), voice.TurnModelRequest{
		SalonID: "salon_1", SessionID: "session_1", CustomerMessage: "private caller wording",
	})
	if err == nil {
		t.Fatal("InterpretTurn error = nil")
	}
	var providerErr *voice.ProviderRequestError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T", err)
	}
	diagnostics := providerErr.SafeDiagnostics()
	if diagnostics["provider"] != voice.ProviderOpenAI || diagnostics["failure_stage"] != "turn_interpretation_response" || diagnostics["http_status_class"] != "5xx" || diagnostics["request_id"] != "req_provider_safe_1" ||
		diagnostics["error_type"] != "invalid_request_error" || diagnostics["error_code"] != "invalid_json_schema" || diagnostics["error_param"] != "text.format.schema" || diagnostics["schema_fingerprint"] == "" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if strings.Contains(err.Error(), "customer transcript") || strings.Contains(err.Error(), "private caller wording") {
		t.Fatalf("error leaked provider body or request text: %q", err.Error())
	}
}

func TestTurnUnderstandingSchemaUsesSupportedSubsetRecursively(t *testing.T) {
	for _, contract := range []string{conversation.TurnSemanticContractFull, conversation.TurnSemanticContractGuidance} {
		schema := turnUnderstandingSchemaForContract(contract)
		if err := validateStructuredOutputSchema(schema); err != nil {
			t.Fatalf("%s turn schema is invalid: %v", contract, err)
		}
	}
	schema := turnUnderstandingSchema()
	properties := schema["properties"].(map[string]any)
	consultation := properties["consultation"].(map[string]any)
	consultationProperties := consultation["properties"].(map[string]any)
	priorities := consultationProperties["priorities"].(map[string]any)
	priorities["uniqueItems"] = true
	if err := validateStructuredOutputSchema(schema); err == nil || !strings.Contains(err.Error(), "uniqueItems") || !strings.Contains(err.Error(), "consultation") {
		t.Fatalf("recursive unsupported-key validation error = %v", err)
	}
}

func TestConsultationMutationSchemaUsesControlledVocabularyAndRequestCatalogIDs(t *testing.T) {
	schema := turnUnderstandingSchema("service_from_catalog")
	properties := schema["properties"].(map[string]any)
	consultation := properties["consultation"].(map[string]any)
	consultationProperties := consultation["properties"].(map[string]any)
	mutations := consultationProperties["mutations"].(map[string]any)
	mutation := mutations["items"].(map[string]any)
	mutationProperties := mutation["properties"].(map[string]any)
	values := mutationProperties["values"].(map[string]any)
	items := values["items"].(map[string]any)
	enum := items["enum"].([]string)
	if !containsExactString(enum, "service_from_catalog") || !containsExactString(enum, conversation.ConsultationOutcomeAddStrength) {
		t.Fatalf("mutation values enum = %#v", enum)
	}
	if containsExactString(enum, conversation.ConsultationOutcomeUnknown) || containsExactString(enum, conversation.ConsultationSystemUnknown) {
		t.Fatalf("unknown was exposed as a persisted mutation value: %#v", enum)
	}
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestTurnContractCircuitSuppressesRepeatedInvalidRequestsAndProbeClearsIt(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test",
	})
	requestCount := 0
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		var requestPayload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestPayload); err != nil {
			t.Fatalf("decode contract request: %v", err)
		}
		if requestCount == 1 {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"X-Request-Id": []string{"req_contract_bad_1"}},
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"type":"invalid_request_error","code":"invalid_json_schema","param":"text.format.schema","message":"do not expose this body"}}`,
				)),
			}, nil
		}
		output := validEmptyTurnOutput()
		textConfig, _ := requestPayload["text"].(map[string]any)
		format, _ := textConfig["format"].(map[string]any)
		if format["name"] == "guidance_turn_understanding" {
			output = validGuidanceTurnOutput()
		}
		body, _ := json.Marshal(map[string]any{"output_text": output})
		return jsonResponse(body), nil
	})}

	request := voice.TurnModelRequest{SalonID: "salon_1", CustomerMessage: "Please help with a service."}
	_, err := adapter.InterpretTurn(context.Background(), request)
	var firstErr *voice.ProviderRequestError
	if !errors.As(err, &firstErr) || firstErr.StatusCode != http.StatusBadRequest || firstErr.CircuitOpen {
		t.Fatalf("first contract error = %#v", err)
	}
	_, err = adapter.InterpretTurn(context.Background(), request)
	var circuitErr *voice.ProviderRequestError
	if !errors.As(err, &circuitErr) || !circuitErr.CircuitOpen || requestCount != 1 {
		t.Fatalf("open circuit error=%#v request_count=%d", err, requestCount)
	}
	if diagnostics := circuitErr.SafeDiagnostics(); diagnostics["circuit_open"] != "true" || diagnostics["error_code"] != "invalid_json_schema" {
		t.Fatalf("circuit diagnostics = %#v", diagnostics)
	}

	check, err := adapter.CheckTurnContract(context.Background(), "salon_1")
	if err != nil || check.SchemaFingerprint == "" || requestCount != 3 {
		t.Fatalf("semantic probe check=%#v err=%v request_count=%d", check, err, requestCount)
	}
	if _, err := adapter.InterpretTurn(context.Background(), request); err != nil || requestCount != 4 {
		t.Fatalf("post-probe interpretation err=%v request_count=%d", err, requestCount)
	}
}

func TestTurnContractCircuitResetsWhenSalonRuntimeConfigurationChanges(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test-v1",
	})
	requestCount := 0
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		if requestCount == 1 {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"invalid_json_schema","param":"text.format.schema"}}`)),
			}, nil
		}
		body, _ := json.Marshal(map[string]any{"output_text": validEmptyTurnOutput()})
		return jsonResponse(body), nil
	})}

	request := voice.TurnModelRequest{SalonID: "salon_runtime_change"}
	if _, err := adapter.InterpretTurn(context.Background(), request); err == nil {
		t.Fatal("first invalid contract request succeeded")
	}
	adapter.cfg.ReplyModel = "gpt-test-v2"
	if _, err := adapter.InterpretTurn(context.Background(), request); err != nil || requestCount != 2 {
		t.Fatalf("changed runtime config did not reset circuit: err=%v request_count=%d", err, requestCount)
	}
}

func validEmptyTurnOutput() string {
	return `{"goal":"unknown","acts":[],"questions":[],"confidence":0,"reason":"contract check","consultation":{"current_system":"","desired_outcome":"","length_change":"","priorities":[],"desired_finishes":[],"compared_service_ids":[],"booking_requested":false,"conversation_complete":false,"confidence":0,"reason":"","mutations":[]},"safety":{"concern":false,"category":"","confidence":0,"reason":""}}`
}

func validGuidanceTurnOutput() string {
	return `{"guidance_action":"consultation","guidance_catalog_mode":"","guidance_question_subject":"","guidance_party_size":0,"confidence":0.96,"reason":"caller wants help choosing","consultation":{"current_system":"natural","desired_outcome":"maintain","length_change":"","priorities":[],"desired_finishes":[],"compared_service_ids":[],"booking_requested":false,"conversation_complete":false,"confidence":0.91,"reason":"caller wants upkeep guidance","mutations":[]},"safety":{"concern":false,"category":"","confidence":0,"reason":""}}`
}

func testEnumContains(values any, expected string) bool {
	switch typed := values.(type) {
	case []string:
		for _, value := range typed {
			if value == expected {
				return true
			}
		}
	case []any:
		for _, value := range typed {
			if value == expected {
				return true
			}
		}
	}
	return false
}

func TestInterpretTurnClassifiesEmptyAndInvalidStructuredOutput(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{name: "empty", text: "", want: voice.ErrTurnModelEmptyOutput},
		{name: "invalid", text: "not-json", want: voice.ErrTurnModelInvalidOutput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewAdapter(config.OpenAIVoiceConfig{APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test"})
			adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, _ := json.Marshal(map[string]any{"output_text": test.text})
				return jsonResponse(body), nil
			})}
			_, err := adapter.InterpretTurn(context.Background(), voice.TurnModelRequest{SalonID: "salon_1"})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestGenerateReplyParsesStructuredResponse(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey:     "test-key",
		BaseURL:    "https://openai.test/v1",
		ReplyModel: "gpt-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s, want /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		instructions, _ := req["instructions"].(string)
		if !strings.Contains(instructions, "Do not mention POS providers") {
			t.Fatalf("instructions should hide POS provider names: %s", instructions)
		}
		if !strings.Contains(instructions, "natural, human spoken tone") {
			t.Fatalf("instructions should include selected tone guidance: %s", instructions)
		}
		input, _ := req["input"].(string)
		var inputPayload map[string]any
		if err := json.Unmarshal([]byte(input), &inputPayload); err != nil {
			t.Fatalf("decode model input: %v", err)
		}
		if inputPayload["ai_tone"] != "natural_human" {
			t.Fatalf("ai_tone = %#v, want natural_human", inputPayload["ai_tone"])
		}
		selected, ok := inputPayload["selected_service_names"].([]any)
		if !ok || len(selected) != 2 || selected[0] != "Classic Manicure" || selected[1] != "Gel Removal" {
			t.Fatalf("selected_service_names = %#v, want Classic Manicure and Gel Removal", inputPayload["selected_service_names"])
		}
		body, _ := json.Marshal(map[string]any{
			"output_text": `{"message":"What phone number should we use?","confidence":0.9,"handoff":false,"reason":""}`,
			"usage":       map[string]any{"input_tokens": 17, "output_tokens": 6, "total_tokens": 23},
		})
		return jsonResponse(body), nil
	})}
	var observed Usage
	adapter.SetUsageObserver(func(stage string, usage Usage) {
		if stage != "reply" {
			t.Fatalf("usage stage = %q", stage)
		}
		observed = usage
	})

	reply, err := adapter.GenerateReply(context.Background(), voice.ModelRequest{
		SalonID:              "salon_1",
		SafeReply:            "What phone number should we use?",
		AITone:               "natural_human",
		SelectedServiceNames: []string{"Classic Manicure", "Gel Removal"},
	})
	if err != nil {
		t.Fatalf("GenerateReply returned error: %v", err)
	}
	if reply.Message != "What phone number should we use?" || reply.Confidence != 0.9 {
		t.Fatalf("reply = %#v", reply)
	}
	if observed.InputTokens != 17 || observed.OutputTokens != 6 || observed.TotalTokens != 23 {
		t.Fatalf("observed usage = %#v", observed)
	}
}

func TestGenerateConsultationQuestionUsesCallerLanguageInstructions(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey: "test-key", BaseURL: "https://openai.test/v1", ReplyModel: "gpt-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		instructions, _ := payload["instructions"].(string)
		for _, required := range []string{"helpful consultation", "semantic labels", "ordinary caller language", "form validator"} {
			if !strings.Contains(instructions, required) {
				t.Fatalf("consultation instructions missing %q: %s", required, instructions)
			}
		}
		body, _ := json.Marshal(map[string]any{
			"output_text": `{"message":"I can help with that. What do you currently have on your nails?","confidence":0.95,"handoff":false,"reason":""}`,
		})
		return jsonResponse(body), nil
	})}
	reply, err := adapter.GenerateReply(context.Background(), voice.ModelRequest{
		SalonID: "salon_1", ReplyPolicy: conversation.ReplyPolicyConsultationQuestion,
		ConsultationQuestion: &conversation.ConsultationQuestionSpec{
			Field:   conversation.ConsultationNeedFieldCurrentSystem,
			Options: []string{conversation.ConsultationSystemNatural, conversation.ConsultationSystemGel},
		},
	})
	if err != nil || reply.Message != "I can help with that. What do you currently have on your nails?" {
		t.Fatalf("consultation reply = %#v err=%v", reply, err)
	}
}

func TestTranscribeSendsMultipartAudio(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey:             "test-key",
		BaseURL:            "https://openai.test/v1",
		TranscriptionModel: "transcribe-test",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path = %s, want /v1/audio/transcriptions", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("model"); got != "transcribe-test" {
			t.Fatalf("model = %q", got)
		}
		if got := r.FormValue("prompt"); got != "Active service names: Classic Manicure." {
			t.Fatalf("prompt = %q", got)
		}
		body, _ := json.Marshal(map[string]any{"text": "classic manicure"})
		return jsonResponse(body), nil
	})}

	text, err := adapter.Transcribe(context.Background(), "salon_1", voice.SpeechToTextRequest{
		Audio:       []byte("audio"),
		ContentType: "audio/wav",
		Prompt:      "Active service names: Classic Manicure.",
	})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if text != "classic manicure" {
		t.Fatalf("text = %q", text)
	}
}

func TestSynthesizeReturnsAudioBytes(t *testing.T) {
	adapter := NewAdapter(config.OpenAIVoiceConfig{
		APIKey:      "test-key",
		BaseURL:     "https://openai.test/v1",
		SpeechModel: "speech-test",
		SpeechVoice: "alloy",
	})
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("path = %s, want /v1/audio/speech", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode speech payload: %v", err)
		}
		if got := payload["model"]; got != "speech-test" {
			t.Fatalf("model = %#v, want speech-test", got)
		}
		if got := payload["voice"]; got != "alloy" {
			t.Fatalf("voice = %#v, want alloy", got)
		}
		if got := payload["input"]; got != "How can I help?" {
			t.Fatalf("input = %#v, want request text", got)
		}
		if got := payload["response_format"]; got != "mp3" {
			t.Fatalf("response_format = %#v, want mp3", got)
		}
		if _, ok := payload["format"]; ok {
			t.Fatalf("speech payload should not include deprecated format key: %#v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
			Body:       io.NopCloser(strings.NewReader("mp3-bytes")),
		}, nil
	})}

	audio, err := adapter.Synthesize(context.Background(), "salon_1", "How can I help?", "")
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	if string(audio) != "mp3-bytes" {
		t.Fatalf("audio = %q", string(audio))
	}
}

func TestParseRealtimeEvents(t *testing.T) {
	audio := parseRealtimeEvent([]byte(`{"type":"response.output_audio.delta","response_id":"resp_1","delta":"abc123"}`))
	if audio.Type != voice.RealtimeEventAudioDelta || audio.ResponseID != "resp_1" || audio.AudioBase64 != "abc123" {
		t.Fatalf("audio event = %#v", audio)
	}
	audioTranscript := parseRealtimeEvent([]byte(`{"type":"response.output_audio_transcript.done","response_id":"resp_1","transcript":"Backend reply."}`))
	if audioTranscript.Type != voice.RealtimeEventAudioTranscriptDone || audioTranscript.ResponseID != "resp_1" || audioTranscript.AudioTranscript != "Backend reply." {
		t.Fatalf("audio transcript event = %#v", audioTranscript)
	}

	transcript := parseRealtimeEvent([]byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"item_1","transcript":"gel removal","logprobs":[{"token":"gel","logprob":-0.2},{"token":" removal","logprob":-0.4}]}`))
	if transcript.Type != voice.RealtimeEventTranscriptDone || transcript.ItemID != "item_1" || transcript.Transcript != "gel removal" {
		t.Fatalf("transcript event = %#v", transcript)
	}
	if len(transcript.TranscriptLogProbs) != 2 || transcript.TranscriptLogProbs[0] != -0.2 || transcript.TranscriptLogProbs[1] != -0.4 {
		t.Fatalf("transcript logprobs = %#v", transcript.TranscriptLogProbs)
	}
	speechStarted := parseRealtimeEvent([]byte(`{"type":"input_audio_buffer.speech_started","item_id":"item_1","audio_start_ms":120}`))
	if speechStarted.Type != voice.RealtimeEventSpeechStarted || speechStarted.ItemID != "item_1" || speechStarted.AudioStartMS != 120 {
		t.Fatalf("speech started event = %#v", speechStarted)
	}
	speechStopped := parseRealtimeEvent([]byte(`{"type":"input_audio_buffer.speech_stopped","item_id":"item_1","audio_end_ms":1460}`))
	if speechStopped.Type != voice.RealtimeEventSpeechStopped || speechStopped.ItemID != "item_1" || speechStopped.AudioEndMS != 1460 {
		t.Fatalf("speech stopped event = %#v", speechStopped)
	}

	created := parseRealtimeEvent([]byte(`{"type":"response.created","response":{"id":"resp_1","metadata":{"manleai_request_id":"reply_7"}}}`))
	if created.Type != voice.RealtimeEventResponseCreated || created.ResponseID != "resp_1" || created.ResponseRequestID != "reply_7" {
		t.Fatalf("created event = %#v", created)
	}

	done := parseRealtimeEvent([]byte(`{"type":"response.done","response":{"id":"resp_1","status":"completed","metadata":{"manleai_request_id":"reply_7"}}}`))
	if done.Type != voice.RealtimeEventResponseDone || done.ResponseID != "resp_1" || done.ResponseRequestID != "reply_7" || done.ResponseStatus != "completed" {
		t.Fatalf("done event = %#v", done)
	}

	sessionUpdated := parseRealtimeEvent([]byte(`{"type":"session.updated"}`))
	if sessionUpdated.Type != voice.RealtimeEventSessionUpdated {
		t.Fatalf("session updated event = %#v", sessionUpdated)
	}

	apiErr := parseRealtimeEvent([]byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","param":"session.audio.input.format","message":"Unsupported audio format."}}`))
	if apiErr.Type != voice.RealtimeEventError || apiErr.ErrorCode != "invalid_value" || apiErr.ErrorParam != "session.audio.input.format" || !strings.Contains(apiErr.Error, "invalid_request_error") || !strings.Contains(apiErr.Error, "Unsupported audio format.") {
		t.Fatalf("api error event = %#v", apiErr)
	}
}

func TestRealtimeSessionConfigUsesLegacyShapeForPreviewModel(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-4o-realtime-preview",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{})

	if _, ok := session["input_audio_format"]; !ok {
		t.Fatalf("preview realtime session should use legacy input_audio_format shape: %#v", session)
	}
	if _, ok := session["audio"]; ok {
		t.Fatalf("preview realtime session should not use nested GA audio shape: %#v", session)
	}
	if realtimeHeaders(cfg).Get("OpenAI-Beta") != "realtime=v1" {
		t.Fatalf("preview realtime headers should include beta header")
	}
}

func TestRealtimeSessionConfigUsesGAShapeForRealtimeModel(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-realtime-2",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{})

	if _, ok := session["modalities"]; ok {
		t.Fatalf("GA realtime session should not include legacy modalities: %#v", session)
	}
	if outputModalities, ok := session["output_modalities"].([]string); !ok || len(outputModalities) != 1 || outputModalities[0] != "audio" {
		t.Fatalf("GA realtime session should include audio output_modalities: %#v", session)
	}
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should use nested audio shape: %#v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio.input: %#v", session)
	}
	inputFormat, ok := input["format"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include structured input format: %#v", session)
	}
	if inputFormat["type"] != "audio/pcmu" {
		t.Fatalf("GA realtime session input format = %#v, want audio/pcmu", inputFormat["type"])
	}
	output, ok := audio["output"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio.output: %#v", session)
	}
	outputFormat, ok := output["format"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include structured output format: %#v", session)
	}
	if outputFormat["type"] != "audio/pcmu" {
		t.Fatalf("GA realtime session output format = %#v, want audio/pcmu", outputFormat["type"])
	}
	if _, ok := session["input_audio_format"]; ok {
		t.Fatalf("GA realtime session should not use legacy input_audio_format shape: %#v", session)
	}
	if realtimeHeaders(cfg).Get("OpenAI-Beta") != "" {
		t.Fatalf("GA realtime headers should not include beta header")
	}
}

func TestRealtimeSessionConfigIncludesTranscriptionPrompt(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-realtime-2",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{
		TranscriptionPrompt: "Active service names: Classic Manicure.",
	})
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio: %#v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio.input: %#v", session)
	}
	transcription, ok := input["transcription"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include transcription config: %#v", input)
	}
	if transcription["prompt"] != "Active service names: Classic Manicure." {
		t.Fatalf("transcription prompt = %#v", transcription["prompt"])
	}
}

func TestRealtimeSessionConfigCapsGATranscriptionPrompt(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-realtime-2",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{
		TranscriptionPrompt: strings.Repeat("a", realtimeTranscriptionPromptMaxLength+50),
	})
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio: %#v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio.input: %#v", session)
	}
	transcription, ok := input["transcription"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include transcription config: %#v", input)
	}
	prompt, ok := transcription["prompt"].(string)
	if !ok {
		t.Fatalf("transcription prompt should be a string: %#v", transcription["prompt"])
	}
	if got := len([]rune(prompt)); got != realtimeTranscriptionPromptMaxLength {
		t.Fatalf("transcription prompt length = %d, want %d", got, realtimeTranscriptionPromptMaxLength)
	}
}

func TestRealtimeSessionConfigCapsLegacyTranscriptionPrompt(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-4o-realtime-preview",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{
		TranscriptionPrompt: strings.Repeat("a", realtimeTranscriptionPromptMaxLength+50),
	})
	transcription, ok := session["input_audio_transcription"].(map[string]any)
	if !ok {
		t.Fatalf("legacy realtime session should include transcription config: %#v", session)
	}
	prompt, ok := transcription["prompt"].(string)
	if !ok {
		t.Fatalf("transcription prompt should be a string: %#v", transcription["prompt"])
	}
	if got := len([]rune(prompt)); got != realtimeTranscriptionPromptMaxLength {
		t.Fatalf("transcription prompt length = %d, want %d", got, realtimeTranscriptionPromptMaxLength)
	}
}

func TestRealtimeSessionConfigDefaultsToAutomaticBackgroundNoiseHandling(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-realtime-2",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	turnDetection := gaTurnDetection(t, realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{}))

	if turnDetection["type"] != "server_vad" {
		t.Fatalf("turn detection type = %#v, want server_vad", turnDetection["type"])
	}
	if turnDetection["threshold"] != 0.65 {
		t.Fatalf("threshold = %#v, want automatic baseline threshold", turnDetection["threshold"])
	}
	if turnDetection["silence_duration_ms"] != 650 {
		t.Fatalf("silence duration = %#v, want automatic baseline duration", turnDetection["silence_duration_ms"])
	}
	if turnDetection["create_response"] != false || turnDetection["interrupt_response"] != false {
		t.Fatalf("realtime bridge should disable provider autonomous response/interrupt: %#v", turnDetection)
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{})
	include, ok := session["include"].([]string)
	if !ok || len(include) != 1 || include[0] != "item.input_audio_transcription.logprobs" {
		t.Fatalf("GA realtime session include = %#v, want transcription logprobs", session["include"])
	}
	audio := session["audio"].(map[string]any)
	input := audio["input"].(map[string]any)
	noiseReduction, ok := input["noise_reduction"].(map[string]any)
	if !ok || noiseReduction["type"] != "near_field" {
		t.Fatalf("automatic input noise reduction = %#v", input["noise_reduction"])
	}
}

func TestRealtimeSessionConfigPreservesLegacyQuietRoomAsMinimalProcessing(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:        "gpt-realtime-2",
		TranscriptionModel:   "gpt-4o-mini-transcribe",
		RealtimeVoice:        "alloy",
		RealtimeNoiseProfile: "quiet_room",
	}
	turnDetection := gaTurnDetection(t, realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{}))

	if turnDetection["threshold"] != 0.5 {
		t.Fatalf("threshold = %#v, want minimal processing threshold", turnDetection["threshold"])
	}
	if turnDetection["silence_duration_ms"] != 450 {
		t.Fatalf("silence duration = %#v, want minimal processing duration", turnDetection["silence_duration_ms"])
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{})
	audio := session["audio"].(map[string]any)
	input := audio["input"].(map[string]any)
	if _, ok := input["noise_reduction"]; ok {
		t.Fatalf("minimal processing should not force input noise reduction: %#v", input)
	}
}

func TestRealtimeResponseCreatePayloadUsesProtocolShape(t *testing.T) {
	ga := realtimeResponseCreatePayload(false, "reply-1", "Hello.")
	gaResponse, ok := ga["response"].(map[string]any)
	if !ok {
		t.Fatalf("GA response.create payload missing response: %#v", ga)
	}
	if _, ok := gaResponse["modalities"]; ok {
		t.Fatalf("GA response.create should not include legacy modalities: %#v", gaResponse)
	}
	if outputModalities, ok := gaResponse["output_modalities"].([]string); !ok || len(outputModalities) != 1 || outputModalities[0] != "audio" {
		t.Fatalf("GA response.create should include audio output_modalities: %#v", gaResponse)
	}
	if gaResponse["conversation"] != "none" {
		t.Fatalf("GA response.create should be isolated from the default conversation: %#v", gaResponse)
	}
	if input, ok := gaResponse["input"].([]any); !ok || len(input) != 0 {
		t.Fatalf("GA response.create should use isolated empty input: %#v", gaResponse["input"])
	}
	metadata, ok := gaResponse["metadata"].(map[string]string)
	if !ok || metadata["manleai_request_id"] != "reply-1" {
		t.Fatalf("GA response metadata = %#v", gaResponse["metadata"])
	}

	legacy := realtimeResponseCreatePayload(true, "reply-2", "Hello.")
	legacyResponse, ok := legacy["response"].(map[string]any)
	if !ok {
		t.Fatalf("legacy response.create payload missing response: %#v", legacy)
	}
	if modalities, ok := legacyResponse["modalities"].([]string); !ok || len(modalities) != 1 || modalities[0] != "audio" {
		t.Fatalf("legacy response.create should include audio modalities: %#v", legacyResponse)
	}
	if _, ok := legacyResponse["output_modalities"]; ok {
		t.Fatalf("legacy response.create should not include GA output_modalities: %#v", legacyResponse)
	}
	if _, ok := legacyResponse["conversation"]; ok {
		t.Fatalf("legacy response.create should not include GA conversation isolation: %#v", legacyResponse)
	}
}

func TestRealtimeTranscriptPolicyUsesNoiseProfileAndRequiresGAConfidence(t *testing.T) {
	ga := realtimeTranscriptPolicyForConfig(config.OpenAIVoiceConfig{RealtimeModel: "gpt-realtime-2"})
	if !ga.RequireLogProbs || ga.Profile != config.OpenAIRealtimeNoiseAutomatic || ga.EffectiveProfile != config.OpenAIRealtimeNoiseStandard || ga.MinMeanLogProb != -1 || ga.MinTokenLogProb != -2 || ga.MaxTokensPerSecond != 10 {
		t.Fatalf("automatic GA transcript policy = %#v", ga)
	}
	if ga.AdaptiveStrongNoise == nil || ga.AdaptiveStrongNoise.Profile != config.OpenAIRealtimeNoiseStrongRejection || ga.AdaptiveStrongNoise.MinMeanLogProb != -0.8 || ga.AdaptiveStrongNoise.MinTokenLogProb != -1.6 || ga.AdaptiveStrongNoise.MaxTokensPerSecond != 8 {
		t.Fatalf("automatic strong-noise policy = %#v", ga.AdaptiveStrongNoise)
	}
	legacyStrong := realtimeTranscriptPolicyForConfig(config.OpenAIVoiceConfig{RealtimeModel: "gpt-realtime-2", RealtimeNoiseProfile: "noisy_salon"})
	if legacyStrong.Profile != config.OpenAIRealtimeNoiseStrongRejection || legacyStrong.AdaptiveStrongNoise != nil || legacyStrong.MinMeanLogProb != -0.8 {
		t.Fatalf("legacy noisy_salon policy = %#v", legacyStrong)
	}
	legacy := realtimeTranscriptPolicyForConfig(config.OpenAIVoiceConfig{RealtimeModel: "gpt-4o-realtime-preview", RealtimeNoiseProfile: "quiet_room"})
	if legacy.RequireLogProbs {
		t.Fatalf("legacy protocol must not require unavailable GA logprobs: %#v", legacy)
	}
}

func gaTurnDetection(t *testing.T, session map[string]any) map[string]any {
	t.Helper()
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("session missing audio: %#v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("session missing audio.input: %#v", session)
	}
	turnDetection, ok := input["turn_detection"].(map[string]any)
	if !ok {
		t.Fatalf("session missing turn_detection: %#v", session)
	}
	return turnDetection
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}
