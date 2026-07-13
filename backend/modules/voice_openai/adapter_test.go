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
		if !strings.Contains(instructions, "Use only service and category IDs present in catalog_services") || !strings.Contains(instructions, "pending clarification is context, not a restriction") {
			t.Fatalf("conversation act instructions = %s", instructions)
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
		body, _ := json.Marshal(map[string]any{
			"output_text": `{"goal":"book_appointment","acts":[{"kind":"replace_service","entity":"service","source_ids":["service_gel"],"target_ids":["service_spa"],"source_category_id":"","source_category_name":"","target_category_id":"cat_pedi","target_category_name":"Pedicure","scope":"one","guest_scope":"","guest_ref":"","subject":"","value":"","count":0,"confidence":0.95,"reason":"explicit replacement"}],"questions":[{"subject":"availability","service_ids":["service_spa"],"staff_ids":[],"time_preference":{"direction":"","minutes":-1},"confidence":0.92,"reason":"caller asked about availability"}],"confidence":0.95,"reason":"correction plus question"}`,
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
		}},
	})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	if len(reply.Acts) != 1 || reply.Acts[0].Kind != conversation.ConversationActReplace || reply.Acts[0].TargetIDs[0] != "service_spa" || len(reply.Questions) != 1 || reply.Confidence != 0.95 {
		t.Fatalf("reply = %#v", reply)
	}
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
		})
		return jsonResponse(body), nil
	})}

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

func TestRealtimeSessionConfigDefaultsToNoisySalonVAD(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:      "gpt-realtime-2",
		TranscriptionModel: "gpt-4o-mini-transcribe",
		RealtimeVoice:      "alloy",
	}
	turnDetection := gaTurnDetection(t, realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{}))

	if turnDetection["type"] != "server_vad" {
		t.Fatalf("turn detection type = %#v, want server_vad", turnDetection["type"])
	}
	if turnDetection["threshold"] != 0.78 {
		t.Fatalf("threshold = %#v, want noisy salon threshold", turnDetection["threshold"])
	}
	if turnDetection["silence_duration_ms"] != 850 {
		t.Fatalf("silence duration = %#v, want noisy salon duration", turnDetection["silence_duration_ms"])
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
		t.Fatalf("noisy salon input noise reduction = %#v", input["noise_reduction"])
	}
}

func TestRealtimeSessionConfigUsesQuietRoomVADProfile(t *testing.T) {
	cfg := config.OpenAIVoiceConfig{
		RealtimeModel:        "gpt-realtime-2",
		TranscriptionModel:   "gpt-4o-mini-transcribe",
		RealtimeVoice:        "alloy",
		RealtimeNoiseProfile: "quiet_room",
	}
	turnDetection := gaTurnDetection(t, realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{}))

	if turnDetection["threshold"] != 0.5 {
		t.Fatalf("threshold = %#v, want quiet room threshold", turnDetection["threshold"])
	}
	if turnDetection["silence_duration_ms"] != 450 {
		t.Fatalf("silence duration = %#v, want quiet room duration", turnDetection["silence_duration_ms"])
	}
	session := realtimeSessionConfig(cfg, voice.RealtimeSessionOptions{})
	audio := session["audio"].(map[string]any)
	input := audio["input"].(map[string]any)
	if _, ok := input["noise_reduction"]; ok {
		t.Fatalf("quiet room should not force input noise reduction: %#v", input)
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
	ga := realtimeTranscriptPolicyForConfig(config.OpenAIVoiceConfig{RealtimeModel: "gpt-realtime-2", RealtimeNoiseProfile: "noisy"})
	if !ga.RequireLogProbs || ga.Profile != "noisy_salon" || ga.MinMeanLogProb != -0.8 || ga.MinTokenLogProb != -1.6 || ga.MaxTokensPerSecond != 8 {
		t.Fatalf("noisy GA transcript policy = %#v", ga)
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
