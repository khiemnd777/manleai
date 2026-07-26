package integrationconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
)

var ErrValidation = errors.New("integration config validation failed")

type configStore interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	ListForOwner(ctx context.Context, salonID string, ownerUserID string) ([]StoredConfig, error)
	Get(ctx context.Context, salonID string, provider string) (*StoredConfig, error)
	Upsert(ctx context.Context, cfg StoredConfig) (*StoredConfig, error)
}

type secretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

type Service struct {
	repo   configStore
	cipher secretCipher
	cfg    config.Config
}

func NewService(repo *Repository, cipher *encryption.TokenCipher, cfg config.Config) *Service {
	return &Service{repo: repo, cipher: cipher, cfg: cfg}
}

func (s *Service) GetAll(ctx context.Context, salonID string, ownerUserID string) (*IntegrationConfigsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListForOwner(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	byProvider := map[string]*StoredConfig{}
	for i := range items {
		item := items[i]
		byProvider[item.Provider] = &item
	}
	return &IntegrationConfigsResponse{
		Square: s.squareResponse(byProvider[ProviderSquare]),
		Twilio: s.twilioResponse(byProvider[ProviderTwilio]),
		OpenAI: s.openAIResponse(byProvider[ProviderOpenAI]),
	}, nil
}

func (s *Service) UpdateSquare(ctx context.Context, salonID string, ownerUserID string, req UpdateSquareSettingsRequest) (*SquareSettingsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	existing, secrets, err := s.existingConfigAndSecrets(ctx, salonID, ProviderSquare)
	if err != nil {
		return nil, err
	}
	webhookNotificationURL := ""
	if existing != nil {
		webhookNotificationURL = strings.TrimSpace(existing.Settings["webhook_notification_url"])
	}
	if req.WebhookNotificationURL != nil {
		webhookNotificationURL = strings.TrimSpace(*req.WebhookNotificationURL)
	}
	settings := map[string]string{
		"environment":              defaultString(normalizeEnvironment(req.Environment), defaultString(s.cfg.Square.Environment, "sandbox")),
		"client_id":                strings.TrimSpace(req.ClientID),
		"redirect_url":             strings.TrimSpace(req.RedirectURL),
		"api_version":              strings.TrimSpace(req.APIVersion),
		"api_base_url":             strings.TrimRight(strings.TrimSpace(req.APIBaseURL), "/"),
		"webhook_notification_url": webhookNotificationURL,
	}
	if settings["redirect_url"] == "" || settings["api_version"] == "" {
		return nil, ErrValidation
	}
	if settings["client_id"] == "" && strings.TrimSpace(req.ClientSecret) != "" {
		return nil, ErrValidation
	}
	if settings["redirect_url"] != "" {
		if _, err := url.ParseRequestURI(settings["redirect_url"]); err != nil {
			return nil, ErrValidation
		}
	}
	if webhookNotificationURL != "" {
		if !validSquareWebhookNotificationURL(webhookNotificationURL) {
			return nil, ErrValidation
		}
	}
	secrets = updateSecret(secrets, "client_secret", req.ClientSecret, req.ClearClientSecret)
	secrets = updateSecret(secrets, "webhook_signature_key", req.WebhookSignatureKey, req.ClearWebhookSignatureKey)
	if strings.TrimSpace(secrets["webhook_signature_key"]) != "" && webhookNotificationURL == "" {
		return nil, ErrValidation
	}
	secretMutation := strings.TrimSpace(req.ClientSecret) != "" || req.ClearClientSecret ||
		strings.TrimSpace(req.WebhookSignatureKey) != "" || req.ClearWebhookSignatureKey
	encryptedSecrets := ""
	if existing != nil && !secretMutation {
		encryptedSecrets = existing.SecretsEncrypted
	} else {
		encryptedSecrets, err = s.encryptSecrets(secrets)
		if err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderSquare,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: encryptedSecrets,
	})
	if err != nil {
		return nil, err
	}
	returnPtr := s.squareResponse(updated)
	return &returnPtr, nil
}

