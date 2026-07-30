package integrationconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
)

func TestIntegrationConfigResponseJSONNeverSerializesProviderSecrets(t *testing.T) {
	service := &Service{
		cipher: &fakeSecretCipher{decryptPlaintext: `{"client_secret":"square-secret-value","webhook_signature_key":"square-webhook-secret-value","auth_token":"twilio-auth-secret","account_sid":"AC00000000000000000000000000000000","api_key":"openai-secret-value"}`},
		cfg:    config.Config{},
	}
	response := IntegrationConfigsResponse{
		Square: service.squareResponse(&StoredConfig{
			Provider: ProviderSquare, Enabled: true, SecretsEncrypted: "ciphertext",
			Settings: map[string]string{"client_id": "square-client-id", "redirect_url": "https://api.example.com/square/callback"},
		}),
		Twilio: service.twilioResponse(&StoredConfig{
			Provider: ProviderTwilio, Enabled: true, SecretsEncrypted: "ciphertext",
			Settings: map[string]string{
				"public_base_url": "https://api.example.com", "owner_sms_destination": "+15555550123",
				"messaging_service_sid": "MG11111111111111111111111111111111", "sender_phone": "+15555550456",
			},
		}),
		OpenAI: service.openAIResponse(&StoredConfig{
			Provider: ProviderOpenAI, Enabled: true, SecretsEncrypted: "ciphertext",
			CredentialFingerprintHMAC: "openai-credential-fingerprint-must-not-leak",
			Settings:                  map[string]string{"base_url": "https://api.openai.com/v1", "reply_model": "gpt-safe"},
		}),
	}

	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	serialized := string(raw)
	for _, secret := range []string{
		"square-secret-value", "square-webhook-secret-value", "twilio-auth-secret",
		"AC00000000000000000000000000000000", "openai-secret-value",
		"openai-credential-fingerprint-must-not-leak",
		"+15555550123", "MG11111111111111111111111111111111", "+15555550456",
	} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("serialized response leaked %q: %s", secret, serialized)
		}
	}
	for _, secretField := range []string{
		`"client_secret":`, `"webhook_signature_key":`, `"auth_token":`, `"account_sid":`,
		`"api_key":`, `"owner_sms_destination":`, `"messaging_service_sid":`, `"sender_phone":`,
		`"clear_client_secret":`, `"clear_webhook_signature_key":`, `"clear_auth_token":`,
		`"clear_account_sid":`, `"clear_api_key":`, `"clear_owner_sms_destination":`,
		`"clear_messaging_service_sid":`, `"clear_sender_phone":`,
	} {
		if strings.Contains(serialized, secretField) {
			t.Fatalf("serialized response exposed write-only field %s: %s", secretField, serialized)
		}
	}
}

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

