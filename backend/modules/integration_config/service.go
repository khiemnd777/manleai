package integrationconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
)

var ErrValidation = errors.New("integration config validation failed")

type Service struct {
	repo   *Repository
	cipher *encryption.TokenCipher
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
	settings := map[string]string{
		"environment":  defaultString(normalizeEnvironment(req.Environment), defaultString(s.cfg.Square.Environment, "sandbox")),
		"client_id":    strings.TrimSpace(req.ClientID),
		"redirect_url": strings.TrimSpace(req.RedirectURL),
		"api_version":  strings.TrimSpace(req.APIVersion),
		"api_base_url": strings.TrimRight(strings.TrimSpace(req.APIBaseURL), "/"),
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
	existing, secrets, err := s.existingConfigAndSecrets(ctx, salonID, ProviderSquare)
	if err != nil {
		return nil, err
	}
	secrets = updateSecret(secrets, "client_secret", req.ClientSecret, req.ClearClientSecret)
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderSquare,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: s.mustEncryptSecrets(secrets),
	})
	if err != nil {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(req.ClientSecret) == "" && !req.ClearClientSecret {
		updated.SecretsEncrypted = existing.SecretsEncrypted
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
	settings := map[string]string{
		"public_base_url": strings.TrimRight(strings.TrimSpace(req.PublicBaseURL), "/"),
		"incoming_path":   defaultString(strings.TrimSpace(req.IncomingPath), "/api/voice/twilio/incoming"),
		"turn_path":       defaultString(strings.TrimSpace(req.TurnPath), "/api/voice/twilio/turn"),
		"recording_path":  defaultString(strings.TrimSpace(req.RecordingPath), "/api/voice/twilio/recording"),
		"stream_path":     defaultString(strings.TrimSpace(req.StreamPath), "/api/voice/twilio/stream"),
		"voice_transport": normalizeVoiceTransport(req.VoiceTransport),
	}
	if settings["public_base_url"] != "" {
		parsed, err := url.ParseRequestURI(settings["public_base_url"])
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, ErrValidation
		}
	}
	existing, secrets, err := s.existingConfigAndSecrets(ctx, salonID, ProviderTwilio)
	if err != nil {
		return nil, err
	}
	secrets = updateSecret(secrets, "auth_token", req.AuthToken, req.ClearAuthToken)
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderTwilio,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: s.mustEncryptSecrets(secrets),
	})
	if err != nil {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(req.AuthToken) == "" && !req.ClearAuthToken {
		updated.SecretsEncrypted = existing.SecretsEncrypted
	}
	returnPtr := s.twilioResponse(updated)
	return &returnPtr, nil
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
		"base_url":              strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		"transcription_model":   strings.TrimSpace(req.TranscriptionModel),
		"reply_model":           strings.TrimSpace(req.ReplyModel),
		"speech_model":          strings.TrimSpace(req.SpeechModel),
		"speech_voice":          strings.TrimSpace(req.SpeechVoice),
		"realtime_enabled":      boolString(req.RealtimeEnabled),
		"realtime_model":        strings.TrimSpace(req.RealtimeModel),
		"realtime_voice":        strings.TrimSpace(req.RealtimeVoice),
		"realtime_instructions": strings.TrimSpace(req.RealtimeInstructions),
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
	updated, err := s.repo.Upsert(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderOpenAI,
		Enabled:          req.Enabled,
		Settings:         settings,
		SecretsEncrypted: s.mustEncryptSecrets(secrets),
	})
	if err != nil {
		return nil, err
	}
	if existing != nil && strings.TrimSpace(req.APIKey) == "" && !req.ClearAPIKey {
		updated.SecretsEncrypted = existing.SecretsEncrypted
	}
	returnPtr := s.openAIResponse(updated)
	return &returnPtr, nil
}

func (s *Service) ResolveSquareConfig(ctx context.Context, salonID string) (config.SquareConfig, error) {
	cfg := s.cfg.Square
	cfg.Environment = defaultString(normalizeEnvironment(cfg.Environment), "sandbox")
	cfg.RedirectURL = defaultString(strings.TrimSpace(cfg.RedirectURL), "http://localhost:18089/api/integrations/square/callback")
	cfg.APIVersion = defaultString(strings.TrimSpace(cfg.APIVersion), "2026-05-20")
	item, secrets := s.resolveStored(ctx, salonID, ProviderSquare)
	if item != nil {
		cfg.Environment = defaultString(normalizeEnvironment(item.Settings["environment"]), cfg.Environment)
		cfg.ClientID = strings.TrimSpace(item.Settings["client_id"])
		cfg.RedirectURL = defaultString(strings.TrimSpace(item.Settings["redirect_url"]), cfg.RedirectURL)
		cfg.APIVersion = defaultString(strings.TrimSpace(item.Settings["api_version"]), cfg.APIVersion)
		cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(item.Settings["api_base_url"]), "/")
	}
	if secret := strings.TrimSpace(secrets["client_secret"]); secret != "" {
		cfg.ClientSecret = secret
	}
	return cfg, nil
}