func (s *Service) UpdateTwilio(ctx context.Context, salonID string, ownerUserID string, req UpdateTwilioSettingsRequest) (*TwilioSettingsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	existing, secrets, err := s.existingConfigAndSecrets(ctx, salonID, ProviderTwilio)
	if err != nil {
		return nil, err
	}
	settings := map[string]string{}
	if existing != nil {
		for key, value := range existing.Settings {
			settings[key] = value
		}
	}
	settings["public_base_url"] = strings.TrimRight(strings.TrimSpace(req.PublicBaseURL), "/")
	settings["incoming_path"] = defaultString(strings.TrimSpace(req.IncomingPath), "/api/voice/twilio/incoming")
	settings["turn_path"] = defaultString(strings.TrimSpace(req.TurnPath), "/api/voice/twilio/turn")
	settings["recording_path"] = defaultString(strings.TrimSpace(req.RecordingPath), "/api/voice/twilio/recording")
	settings["stream_path"] = defaultString(strings.TrimSpace(req.StreamPath), "/api/voice/twilio/stream")
	settings["voice_transport"] = normalizeVoiceTransport(req.VoiceTransport)
	if req.OwnerSMSEnabled != nil {
		settings["owner_sms_enabled"] = strconv.FormatBool(*req.OwnerSMSEnabled)
	}
	destinationChanged := false
	if req.ClearOwnerSMSDestination {
		destinationChanged = strings.TrimSpace(settings["owner_sms_destination"]) != ""
		settings["owner_sms_destination"] = ""
	} else if req.OwnerSMSDestination != nil {
		next := strings.TrimSpace(*req.OwnerSMSDestination)
		destinationChanged = next != strings.TrimSpace(settings["owner_sms_destination"])
		settings["owner_sms_destination"] = next
	}
	if req.ClearMessagingServiceSID {
		settings["messaging_service_sid"] = ""
	} else if req.MessagingServiceSID != nil {
		settings["messaging_service_sid"] = strings.TrimSpace(*req.MessagingServiceSID)
	}
	if req.ClearSenderPhone {
		settings["sender_phone"] = ""
	} else if req.SenderPhone != nil {
		settings["sender_phone"] = strings.TrimSpace(*req.SenderPhone)
	}
	if req.NotificationStatusPath != nil {
		settings["notification_status_path"] = strings.TrimSpace(*req.NotificationStatusPath)
	}
	if req.NotificationInboundPath != nil {
		settings["notification_inbound_path"] = strings.TrimSpace(*req.NotificationInboundPath)
	}
	settings["notification_status_path"] = defaultString(settings["notification_status_path"], "/api/notifications/twilio/status")
	settings["notification_inbound_path"] = twilioInboundPath(settings["notification_inbound_path"], salonID)
	if req.OwnerSMSConsentAttested != nil {
		settings["owner_sms_consent_attested"] = strconv.FormatBool(*req.OwnerSMSConsentAttested)
		if *req.OwnerSMSConsentAttested {
			if destinationChanged || strings.TrimSpace(settings["owner_sms_consent_attested_at"]) == "" {
				settings["owner_sms_consent_attested_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			}
		} else {
			delete(settings, "owner_sms_consent_attested_at")
		}
	}
	if destinationChanged && req.OwnerSMSConsentAttested == nil {
		settings["owner_sms_consent_attested"] = "false"
		delete(settings, "owner_sms_consent_attested_at")
	}
	if settings["public_base_url"] != "" {
		parsed, err := url.ParseRequestURI(settings["public_base_url"])
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, ErrValidation
		}
	}
	secrets = updateSecret(secrets, "auth_token", req.AuthToken, req.ClearAuthToken)
	secrets = updateSecret(secrets, "account_sid", req.AccountSID, req.ClearAccountSID)
	secretMutation := strings.TrimSpace(req.AuthToken) != "" || req.ClearAuthToken ||
		strings.TrimSpace(req.AccountSID) != "" || req.ClearAccountSID
	if err := validateTwilioMessagingSettings(settings, secrets); err != nil {
		return nil, err
	}
	encryptedSecrets := ""
	if existing != nil && !secretMutation {
		encryptedSecrets = existing.SecretsEncrypted
	} else {
		encryptedSecrets, err = s.encryptSecrets(secrets)
		if err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderTwilio,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: encryptedSecrets,
	})
	if err != nil {
		return nil, err
	}
	returnPtr := s.twilioResponse(updated)
	return &returnPtr, nil
}

func (s *Service) ResolveTwilioMessagingConfig(ctx context.Context, salonID string) (TwilioMessagingConfig, error) {
	if s == nil || s.repo == nil || s.cipher == nil || strings.TrimSpace(salonID) == "" {
		return TwilioMessagingConfig{}, ErrValidation
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), ProviderTwilio)
	if err != nil {
		return TwilioMessagingConfig{}, err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return TwilioMessagingConfig{}, err
	}
	publicBaseURL := strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
	cfg := TwilioMessagingConfig{
		Enabled:                 item.Enabled && boolSetting(item.Settings["owner_sms_enabled"]),
		OwnerSMSConsentAttested: boolSetting(item.Settings["owner_sms_consent_attested"]),
		OwnerSMSDestination:     strings.TrimSpace(item.Settings["owner_sms_destination"]),
		AccountSID:              strings.TrimSpace(secrets["account_sid"]),
		AuthToken:               strings.TrimSpace(secrets["auth_token"]),
		MessagingServiceSID:     strings.TrimSpace(item.Settings["messaging_service_sid"]),
		SenderPhone:             strings.TrimSpace(item.Settings["sender_phone"]),
		StatusCallbackURL:       urlForPath(publicBaseURL, defaultString(item.Settings["notification_status_path"], "/api/notifications/twilio/status")),
		InboundCallbackURL:      urlForPath(publicBaseURL, twilioInboundPath(item.Settings["notification_inbound_path"], salonID)),
	}
	if value := strings.TrimSpace(item.Settings["owner_sms_consent_attested_at"]); value != "" {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, value); parseErr == nil {
			cfg.OwnerSMSConsentAt = &parsed
		}
	}
	if cfg.Enabled {
		if err := validateTwilioMessagingConfig(cfg); err != nil {
			return TwilioMessagingConfig{}, err
		}
	}
	return cfg, nil
}

