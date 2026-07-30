package integrationconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
	"github.com/manleai/ai-receptionist/internal/twiliovoice"
)

var ErrValidation = errors.New("integration config validation failed")

type twilioVoiceRouteStore interface {
	LocateTwilioVoiceRouteTenant(context.Context, string) (string, error)
	GetTwilioVoiceRoute(context.Context, string, string) (*StoredConfig, error)
}

type configStore interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	ListForOwner(ctx context.Context, salonID string, ownerUserID string) ([]StoredConfig, error)
	ListForSalon(ctx context.Context, salonID string) ([]StoredConfig, error)
	Get(ctx context.Context, salonID string, provider string) (*StoredConfig, error)
	Upsert(ctx context.Context, cfg StoredConfig) (*StoredConfig, error)
}

type controlledConfigStore interface {
	UpsertControlled(context.Context, StoredConfig, TechnicalMutationCommand) (*StoredConfig, bool, error)
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
	return s.integrationConfigsResponse(items, err)
}

func (s *Service) getAllForSalon(ctx context.Context, salonID string) (*IntegrationConfigsResponse, error) {
	response, _, err := s.GetAllPersistedForPlatform(ctx, salonID)
	return response, err
}

// GetAllPersistedForPlatform returns only salon-scoped database configuration.
// It deliberately does not surface legacy environment fallback as portable
// tenant configuration.
func (s *Service) GetAllPersistedForPlatform(ctx context.Context, salonID string) (*IntegrationConfigsResponse, []string, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return nil, nil, ErrValidation
	}
	items, err := s.repo.ListForSalon(ctx, salonID)
	if err != nil {
		return nil, nil, err
	}
	byProvider := map[string]*StoredConfig{}
	for i := range items {
		item := items[i]
		byProvider[item.Provider] = &item
	}
	providers := []string{}
	for _, provider := range []string{ProviderSquare, ProviderTwilio, ProviderOpenAI} {
		if byProvider[provider] != nil {
			providers = append(providers, provider)
		}
	}
	response := &IntegrationConfigsResponse{
		Square: strictSquareResponse(s, byProvider[ProviderSquare]),
		Twilio: strictTwilioResponse(s, byProvider[ProviderTwilio]),
		OpenAI: strictOpenAIResponse(s, byProvider[ProviderOpenAI]),
	}
	return response, providers, nil
}

func strictSquareResponse(s *Service, item *StoredConfig) SquareSettingsResponse {
	if item == nil {
		return SquareSettingsResponse{Provider: ProviderSquare, ClientSecretSource: SecretSourceNone, WebhookSignatureKeySource: SecretSourceNone}
	}
	return s.squareResponse(item)
}

func strictTwilioResponse(s *Service, item *StoredConfig) TwilioSettingsResponse {
	if item == nil {
		return TwilioSettingsResponse{Provider: ProviderTwilio, AuthTokenSource: SecretSourceNone}
	}
	return s.twilioResponse(item)
}

func strictOpenAIResponse(s *Service, item *StoredConfig) OpenAISettingsResponse {
	if item == nil {
		return OpenAISettingsResponse{
			Provider: ProviderOpenAI, APIKeySource: SecretSourceNone,
			BaseURL: openairuntime.CanonicalBaseURL, DestinationProfile: openairuntime.DestinationProfile,
			DestinationManaged: true, RuntimeBlockers: []string{"integration_config_missing"},
		}
	}
	return s.openAIResponse(item)
}

func (s *Service) integrationConfigsResponse(items []StoredConfig, err error) (*IntegrationConfigsResponse, error) {
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
	return s.updateSquareForSalon(ctx, salonID, req, nil)
}

func (s *Service) UpdateSquareForPlatform(ctx context.Context, salonID, actorUserID string, req UpdateSquareSettingsRequest) (*SquareSettingsResponse, error) {
	command, err := technicalMutationCommand(actorUserID, ProviderSquare, "integration_config.square.updated", req.TechnicalMutationControl, req,
		[]string{"environment", "client_id", "client_secret", "redirect_url", "api_version", "api_base_url", "webhook_notification_url", "webhook_signature_key"})
	if err != nil {
		return nil, err
	}
	return s.updateSquareForSalon(ctx, strings.TrimSpace(salonID), req, &command)
}