func (s *Service) ResolveTwilioConfig(ctx context.Context, salonID string) (config.TwilioVoiceConfig, string, error) {
	cfg := s.cfg.Voice.Twilio
	publicBaseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Voice.PublicBaseURL), "/")
	cfg.IncomingPath = defaultString(strings.TrimSpace(cfg.IncomingPath), "/api/voice/twilio/incoming")
	cfg.TurnPath = defaultString(strings.TrimSpace(cfg.TurnPath), "/api/voice/twilio/turn")
	cfg.RecordingPath = defaultString(strings.TrimSpace(cfg.RecordingPath), "/api/voice/twilio/recording")
	cfg.StreamPath = defaultString(strings.TrimSpace(cfg.StreamPath), "/api/voice/twilio/stream")
	cfg.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.VoiceTransport, "recording"))
	item, secrets := s.resolveStored(ctx, salonID, ProviderTwilio)
	if item != nil {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
		cfg.IncomingPath = defaultString(strings.TrimSpace(item.Settings["incoming_path"]), cfg.IncomingPath)
		cfg.TurnPath = defaultString(strings.TrimSpace(item.Settings["turn_path"]), cfg.TurnPath)
		cfg.RecordingPath = defaultString(strings.TrimSpace(item.Settings["recording_path"]), cfg.RecordingPath)
		cfg.StreamPath = defaultString(strings.TrimSpace(item.Settings["stream_path"]), cfg.StreamPath)
		cfg.VoiceTransport = normalizeVoiceTransport(defaultString(item.Settings["voice_transport"], cfg.VoiceTransport))
	}
	if secret := strings.TrimSpace(secrets["auth_token"]); secret != "" {
		cfg.AuthToken = secret
	}
	return cfg, publicBaseURL, nil
}

func (s *Service) ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	cfg := s.cfg.Voice.AI.OpenAI
	cfg.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), "https://api.openai.com/v1")
	cfg.TranscriptionModel = defaultString(strings.TrimSpace(cfg.TranscriptionModel), "gpt-4o-mini-transcribe")
	cfg.ReplyModel = defaultString(strings.TrimSpace(cfg.ReplyModel), "gpt-4.1-mini")
	cfg.SpeechModel = defaultString(strings.TrimSpace(cfg.SpeechModel), "gpt-4o-mini-tts")
	cfg.SpeechVoice = defaultString(strings.TrimSpace(cfg.SpeechVoice), "alloy")
	cfg.RealtimeModel = defaultString(strings.TrimSpace(cfg.RealtimeModel), "gpt-4o-realtime-preview")
	cfg.RealtimeVoice = defaultString(strings.TrimSpace(cfg.RealtimeVoice), cfg.SpeechVoice)
	cfg.RealtimeInstructions = strings.TrimSpace(cfg.RealtimeInstructions)
	enabled := strings.TrimSpace(s.cfg.Voice.AI.Provider) == ProviderOpenAI
	item, secrets := s.resolveStored(ctx, salonID, ProviderOpenAI)
	if item != nil {
		enabled = item.Enabled
		cfg.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(item.Settings["base_url"]), "/"), cfg.BaseURL)
		cfg.TranscriptionModel = defaultString(strings.TrimSpace(item.Settings["transcription_model"]), cfg.TranscriptionModel)
		cfg.ReplyModel = defaultString(strings.TrimSpace(item.Settings["reply_model"]), cfg.ReplyModel)
		cfg.SpeechModel = defaultString(strings.TrimSpace(item.Settings["speech_model"]), cfg.SpeechModel)
		cfg.SpeechVoice = defaultString(strings.TrimSpace(item.Settings["speech_voice"]), cfg.SpeechVoice)
		cfg.RealtimeEnabled = boolSetting(item.Settings["realtime_enabled"])
		cfg.RealtimeModel = defaultString(strings.TrimSpace(item.Settings["realtime_model"]), cfg.RealtimeModel)
		cfg.RealtimeVoice = defaultString(strings.TrimSpace(item.Settings["realtime_voice"]), cfg.SpeechVoice)
		cfg.RealtimeInstructions = strings.TrimSpace(item.Settings["realtime_instructions"])
	}
	if secret := strings.TrimSpace(secrets["api_key"]); secret != "" {
		cfg.APIKey = secret
	}
	return cfg, enabled, nil
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