// ResolveTwilioCustomerMessagingConfig returns the salon-scoped Twilio
// transport without coupling customer consent to the owner's SMS destination
// or owner-notification enablement. Customer policy and per-destination consent
// are independently revalidated by the customer notification engine.
func (s *Service) ResolveTwilioCustomerMessagingConfig(ctx context.Context, salonID string) (TwilioMessagingConfig, error) {
	if s == nil || s.repo == nil || s.cipher == nil || strings.TrimSpace(salonID) == "" {
		return TwilioMessagingConfig{}, ErrValidation
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), ProviderTwilio)
	if err != nil {
		return TwilioMessagingConfig{}, err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return TwilioMessagingConfig{}, err
	}
	publicBaseURL := strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
	cfg := TwilioMessagingConfig{
		Enabled:             item.Enabled,
		AccountSID:          strings.TrimSpace(secrets["account_sid"]),
		AuthToken:           strings.TrimSpace(secrets["auth_token"]),
		MessagingServiceSID: strings.TrimSpace(item.Settings["messaging_service_sid"]),
		SenderPhone:         strings.TrimSpace(item.Settings["sender_phone"]),
		StatusCallbackURL:   urlForPath(publicBaseURL, defaultString(item.Settings["notification_status_path"], "/api/notifications/twilio/status")),
		InboundCallbackURL:  urlForPath(publicBaseURL, twilioInboundPath(item.Settings["notification_inbound_path"], salonID)),
	}
	if cfg.Enabled {
		if err := validateTwilioTransportConfig(cfg); err != nil {
			return TwilioMessagingConfig{}, err
		}
	}
	return cfg, nil
}

// ResolveStoredTwilioAuthToken returns only the salon-scoped Twilio secret used
// for runtime capability signing. It deliberately bypasses legacy environment
// fallback and does not couple voice readiness to owner-SMS configuration.
func (s *Service) ResolveStoredTwilioAuthToken(ctx context.Context, salonID string) (string, error) {
	if s == nil || s.repo == nil || s.cipher == nil || strings.TrimSpace(salonID) == "" {
		return "", ErrValidation
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), ProviderTwilio)
	if err != nil {
		return "", err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(secrets["auth_token"]), nil
}

func (s *Service) UpdateOpenAI(ctx context.Context, salonID string, ownerUserID string, req UpdateOpenAISettingsRequest) (*OpenAISettingsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	settings := map[string]string{
		"base_url":               strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		"transcription_model":    strings.TrimSpace(req.TranscriptionModel),
		"reply_model":            strings.TrimSpace(req.ReplyModel),
		"speech_model":           strings.TrimSpace(req.SpeechModel),
		"speech_voice":           strings.TrimSpace(req.SpeechVoice),
		"speech_output_mode":     config.NormalizeOpenAISpeechOutputMode(req.SpeechOutputMode),
		"realtime_enabled":       boolString(req.RealtimeEnabled),
		"realtime_model":         config.NormalizeOpenAIRealtimeModel(req.RealtimeModel),
		"realtime_voice":         strings.TrimSpace(req.RealtimeVoice),
		"realtime_noise_profile": config.NormalizeOpenAIRealtimeNoiseProfile(req.RealtimeNoiseProfile),
		"realtime_instructions":  strings.TrimSpace(req.RealtimeInstructions),
	}
	if settings["base_url"] != "" {
		parsed, err := url.ParseRequestURI(settings["base_url"])
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, ErrValidation
		}
	}
	if req.Enabled && (settings["base_url"] == "" || settings["transcription_model"] == "" || settings["reply_model"] == "" || settings["speech_model"] == "" || settings["speech_voice"] == "") {
		return nil, ErrValidation
	}
	if req.Enabled && req.RealtimeEnabled && (settings["realtime_model"] == "" || settings["realtime_voice"] == "") {
		return nil, ErrValidation
	}
	existing, secrets, err := s.existingConfigAndSecrets(ctx, salonID, ProviderOpenAI)
	if err != nil {
		return nil, err
	}
	secrets = updateSecret(secrets, "api_key", req.APIKey, req.ClearAPIKey)
	secretMutation := strings.TrimSpace(req.APIKey) != "" || req.ClearAPIKey
	encryptedSecrets := ""
	if existing != nil && !secretMutation {
		encryptedSecrets = existing.SecretsEncrypted
	} else {
		encryptedSecrets, err = s.encryptSecrets(secrets)
		if err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderOpenAI,
		Enabled:          req.Enabled,
		Settings:         settings,
		SecretsEncrypted: encryptedSecrets,
	})
	if err != nil {
		return nil, err
	}
	returnPtr := s.openAIResponse(updated)
	return &returnPtr, nil
}

