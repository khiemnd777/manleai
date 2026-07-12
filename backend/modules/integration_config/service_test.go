package integrationconfig

import (
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
	encrypted := service.mustEncryptSecrets(map[string]string{"client_secret": "square-secret-value"})
	if encrypted == "" || strings.Contains(encrypted, "square-secret-value") {
		t.Fatalf("secret was not encrypted: %q", encrypted)
	}

	updatedAt := time.Now().UTC()
	response := service.squareResponse(&StoredConfig{
		SalonID:  "salon_1",
		Provider: ProviderSquare,
		Enabled:  true,
		Settings: map[string]string{
			"environment":  "sandbox",
			"client_id":    "square-client-id",
			"redirect_url": "https://api.example.com/api/integrations/square/callback",
			"api_version":  "2026-05-20",
		},
		SecretsEncrypted: encrypted,
		UpdatedAt:        updatedAt,
	})

	if !response.Configured || !response.ClientSecretConfigured {
		t.Fatalf("response should report configured secret: %#v", response)
	}
	if response.ClientSecretSource != SecretSourceDatabase {
		t.Fatalf("secret source = %q, want database", response.ClientSecretSource)
	}
	if strings.Contains(response.ClientID+response.RedirectURL+response.APIVersion+response.ClientSecretSource, "square-secret-value") {
		t.Fatalf("response leaked secret: %#v", response)
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