func (s *Service) resolveStored(ctx context.Context, salonID string, provider string) (*StoredConfig, map[string]string) {
	item, err := s.repo.Get(ctx, strings.TrimSpace(salonID), provider)
	if err != nil {
		return nil, map[string]string{}
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return item, map[string]string{}
	}
	return item, secrets
}

func (s *Service) squareResponse(item *StoredConfig) SquareSettingsResponse {
	cfg := s.cfg.Square
	cfg.Environment = defaultString(normalizeEnvironment(cfg.Environment), "sandbox")
	cfg.RedirectURL = defaultString(strings.TrimSpace(cfg.RedirectURL), "http://localhost:18089/api/integrations/square/callback")
	cfg.APIVersion = defaultString(strings.TrimSpace(cfg.APIVersion), "2026-05-20")
	if item == nil {
		// Environment defaults already applied above.
	} else {
		cfg.Environment = defaultString(normalizeEnvironment(item.Settings["environment"]), cfg.Environment)
		cfg.ClientID = strings.TrimSpace(item.Settings["client_id"])
		cfg.RedirectURL = defaultString(strings.TrimSpace(item.Settings["redirect_url"]), cfg.RedirectURL)
		cfg.APIVersion = defaultString(strings.TrimSpace(item.Settings["api_version"]), cfg.APIVersion)
		cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(item.Settings["api_base_url"]), "/")
	}
	secretSource := SecretSourceNone
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil && strings.TrimSpace(secrets["client_secret"]) != "" {
			secretSource = SecretSourceDatabase
		}
	}
	clientSecret := strings.TrimSpace(s.cfg.Square.ClientSecret)
	if secretSource == SecretSourceNone && clientSecret != "" {
		secretSource = SecretSourceEnvironment
	}
	updatedAt := updatedAt(item)
	return SquareSettingsResponse{
		Provider:               ProviderSquare,
		Configured:             strings.TrimSpace(cfg.ClientID) != "" && (secretSource != SecretSourceNone) && strings.TrimSpace(cfg.RedirectURL) != "",
		Environment:            defaultString(normalizeEnvironment(cfg.Environment), "sandbox"),
		ClientID:               cfg.ClientID,
		RedirectURL:            cfg.RedirectURL,
		APIVersion:             defaultString(cfg.APIVersion, "2026-05-20"),
		APIBaseURL:             cfg.APIBaseURL,
		ClientSecretConfigured: secretSource != SecretSourceNone,
		ClientSecretSource:     secretSource,
		UpdatedAt:              updatedAt,
	}
}

func (s *Service) twilioResponse(item *StoredConfig) TwilioSettingsResponse {
	cfg := s.cfg.Voice.Twilio
	publicBaseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Voice.PublicBaseURL), "/")
	if item == nil {
		// Fallback config already copied above.
	} else {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
		cfg.IncomingPath = defaultString(strings.TrimSpace(item.Settings["incoming_path"]), cfg.IncomingPath)
		cfg.TurnPath = defaultString(strings.TrimSpace(item.Settings["turn_path"]), cfg.TurnPath)
		cfg.RecordingPath = defaultString(strings.TrimSpace(item.Settings["recording_path"]), cfg.RecordingPath)
		cfg.StreamPath = defaultString(strings.TrimSpace(item.Settings["stream_path"]), cfg.StreamPath)
		cfg.VoiceTransport = normalizeVoiceTransport(defaultString(item.Settings["voice_transport"], cfg.VoiceTransport))
	}
	cfg.IncomingPath = defaultString(strings.TrimSpace(cfg.IncomingPath), "/api/voice/twilio/incoming")
	cfg.TurnPath = defaultString(strings.TrimSpace(cfg.TurnPath), "/api/voice/twilio/turn")
	cfg.RecordingPath = defaultString(strings.TrimSpace(cfg.RecordingPath), "/api/voice/twilio/recording")
	cfg.StreamPath = defaultString(strings.TrimSpace(cfg.StreamPath), "/api/voice/twilio/stream")
	cfg.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.VoiceTransport, "recording"))
	secretSource := SecretSourceNone
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil && strings.TrimSpace(secrets["auth_token"]) != "" {
			secretSource = SecretSourceDatabase
		}
	}
	if secretSource == SecretSourceNone && strings.TrimSpace(s.cfg.Voice.Twilio.AuthToken) != "" {
		secretSource = SecretSourceEnvironment
	}
	updatedAt := updatedAt(item)
	return TwilioSettingsResponse{
		Provider:            ProviderTwilio,
		Configured:          secretSource != SecretSourceNone,
		PublicBaseURL:       publicBaseURL,
		IncomingPath:        cfg.IncomingPath,
		TurnPath:            cfg.TurnPath,
		RecordingPath:       cfg.RecordingPath,
		StreamPath:          cfg.StreamPath,
		VoiceTransport:      cfg.VoiceTransport,
		InboundWebhookURL:   urlForPath(publicBaseURL, cfg.IncomingPath),
		TurnWebhookURL:      urlForPath(publicBaseURL, cfg.TurnPath),
		RecordingWebhookURL: urlForPath(publicBaseURL, cfg.RecordingPath),
		StreamWebhookURL:    wsURLForPath(publicBaseURL, cfg.StreamPath),
		AuthTokenConfigured: secretSource != SecretSourceNone,
		AuthTokenSource:     secretSource,
		UpdatedAt:           updatedAt,
	}
}

