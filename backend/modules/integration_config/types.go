package integrationconfig

import "time"

const (
	ProviderSquare = "square"
	ProviderTwilio = "twilio"
	ProviderOpenAI = "openai"

	SecretSourceNone        = "none"
	SecretSourceDatabase    = "database"
	SecretSourceEnvironment = "environment"
)

type StoredConfig struct {
	ID               string
	SalonID          string
	Provider         string
	Enabled          bool
	Settings         map[string]string
	SecretsEncrypted string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int64
}

type TechnicalMutationControl struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

type IntegrationConfigsResponse struct {
	Square SquareSettingsResponse `json:"square"`
	Twilio TwilioSettingsResponse `json:"twilio"`
	OpenAI OpenAISettingsResponse `json:"openai"`
}

type SquareSettingsResponse struct {
	Provider                      string     `json:"provider"`
	Configured                    bool       `json:"configured"`
	Environment                   string     `json:"environment"`
	ClientID                      string     `json:"client_id"`
	RedirectURL                   string     `json:"redirect_url"`
	APIVersion                    string     `json:"api_version"`
	APIBaseURL                    string     `json:"api_base_url,omitempty"`
	ClientSecretConfigured        bool       `json:"client_secret_configured"`
	ClientSecretSource            string     `json:"client_secret_source"`
	WebhookNotificationURL        string     `json:"webhook_notification_url,omitempty"`
	WebhookConfigured             bool       `json:"webhook_configured"`
	WebhookSignatureKeyConfigured bool       `json:"webhook_signature_key_configured"`
	WebhookSignatureKeySource     string     `json:"webhook_signature_key_source"`
	UpdatedAt                     *time.Time `json:"updated_at,omitempty"`
	Version                       int64      `json:"version"`
	Replayed                      bool       `json:"replayed,omitempty"`
}

type TwilioSettingsResponse struct {
	Provider                   string     `json:"provider"`
	Configured                 bool       `json:"configured"`
	VoiceRouteID               string     `json:"voice_route_id,omitempty"`
	VoiceRoutingEnabled        bool       `json:"voice_routing_enabled"`
	VoiceInboundNumber         string     `json:"voice_inbound_number"`
	VoiceRoutingConfigured     bool       `json:"voice_routing_configured"`
	VoiceRoutingBlockers       []string   `json:"voice_routing_blockers"`
	AccountSIDHint             string     `json:"account_sid_hint,omitempty"`
	PublicBaseURL              string     `json:"public_base_url"`
	IncomingPath               string     `json:"incoming_path"`
	TurnPath                   string     `json:"turn_path"`
	RecordingPath              string     `json:"recording_path"`
	StreamPath                 string     `json:"stream_path"`
	VoiceTransport             string     `json:"voice_transport"`
	InboundWebhookURL          string     `json:"inbound_webhook_url"`
	TurnWebhookURL             string     `json:"turn_webhook_url"`
	RecordingWebhookURL        string     `json:"recording_webhook_url"`
	StreamWebhookURL           string     `json:"stream_webhook_url"`
	AuthTokenConfigured        bool       `json:"auth_token_configured"`
	AuthTokenSource            string     `json:"auth_token_source"`
	OwnerSMSEnabled            bool       `json:"owner_sms_enabled"`
	OwnerSMSDestinationMasked  string     `json:"owner_sms_destination_masked,omitempty"`
	OwnerSMSConsentAttested    bool       `json:"owner_sms_consent_attested"`
	OwnerSMSConsentAttestedAt  *time.Time `json:"owner_sms_consent_attested_at,omitempty"`
	AccountSIDConfigured       bool       `json:"account_sid_configured"`
	MessagingServiceConfigured bool       `json:"messaging_service_configured"`
	SenderConfigured           bool       `json:"sender_configured"`
	NotificationStatusPath     string     `json:"notification_status_path"`
	NotificationInboundPath    string     `json:"notification_inbound_path"`
	NotificationStatusURL      string     `json:"notification_status_url"`
	NotificationInboundURL     string     `json:"notification_inbound_url"`
	UpdatedAt                  *time.Time `json:"updated_at,omitempty"`
	Version                    int64      `json:"version"`
	Replayed                   bool       `json:"replayed,omitempty"`
}

// TwilioMessagingConfig is the strict database-backed runtime contract used by
// notification delivery. Owner delivery additionally requires the owner SMS
// destination/attestation fields; customer delivery resolves only the shared
// transport fields. It is never serialized by an API response.
type TwilioMessagingConfig struct {
	Enabled                 bool
	OwnerSMSConsentAttested bool
	OwnerSMSDestination     string
	OwnerSMSConsentAt       *time.Time
	AccountSID              string
	AuthToken               string
	MessagingServiceSID     string
	SenderPhone             string
	StatusCallbackURL       string
	InboundCallbackURL      string
}