func (s *Service) updateSquareForSalon(ctx context.Context, salonID string, req UpdateSquareSettingsRequest, command *TechnicalMutationCommand) (*SquareSettingsResponse, error) {
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
	updated, replayed, err := s.persistConfig(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderSquare,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: encryptedSecrets,
	}, command)
	if err != nil {
		return nil, err
	}
	returnPtr := s.squareResponse(updated)
	returnPtr.Replayed = replayed
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
	return s.updateTwilioForSalon(ctx, salonID, req, nil)
}

func (s *Service) UpdateTwilioForPlatform(ctx context.Context, salonID, actorUserID string, req UpdateTwilioSettingsRequest) (*TwilioSettingsResponse, error) {
	command, err := technicalMutationCommand(actorUserID, ProviderTwilio, "integration_config.twilio.updated", req.TechnicalMutationControl, req,
		[]string{"public_base_url", "auth_token", "voice_inbound_number", "voice_routing_enabled", "voice_paths", "voice_transport", "owner_sms_policy", "account_sid", "messaging_transport", "notification_paths"})
	if err != nil {
		return nil, err
	}
	return s.updateTwilioForSalon(ctx, strings.TrimSpace(salonID), req, &command)
}

func (s *Service) updateTwilioForSalon(ctx context.Context, salonID string, req UpdateTwilioSettingsRequest, command *TechnicalMutationCommand) (*TwilioSettingsResponse, error) {
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
	if req.VoiceInboundNumber != nil {
		settings["voice_inbound_number"] = strings.TrimSpace(*req.VoiceInboundNumber)
	}
	if req.VoiceRoutingEnabled != nil {
		settings["voice_routing_enabled"] = strconv.FormatBool(*req.VoiceRoutingEnabled)
	}
	settings["voice_routing_enabled"] = defaultString(settings["voice_routing_enabled"], "false")
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
	if err := validateTwilioVoiceRoutingSettings(settings, secrets); err != nil {
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
	updated, replayed, err := s.persistConfig(ctx, StoredConfig{
		SalonID:          salonID,
		Provider:         ProviderTwilio,
		Enabled:          true,
		Settings:         settings,
		SecretsEncrypted: encryptedSecrets,
	}, command)
	if err != nil {
		return nil, err
	}
	returnPtr := s.twilioResponse(updated)
	returnPtr.Replayed = replayed
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
	return s.updateOpenAIForSalon(ctx, salonID, req, nil)
}

func (s *Service) UpdateOpenAIForPlatform(ctx context.Context, salonID, actorUserID string, req UpdateOpenAISettingsRequest) (*OpenAISettingsResponse, error) {
	command, err := technicalMutationCommand(actorUserID, ProviderOpenAI, "integration_config.openai.updated", req.TechnicalMutationControl, req,
		[]string{"enabled", "api_key", "destination_profile", "transcription_model", "reply_model", "speech_model", "speech_voice", "speech_output_mode", "realtime"})
	if err != nil {
		return nil, err
	}
	return s.updateOpenAIForSalon(ctx, strings.TrimSpace(salonID), req, &command)
}

func (s *Service) updateOpenAIForSalon(ctx context.Context, salonID string, req UpdateOpenAISettingsRequest, command *TechnicalMutationCommand) (*OpenAISettingsResponse, error) {
	requestedBaseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if requestedBaseURL != "" && requestedBaseURL != openairuntime.CanonicalBaseURL {
		return nil, ErrValidation
	}
	settings := map[string]string{
		"base_url":               openairuntime.CanonicalBaseURL,
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
	if req.Enabled && (settings["transcription_model"] == "" || settings["reply_model"] == "" || settings["speech_model"] == "" || settings["speech_voice"] == "") {
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
	apiKey := strings.TrimSpace(secrets["api_key"])
	if req.Enabled && apiKey == "" {
		return nil, ErrValidation
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
	credentialFingerprint := ""
	credentialRevision := int64(0)
	if existing != nil {
		credentialRevision = existing.CredentialRevision
	}
	if secretMutation {
		credentialRevision++
	}
	if apiKey != "" {
		credentialFingerprint, err = openairuntime.CredentialFingerprint(s.cfg.EncryptionKey, apiKey)
		if err != nil {
			return nil, err
		}
		if credentialRevision == 0 {
			credentialRevision = 1
		}
	}
	preflight := openairuntime.ResolvedConfig{
		SalonID: salonID, IntegrationConfigID: "pending", ConfigVersion: 1,
		CredentialRevision: credentialRevision, CredentialIdentityEstablished: credentialFingerprint != "",
		DestinationProfile: openairuntime.DestinationProfile,
		Enabled:            req.Enabled, Config: openAIConfigFromSettings(settings, apiKey),
	}
	if req.Enabled && !openairuntime.Validate(preflight).Ready {
		return nil, ErrValidation
	}
	updated, replayed, err := s.persistConfig(ctx, StoredConfig{
		SalonID:                   salonID,
		Provider:                  ProviderOpenAI,
		Enabled:                   req.Enabled,
		Settings:                  settings,
		SecretsEncrypted:          encryptedSecrets,
		CredentialFingerprintHMAC: credentialFingerprint,
		CredentialRevision:        credentialRevision,
		DestinationProfile:        openairuntime.DestinationProfile,
	}, command)
	if err != nil {
		return nil, err
	}
	returnPtr := s.openAIResponse(updated)
	returnPtr.Replayed = replayed
	return &returnPtr, nil
}

func (s *Service) persistConfig(ctx context.Context, cfg StoredConfig, command *TechnicalMutationCommand) (*StoredConfig, bool, error) {
	if command == nil {
		item, err := s.repo.Upsert(ctx, cfg)
		return item, false, err
	}
	store, ok := s.repo.(controlledConfigStore)
	if !ok {
		return nil, false, errors.New("controlled integration config store is unavailable")
	}
	return store.UpsertControlled(ctx, cfg, *command)
}

func technicalMutationCommand(actorUserID, provider, actionType string, control TechnicalMutationControl, payload any, changedFields []string) (TechnicalMutationCommand, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	control.ActionKey = strings.TrimSpace(control.ActionKey)
	if actorUserID == "" || control.ActionKey == "" || len(control.ActionKey) > 256 || control.ExpectedVersion < 0 {
		return TechnicalMutationCommand{}, ErrValidation
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return TechnicalMutationCommand{}, err
	}
	digest := sha256.Sum256(raw)
	return TechnicalMutationCommand{
		ActorUserID: actorUserID, ActionKey: control.ActionKey, ActionType: actionType,
		RequestFingerprint: hex.EncodeToString(digest[:]), ExpectedVersion: control.ExpectedVersion,
		ChangedFields: changedFields,
	}, nil
}

// SquareWebhookConfigured reports only whether this salon has a complete,
// stored Square booking-webhook verification pair. It never falls back to
// process configuration and never exposes either write-only value.
func (s *Service) SquareWebhookConfigured(ctx context.Context, salonID string) (bool, error) {
	item, secrets, err := s.resolveStored(ctx, strings.TrimSpace(salonID), ProviderSquare)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cfg := squareConfigFromStored(item, secrets)
	return item.Enabled && validSquareWebhookNotificationURL(cfg.WebhookNotificationURL) &&
		strings.TrimSpace(cfg.WebhookSignatureKey) != "", nil
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

// ResolveTwilioVoiceRoute resolves an opaque integration-config UUID through a
// provider-only locator, binds the returned tenant immediately, then reloads
// the exact Twilio row under tenant RLS before decrypting any secret.
func (s *Service) ResolveTwilioVoiceRoute(ctx context.Context, routeID string) (config.TwilioVoiceRouteConfig, error) {
	routeID = strings.TrimSpace(routeID)
	if _, err := uuid.Parse(routeID); err != nil {
		return config.TwilioVoiceRouteConfig{}, ErrNotFound
	}
	store, ok := s.repo.(twilioVoiceRouteStore)
	if !ok || s.cipher == nil {
		return config.TwilioVoiceRouteConfig{}, ErrValidation
	}
	salonID, err := store.LocateTwilioVoiceRouteTenant(ctx, routeID)
	if err != nil {
		return config.TwilioVoiceRouteConfig{}, err
	}
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, salonID)
	item, err := store.GetTwilioVoiceRoute(boundCtx, salonID, routeID)
	if err != nil {
		return config.TwilioVoiceRouteConfig{}, err
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return config.TwilioVoiceRouteConfig{}, err
	}
	voiceConfig, publicBaseURL := twilioConfigFromStored(item, secrets)
	if !voiceConfig.RoutingEnabled || !twiliovoice.ValidE164(voiceConfig.InboundNumber) ||
		!twiliovoice.ValidAccountSID(voiceConfig.AccountSID) || strings.TrimSpace(voiceConfig.AuthToken) == "" ||
		!twiliovoice.ValidPublicHTTPSBase(publicBaseURL) {
		return config.TwilioVoiceRouteConfig{}, ErrValidation
	}
	return config.TwilioVoiceRouteConfig{
		SalonID:           salonID,
		PublicBaseURL:     publicBaseURL,
		ConfigUpdatedAt:   item.UpdatedAt,
		TwilioVoiceConfig: voiceConfig,
	}, nil
}

func (s *Service) ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	resolved, err := s.ResolveOpenAIRuntimeConfig(ctx, salonID)
	if err != nil {
		return config.OpenAIVoiceConfig{}, false, err
	}
	return resolved.Config, resolved.Enabled, nil
}

func (s *Service) ResolveOpenAIRuntimeConfig(ctx context.Context, salonID string) (openairuntime.ResolvedConfig, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return openairuntime.ResolvedConfig{}, openairuntime.ErrInvalidSalon
	}
	item, err := s.repo.Get(ctx, salonID, ProviderOpenAI)
	if err != nil {
		return openairuntime.ResolvedConfig{}, err
	}
	resolved := openairuntime.ResolvedConfig{
		SalonID: item.SalonID, IntegrationConfigID: item.ID, ConfigVersion: item.Version,
		CredentialRevision:            item.CredentialRevision,
		CredentialIdentityEstablished: strings.TrimSpace(item.CredentialFingerprintHMAC) != "",
		DestinationProfile:            item.DestinationProfile,
		Enabled:                       item.Enabled,
	}
	if !item.Enabled {
		resolved.Config = openAIConfigFromStored(item, nil)
		return resolved, nil
	}
	secrets, err := s.decryptSecrets(item.SecretsEncrypted)
	if err != nil {
		return openairuntime.ResolvedConfig{}, err
	}
	resolved.Config = openAIConfigFromStored(item, secrets)
	validation := openairuntime.Validate(resolved)
	if !validation.Ready {
		return openairuntime.ResolvedConfig{}, ErrValidation
	}
	return resolved, nil
}

// ResolveOpenAIConfigStrict resolves only the encrypted salon-scoped database
// record. It deliberately has no legacy environment fallback and is used by
// paid evaluation workflows that must fail closed on missing, unreadable, or
// incomplete stored configuration.
func (s *Service) ResolveOpenAIConfigStrict(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	return s.ResolveOpenAIConfig(ctx, salonID)
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

func twilioConfigFromStored(item *StoredConfig, secrets map[string]string) (config.TwilioVoiceConfig, string) {
	if item == nil {
		return config.TwilioVoiceConfig{}, ""
	}
	paths := twiliovoice.CanonicalPaths(item.ID)
	routingEnabled := item.Enabled && boolSetting(item.Settings["voice_routing_enabled"])
	cfg := config.TwilioVoiceConfig{
		AuthToken:      strings.TrimSpace(secrets["auth_token"]),
		AccountSID:     strings.TrimSpace(secrets["account_sid"]),
		RouteID:        strings.TrimSpace(item.ID),
		InboundNumber:  strings.TrimSpace(item.Settings["voice_inbound_number"]),
		RoutingEnabled: routingEnabled,
		IncomingPath:   defaultString(strings.TrimSpace(item.Settings["incoming_path"]), "/api/voice/twilio/incoming"),
		TurnPath:       defaultString(strings.TrimSpace(item.Settings["turn_path"]), "/api/voice/twilio/turn"),
		RecordingPath:  defaultString(strings.TrimSpace(item.Settings["recording_path"]), "/api/voice/twilio/recording"),
		StreamPath:     defaultString(strings.TrimSpace(item.Settings["stream_path"]), "/api/voice/twilio/stream"),
		VoiceTransport: normalizeVoiceTransport(defaultString(item.Settings["voice_transport"], "recording")),
	}
	if routingEnabled {
		cfg.IncomingPath = paths.Incoming
		cfg.TurnPath = paths.Turn
		cfg.RecordingPath = paths.Recording
		cfg.StreamPath = paths.Stream
	}
	return cfg, strings.TrimRight(strings.TrimSpace(item.Settings["public_base_url"]), "/")
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

func openAIConfigFromSettings(settings map[string]string, apiKey string) config.OpenAIVoiceConfig {
	return openAIConfigFromStored(&StoredConfig{Settings: settings}, map[string]string{"api_key": apiKey})
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
		Version:                       storedVersion(item),
	}
}

func (s *Service) twilioResponse(item *StoredConfig) TwilioSettingsResponse {
	cfg, publicBaseURL := twilioConfigFromStored(item, nil)
	enabled := false
	if item != nil {
		enabled = item.Enabled
	}
	secretSource := SecretSourceNone
	accountSIDConfigured := false
	accountSIDHint := ""
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil {
			if strings.TrimSpace(secrets["auth_token"]) != "" {
				secretSource = SecretSourceDatabase
			}
			accountSIDConfigured = strings.TrimSpace(secrets["account_sid"]) != ""
			accountSIDHint = maskedSID(secrets["account_sid"])
		}
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
	routingEnabled := item != nil && item.Enabled && boolSetting(item.Settings["voice_routing_enabled"])
	voiceInboundNumber := strings.TrimSpace(setting(item, "voice_inbound_number"))
	routingBlockers := twilioVoiceRoutingBlockers(item, publicBaseURL, voiceInboundNumber, accountSIDConfigured, secretSource != SecretSourceNone)
	voicePaths := twiliovoice.Paths{
		Incoming: cfg.IncomingPath, Turn: cfg.TurnPath, Recording: cfg.RecordingPath, Stream: cfg.StreamPath,
	}
	if item != nil {
		voicePaths = twiliovoice.CanonicalPaths(item.ID)
	}
	return TwilioSettingsResponse{
		Provider:                   ProviderTwilio,
		Configured:                 enabled && secretSource != SecretSourceNone,
		VoiceRouteID:               settingID(item),
		VoiceRoutingEnabled:        routingEnabled,
		VoiceInboundNumber:         voiceInboundNumber,
		VoiceRoutingConfigured:     len(routingBlockers) == 0,
		VoiceRoutingBlockers:       routingBlockers,
		AccountSIDHint:             accountSIDHint,
		PublicBaseURL:              publicBaseURL,
		IncomingPath:               cfg.IncomingPath,
		TurnPath:                   cfg.TurnPath,
		RecordingPath:              cfg.RecordingPath,
		StreamPath:                 cfg.StreamPath,
		VoiceTransport:             cfg.VoiceTransport,
		InboundWebhookURL:          urlForPath(publicBaseURL, voicePaths.Incoming),
		TurnWebhookURL:             urlForPath(publicBaseURL, voicePaths.Turn),
		RecordingWebhookURL:        urlForPath(publicBaseURL, voicePaths.Recording),
		StreamWebhookURL:           wsURLForPath(publicBaseURL, voicePaths.Stream),
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
		Version:                    storedVersion(item),
	}
}

func settingID(item *StoredConfig) string {
	if item == nil {
		return ""
	}
	return strings.TrimSpace(item.ID)
}

func maskedSID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 6 {
		return ""
	}
	return value[:2] + "••••" + value[len(value)-4:]
}

func twilioVoiceRoutingBlockers(item *StoredConfig, publicBaseURL, inboundNumber string, accountSIDConfigured, authTokenConfigured bool) []string {
	blockers := []string{}
	if item == nil || !item.Enabled || !boolSetting(item.Settings["voice_routing_enabled"]) {
		blockers = append(blockers, "TWILIO_VOICE_ROUTING_DISABLED")
	}
	if !twiliovoice.ValidE164(inboundNumber) {
		blockers = append(blockers, "TWILIO_VOICE_INBOUND_NUMBER_INVALID")
	}
	if !accountSIDConfigured {
		blockers = append(blockers, "TWILIO_ACCOUNT_SID_REQUIRED")
	}
	if !authTokenConfigured {
		blockers = append(blockers, "TWILIO_AUTH_TOKEN_REQUIRED")
	}
	if !twiliovoice.ValidPublicHTTPSBase(publicBaseURL) {
		blockers = append(blockers, "TWILIO_PUBLIC_HTTPS_BASE_REQUIRED")
	}
	return blockers
}

func validateTwilioVoiceRoutingSettings(settings map[string]string, secrets map[string]string) error {
	enabled := boolSetting(settings["voice_routing_enabled"])
	number := strings.TrimSpace(settings["voice_inbound_number"])
	if number != "" && !twiliovoice.ValidE164(number) {
		return ErrValidation
	}
	if !enabled {
		return nil
	}
	if !twiliovoice.ValidE164(number) || !twiliovoice.ValidAccountSID(secrets["account_sid"]) ||
		strings.TrimSpace(secrets["auth_token"]) == "" || !twiliovoice.ValidPublicHTTPSBase(settings["public_base_url"]) {
		return ErrValidation
	}
	return nil
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
	cfg := openAIConfigFromStored(item, nil)
	enabled := false
	if item != nil {
		enabled = item.Enabled
	}
	secretSource := SecretSourceNone
	apiKey := ""
	if item != nil {
		if secrets, err := s.decryptSecrets(item.SecretsEncrypted); err == nil {
			apiKey = strings.TrimSpace(secrets["api_key"])
			if apiKey != "" {
				secretSource = SecretSourceDatabase
			}
		}
	}
	resolved := openairuntime.ResolvedConfig{
		SalonID: salonIDFromItem(item), IntegrationConfigID: storedID(item), ConfigVersion: storedVersion(item),
		CredentialRevision:            storedCredentialRevision(item),
		CredentialIdentityEstablished: item != nil && strings.TrimSpace(item.CredentialFingerprintHMAC) != "",
		DestinationProfile:            storedDestinationProfile(item),
		Enabled:                       enabled, Config: openAIConfigFromSettings(settingsFromItem(item), apiKey),
	}
	validation := openairuntime.Validate(resolved)
	updatedAt := updatedAt(item)
	return OpenAISettingsResponse{
		Provider:             ProviderOpenAI,
		Enabled:              enabled,
		Configured:           validation.Ready,
		RuntimeResolvable:    validation.Ready,
		RuntimeBlockers:      validation.Blockers,
		BaseURL:              openairuntime.CanonicalBaseURL,
		DestinationProfile:   openairuntime.DestinationProfile,
		DestinationManaged:   true,
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
		CredentialRevision:   storedCredentialRevision(item),
		CredentialUnique:     item != nil && item.CredentialFingerprintHMAC != "",
		UpdatedAt:            updatedAt,
		Version:              storedVersion(item),
	}
}

func storedVersion(item *StoredConfig) int64 {
	if item == nil {
		return 0
	}
	return item.Version
}

func storedID(item *StoredConfig) string {
	if item == nil {
		return ""
	}
	return item.ID
}

func storedCredentialRevision(item *StoredConfig) int64 {
	if item == nil {
		return 0
	}
	return item.CredentialRevision
}

func storedDestinationProfile(item *StoredConfig) string {
	if item == nil {
		return ""
	}
	return item.DestinationProfile
}

func settingsFromItem(item *StoredConfig) map[string]string {
	if item == nil {
		return map[string]string{"base_url": openairuntime.CanonicalBaseURL}
	}
	return item.Settings
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