func (s *Service) openAIResponse(item *StoredConfig) OpenAISettingsResponse {
	cfg := s.cfg.Voice.AI.OpenAI
	enabled := strings.TrimSpace(s.cfg.Voice.AI.Provider) == ProviderOpenAI
	if item == nil {
		// Fallback config already copied above.
	} else {
		enabled = item.Enabled
		cfg.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(item.Settings["base_url"]), "/"), cfg.BaseURL)
		cfg.TranscriptionModel = defaultString(strings.TrimSpace(item.Settings["transcription_model"]), cfg.TranscriptionModel)
		cfg.ReplyModel = defaultString(strings.TrimSpace(item.Settings["reply_model"]), cfg.ReplyModel)
		cfg.SpeechModel = defaultString(strings.TrimSpace(item.Settings["speech_model"]), cfg.SpeechModel)
		cfg.SpeechVoice = defaultString(strings.TrimSpace(item.Settings["speech_voice"]), cfg.SpeechVoice)
		cfg.RealtimeEnabled = boolSetting(item.Settings["realtime_enabled"])
		cfg.RealtimeModel = defaultString(strings.TrimSpace(item.Settings["realtime_model"]), cfg.RealtimeModel)
		cfg.RealtimeVoice = defaultString(strings.TrimSpace(item.Settings["realtime_voice"]), cfg.SpeechVoice)
		cfg.RealtimeInstructions = strings.TrimSpace(item.Settings["realtime_instructions"])
	}
	cfg.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), "https://api.openai.com/v1")
	cfg.TranscriptionModel = defaultString(strings.TrimSpace(cfg.TranscriptionModel), "gpt-4o-mini-transcribe")
	cfg.ReplyModel = defaultString(strings.TrimSpace(cfg.ReplyModel), "gpt-4.1-mini")
	cfg.SpeechModel = defaultString(strings.TrimSpace(cfg.SpeechModel), "gpt-4o-mini-tts")
	cfg.SpeechVoice = defaultString(strings.TrimSpace(cfg.SpeechVoice), "alloy")
	cfg.RealtimeModel = defaultString(strings.TrimSpace(cfg.RealtimeModel), "gpt-4o-realtime-preview")
	cfg.RealtimeVoice = defaultString(strings.TrimSpace(cfg.RealtimeVoice), cfg.SpeechVoice)
	cfg.RealtimeInstructions = strings.TrimSpace(cfg.RealtimeInstructions)
	secretSource := SecretSourceNone
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil && strings.TrimSpace(secrets["api_key"]) != "" {
			secretSource = SecretSourceDatabase
		}
	}
	if secretSource == SecretSourceNone && strings.TrimSpace(s.cfg.Voice.AI.OpenAI.APIKey) != "" {
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
		RealtimeEnabled:      cfg.RealtimeEnabled,
		RealtimeModel:        cfg.RealtimeModel,
		RealtimeVoice:        cfg.RealtimeVoice,
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

func (s *Service) mustEncryptSecrets(secrets map[string]string) string {
	normalized := map[string]string{}
	for key, value := range secrets {
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			normalized[key] = value
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	encrypted, err := s.cipher.Encrypt(string(raw))
	if err != nil {
		return ""
	}
	return encrypted
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