func TestSquareWebhookConfiguredUsesOnlyCompleteStoredSalonConfiguration(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderSquare, Enabled: true, SecretsEncrypted: "ciphertext",
		Settings: map[string]string{
			"webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
		},
	}}
	service := &Service{
		repo: store,
		cipher: &fakeSecretCipher{
			decryptPlaintext: `{"webhook_signature_key":"square-webhook-secret-value"}`,
		},
	}

	configured, err := service.SquareWebhookConfigured(context.Background(), "salon_1")
	if err != nil || !configured {
		t.Fatalf("configured/error = %t/%v, want true/nil", configured, err)
	}

	store.existing.Settings["webhook_notification_url"] = ""
	configured, err = service.SquareWebhookConfigured(context.Background(), "salon_1")
	if err != nil || configured {
		t.Fatalf("incomplete configured/error = %t/%v, want false/nil", configured, err)
	}

	store.existing = nil
	configured, err = service.SquareWebhookConfigured(context.Background(), "salon_1")
	if err != nil || configured {
		t.Fatalf("missing configured/error = %t/%v, want false/nil", configured, err)
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

func TestPlatformTechnicalMutationPreservesActualActorAndVersionFence(t *testing.T) {
	store := &fakeIntegrationConfigStore{}
	service := &Service{repo: store, cipher: &fakeSecretCipher{}, cfg: config.Config{Square: config.SquareConfig{Environment: "sandbox"}}}

	response, err := service.UpdateSquareForPlatform(context.Background(), "salon_1", "platform_ops_1", UpdateSquareSettingsRequest{
		TechnicalMutationControl: TechnicalMutationControl{ActionKey: "square-settings-1", ExpectedVersion: 4},
		Environment:              "sandbox",
		ClientID:                 "application-id",
		RedirectURL:              "https://api.example.com/api/integrations/square/callback",
		APIVersion:               "2026-05-20",
	})
	if err != nil {
		t.Fatalf("UpdateSquareForPlatform: %v", err)
	}
	if store.controlledCalls != 1 || store.command.ActorUserID != "platform_ops_1" || store.command.ActionKey != "square-settings-1" || store.command.ExpectedVersion != 4 {
		t.Fatalf("controlled command/calls = %#v/%d", store.command, store.controlledCalls)
	}
	if response.Version != 5 || response.Replayed {
		t.Fatalf("response = %#v, want version 5 non-replay", response)
	}
}

func TestPlatformTechnicalMutationRequiresActionIdentity(t *testing.T) {
	service := &Service{repo: &fakeIntegrationConfigStore{}, cipher: &fakeSecretCipher{}}
	_, err := service.UpdateOpenAIForPlatform(context.Background(), "salon_1", "platform_ops_1", UpdateOpenAISettingsRequest{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
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

func TestOpenAIResponseCanonicalizesLegacyRealtimeNoiseProfile(t *testing.T) {
	service := NewService(nil, nil, config.Config{
		Voice: config.VoiceConfig{AI: config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{}}},
	})

	response := service.openAIResponse(&StoredConfig{
		SalonID:  "salon_1",
		Provider: ProviderOpenAI,
		Settings: map[string]string{"realtime_noise_profile": "noisy_salon"},
	})

	if response.RealtimeNoiseProfile != config.OpenAIRealtimeNoiseStrongRejection {
		t.Fatalf("legacy noise profile = %q, want %q", response.RealtimeNoiseProfile, config.OpenAIRealtimeNoiseStrongRejection)
	}
}

func TestResolveOpenAIConfigStrictUsesOnlyEncryptedDatabaseRecord(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		ID: "config_1", SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true,
		Version: 1, CredentialRevision: 1, DestinationProfile: "openai_public", CredentialFingerprintHMAC: strings.Repeat("a", 64),
		Settings: map[string]string{
			"base_url": "https://api.openai.com/v1", "reply_model": "stored-model",
			"transcription_model": "stored-transcribe", "speech_model": "stored-speech", "speech_voice": "alloy",
		},
		SecretsEncrypted: "ciphertext",
	}}
	service := &Service{
		repo:   store,
		cipher: &fakeSecretCipher{decryptPlaintext: `{"api_key":"database-key"}`},
		cfg: config.Config{Voice: config.VoiceConfig{AI: config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{
			APIKey: "legacy-environment-key", BaseURL: "https://legacy.test/v1", ReplyModel: "legacy-model",
		}}}},
	}

	cfg, enabled, err := service.ResolveOpenAIConfigStrict(context.Background(), "salon_1")
	if err != nil || !enabled {
		t.Fatalf("strict resolve enabled=%t err=%v", enabled, err)
	}
	if cfg.APIKey != "database-key" || cfg.BaseURL != "https://api.openai.com/v1" || cfg.ReplyModel != "stored-model" {
		t.Fatalf("strict config used fallback or wrong source: %#v", cfg)
	}
}

func TestResolveOpenAIConfigStrictFailsClosedWithoutStoredRecord(t *testing.T) {
	service := &Service{
		repo: &fakeIntegrationConfigStore{}, cipher: &fakeSecretCipher{},
		cfg: config.Config{Voice: config.VoiceConfig{AI: config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-key"}}}},
	}
	if _, _, err := service.ResolveOpenAIConfigStrict(context.Background(), "salon_1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("strict resolve error=%v, want ErrNotFound", err)
	}
}

func TestUpdateOpenAIRejectsEveryTenantControlledDestinationBeforePersistence(t *testing.T) {
	store := &fakeIntegrationConfigStore{}
	service := &Service{repo: store, cipher: &fakeSecretCipher{}, cfg: config.Config{EncryptionKey: "identity-root"}}
	for _, destination := range []string{
		"http://api.openai.com/v1",
		"https://localhost/v1",
		"https://10.0.0.1/v1",
		"https://169.254.169.254/v1",
		"https://api.openai.com.evil.test/v1",
		"https://user:password@api.openai.com/v1",
	} {
		request := validOpenAIUpdateRequest()
		request.BaseURL = destination
		if _, err := service.UpdateOpenAI(context.Background(), "salon_1", "owner_1", request); !errors.Is(err, ErrValidation) {
			t.Fatalf("destination %q error=%v", destination, err)
		}
	}
	if store.upsertCalls != 0 {
		t.Fatalf("invalid destinations persisted %d times", store.upsertCalls)
	}
}

func TestUpdateOpenAIBlankKeyPreservesSameDestinationCredentialIdentity(t *testing.T) {
	existingFingerprint, err := openairuntime.CredentialFingerprint("identity-root", "existing-key")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		ID: "config_1", SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true,
		Version: 4, CredentialRevision: 5, CredentialFingerprintHMAC: existingFingerprint,
		DestinationProfile: openairuntime.DestinationProfile, SecretsEncrypted: "existing-ciphertext",
		Settings: map[string]string{"base_url": openairuntime.CanonicalBaseURL},
	}}
	cipher := &fakeSecretCipher{decryptPlaintext: `{"api_key":"existing-key"}`}
	service := &Service{repo: store, cipher: cipher, cfg: config.Config{EncryptionKey: "identity-root"}}
	request := validOpenAIUpdateRequest()
	request.APIKey = ""
	response, err := service.UpdateOpenAI(context.Background(), "salon_1", "owner_1", request)
	if err != nil {
		t.Fatal(err)
	}
	if cipher.encryptCalls != 0 || store.upserted.SecretsEncrypted != "existing-ciphertext" {
		t.Fatalf("unchanged key was re-encrypted: calls=%d stored=%q", cipher.encryptCalls, store.upserted.SecretsEncrypted)
	}
	if store.upserted.CredentialRevision != 5 || store.upserted.CredentialFingerprintHMAC != existingFingerprint || store.upserted.DestinationProfile != openairuntime.DestinationProfile {
		t.Fatalf("preserved identity=%#v", store.upserted)
	}
	if response.BaseURL != openairuntime.CanonicalBaseURL || !response.DestinationManaged || !response.RuntimeResolvable {
		t.Fatalf("response=%#v", response)
	}
}

