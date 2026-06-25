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
}

type IntegrationConfigsResponse struct {
	Square SquareSettingsResponse `json:"square"`
	Twilio TwilioSettingsResponse `json:"twilio"`
	OpenAI OpenAISettingsResponse `json:"openai"`
}

type SquareSettingsResponse struct {
	Provider               string     `json:"provider"`
	Configured             bool       `json:"configured"`
	Environment            string     `json:"environment"`
	ClientID               string     `json:"client_id"`
	RedirectURL            string     `json:"redirect_url"`
	APIVersion             string     `json:"api_version"`
	APIBaseURL             string     `json:"api_base_url,omitempty"`
	ClientSecretConfigured bool       `json:"client_secret_configured"`
	ClientSecretSource     string     `json:"client_secret_source"`
	UpdatedAt              *time.Time `json:"updated_at,omitempty"`
}

type TwilioSettingsResponse struct {
	Provider            string     `json:"provider"`
	Configured          bool       `json:"configured"`
	PublicBaseURL       string     `json:"public_base_url"`
	IncomingPath        string     `json:"incoming_path"`
	TurnPath            string     `json:"turn_path"`
	RecordingPath       string     `json:"recording_path"`
	InboundWebhookURL   string     `json:"inbound_webhook_url"`
	TurnWebhookURL      string     `json:"turn_webhook_url"`
	RecordingWebhookURL string     `json:"recording_webhook_url"`
	AuthTokenConfigured bool       `json:"auth_token_configured"`
	AuthTokenSource     string     `json:"auth_token_source"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

type OpenAISettingsResponse struct {
	Provider           string     `json:"provider"`
	Enabled            bool       `json:"enabled"`
	Configured         bool       `json:"configured"`
	BaseURL            string     `json:"base_url"`
	TranscriptionModel string     `json:"transcription_model"`
	ReplyModel         string     `json:"reply_model"`
	SpeechModel        string     `json:"speech_model"`
	SpeechVoice        string     `json:"speech_voice"`
	APIKeyConfigured   bool       `json:"api_key_configured"`
	APIKeySource       string     `json:"api_key_source"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

type UpdateSquareSettingsRequest struct {
	Environment       string `json:"environment"`
	ClientID          string `json:"client_id"`
	ClientSecret      string `json:"client_secret"`
	ClearClientSecret bool   `json:"clear_client_secret"`
	RedirectURL       string `json:"redirect_url"`
	APIVersion        string `json:"api_version"`
	APIBaseURL        string `json:"api_base_url"`
}

type UpdateTwilioSettingsRequest struct {
	PublicBaseURL  string `json:"public_base_url"`
	AuthToken      string `json:"auth_token"`
	ClearAuthToken bool   `json:"clear_auth_token"`
	IncomingPath   string `json:"incoming_path"`
	TurnPath       string `json:"turn_path"`
	RecordingPath  string `json:"recording_path"`
}

type UpdateOpenAISettingsRequest struct {
	Enabled            bool   `json:"enabled"`
	APIKey             string `json:"api_key"`
	ClearAPIKey        bool   `json:"clear_api_key"`
	BaseURL            string `json:"base_url"`
	TranscriptionModel string `json:"transcription_model"`
	ReplyModel         string `json:"reply_model"`
	SpeechModel        string `json:"speech_model"`
	SpeechVoice        string `json:"speech_voice"`
}