func (s *Service) ResolveSquareConfig(ctx context.Context, salonID string) (config.SquareConfig, error) {
	item, secrets, err := s.resolveStored(ctx, salonID, ProviderSquare)
	if errors.Is(err, ErrNotFound) {
		return s.legacySquareConfig(), nil
	}
	if err != nil {
		return config.SquareConfig{}, err
	}
	if !item.Enabled {
		return config.SquareConfig{}, ErrValidation
	}
	cfg := squareConfigFromStored(item, secrets)
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return config.SquareConfig{}, ErrValidation
	}
	if (cfg.WebhookNotificationURL == "") != (cfg.WebhookSignatureKey == "") ||
		(cfg.WebhookNotificationURL != "" && !validSquareWebhookNotificationURL(cfg.WebhookNotificationURL)) {
		return config.SquareConfig{}, ErrValidation
	}
	return cfg, nil
}

func (s *Service) ResolveTwilioConfig(ctx context.Context, salonID string) (config.TwilioVoiceConfig, string, error) {
	item, secrets, err := s.resolveStored(ctx, salonID, ProviderTwilio)
	if errors.Is(err, ErrNotFound) {
		cfg, publicBaseURL := s.legacyTwilioConfig()
		return cfg, publicBaseURL, nil
	}
	if err != nil {
		return config.TwilioVoiceConfig{}, "", err
	}
	if !item.Enabled {
		return config.TwilioVoiceConfig{}, "", ErrValidation
	}
	cfg, publicBaseURL := twilioConfigFromStored(item, secrets)
	if cfg.AuthToken == "" {
		return config.TwilioVoiceConfig{}, "", ErrValidation
	}
	return cfg, publicBaseURL, nil
}

func (s *Service) ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	item, secrets, err := s.resolveStored(ctx, salonID, ProviderOpenAI)
	if errors.Is(err, ErrNotFound) {
		cfg, enabled := s.legacyOpenAIConfig()
		return cfg, enabled, nil
	}
	if err != nil {
		return config.OpenAIVoiceConfig{}, false, err
	}
	cfg := openAIConfigFromStored(item, secrets)
	if !item.Enabled {
		return cfg, false, nil
	}
	if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.TranscriptionModel == "" ||
		cfg.ReplyModel == "" || cfg.SpeechModel == "" || cfg.SpeechVoice == "" ||
		(cfg.RealtimeEnabled && (strings.TrimSpace(item.Settings["realtime_model"]) == "" || cfg.RealtimeVoice == "")) {
		return config.OpenAIVoiceConfig{}, false, ErrValidation
	}
	return cfg, true, nil
}

// ResolveOpenAIConfigStrict resolves only the encrypted salon-scoped database
// record. It deliberately has no legacy environment fallback and is used by
// paid evaluation workflows that must fail closed on missing, unreadable, or
// incomplete stored configuration.
func (s *Service) ResolveOpenAIConfigStrict(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	if s == nil || s.repo == nil || s.cipher == nil || strings.TrimSpace(salonID) == "" {
		return config.OpenAIVoiceConfig{}, false, ErrValidation
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), ProviderOpenAI)
	if err != nil {
		return config.OpenAIVoiceConfig{}, false, err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return config.OpenAIVoiceConfig{}, false, err
	}
	cfg := config.OpenAIVoiceConfig{
		APIKey:               strings.TrimSpace(secrets["api_key"]),
		BaseURL:              strings.TrimRight(strings.TrimSpace(item.Settings["base_url"]), "/"),
		TranscriptionModel:   strings.TrimSpace(item.Settings["transcription_model"]),
		ReplyModel:           strings.TrimSpace(item.Settings["reply_model"]),
		SpeechModel:          strings.TrimSpace(item.Settings["speech_model"]),
		SpeechVoice:          strings.TrimSpace(item.Settings["speech_voice"]),
		SpeechOutputMode:     config.NormalizeOpenAISpeechOutputMode(item.Settings["speech_output_mode"]),
		RealtimeEnabled:      boolSetting(item.Settings["realtime_enabled"]),
		RealtimeModel:        config.NormalizeOpenAIRealtimeModel(item.Settings["realtime_model"]),
		RealtimeVoice:        strings.TrimSpace(item.Settings["realtime_voice"]),
		RealtimeNoiseProfile: config.NormalizeOpenAIRealtimeNoiseProfile(item.Settings["realtime_noise_profile"]),
		RealtimeInstructions: strings.TrimSpace(item.Settings["realtime_instructions"]),
	}
	if !item.Enabled {
		return cfg, false, nil
	}
	if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.ReplyModel == "" {
		return config.OpenAIVoiceConfig{}, false, ErrValidation
	}
	return cfg, true, nil
}