func TestUpdateOpenAINewKeyRotatesCredentialIdentityAndRevision(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		ID: "config_1", SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true,
		Version: 4, CredentialRevision: 5,
		CredentialFingerprintHMAC: strings.Repeat("a", 64), DestinationProfile: openairuntime.DestinationProfile,
		SecretsEncrypted: "existing-ciphertext", Settings: map[string]string{"base_url": openairuntime.CanonicalBaseURL},
	}}
	cipher := &fakeSecretCipher{decryptPlaintext: `{"api_key":"existing-key"}`}
	service := &Service{repo: store, cipher: cipher, cfg: config.Config{EncryptionKey: "identity-root"}}
	request := validOpenAIUpdateRequest()
	request.APIKey = "rotated-key"
	if _, err := service.UpdateOpenAI(context.Background(), "salon_1", "owner_1", request); err != nil {
		t.Fatal(err)
	}
	if cipher.encryptCalls != 1 || store.upserted.CredentialRevision != 6 || len(store.upserted.CredentialFingerprintHMAC) != 64 || store.upserted.CredentialFingerprintHMAC == strings.Repeat("a", 64) || strings.Contains(store.upserted.CredentialFingerprintHMAC, "rotated-key") {
		t.Fatalf("rotated identity=%#v encrypt_calls=%d", store.upserted, cipher.encryptCalls)
	}
}

func validOpenAIUpdateRequest() UpdateOpenAISettingsRequest {
	return UpdateOpenAISettingsRequest{
		Enabled: true, APIKey: "new-key", BaseURL: openairuntime.CanonicalBaseURL,
		TranscriptionModel: "transcribe", ReplyModel: "reply",
		SpeechModel: "speech", SpeechVoice: "voice",
		SpeechOutputMode: config.OpenAISpeechOutputStreamingTTS,
	}
}

func TestRuntimeResolversKeepSquareBootstrapButFailClosedForMissingVoiceConfigs(t *testing.T) {
	service := &Service{
		repo:   &fakeIntegrationConfigStore{},
		cipher: &fakeSecretCipher{},
		cfg: config.Config{
			Square: config.SquareConfig{
				ClientID: "legacy-square-client", ClientSecret: "legacy-square-secret",
				RedirectURL: "https://legacy.example.com/square/callback", APIVersion: "legacy-version",
			},
			Voice: config.VoiceConfig{
				Provider: "twilio", PublicBaseURL: "https://legacy.example.com",
				Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
				AI: config.VoiceAIConfig{Provider: ProviderOpenAI, OpenAI: config.OpenAIVoiceConfig{
					APIKey: "legacy-openai-key", BaseURL: "https://legacy-openai.example.com/v1", ReplyModel: "legacy-reply",
				}},
			},
		},
	}

	squareCfg, err := service.ResolveSquareConfig(context.Background(), "salon_1")
	if err != nil || squareCfg.ClientSecret != "legacy-square-secret" || squareCfg.ClientID != "legacy-square-client" {
		t.Fatalf("legacy Square config/error = %#v/%v", squareCfg, err)
	}
	twilioCfg, publicBaseURL, err := service.ResolveTwilioConfig(context.Background(), "salon_1")
	if !errors.Is(err, ErrNotFound) || twilioCfg.AuthToken != "" || publicBaseURL != "" {
		t.Fatalf("Twilio config/base/error = %#v/%q/%v, want missing stored config", twilioCfg, publicBaseURL, err)
	}
	openAICfg, enabled, err := service.ResolveOpenAIConfig(context.Background(), "salon_1")
	if !errors.Is(err, ErrNotFound) || enabled || openAICfg.APIKey != "" || openAICfg.ReplyModel != "" {
		t.Fatalf("OpenAI config/enabled/error = %#v/%t/%v, want missing stored config", openAICfg, enabled, err)
	}
	twilioResponse := service.twilioResponse(nil)
	if twilioResponse.Configured || twilioResponse.PublicBaseURL != "" || twilioResponse.IncomingPath != "" || twilioResponse.AuthTokenSource != SecretSourceNone {
		t.Fatalf("missing Twilio response leaked legacy environment config: %#v", twilioResponse)
	}
	openAIResponse := service.openAIResponse(nil)
	if openAIResponse.Enabled || openAIResponse.Configured || openAIResponse.BaseURL != "https://api.openai.com/v1" || !openAIResponse.DestinationManaged || openAIResponse.ReplyModel != "" || openAIResponse.APIKeySource != SecretSourceNone {
		t.Fatalf("missing OpenAI response leaked legacy environment config: %#v", openAIResponse)
	}
}