type OpenAISettingsResponse struct {
	Provider             string     `json:"provider"`
	Enabled              bool       `json:"enabled"`
	Configured           bool       `json:"configured"`
	BaseURL              string     `json:"base_url"`
	TranscriptionModel   string     `json:"transcription_model"`
	ReplyModel           string     `json:"reply_model"`
	SpeechModel          string     `json:"speech_model"`
	SpeechVoice          string     `json:"speech_voice"`
	SpeechOutputMode     string     `json:"speech_output_mode"`
	RealtimeEnabled      bool       `json:"realtime_enabled"`
	RealtimeModel        string     `json:"realtime_model"`
	RealtimeVoice        string     `json:"realtime_voice"`
	RealtimeNoiseProfile string     `json:"realtime_noise_profile"`
	RealtimeInstructions string     `json:"realtime_instructions"`
	APIKeyConfigured     bool       `json:"api_key_configured"`
	APIKeySource         string     `json:"api_key_source"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
	Version              int64      `json:"version"`
	Replayed             bool       `json:"replayed,omitempty"`
}

type UpdateSquareSettingsRequest struct {
	TechnicalMutationControl
	Environment              string  `json:"environment"`
	ClientID                 string  `json:"client_id"`
	ClientSecret             string  `json:"client_secret"`
	ClearClientSecret        bool    `json:"clear_client_secret"`
	RedirectURL              string  `json:"redirect_url"`
	APIVersion               string  `json:"api_version"`
	APIBaseURL               string  `json:"api_base_url"`
	WebhookNotificationURL   *string `json:"webhook_notification_url,omitempty"`
	WebhookSignatureKey      string  `json:"webhook_signature_key"`
	ClearWebhookSignatureKey bool    `json:"clear_webhook_signature_key"`
}

type UpdateTwilioSettingsRequest struct {
	TechnicalMutationControl
	PublicBaseURL            string  `json:"public_base_url"`
	VoiceInboundNumber       *string `json:"voice_inbound_number,omitempty"`
	VoiceRoutingEnabled      *bool   `json:"voice_routing_enabled,omitempty"`
	AuthToken                string  `json:"auth_token"`
	ClearAuthToken           bool    `json:"clear_auth_token"`
	IncomingPath             string  `json:"incoming_path"`
	TurnPath                 string  `json:"turn_path"`
	RecordingPath            string  `json:"recording_path"`
	StreamPath               string  `json:"stream_path"`
	VoiceTransport           string  `json:"voice_transport"`
	OwnerSMSEnabled          *bool   `json:"owner_sms_enabled,omitempty"`
	OwnerSMSDestination      *string `json:"owner_sms_destination,omitempty"`
	ClearOwnerSMSDestination bool    `json:"clear_owner_sms_destination"`
	OwnerSMSConsentAttested  *bool   `json:"owner_sms_consent_attested,omitempty"`
	AccountSID               string  `json:"account_sid"`
	ClearAccountSID          bool    `json:"clear_account_sid"`
	MessagingServiceSID      *string `json:"messaging_service_sid,omitempty"`
	ClearMessagingServiceSID bool    `json:"clear_messaging_service_sid"`
	SenderPhone              *string `json:"sender_phone,omitempty"`
	ClearSenderPhone         bool    `json:"clear_sender_phone"`
	NotificationStatusPath   *string `json:"notification_status_path,omitempty"`
	NotificationInboundPath  *string `json:"notification_inbound_path,omitempty"`
}

type UpdateOpenAISettingsRequest struct {
	TechnicalMutationControl
	Enabled              bool   `json:"enabled"`
	APIKey               string `json:"api_key"`
	ClearAPIKey          bool   `json:"clear_api_key"`
	BaseURL              string `json:"base_url"`
	TranscriptionModel   string `json:"transcription_model"`
	ReplyModel           string `json:"reply_model"`
	SpeechModel          string `json:"speech_model"`
	SpeechVoice          string `json:"speech_voice"`
	SpeechOutputMode     string `json:"speech_output_mode"`
	RealtimeEnabled      bool   `json:"realtime_enabled"`
	RealtimeModel        string `json:"realtime_model"`
	RealtimeVoice        string `json:"realtime_voice"`
	RealtimeNoiseProfile string `json:"realtime_noise_profile"`
	RealtimeInstructions string `json:"realtime_instructions"`
}