func (s *Service) existingConfigAndSecrets(ctx context.Context, salonID string, provider string) (*StoredConfig, map[string]string, error) {
	existing, err := s.repo.Get(ctx, salonID, provider)
	if errors.Is(err, ErrNotFound) {
		return nil, map[string]string{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	secrets, err := s.decryptSecrets(existing.SecretsEncrypted)
	if err != nil {
		return nil, nil, err
	}
	return existing, secrets, nil
}

func (s *Service) resolveStored(ctx context.Context, salonID string, provider string) (*StoredConfig, map[string]string, error) {
	if s == nil || s.repo == nil || strings.TrimSpace(salonID) == "" || strings.TrimSpace(provider) == "" {
		return nil, nil, ErrValidation
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), provider)
	if err != nil {
		return nil, nil, err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return nil, nil, err
	}
	return item, secrets, nil
}

func (s *Service) legacySquareConfig() config.SquareConfig {
	cfg := s.cfg.Square
	cfg.WebhookNotificationURL = ""
	cfg.WebhookSignatureKey = ""
	cfg.Environment = defaultString(normalizeEnvironment(cfg.Environment), "sandbox")
	cfg.RedirectURL = defaultString(strings.TrimSpace(cfg.RedirectURL), "http://localhost:18089/api/integrations/square/callback")
	cfg.APIVersion = defaultString(strings.TrimSpace(cfg.APIVersion), "2026-05-20")
	cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	return cfg
}

func squareConfigFromStored(item *StoredConfig, secrets map[string]string) config.SquareConfig {
	if item == nil {
		return config.SquareConfig{}
	}
	return config.SquareConfig{
		Environment:            defaultString(normalizeEnvironment(item.Settings["environment"]), "sandbox"),
		ClientID:               strings.TrimSpace(item.Settings["client_id"]),
		ClientSecret:           strings.TrimSpace(secrets["client_secret"]),
		RedirectURL:            strings.TrimSpace(item.Settings["redirect_url"]),
		APIVersion:             defaultString(strings.TrimSpace(item.Settings["api_version"]), "2026-05-20"),
		APIBaseURL:             strings.TrimRight(strings.TrimSpace(item.Settings["api_base_url"]), "/"),
		WebhookNotificationURL: strings.TrimSpace(item.Settings["webhook_notification_url"]),
		WebhookSignatureKey:    strings.TrimSpace(secrets["webhook_signature_key"]),
	}
}

func (s *Service) legacyTwilioConfig() (config.TwilioVoiceConfig, string) {
	cfg := s.cfg.Voice.Twilio
	cfg.IncomingPath = defaultString(strings.TrimSpace(cfg.IncomingPath), "/api/voice/twilio/incoming")
	cfg.TurnPath = defaultString(strings.TrimSpace(cfg.TurnPath), "/api/voice/twilio/turn")
	cfg.RecordingPath = defaultString(strings.TrimSpace(cfg.RecordingPath), "/api/voice/twilio/recording")
	cfg.StreamPath = defaultString(strings.TrimSpace(cfg.StreamPath), "/api/voice/twilio/stream")
	cfg.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.VoiceTransport, "recording"))
	return cfg, strings.TrimRight(strings.TrimSpace(s.cfg.Voice.PublicBaseURL), "/")
}

func twilioConfigFromStored(item *StoredConfig, secrets map[string]string) (config.TwilioVoiceConfig, string) {
	if item == nil {
		return config.TwilioVoiceConfig{}, ""
	}
	cfg := config.TwilioVoiceConfig{
		AuthToken:      strings.TrimSpace(secrets["auth_token"]),
		IncomingPath:   defaultString(strings.TrimSpace(item.Settings["incoming_path"]), "/api/voice/twilio/incoming"),
		TurnPath:       defaultString(strings.TrimSpace(item.Settings["turn_path"]), "/api/voice/twilio/turn"),
		RecordingPath:  defaultString(strings.TrimSpace(item.Settings["recording_path"]), "/api/voice/twilio/recording"),
		StreamPath:     defaultString(strings.TrimSpace(item.Settings["stream_path"]), "/api/voice/twilio/stream"),
		VoiceTransport: normalizeVoiceTransport(defaultString(item.Settings["voice_transport"], "recording")),
	}
	return cfg, strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
}