func TestRuntimeResolversPropagateRepositoryErrorsWithoutLegacyFallback(t *testing.T) {
	repositoryErr := errors.New("repository unavailable")
	service := &Service{
		repo:   &fakeIntegrationConfigStore{getErr: repositoryErr},
		cipher: &fakeSecretCipher{},
		cfg: config.Config{
			Square: config.SquareConfig{ClientSecret: "legacy-square-secret"},
			Voice: config.VoiceConfig{
				Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
				AI:     config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-openai-key"}},
			},
		},
	}

	if cfg, err := service.ResolveSquareConfig(context.Background(), "salon_1"); !errors.Is(err, repositoryErr) || cfg.ClientSecret != "" {
		t.Fatalf("Square config/error = %#v/%v, want repository failure and no fallback", cfg, err)
	}
	if cfg, baseURL, err := service.ResolveTwilioConfig(context.Background(), "salon_1"); !errors.Is(err, repositoryErr) || cfg.AuthToken != "" || baseURL != "" {
		t.Fatalf("Twilio config/base/error = %#v/%q/%v, want repository failure and no fallback", cfg, baseURL, err)
	}
	if cfg, enabled, err := service.ResolveOpenAIConfig(context.Background(), "salon_1"); !errors.Is(err, repositoryErr) || cfg.APIKey != "" || enabled {
		t.Fatalf("OpenAI config/enabled/error = %#v/%t/%v, want repository failure and no fallback", cfg, enabled, err)
	}
}

func TestRuntimeResolversPropagateStoredSecretDecryptionErrors(t *testing.T) {
	decryptErr := errors.New("decrypt failed")
	providers := []struct {
		name     string
		provider string
		resolve  func(*Service) error
	}{
		{name: "Square", provider: ProviderSquare, resolve: func(service *Service) error {
			_, err := service.ResolveSquareConfig(context.Background(), "salon_1")
			return err
		}},
		{name: "Twilio", provider: ProviderTwilio, resolve: func(service *Service) error {
			_, _, err := service.ResolveTwilioConfig(context.Background(), "salon_1")
			return err
		}},
		{name: "OpenAI", provider: ProviderOpenAI, resolve: func(service *Service) error {
			_, _, err := service.ResolveOpenAIConfig(context.Background(), "salon_1")
			return err
		}},
	}
	for _, test := range providers {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{
				repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
					SalonID: "salon_1", Provider: test.provider, Enabled: true,
					Settings: map[string]string{}, SecretsEncrypted: "ciphertext",
				}},
				cipher: &fakeSecretCipher{decryptErr: decryptErr},
				cfg: config.Config{Square: config.SquareConfig{ClientSecret: "legacy-square-secret"}, Voice: config.VoiceConfig{
					Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
					AI:     config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-openai-key"}},
				}},
			}
			if err := test.resolve(service); !errors.Is(err, decryptErr) {
				t.Fatalf("resolve error = %v, want decrypt error", err)
			}
		})
	}
}

