package integrationconfig

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
)

func TestSquareResponseMasksEncryptedClientSecret(t *testing.T) {
	cipher, err := encryption.NewTokenCipher("test-secret")
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	service := NewService(nil, cipher, config.Config{
		Square: config.SquareConfig{
			Environment: "sandbox",
			RedirectURL: "https://api.example.com/api/integrations/square/callback",
			APIVersion:  "2026-05-20",
		},
	})
	encrypted, err := service.encryptSecrets(map[string]string{
		"client_secret":         "square-secret-value",
		"webhook_signature_key": "square-webhook-secret-value",
	})
	if err != nil {
		t.Fatalf("encryptSecrets: %v", err)
	}
	if encrypted == "" || strings.Contains(encrypted, "square-secret-value") {
		t.Fatalf("secret was not encrypted: %q", encrypted)
	}

	updatedAt := time.Now().UTC()
	response := service.squareResponse(&StoredConfig{
		SalonID:  "salon_1",
		Provider: ProviderSquare,
		Enabled:  true,
		Settings: map[string]string{
			"environment":              "sandbox",
			"client_id":                "square-client-id",
			"redirect_url":             "https://api.example.com/api/integrations/square/callback",
			"api_version":              "2026-05-20",
			"webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
		},
		SecretsEncrypted: encrypted,
		UpdatedAt:        updatedAt,
	})

	if !response.Configured || !response.ClientSecretConfigured || !response.WebhookConfigured || !response.WebhookSignatureKeyConfigured {
		t.Fatalf("response should report configured secret: %#v", response)
	}
	if response.ClientSecretSource != SecretSourceDatabase {
		t.Fatalf("secret source = %q, want database", response.ClientSecretSource)
	}
	if response.WebhookSignatureKeySource != SecretSourceDatabase {
		t.Fatalf("webhook secret source = %q, want database", response.WebhookSignatureKeySource)
	}
	visible := response.ClientID + response.RedirectURL + response.APIVersion + response.ClientSecretSource +
		response.WebhookNotificationURL + response.WebhookSignatureKeySource
	if strings.Contains(visible, "square-secret-value") || strings.Contains(visible, "square-webhook-secret-value") {
		t.Fatalf("response leaked secret: %#v", response)
	}
}

func TestUpdateSquareAbortsBeforePersistenceWhenSecretEncryptionFails(t *testing.T) {
	encryptionErr := errors.New("injected encryption failure")
	store := &fakeIntegrationConfigStore{}
	cipher := &fakeSecretCipher{encryptErr: encryptionErr}
	service := &Service{repo: store, cipher: cipher, cfg: config.Config{Square: config.SquareConfig{Environment: "sandbox"}}}
	webhookURL := "https://api.example.com/api/integrations/square/webhook"

	_, err := service.UpdateSquare(context.Background(), "salon_1", "owner_1", UpdateSquareSettingsRequest{
		Environment:            "sandbox",
		ClientID:               "square-client-id",
		RedirectURL:            "https://api.example.com/api/integrations/square/callback",
		APIVersion:             "2026-05-20",
		WebhookNotificationURL: &webhookURL,
		WebhookSignatureKey:    "new-webhook-secret",
	})
	if !errors.Is(err, encryptionErr) {
		t.Fatalf("UpdateSquare error = %v, want injected encryption failure", err)
	}
	if store.upsertCalls != 0 {
		t.Fatalf("upsert calls = %d, want 0 when encryption fails", store.upsertCalls)
	}
}

func TestUpdateSquarePreservesCiphertextWhenSecretsAreUnchanged(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID:          "salon_1",
		Provider:         ProviderSquare,
		Enabled:          true,
		SecretsEncrypted: "existing-ciphertext",
		Settings: map[string]string{
			"webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
		},
	}}
	cipher := &fakeSecretCipher{
		decryptPlaintext: `{"client_secret":"existing-client-secret","webhook_signature_key":"existing-webhook-secret"}`,
		encryptErr:       errors.New("encrypt must not be called"),
	}
	service := &Service{repo: store, cipher: cipher, cfg: config.Config{Square: config.SquareConfig{Environment: "sandbox"}}}

	_, err := service.UpdateSquare(context.Background(), "salon_1", "owner_1", UpdateSquareSettingsRequest{
		Environment: "sandbox",
		ClientID:    "square-client-id",
		RedirectURL: "https://api.example.com/api/integrations/square/callback",
		APIVersion:  "2026-05-20",
	})
	if err != nil {
		t.Fatalf("UpdateSquare returned error: %v", err)
	}
	if cipher.encryptCalls != 0 {
		t.Fatalf("encrypt calls = %d, want 0 for unchanged secrets", cipher.encryptCalls)
	}
	if store.upsertCalls != 1 || store.upserted.SecretsEncrypted != "existing-ciphertext" {
		t.Fatalf("persisted config = %#v calls=%d, want preserved ciphertext", store.upserted, store.upsertCalls)
	}
}