func (s *Service) legacyOpenAIConfig() (config.OpenAIVoiceConfig, bool) {
	cfg := s.cfg.Voice.AI.OpenAI
	cfg.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), "https://api.openai.com/v1")
	cfg.TranscriptionModel = defaultString(strings.TrimSpace(cfg.TranscriptionModel), "gpt-4o-mini-transcribe")
	cfg.ReplyModel = defaultString(strings.TrimSpace(cfg.ReplyModel), "gpt-4.1-mini")
	cfg.SpeechModel = defaultString(strings.TrimSpace(cfg.SpeechModel), "tts-1")
	cfg.SpeechVoice = defaultString(strings.TrimSpace(cfg.SpeechVoice), "alloy")
	cfg.SpeechOutputMode = config.NormalizeOpenAISpeechOutputMode(cfg.SpeechOutputMode)
	cfg.RealtimeModel = config.NormalizeOpenAIRealtimeModel(cfg.RealtimeModel)
	cfg.RealtimeVoice = defaultString(strings.TrimSpace(cfg.RealtimeVoice), cfg.SpeechVoice)
	cfg.RealtimeNoiseProfile = config.NormalizeOpenAIRealtimeNoiseProfile(cfg.RealtimeNoiseProfile)
	cfg.RealtimeInstructions = strings.TrimSpace(cfg.RealtimeInstructions)
	return cfg, strings.TrimSpace(s.cfg.Voice.AI.Provider) == ProviderOpenAI
}

func openAIConfigFromStored(item *StoredConfig, secrets map[string]string) config.OpenAIVoiceConfig {
	if item == nil {
		return config.OpenAIVoiceConfig{}
	}
	return config.OpenAIVoiceConfig{
		APIKey:               strings.TrimSpace(secrets["api_key"]),
		BaseURL:              strings.TrimRight(strings.TrimSpace(item.Settings["base_url"]), "/"),
		TranscriptionModel:   strings.TrimSpace(item.Settings["transcription_model"]),
		ReplyModel:           strings.TrimSpace(item.Settings["reply_model"]),
		SpeechModel:          strings.TrimSpace(item.Settings["speech_model"]),
		SpeechVoice:          strings.TrimSpace(item.Settings["speech_voice"]),
		SpeechOutputMode:     config.NormalizeOpenAISpeechOutputMode(item.Settings["speech_output_mode"]),
		RealtimeEnabled:      boolSetting(item.Settings["realtime_enabled"]),
		RealtimeModel:        config.NormalizeOpenAIRealtimeModel(item.Settings["realtime_model"]),
		RealtimeVoice:        strings.TrimSpace(item.Settings["realtime_voice"]),
		RealtimeNoiseProfile: config.NormalizeOpenAIRealtimeNoiseProfile(item.Settings["realtime_noise_profile"]),
		RealtimeInstructions: strings.TrimSpace(item.Settings["realtime_instructions"]),
	}
}

func (s *Service) squareResponse(item *StoredConfig) SquareSettingsResponse {
	cfg := s.legacySquareConfig()
	enabled := true
	if item != nil {
		cfg = squareConfigFromStored(item, nil)
		enabled = item.Enabled
	}
	secretSource := SecretSourceNone
	webhookSecretSource := SecretSourceNone
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil {
			if strings.TrimSpace(secrets["client_secret"]) != "" {
				secretSource = SecretSourceDatabase
			}
			if strings.TrimSpace(secrets["webhook_signature_key"]) != "" {
				webhookSecretSource = SecretSourceDatabase
			}
		}
	}
	if item == nil && strings.TrimSpace(s.cfg.Square.ClientSecret) != "" {
		secretSource = SecretSourceEnvironment
	}
	updatedAt := updatedAt(item)
	return SquareSettingsResponse{
		Provider:                      ProviderSquare,
		Configured:                    enabled && strings.TrimSpace(cfg.ClientID) != "" && (secretSource != SecretSourceNone) && strings.TrimSpace(cfg.RedirectURL) != "",
		Environment:                   defaultString(normalizeEnvironment(cfg.Environment), "sandbox"),
		ClientID:                      cfg.ClientID,
		RedirectURL:                   cfg.RedirectURL,
		APIVersion:                    defaultString(cfg.APIVersion, "2026-05-20"),
		APIBaseURL:                    cfg.APIBaseURL,
		ClientSecretConfigured:        secretSource != SecretSourceNone,
		ClientSecretSource:            secretSource,
		WebhookNotificationURL:        cfg.WebhookNotificationURL,
		WebhookConfigured:             enabled && cfg.WebhookNotificationURL != "" && webhookSecretSource == SecretSourceDatabase,
		WebhookSignatureKeyConfigured: webhookSecretSource == SecretSourceDatabase,
		WebhookSignatureKeySource:     webhookSecretSource,
		UpdatedAt:                     updatedAt,
	}
}