func TestStoredConfigsNeverInheritLegacySecretsOrEnabledState(t *testing.T) {
	legacy := config.Config{
		Square: config.SquareConfig{ClientID: "legacy-client", ClientSecret: "legacy-square-secret", RedirectURL: "https://legacy.example.com/callback"},
		Voice: config.VoiceConfig{
			Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
			AI: config.VoiceAIConfig{Provider: ProviderOpenAI, OpenAI: config.OpenAIVoiceConfig{
				APIKey: "legacy-openai-key", BaseURL: "https://legacy-openai.example.com/v1", TranscriptionModel: "legacy-transcribe",
				ReplyModel: "legacy-reply", SpeechModel: "legacy-speech", SpeechVoice: "legacy-voice",
			}},
		},
	}

	squareService := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderSquare, Enabled: true,
		Settings: map[string]string{"client_id": "stored-client", "redirect_url": "https://stored.example.com/callback"},
	}}, cipher: &fakeSecretCipher{}, cfg: legacy}
	if cfg, err := squareService.ResolveSquareConfig(context.Background(), "salon_1"); !errors.Is(err, ErrValidation) || cfg.ClientSecret != "" {
		t.Fatalf("stored-empty Square config/error = %#v/%v", cfg, err)
	}
	if response := squareService.squareResponse(squareService.repo.(*fakeIntegrationConfigStore).existing); response.ClientSecretSource != SecretSourceNone || response.ClientSecretConfigured || response.Configured {
		t.Fatalf("stored-empty Square response inherited legacy secret: %#v", response)
	}

	twilioService := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderTwilio, Enabled: true, Settings: map[string]string{},
	}}, cipher: &fakeSecretCipher{}, cfg: legacy}
	if cfg, baseURL, err := twilioService.ResolveTwilioConfig(context.Background(), "salon_1"); !errors.Is(err, ErrValidation) || cfg.AuthToken != "" || baseURL != "" {
		t.Fatalf("stored-empty Twilio config/base/error = %#v/%q/%v", cfg, baseURL, err)
	}
	if response := twilioService.twilioResponse(twilioService.repo.(*fakeIntegrationConfigStore).existing); response.AuthTokenSource != SecretSourceNone || response.AuthTokenConfigured || response.Configured {
		t.Fatalf("stored-empty Twilio response inherited legacy secret: %#v", response)
	}

	openAIService := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true,
		Settings: map[string]string{"base_url": "https://stored-openai.example.com/v1", "transcription_model": "stored-transcribe", "reply_model": "stored-reply", "speech_model": "stored-speech", "speech_voice": "stored-voice"},
	}}, cipher: &fakeSecretCipher{}, cfg: legacy}
	if cfg, enabled, err := openAIService.ResolveOpenAIConfig(context.Background(), "salon_1"); !errors.Is(err, ErrValidation) || cfg.APIKey != "" || enabled {
		t.Fatalf("stored-empty OpenAI config/enabled/error = %#v/%t/%v", cfg, enabled, err)
	}
	if response := openAIService.openAIResponse(openAIService.repo.(*fakeIntegrationConfigStore).existing); response.APIKeySource != SecretSourceNone || response.APIKeyConfigured || response.Configured {
		t.Fatalf("stored-empty OpenAI response inherited legacy secret: %#v", response)
	}
}

func TestPlatformPersistedConfigurationNeverExportsLegacyFallback(t *testing.T) {
	service := &Service{
		repo: &fakeIntegrationConfigStore{items: []StoredConfig{{
			SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true,
			Settings: map[string]string{"base_url": "https://stored.example.com/v1", "reply_model": "stored-model"},
		}}},
		cipher: &fakeSecretCipher{},
		cfg: config.Config{
			Square: config.SquareConfig{ClientID: "legacy-client", ClientSecret: "legacy-secret", RedirectURL: "https://legacy.example.com/callback"},
			Voice: config.VoiceConfig{
				Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-token"},
				AI:     config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-key"}},
			},
		},
	}
	response, providers, err := service.GetAllPersistedForPlatform(context.Background(), "salon_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0] != ProviderOpenAI {
		t.Fatalf("providers=%v, want only persisted OpenAI", providers)
	}
	if response.Square.ClientID != "" || response.Square.ClientSecretConfigured || response.Twilio.AuthTokenConfigured {
		t.Fatalf("strict Platform response inherited legacy fallback: %#v", response)
	}
	if response.OpenAI.BaseURL != "https://api.openai.com/v1" || !response.OpenAI.DestinationManaged || response.OpenAI.ReplyModel != "stored-model" {
		t.Fatalf("strict Platform response lost persisted settings: %#v", response.OpenAI)
	}
}

