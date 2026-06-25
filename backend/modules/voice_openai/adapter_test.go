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