func (s *Service) twilioResponse(item *StoredConfig) TwilioSettingsResponse {
	cfg, publicBaseURL := s.legacyTwilioConfig()
	enabled := true
	if item != nil {
		cfg, publicBaseURL = twilioConfigFromStored(item, nil)
		enabled = item.Enabled
	}
	secretSource := SecretSourceNone
	accountSIDConfigured := false
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil {
			if strings.TrimSpace(secrets["auth_token"]) != "" {
				secretSource = SecretSourceDatabase
			}
			accountSIDConfigured = strings.TrimSpace(secrets["account_sid"]) != ""
		}
	}
	if item == nil && strings.TrimSpace(s.cfg.Voice.Twilio.AuthToken) != "" {
		secretSource = SecretSourceEnvironment
	}
	updatedAt := updatedAt(item)
	ownerSMSEnabled := item != nil && boolSetting(item.Settings["owner_sms_enabled"])
	ownerSMSConsent := item != nil && boolSetting(item.Settings["owner_sms_consent_attested"])
	var ownerSMSConsentAt *time.Time
	if item != nil {
		if value := strings.TrimSpace(item.Settings["owner_sms_consent_attested_at"]); value != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				ownerSMSConsentAt = &parsed
			}
		}
	}
	statusPath := "/api/notifications/twilio/status"
	inboundPath := "/api/notifications/twilio/inbound"
	if item != nil {
		statusPath = defaultString(strings.TrimSpace(item.Settings["notification_status_path"]), statusPath)
		inboundPath = twilioInboundPath(item.Settings["notification_inbound_path"], item.SalonID)
	}
	return TwilioSettingsResponse{
		Provider:                   ProviderTwilio,
		Configured:                 enabled && secretSource != SecretSourceNone,
		PublicBaseURL:              publicBaseURL,
		IncomingPath:               cfg.IncomingPath,
		TurnPath:                   cfg.TurnPath,
		RecordingPath:              cfg.RecordingPath,
		StreamPath:                 cfg.StreamPath,
		VoiceTransport:             cfg.VoiceTransport,
		InboundWebhookURL:          urlForPath(publicBaseURL, cfg.IncomingPath),
		TurnWebhookURL:             urlForPath(publicBaseURL, cfg.TurnPath),
		RecordingWebhookURL:        urlForPath(publicBaseURL, cfg.RecordingPath),
		StreamWebhookURL:           wsURLForPath(publicBaseURL, cfg.StreamPath),
		AuthTokenConfigured:        secretSource != SecretSourceNone,
		AuthTokenSource:            secretSource,
		OwnerSMSEnabled:            ownerSMSEnabled,
		OwnerSMSDestinationMasked:  maskedPhone(setting(item, "owner_sms_destination")),
		OwnerSMSConsentAttested:    ownerSMSConsent,
		OwnerSMSConsentAttestedAt:  ownerSMSConsentAt,
		AccountSIDConfigured:       accountSIDConfigured,
		MessagingServiceConfigured: strings.TrimSpace(setting(item, "messaging_service_sid")) != "",
		SenderConfigured:           strings.TrimSpace(setting(item, "sender_phone")) != "",
		NotificationStatusPath:     statusPath,
		NotificationInboundPath:    inboundPath,
		NotificationStatusURL:      urlForPath(publicBaseURL, statusPath),
		NotificationInboundURL:     urlForPath(publicBaseURL, inboundPath),
		UpdatedAt:                  updatedAt,
	}
}

func setting(item *StoredConfig, key string) string {
	if item == nil {
		return ""
	}
	return item.Settings[key]
}

func maskedPhone(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return ""
	}
	return "••••" + value[len(value)-4:]
}

func validateTwilioMessagingSettings(settings map[string]string, secrets map[string]string) error {
	if !boolSetting(settings["owner_sms_enabled"]) {
		return nil
	}
	cfg := TwilioMessagingConfig{
		Enabled:                 true,
		OwnerSMSConsentAttested: boolSetting(settings["owner_sms_consent_attested"]),
		OwnerSMSDestination:     strings.TrimSpace(settings["owner_sms_destination"]),
		AccountSID:              strings.TrimSpace(secrets["account_sid"]),
		AuthToken:               strings.TrimSpace(secrets["auth_token"]),
		MessagingServiceSID:     strings.TrimSpace(settings["messaging_service_sid"]),
		SenderPhone:             strings.TrimSpace(settings["sender_phone"]),
		StatusCallbackURL:       urlForPath(strings.TrimRight(strings.TrimSpace(settings["public_base_url"]), "/"), settings["notification_status_path"]),
		InboundCallbackURL:      urlForPath(strings.TrimRight(strings.TrimSpace(settings["public_base_url"]), "/"), settings["notification_inbound_path"]),
	}
	return validateTwilioMessagingConfig(cfg)
}

func validateTwilioMessagingConfig(cfg TwilioMessagingConfig) error {
	if !cfg.OwnerSMSConsentAttested || !validE164(cfg.OwnerSMSDestination) ||
		validateTwilioTransportConfig(cfg) != nil {
		return ErrValidation
	}
	return nil
}

func validateTwilioTransportConfig(cfg TwilioMessagingConfig) error {
	if !validTwilioSID(cfg.AccountSID, "AC") || cfg.AuthToken == "" ||
		(cfg.MessagingServiceSID == "" && !validE164(cfg.SenderPhone)) ||
		(cfg.MessagingServiceSID != "" && !validTwilioSID(cfg.MessagingServiceSID, "MG")) ||
		!validHTTPSURL(cfg.StatusCallbackURL) || !validHTTPSURL(cfg.InboundCallbackURL) {
		return ErrValidation
	}
	return nil
}