func TestDashboardResponsesNeverReportEnvironmentForUnreadableStoredSecrets(t *testing.T) {
	service := &Service{
		cipher: &fakeSecretCipher{decryptErr: errors.New("stored secret unreadable")},
		cfg: config.Config{
			Square: config.SquareConfig{ClientSecret: "legacy-square-secret"},
			Voice: config.VoiceConfig{
				Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
				AI:     config.VoiceAIConfig{OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-openai-key"}},
			},
		},
	}
	stored := &StoredConfig{SalonID: "salon_1", Enabled: true, Settings: map[string]string{}, SecretsEncrypted: "ciphertext"}

	stored.Provider = ProviderSquare
	if response := service.squareResponse(stored); response.ClientSecretSource != SecretSourceNone || response.WebhookSignatureKeySource != SecretSourceNone {
		t.Fatalf("Square response reported fallback for unreadable stored secret: %#v", response)
	}
	stored.Provider = ProviderTwilio
	if response := service.twilioResponse(stored); response.AuthTokenSource != SecretSourceNone {
		t.Fatalf("Twilio response reported fallback for unreadable stored secret: %#v", response)
	}
	stored.Provider = ProviderOpenAI
	if response := service.openAIResponse(stored); response.APIKeySource != SecretSourceNone {
		t.Fatalf("OpenAI response reported fallback for unreadable stored secret: %#v", response)
	}
}

func TestRuntimeResolversUseCompleteStoredConfigsWithoutLegacyMixing(t *testing.T) {
	legacy := config.Config{
		Square: config.SquareConfig{ClientID: "legacy-client", ClientSecret: "legacy-secret", RedirectURL: "https://legacy.example.com/callback"},
		Voice: config.VoiceConfig{
			PublicBaseURL: "https://legacy.example.com", Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-token"},
			AI: config.VoiceAIConfig{Provider: ProviderOpenAI, OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-key", ReplyModel: "legacy-model"}},
		},
	}

	square := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderSquare, Enabled: true, SecretsEncrypted: "ciphertext",
		Settings: map[string]string{"environment": "production", "client_id": "stored-client", "redirect_url": "https://stored.example.com/callback", "api_version": "stored-version"},
	}}, cipher: &fakeSecretCipher{decryptPlaintext: `{"client_secret":"stored-secret"}`}, cfg: legacy}
	squareCfg, err := square.ResolveSquareConfig(context.Background(), "salon_1")
	if err != nil || squareCfg.ClientID != "stored-client" || squareCfg.ClientSecret != "stored-secret" || squareCfg.RedirectURL != "https://stored.example.com/callback" {
		t.Fatalf("stored Square config/error = %#v/%v", squareCfg, err)
	}

	twilio := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon_1", Provider: ProviderTwilio, Enabled: true, SecretsEncrypted: "ciphertext",
		Settings: map[string]string{"public_base_url": "https://stored.example.com", "incoming_path": "/stored/incoming"},
	}}, cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"stored-token"}`}, cfg: legacy}
	twilioCfg, baseURL, err := twilio.ResolveTwilioConfig(context.Background(), "salon_1")
	if err != nil || twilioCfg.AuthToken != "stored-token" || twilioCfg.IncomingPath != "/stored/incoming" || baseURL != "https://stored.example.com" {
		t.Fatalf("stored Twilio config/base/error = %#v/%q/%v", twilioCfg, baseURL, err)
	}

	openAI := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{
		ID: "config_1", SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: true, SecretsEncrypted: "ciphertext",
		Version: 1, CredentialRevision: 1, DestinationProfile: "openai_public", CredentialFingerprintHMAC: strings.Repeat("b", 64),
		Settings: map[string]string{"base_url": "https://api.openai.com/v1", "transcription_model": "stored-transcribe", "reply_model": "stored-reply", "speech_model": "stored-speech", "speech_voice": "stored-voice"},
	}}, cipher: &fakeSecretCipher{decryptPlaintext: `{"api_key":"stored-key"}`}, cfg: legacy}
	openAICfg, enabled, err := openAI.ResolveOpenAIConfig(context.Background(), "salon_1")
	if err != nil || !enabled || openAICfg.APIKey != "stored-key" || openAICfg.ReplyModel != "stored-reply" || openAICfg.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("stored OpenAI config/enabled/error = %#v/%t/%v", openAICfg, enabled, err)
	}
}

func TestStoredDisabledProvidersStayDisabled(t *testing.T) {
	legacy := config.Config{
		Square: config.SquareConfig{ClientSecret: "legacy-square-secret"},
		Voice: config.VoiceConfig{
			Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-twilio-token"},
			AI:     config.VoiceAIConfig{Provider: ProviderOpenAI, OpenAI: config.OpenAIVoiceConfig{APIKey: "legacy-openai-key"}},
		},
	}
	secrets := `{"client_secret":"stored-square-secret","auth_token":"stored-twilio-token","api_key":"stored-openai-key"}`

	square := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{SalonID: "salon_1", Provider: ProviderSquare, Enabled: false, Settings: map[string]string{}, SecretsEncrypted: "ciphertext"}}, cipher: &fakeSecretCipher{decryptPlaintext: secrets}, cfg: legacy}
	if _, err := square.ResolveSquareConfig(context.Background(), "salon_1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("disabled Square error = %v", err)
	}
	twilio := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{SalonID: "salon_1", Provider: ProviderTwilio, Enabled: false, Settings: map[string]string{}, SecretsEncrypted: "ciphertext"}}, cipher: &fakeSecretCipher{decryptPlaintext: secrets}, cfg: legacy}
	if _, _, err := twilio.ResolveTwilioConfig(context.Background(), "salon_1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("disabled Twilio error = %v", err)
	}
	openAI := &Service{repo: &fakeIntegrationConfigStore{existing: &StoredConfig{SalonID: "salon_1", Provider: ProviderOpenAI, Enabled: false, Settings: map[string]string{}, SecretsEncrypted: "ciphertext"}}, cipher: &fakeSecretCipher{decryptPlaintext: secrets}, cfg: legacy}
	cfg, enabled, err := openAI.ResolveOpenAIConfig(context.Background(), "salon_1")
	if err != nil || enabled || cfg.APIKey != "" {
		t.Fatalf("disabled OpenAI config/enabled/error = %#v/%t/%v", cfg, enabled, err)
	}
}

func TestTwilioMessagingResponseMasksDestinationAndSecrets(t *testing.T) {
	service := &Service{cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"auth-secret","account_sid":"AC00000000000000000000000000000000"}`}, cfg: config.Config{}}
	response := service.twilioResponse(&StoredConfig{
		SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
		Settings: map[string]string{
			"public_base_url": "https://api.example.com", "owner_sms_enabled": "true",
			"owner_sms_destination": "+15555550123", "owner_sms_consent_attested": "true",
			"messaging_service_sid": "MG11111111111111111111111111111111",
		},
		SecretsEncrypted: "ciphertext",
	})
	if response.OwnerSMSDestinationMasked != "••••0123" || !response.AccountSIDConfigured || !response.MessagingServiceConfigured {
		t.Fatalf("masked response=%#v", response)
	}
	visible := response.OwnerSMSDestinationMasked + response.AuthTokenSource + response.NotificationStatusURL
	for _, secret := range []string{"+15555550123", "auth-secret", "AC00000000000000000000000000000000", "MG11111111111111111111111111111111"} {
		if strings.Contains(visible, secret) {
			t.Fatalf("response leaked %q", secret)
		}
	}
}

