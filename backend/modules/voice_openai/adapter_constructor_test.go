package voice_openai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestTenantBoundAdapterRejectsBlankAndCrossTenantResolution(t *testing.T) {
	resolver := &mutableTestResolver{resolved: testResolvedConfig("salon_a", "reply")}
	adapter, err := NewTenantBoundAdapter(resolver)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Configured(context.Background(), "") || adapter.Configured(context.Background(), "salon_b") {
		t.Fatal("adapter accepted a blank or cross-tenant runtime lookup")
	}
	if !adapter.Configured(context.Background(), "salon_a") {
		t.Fatal("adapter rejected its exact tenant-bound runtime config")
	}
}

func TestTenantBoundAdapterUsesEachRequestsExactTenantCredentialAndModel(t *testing.T) {
	tenantA := testResolvedConfig("salon_a", "reply-a")
	tenantA.Config.APIKey = "key-a"
	tenantA.Config.TranscriptionModel = "transcribe-a"
	tenantA.IntegrationConfigID = "config-a"
	tenantA.ConfigVersion = 3
	tenantB := testResolvedConfig("salon_b", "reply-b")
	tenantB.Config.APIKey = "key-b"
	tenantB.Config.TranscriptionModel = "transcribe-b"
	tenantB.IntegrationConfigID = "config-b"
	tenantB.ConfigVersion = 8
	adapter, err := NewTenantBoundAdapter(mapTestResolver{values: map[string]openairuntime.ResolvedConfig{
		"salon_a": tenantA,
		"salon_b": tenantB,
	}})
	if err != nil {
		t.Fatal(err)
	}
	type observation struct{ authorization, model string }
	observed := make([]observation, 0, 2)
	adapter.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := request.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatalf("parse transcription request: %v", err)
		}
		observed = append(observed, observation{
			authorization: request.Header.Get("Authorization"),
			model:         request.FormValue("model"),
		})
		body, _ := json.Marshal(map[string]any{"text": "verified"})
		return jsonResponse(body), nil
	})}

	for _, salonID := range []string{"salon_a", "salon_b"} {
		if _, err := adapter.Transcribe(context.Background(), salonID, voice.SpeechToTextRequest{
			Audio: []byte("audio"), ContentType: "audio/wav",
		}); err != nil {
			t.Fatalf("transcribe %s: %v", salonID, err)
		}
	}
	if len(observed) != 2 || observed[0] != (observation{"Bearer key-a", "transcribe-a"}) || observed[1] != (observation{"Bearer key-b", "transcribe-b"}) {
		t.Fatalf("tenant-bound provider requests=%#v", observed)
	}
}

func TestTurnContractCircuitCannotCrossTenantBoundary(t *testing.T) {
	adapter, err := NewTenantBoundAdapter(mapTestResolver{values: map[string]openairuntime.ResolvedConfig{
		"salon_a": testResolvedConfig("salon_a", "reply-a"),
		"salon_b": testResolvedConfig("salon_b", "reply-b"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	adapter.openTurnCircuit("salon_a", "config-fingerprint", &voice.ProviderRequestError{Provider: "openai", StatusCode: 400})
	if adapter.openTurnCircuitError("salon_a", "config-fingerprint") == nil {
		t.Fatal("tenant A circuit was not opened")
	}
	if err := adapter.openTurnCircuitError("salon_b", "config-fingerprint"); err != nil {
		t.Fatalf("tenant A circuit affected tenant B: %v", err)
	}
}

// NewAdapter is deliberately test-only. Production code must construct the
// adapter with a tenant-bound resolver.
func NewAdapter(cfg config.OpenAIVoiceConfig) *Adapter {
	adapter, err := NewPinnedAdapter("salon_1", cfg)
	if err != nil {
		panic(err)
	}
	return adapter
}

type mutableTestResolver struct {
	resolved openairuntime.ResolvedConfig
}

type mapTestResolver struct {
	values map[string]openairuntime.ResolvedConfig
}

func (r mapTestResolver) ResolveOpenAIRuntimeConfig(_ context.Context, salonID string) (openairuntime.ResolvedConfig, error) {
	value, ok := r.values[salonID]
	if !ok {
		return openairuntime.ResolvedConfig{}, openairuntime.ErrInvalidSalon
	}
	return value, nil
}

func (r *mutableTestResolver) ResolveOpenAIRuntimeConfig(_ context.Context, salonID string) (openairuntime.ResolvedConfig, error) {
	if salonID != r.resolved.SalonID {
		return openairuntime.ResolvedConfig{}, openairuntime.ErrInvalidSalon
	}
	return r.resolved, nil
}

func testResolvedConfig(salonID, replyModel string) openairuntime.ResolvedConfig {
	return openairuntime.ResolvedConfig{
		SalonID: salonID, IntegrationConfigID: "config_1", ConfigVersion: 1,
		CredentialRevision: 1, CredentialIdentityEstablished: true,
		DestinationProfile: openairuntime.DestinationProfile, Enabled: true,
		Config: config.OpenAIVoiceConfig{
			APIKey: "test-key", BaseURL: openairuntime.CanonicalBaseURL,
			TranscriptionModel: "test-transcription", ReplyModel: replyModel,
			SpeechModel: "test-speech", SpeechVoice: "test-voice",
		},
	}
}
