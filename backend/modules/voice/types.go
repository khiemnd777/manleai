package voice

import (
	"context"
	"errors"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

const (
	ProviderTwilio = "twilio"

	EventIncomingCall = "incoming_call"
	EventSpeechTurn   = "speech_turn"
	EventNoSpeech     = "no_speech"
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

type TelephonyProvider interface {
	VerifyWebhook(url string, params map[string]string, signature string) bool
}

type SpeechToTextProvider interface {
	Transcribe(ctx context.Context, audio []byte, contentType string) (string, error)
}

type LanguageModelProvider interface {
	GenerateReply(ctx context.Context, req ModelRequest) (ModelReply, error)
}

type TextToSpeechProvider interface {
	Synthesize(ctx context.Context, text string, voice string) ([]byte, error)
}

type ModelRequest struct {
	SalonID   string
	SessionID string
	Message   string
}

type ModelReply struct {
	Message string
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
	Provider       string
	ProviderCallID string
	FromPhone      string
	ToPhone        string
	SpeechText     string
	Payload        map[string]string
}

type CallReply struct {
	Message  string
	Continue bool
	Session  *conversation.Session
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

type Status struct {
	Provider              string `json:"provider"`
	Configured            bool   `json:"configured"`
	SignatureVerification bool   `json:"signature_verification"`
	InboundWebhookURL     string `json:"inbound_webhook_url"`
	TurnWebhookURL        string `json:"turn_webhook_url"`
	SalonPhone            string `json:"salon_phone,omitempty"`
	Ready                 bool   `json:"ready"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
}