func TestUpdateTwilioBuildsImmutableTenantRouteURLsAndValidatesIdentity(t *testing.T) {
	routeID := "11111111-1111-4111-8111-111111111111"
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		ID: routeID, SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
		Settings: map[string]string{}, SecretsEncrypted: "ciphertext",
	}}
	service := &Service{repo: store, cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"route-auth-token","account_sid":"AC11111111111111111111111111111111"}`}}
	number := "+13125550123"
	enabled := true
	response, err := service.UpdateTwilio(context.Background(), "salon-1", "owner-1", UpdateTwilioSettingsRequest{
		PublicBaseURL: "https://api.example.com", VoiceInboundNumber: &number, VoiceRoutingEnabled: &enabled,
		AccountSID: "AC11111111111111111111111111111111", AuthToken: "route-auth-token",
		VoiceTransport: "recording",
	})
	if err != nil {
		t.Fatalf("UpdateTwilio returned error: %v", err)
	}
	if store.upsertCalls != 1 || store.upserted.Settings["voice_inbound_number"] != number || store.upserted.Settings["voice_routing_enabled"] != "true" {
		t.Fatalf("persisted route settings=%#v calls=%d", store.upserted.Settings, store.upsertCalls)
	}
	wantIncoming := "https://api.example.com/api/voice/twilio/" + routeID + "/incoming"
	if response.VoiceRouteID != routeID || !response.VoiceRoutingConfigured || response.InboundWebhookURL != wantIncoming || response.AccountSIDHint != "AC••••1111" {
		t.Fatalf("route response=%#v", response)
	}

	invalidNumber := "312-555-0123"
	store.upsertCalls = 0
	if _, err := service.UpdateTwilio(context.Background(), "salon-1", "owner-1", UpdateTwilioSettingsRequest{
		PublicBaseURL: "https://api.example.com", VoiceInboundNumber: &invalidNumber, VoiceRoutingEnabled: &enabled,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid inbound number error=%v", err)
	}
	if store.upsertCalls != 0 {
		t.Fatalf("invalid route wrote %d configs", store.upsertCalls)
	}
}

func TestResolveTwilioVoiceRouteBindsLocatedTenantBeforeSecretUse(t *testing.T) {
	routeID := "11111111-1111-4111-8111-111111111111"
	store := &fakeIntegrationConfigStore{
		locatedSalonID: "salon-1",
		route: &StoredConfig{
			ID: routeID, SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
			Settings: map[string]string{
				"voice_inbound_number": "+13125550123", "voice_routing_enabled": "true", "public_base_url": "https://api.example.com",
			},
			SecretsEncrypted: "ciphertext",
		},
	}
	service := &Service{repo: store, cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"route-auth-token","account_sid":"AC11111111111111111111111111111111"}`}}

	resolved, err := service.ResolveTwilioVoiceRoute(context.Background(), routeID)
	if err != nil {
		t.Fatalf("ResolveTwilioVoiceRoute returned error: %v", err)
	}
	if resolved.SalonID != "salon-1" || resolved.RouteID != routeID || store.routeContext.Scope != databasecontext.ScopeProvider || store.routeContext.SystemSalonID != "salon-1" {
		t.Fatalf("resolved route=%#v context=%#v", resolved, store.routeContext)
	}
}