func validTwilioSID(value, prefix string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 34 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, ch := range value[2:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func validE164(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 16 || value[0] != '+' {
		return false
	}
	for _, ch := range value[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func validHTTPSURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func (s *Service) openAIResponse(item *StoredConfig) OpenAISettingsResponse {
	cfg, enabled := s.legacyOpenAIConfig()
	if item != nil {
		cfg = openAIConfigFromStored(item, nil)
		enabled = item.Enabled
	}
	secretSource := SecretSourceNone
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil && strings.TrimSpace(secrets["api_key"]) != "" {
			secretSource = SecretSourceDatabase
		}
	}
	if item == nil && strings.TrimSpace(s.cfg.Voice.AI.OpenAI.APIKey) != "" {
		secretSource = SecretSourceEnvironment
	}
	updatedAt := updatedAt(item)
	return OpenAISettingsResponse{
		Provider:             ProviderOpenAI,
		Enabled:              enabled,
		Configured:           enabled && secretSource != SecretSourceNone && cfg.TranscriptionModel != "" && cfg.ReplyModel != "" && cfg.SpeechModel != "" && cfg.SpeechVoice != "",
		BaseURL:              cfg.BaseURL,
		TranscriptionModel:   cfg.TranscriptionModel,
		ReplyModel:           cfg.ReplyModel,
		SpeechModel:          cfg.SpeechModel,
		SpeechVoice:          cfg.SpeechVoice,
		SpeechOutputMode:     cfg.SpeechOutputMode,
		RealtimeEnabled:      cfg.RealtimeEnabled,
		RealtimeModel:        cfg.RealtimeModel,
		RealtimeVoice:        cfg.RealtimeVoice,
		RealtimeNoiseProfile: cfg.RealtimeNoiseProfile,
		RealtimeInstructions: cfg.RealtimeInstructions,
		APIKeyConfigured:     secretSource != SecretSourceNone,
		APIKeySource:         secretSource,
		UpdatedAt:            updatedAt,
	}
}

func (s *Service) decryptSecrets(encrypted string) (map[string]string, error) {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return map[string]string{}, nil
	}
	if s.cipher == nil {
		return nil, errors.New("integration config secret cipher is unavailable")
	}
	plaintext, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	if strings.TrimSpace(plaintext) == "" {
		return secrets, nil
	}
	if err := json.Unmarshal([]byte(plaintext), &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

func (s *Service) encryptSecrets(secrets map[string]string) (string, error) {
	normalized := map[string]string{}
	for key, value := range secrets {
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			normalized[key] = value
		}
	}
	if len(normalized) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	if s.cipher == nil {
		return "", errors.New("integration config secret cipher is unavailable")
	}
	return s.cipher.Encrypt(string(raw))
}

func updateSecret(secrets map[string]string, key string, value string, clear bool) map[string]string {
	if secrets == nil {
		secrets = map[string]string{}
	}
	if clear {
		delete(secrets, key)
		return secrets
	}
	value = strings.TrimSpace(value)
	if value != "" {
		secrets[key] = value
	}
	return secrets
}

func updatedAt(item *StoredConfig) *time.Time {
	if item == nil {
		return nil
	}
	return &item.UpdatedAt
}

func salonIDFromItem(item *StoredConfig) string {
	if item == nil {
		return ""
	}
	return item.SalonID
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeEnvironment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "production" {
		return "production"
	}
	return "sandbox"
}

func validSquareWebhookNotificationURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func normalizeVoiceTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "realtime_stream":
		return "realtime_stream"
	default:
		return "recording"
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func boolSetting(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func urlForPath(publicBaseURL string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" {
		return path
	}
	return publicBaseURL + "/" + strings.TrimLeft(path, "/")
}

func twilioInboundPath(path, salonID string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	salonID = strings.TrimSpace(salonID)
	if path == "" {
		path = "/api/notifications/twilio/inbound"
	}
	if salonID == "" {
		return path
	}
	// Stored legacy values may already contain the salon suffix. Collapse every
	// duplicate before appending the one canonical tenant segment used for
	// Twilio signature verification.
	for strings.HasSuffix(path, "/"+salonID) {
		path = strings.TrimRight(strings.TrimSuffix(path, "/"+salonID), "/")
	}
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "/" + path
	}
	return path + "/" + salonID
}

func wsURLForPath(publicBaseURL string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "ws://") || strings.HasPrefix(path, "wss://") {
		return path
	}
	webhookURL := urlForPath(publicBaseURL, path)
	switch {
	case strings.HasPrefix(webhookURL, "https://"):
		return "wss://" + strings.TrimPrefix(webhookURL, "https://")
	case strings.HasPrefix(webhookURL, "http://"):
		return "ws://" + strings.TrimPrefix(webhookURL, "http://")
	default:
		return webhookURL
	}
}