func TestValidSquareWebhookNotificationURLRequiresAbsoluteHTTPS(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{value: "https://api.example.com/api/integrations/square/webhook", valid: true},
		{value: "http://api.example.com/api/integrations/square/webhook", valid: false},
		{value: "/api/integrations/square/webhook", valid: false},
		{value: "", valid: false},
	} {
		if got := validSquareWebhookNotificationURL(test.value); got != test.valid {
			t.Fatalf("validSquareWebhookNotificationURL(%q) = %t, want %t", test.value, got, test.valid)
		}
	}
}

func TestOpenAIResponseDefaultsRealtimeNoiseProfileAndStreamingSpeechOutput(t *testing.T) {
	cipher, err := encryption.NewTokenCipher("test-secret")
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	service := NewService(nil, cipher, config.Config{
		Voice: config.VoiceConfig{
			AI: config.VoiceAIConfig{
				Provider: ProviderOpenAI,
				OpenAI: config.OpenAIVoiceConfig{
					BaseURL:            "https://api.openai.com/v1",
					TranscriptionModel: "gpt-4o-mini-transcribe",
					ReplyModel:         "gpt-4.1-mini",
					SpeechModel:        "gpt-4o-mini-tts",
					SpeechVoice:        "alloy",
					RealtimeModel:      "gpt-realtime-2",
					RealtimeVoice:      "alloy",
				},
			},
		},
	})

	response := service.openAIResponse(&StoredConfig{
		SalonID:  "salon_1",
		Provider: ProviderOpenAI,
		Enabled:  true,
		Settings: map[string]string{
			"base_url":            "https://api.openai.com/v1",
			"transcription_model": "gpt-4o-mini-transcribe",
			"reply_model":         "gpt-4.1-mini",
			"speech_model":        "gpt-4o-mini-tts",
			"speech_voice":        "alloy",
			"realtime_enabled":    "true",
			"realtime_model":      "gpt-realtime-2",
			"realtime_voice":      "alloy",
		},
		UpdatedAt: time.Now().UTC(),
	})

	if response.RealtimeNoiseProfile != config.DefaultOpenAIRealtimeNoiseProfile {
		t.Fatalf("noise profile = %q, want %q", response.RealtimeNoiseProfile, config.DefaultOpenAIRealtimeNoiseProfile)
	}
	if response.SpeechOutputMode != config.OpenAISpeechOutputStreamingTTS {
		t.Fatalf("speech output mode = %q, want %q", response.SpeechOutputMode, config.OpenAISpeechOutputStreamingTTS)
	}
}

type fakeIntegrationConfigStore struct {
	existing    *StoredConfig
	upserted    StoredConfig
	upsertCalls int
}

func (f *fakeIntegrationConfigStore) EnsureSalonOwner(context.Context, string, string) error {
	return nil
}

func (f *fakeIntegrationConfigStore) ListForOwner(context.Context, string, string) ([]StoredConfig, error) {
	return nil, nil
}

func (f *fakeIntegrationConfigStore) Get(context.Context, string, string) (*StoredConfig, error) {
	if f.existing == nil {
		return nil, ErrNotFound
	}
	copyValue := *f.existing
	return &copyValue, nil
}

func (f *fakeIntegrationConfigStore) Upsert(_ context.Context, cfg StoredConfig) (*StoredConfig, error) {
	f.upsertCalls++
	f.upserted = cfg
	copyValue := cfg
	return &copyValue, nil
}

type fakeSecretCipher struct {
	decryptPlaintext string
	encryptErr       error
	encryptCalls     int
}

func (f *fakeSecretCipher) Encrypt(string) (string, error) {
	f.encryptCalls++
	if f.encryptErr != nil {
		return "", f.encryptErr
	}
	return "encrypted", nil
}

func (f *fakeSecretCipher) Decrypt(string) (string, error) {
	return f.decryptPlaintext, nil
}