func TestUpdateTwilioRequiresFreshConsentWhenDestinationChanges(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
		Settings: map[string]string{
			"public_base_url": "https://api.example.com", "owner_sms_enabled": "true",
			"owner_sms_destination": "+15555550123", "owner_sms_consent_attested": "true",
			"messaging_service_sid": "MG11111111111111111111111111111111",
		}, SecretsEncrypted: "ciphertext",
	}}
	service := &Service{repo: store, cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"auth-secret","account_sid":"AC00000000000000000000000000000000"}`}}
	newDestination := "+15555550456"
	_, err := service.UpdateTwilio(context.Background(), "salon-1", "owner-1", UpdateTwilioSettingsRequest{
		PublicBaseURL: "https://api.example.com", OwnerSMSDestination: &newDestination,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error=%v, want fresh consent validation", err)
	}
	if store.upsertCalls != 0 {
		t.Fatalf("upsert calls=%d", store.upsertCalls)
	}
}

func TestResolveTwilioMessagingConfigStrictDoesNotUseEnvironmentFallback(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
		Settings: map[string]string{
			"public_base_url": "https://api.example.com", "owner_sms_enabled": "true",
			"owner_sms_destination": "+15555550123", "owner_sms_consent_attested": "true",
			"messaging_service_sid": "MG11111111111111111111111111111111",
		}, SecretsEncrypted: "ciphertext",
	}}
	service := &Service{repo: store, cipher: &fakeSecretCipher{decryptPlaintext: `{"auth_token":"database-token","account_sid":"AC00000000000000000000000000000000"}`}, cfg: config.Config{Voice: config.VoiceConfig{Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-token"}}}}
	cfg, err := service.ResolveTwilioMessagingConfig(context.Background(), "salon-1")
	if err != nil {
		t.Fatalf("ResolveTwilioMessagingConfig: %v", err)
	}
	if cfg.AuthToken != "database-token" || cfg.OwnerSMSDestination != "+15555550123" {
		t.Fatalf("strict config=%#v", cfg)
	}
}

func TestResolveStoredTwilioAuthTokenIsNarrowAndDatabaseBacked(t *testing.T) {
	store := &fakeIntegrationConfigStore{existing: &StoredConfig{
		SalonID: "salon-1", Provider: ProviderTwilio, Enabled: true,
		Settings: map[string]string{
			"owner_sms_enabled": "true",
		},
		SecretsEncrypted: "ciphertext",
	}}
	service := &Service{
		repo: store,
		cipher: &fakeSecretCipher{
			decryptPlaintext: `{"auth_token":"database-token"}`,
		},
		cfg: config.Config{Voice: config.VoiceConfig{Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-environment-token"}}},
	}

	token, err := service.ResolveStoredTwilioAuthToken(context.Background(), "salon-1")
	if err != nil {
		t.Fatalf("ResolveStoredTwilioAuthToken: %v", err)
	}
	if token != "database-token" {
		t.Fatalf("token=%q, want stored token without messaging-field validation", token)
	}
}

func TestResolveStoredTwilioAuthTokenDoesNotFallBackWhenStoredConfigMissing(t *testing.T) {
	service := &Service{
		repo:   &fakeIntegrationConfigStore{},
		cipher: &fakeSecretCipher{},
		cfg:    config.Config{Voice: config.VoiceConfig{Twilio: config.TwilioVoiceConfig{AuthToken: "legacy-environment-token"}}},
	}

	token, err := service.ResolveStoredTwilioAuthToken(context.Background(), "salon-1")
	if !errors.Is(err, ErrNotFound) || token != "" {
		t.Fatalf("token/error=%q/%v, want no legacy fallback", token, err)
	}
}

type fakeIntegrationConfigStore struct {
	existing         *StoredConfig
	items            []StoredConfig
	getErr           error
	upserted         StoredConfig
	upsertCalls      int
	controlledCalls  int
	command          TechnicalMutationCommand
	controlledReplay bool
	locatedSalonID   string
	route            *StoredConfig
	routeContext     databasecontext.AccessContext
}

func (f *fakeIntegrationConfigStore) UpsertControlled(_ context.Context, cfg StoredConfig, command TechnicalMutationCommand) (*StoredConfig, bool, error) {
	f.controlledCalls++
	f.upserted = cfg
	f.command = command
	copyValue := cfg
	copyValue.Version = command.ExpectedVersion + 1
	return &copyValue, f.controlledReplay, nil
}

func (f *fakeIntegrationConfigStore) EnsureSalonOwner(context.Context, string, string) error {
	return nil
}

func (f *fakeIntegrationConfigStore) ListForOwner(context.Context, string, string) ([]StoredConfig, error) {
	return nil, nil
}

func (f *fakeIntegrationConfigStore) ListForSalon(context.Context, string) ([]StoredConfig, error) {
	return append([]StoredConfig{}, f.items...), nil
}

func (f *fakeIntegrationConfigStore) Get(context.Context, string, string) (*StoredConfig, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
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
	if copyValue.ID == "" && f.existing != nil {
		copyValue.ID = f.existing.ID
	}
	if copyValue.Version == 0 {
		copyValue.Version = 1
		if f.existing != nil {
			copyValue.Version = f.existing.Version + 1
		}
	}
	return &copyValue, nil
}

func (f *fakeIntegrationConfigStore) LocateTwilioVoiceRouteTenant(context.Context, string) (string, error) {
	if f.locatedSalonID == "" {
		return "", ErrNotFound
	}
	return f.locatedSalonID, nil
}

func (f *fakeIntegrationConfigStore) GetTwilioVoiceRoute(ctx context.Context, _, _ string) (*StoredConfig, error) {
	f.routeContext = databasecontext.FromContext(ctx)
	if f.route == nil {
		return nil, ErrNotFound
	}
	copyValue := *f.route
	return &copyValue, nil
}

type fakeSecretCipher struct {
	decryptPlaintext string
	decryptErr       error
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
	if f.decryptErr != nil {
		return "", f.decryptErr
	}
	return f.decryptPlaintext, nil
}
