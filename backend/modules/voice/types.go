package voice

import (
	"context"
	"errors"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

const (
	ProviderTwilio = "twilio"
	ProviderOpenAI = "openai"

	EventIncomingCall = "incoming_call"
	EventSpeechTurn   = "speech_turn"
	EventNoSpeech     = "no_speech"
	EventSTTFailed    = "stt_failed"
	EventLLMFailed    = "llm_failed"
	EventTTSFailed    = "tts_failed"

	InputModeGather    = "gather"
	InputModeRecording = "recording"
)

var (
	ErrValidation       = errors.New("voice validation failed")
	ErrNotFound         = errors.New("voice record not found")
	ErrProviderDisabled = errors.New("voice provider is not configured")
	ErrRouteNotFound    = errors.New("voice route not found")
)

type ConversationEngine interface {
	StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error)
	Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error)
	Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error)
}

type Store interface {
	GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error)
	GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*PhoneBookingReadiness, error)
	FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error)
	FindCallRoute(ctx context.Context, provider string, providerCallID string) (*CallRoute, error)
	RecordWebhookEvent(ctx context.Context, event WebhookEvent) error
	SaveAudioOutput(ctx context.Context, record AudioOutputRecord) (*AudioOutput, error)
	GetAudioOutput(ctx context.Context, id string) (*AudioOutput, error)
}

type TelephonyProvider interface {
	VerifyWebhook(url string, params map[string]string, signature string) bool
}

type SpeechToTextProvider interface {
	Name() string
	Configured() bool
	Transcribe(ctx context.Context, audio []byte, contentType string) (string, error)
}

type LanguageModelProvider interface {
	Name() string
	Configured() bool
	GenerateReply(ctx context.Context, req ModelRequest) (ModelReply, error)
}

type TextToSpeechProvider interface {
	Name() string
	Configured() bool
	ContentType() string
	Synthesize(ctx context.Context, text string, voice string) ([]byte, error)
}

type ModelRequest struct {
	SalonID             string
	SessionID           string
	Channel             string
	Intent              string
	Outcome             string
	CustomerMessage     string
	SafeReply           string
	SalonName           string
	BookingConfirmed    bool
	FallbackOrHandoff   bool
	MissingBookingField string
	Summary             string
	KnowledgeContext    string
}

type ModelReply struct {
	Message    string
	Confidence float64
	Handoff    bool
	Reason     string
}

type InboundSalon struct {
	SalonID     string
	OwnerUserID string
	SalonName   string
	Phone       string
}

type CallRoute struct {
	SalonID     string
	OwnerUserID string
	SessionID   string
}

type IncomingCallRequest struct {
	Provider       string
	ProviderCallID string
	FromPhone      string
	ToPhone        string
	Payload        map[string]string
}

type SpeechTurnRequest struct {
	Provider         string
	ProviderCallID   string
	FromPhone        string
	ToPhone          string
	SpeechText       string
	Audio            []byte
	AudioContentType string
	Payload          map[string]string
}

type CallReply struct {
	Message   string
	Continue  bool
	Session   *conversation.Session
	AudioURL  string
	InputMode string
}

type WebhookEvent struct {
	SalonID        string
	CallSessionID  string
	Provider       string
	ProviderCallID string
	EventType      string
	Payload        map[string]string
}

type SalonVoiceStatus struct {
	SalonID string
	Phone   string
}

type ReadinessCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Message  string `json:"message,omitempty"`
}

type PhoneBookingReadiness struct {
	Ready                bool             `json:"ready"`
	AIEnabled            bool             `json:"ai_enabled"`
	ActiveProvider       string           `json:"active_provider"`
	ProviderConnected    bool             `json:"provider_connected"`
	ProviderSynced       bool             `json:"provider_synced"`
	SquareConnected      bool             `json:"square_connected"`
	SquareSynced         bool             `json:"square_synced"`
	TestBookingCancelled bool             `json:"test_booking_cancelled"`
	ServiceCount         int              `json:"service_count"`
	StaffCount           int              `json:"staff_count"`
	BusinessHoursCount   int              `json:"business_hours_count"`
	Checks               []ReadinessCheck `json:"checks"`
	BlockedReason        string           `json:"blocked_reason,omitempty"`
}

type ProviderCapabilityStatus struct {
	Provider      string `json:"provider"`
	Configured    bool   `json:"configured"`
	Ready         bool   `json:"ready"`
	Model         string `json:"model,omitempty"`
	Voice         string `json:"voice,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

type VoiceAIStatus struct {
	Provider   string                   `json:"provider"`
	Configured bool                     `json:"configured"`
	Ready      bool                     `json:"ready"`
	STT        ProviderCapabilityStatus `json:"stt"`
	LLM        ProviderCapabilityStatus `json:"llm"`
	TTS        ProviderCapabilityStatus `json:"tts"`
}

type Status struct {
	Provider              string                `json:"provider"`
	Configured            bool                  `json:"configured"`
	SignatureVerification bool                  `json:"signature_verification"`
	InboundWebhookURL     string                `json:"inbound_webhook_url"`
	TurnWebhookURL        string                `json:"turn_webhook_url"`
	RecordingWebhookURL   string                `json:"recording_webhook_url"`
	SalonPhone            string                `json:"salon_phone,omitempty"`
	Ready                 bool                  `json:"ready"`
	PhoneBookingReady     bool                  `json:"phone_booking_ready"`
	BlockedReason         string                `json:"blocked_reason,omitempty"`
	AI                    VoiceAIStatus         `json:"ai"`
	Booking               PhoneBookingReadiness `json:"booking"`
	InputMode             string                `json:"input_mode"`
}

type AIProviders struct {
	STT SpeechToTextProvider
	LLM LanguageModelProvider
	TTS TextToSpeechProvider
}

type AudioOutputRecord struct {
	SalonID        string
	CallSessionID  string
	Provider       string
	ProviderCallID string
	ContentType    string
	Audio          []byte
}

type AudioOutput struct {
	ID          string
	ContentType string
	Audio       []byte
}
