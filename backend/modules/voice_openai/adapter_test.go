package voice_openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/voice"
)

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
		body, _ := json.Marshal(map[string]any{
			"output_text": `{"message":"What phone number should we use?","confidence":0.9,"handoff":false,"reason":""}`,
		})
		return jsonResponse(body), nil
	})}

	reply, err := adapter.GenerateReply(context.Background(), voice.ModelRequest{SalonID: "salon_1", SafeReply: "What phone number should we use?"})
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
		body, _ := json.Marshal(map[string]any{"text": "classic manicure"})
		return jsonResponse(body), nil
	})}

	text, err := adapter.Transcribe(context.Background(), "salon_1", []byte("audio"), "audio/wav")
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
	audio := parseRealtimeEvent([]byte(`{"type":"response.output_audio.delta","delta":"abc123"}`))
	if audio.Type != voice.RealtimeEventAudioDelta || audio.AudioBase64 != "abc123" {
		t.Fatalf("audio event = %#v", audio)
	}

	transcript := parseRealtimeEvent([]byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"item_1","transcript":"gel removal"}`))
	if transcript.Type != voice.RealtimeEventTranscriptDone || transcript.ItemID != "item_1" || transcript.Transcript != "gel removal" {
		t.Fatalf("transcript event = %#v", transcript)
	}

	done := parseRealtimeEvent([]byte(`{"type":"response.done"}`))
	if done.Type != voice.RealtimeEventResponseDone {
		t.Fatalf("done event = %#v", done)
	}

	sessionUpdated := parseRealtimeEvent([]byte(`{"type":"session.updated"}`))
	if sessionUpdated.Type != voice.RealtimeEventSessionUpdated {
		t.Fatalf("session updated event = %#v", sessionUpdated)
	}

	apiErr := parseRealtimeEvent([]byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","param":"session.audio.input.format","message":"Unsupported audio format."}}`))
	if apiErr.Type != voice.RealtimeEventError || !strings.Contains(apiErr.Error, "invalid_request_error") || !strings.Contains(apiErr.Error, "Unsupported audio format.") {
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

	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should use nested audio shape: %#v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("GA realtime session should include audio.input: %#v", session)
	}
	if _, ok := input["format"].(map[string]any); !ok {
		t.Fatalf("GA realtime session should include structured input format: %#v", session)
	}
	if _, ok := session["input_audio_format"]; ok {
		t.Fatalf("GA realtime session should not use legacy input_audio_format shape: %#v", session)
	}
	if realtimeHeaders(cfg).Get("OpenAI-Beta") != "" {
		t.Fatalf("GA realtime headers should not include beta header")
	}
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
