package voice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
)

const (
	ProviderTwilio = "twilio"
	ProviderOpenAI = "openai"

	EventIncomingCall      = "incoming_call"
	EventSpeechTurn        = "speech_turn"
	EventNoSpeech          = "no_speech"
	EventSTTFailed         = "stt_failed"
	EventLLMFailed         = "llm_failed"
	EventTTSFailed         = "tts_failed"
	EventRealtimeConnected = "realtime_connected"
	EventRealtimeTiming    = "realtime_timing"
	EventRealtimeFailed    = "realtime_failed"
	EventRealtimeStopped   = "realtime_stopped"

	InputModeGather         = "gather"
	InputModeRecording      = "recording"
	InputModeRealtimeStream = "realtime_stream"

	RealtimeEventAudioDelta           = "audio_delta"
	RealtimeEventAudioTranscriptDelta = "audio_transcript_delta"
	RealtimeEventAudioTranscriptDone  = "audio_transcript_done"
	RealtimeEventTranscriptDone       = "transcript_done"
	RealtimeEventSpeechStarted        = "speech_started"
	RealtimeEventSpeechStopped        = "speech_stopped"
	RealtimeEventResponseCreated      = "response_created"
	RealtimeEventResponseDone         = "response_done"
	RealtimeEventSessionUpdated       = "session_updated"
	RealtimeEventError                = "error"
)

var (
	ErrValidation             = errors.New("voice validation failed")
	ErrNotFound               = errors.New("voice record not found")
	ErrProviderDisabled       = errors.New("voice provider is not configured")
	ErrRouteNotFound          = errors.New("voice route not found")
	ErrTurnModelEmptyOutput   = errors.New("turn model returned no structured output")
	ErrTurnModelInvalidOutput = errors.New("turn model returned invalid structured output")
)

// ProviderRequestError carries only bounded, non-secret correlation data from
// a provider request. It deliberately excludes response bodies and request
// payloads so callers can expose the diagnostics in operational timelines.
type ProviderRequestError struct {
	Provider          string
	Stage             string
	StatusCode        int
	RequestID         string
	ErrorType         string
	ErrorCode         string
	ErrorParam        string
	SchemaFingerprint string
	CircuitOpen       bool
	Err               error
}

func (e *ProviderRequestError) Error() string {
	if e == nil {
		return "provider request failed"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s request failed with status %d", strings.TrimSpace(e.Provider), e.StatusCode)
	}
	return strings.TrimSpace(e.Provider) + " request failed"
}

func (e *ProviderRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ProviderRequestError) SafeDiagnostics() map[string]string {
	if e == nil {
		return nil
	}
	diagnostics := map[string]string{}
	if value := safeProviderDiagnosticValue(e.Provider); value != "" {
		diagnostics["provider"] = value
	}
	if value := safeProviderDiagnosticValue(e.Stage); value != "" {
		diagnostics["failure_stage"] = value
	}
	if e.StatusCode >= 100 && e.StatusCode <= 599 {
		diagnostics["http_status"] = strconv.Itoa(e.StatusCode)
		diagnostics["http_status_class"] = strconv.Itoa(e.StatusCode/100) + "xx"
	}
	if value := safeProviderDiagnosticValue(e.RequestID); value != "" {
		diagnostics["request_id"] = value
	}
	if value := safeProviderDiagnosticValue(e.ErrorType); value != "" {
		diagnostics["error_type"] = value
	}
	if value := safeProviderDiagnosticValue(e.ErrorCode); value != "" {
		diagnostics["error_code"] = value
	}
	if value := safeProviderDiagnosticValue(e.ErrorParam); value != "" {
		diagnostics["error_param"] = value
	}
	if value := safeProviderDiagnosticValue(e.SchemaFingerprint); value != "" {
		diagnostics["schema_fingerprint"] = value
	}
	if e.CircuitOpen {
		diagnostics["circuit_open"] = "true"
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return diagnostics
}

func safeProviderDiagnosticValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return ""
	}
	return value
}

type ConversationEngine interface {
	StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error)
	Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error)
	HandleUnintelligibleVoiceInput(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.VoiceInputHandoffRequest) (*conversation.Session, error)
	Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error)
	TranscriptionContext(ctx context.Context, salonID string, ownerUserID string, sessionID string) (conversation.TranscriptionContext, error)
}

type Store interface {
	GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error)
	GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*PhoneBookingReadiness, error)
	FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error)
	FindCallRoute(ctx context.Context, provider string, providerCallID string) (*CallRoute, error)
	RecordWebhookEvent(ctx context.Context, event WebhookEvent) error
	HasTerminalRealtimeFailure(ctx context.Context, provider string, providerCallID string, sessionID string) (bool, error)
	SaveAudioOutput(ctx context.Context, record AudioOutputRecord) (*AudioOutput, error)
	GetAudioOutput(ctx context.Context, id string) (*AudioOutput, error)
}

type TelephonyProvider interface {
	VerifyWebhook(url string, params map[string]string, signature string) bool
}

type SpeechToTextProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	Transcribe(ctx context.Context, salonID string, req SpeechToTextRequest) (string, error)
}

type LanguageModelProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	GenerateReply(ctx context.Context, req ModelRequest) (ModelReply, error)
}

type TurnModelProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	InterpretTurn(ctx context.Context, req TurnModelRequest) (TurnModelReply, error)
}

// TurnContractVerifier runs the same structured-output contract used by live
// turn interpretation without creating conversation state or calling tools.
type TurnContractVerifier interface {
	CheckTurnContract(ctx context.Context, salonID string) (TurnContractCheck, error)
}

type TurnContractCheck struct {
	Provider          string `json:"provider"`
	SchemaFingerprint string `json:"schema_fingerprint"`
	RequestID         string `json:"request_id,omitempty"`
}

type TurnModelRequest struct {
	SalonID             string
	SessionID           string
	Channel             string
	CustomerMessage     string
	ExpectedInput       string
	SelectedServices    []conversation.ConversationServiceRef
	CatalogServices     []conversation.ConversationServiceRef
	SelectedStaff       []conversation.ConversationStaffRef
	CatalogStaff        []conversation.ConversationStaffRef
	Pending             *conversation.PendingConversationAct
	CurrentBookingStage string
	BookingAction       string
	CurrentDraft        conversation.ConversationDraftRef
	Consultation        *conversation.ConsultationState
}

type ActModelReply struct {
	Kind               string   `json:"kind"`
	Entity             string   `json:"entity"`
	SourceIDs          []string `json:"source_ids"`
	TargetIDs          []string `json:"target_ids"`
	SourceCategoryID   string   `json:"source_category_id"`
	SourceCategoryName string   `json:"source_category_name"`
	TargetCategoryID   string   `json:"target_category_id"`
	TargetCategoryName string   `json:"target_category_name"`
	Scope              string   `json:"scope"`
	GuestScope         string   `json:"guest_scope"`
	GuestRef           string   `json:"guest_ref"`
	Subject            string   `json:"subject"`
	Value              string   `json:"value"`
	Count              int      `json:"count"`
	Confidence         float64  `json:"confidence"`
	Reason             string   `json:"reason"`
}

type QuestionModelReply struct {
	Subject        string                   `json:"subject"`
	ServiceIDs     []string                 `json:"service_ids"`
	StaffIDs       []string                 `json:"staff_ids"`
	TimePreference TimePreferenceModelReply `json:"time_preference"`
	Confidence     float64                  `json:"confidence"`
	Reason         string                   `json:"reason"`
}

type TimePreferenceModelReply struct {
	Direction string `json:"direction"`
	Minutes   int    `json:"minutes"`
}

type TurnModelReply struct {
	Goal         string                 `json:"goal"`
	Acts         []ActModelReply        `json:"acts"`
	Questions    []QuestionModelReply   `json:"questions"`
	Confidence   float64                `json:"confidence"`
	Reason       string                 `json:"reason"`
	Consultation ConsultationModelReply `json:"consultation"`
	Safety       SafetyModelReply       `json:"safety"`
}

type ConsultationModelReply struct {
	CurrentSystem        string                           `json:"current_system"`
	DesiredOutcome       string                           `json:"desired_outcome"`
	LengthChange         string                           `json:"length_change"`
	Priorities           []string                         `json:"priorities"`
	DesiredFinishes      []string                         `json:"desired_finishes"`
	ComparedServiceIDs   []string                         `json:"compared_service_ids"`
	BookingRequested     bool                             `json:"booking_requested"`
	ConversationComplete bool                             `json:"conversation_complete"`
	Confidence           float64                          `json:"confidence"`
	Reason               string                           `json:"reason"`
	Mutations            []ConsultationMutationModelReply `json:"mutations"`
}

type ConsultationMutationModelReply struct {
	Field      string   `json:"field"`
	Operation  string   `json:"operation"`
	Values     []string `json:"values"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
}

type SafetyModelReply struct {
	Concern    bool    `json:"concern"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type TextToSpeechProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	ContentType() string
	Synthesize(ctx context.Context, salonID string, text string, voice string) ([]byte, error)
}

// StreamingSpeechProvider renders backend-approved text without generating a
// new assistant response. Chunks are raw audio in the encoding declared by
// SpeechStreamResult and are delivered in sequence order.
type StreamingSpeechProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	StreamSpeech(ctx context.Context, salonID string, req SpeechStreamRequest, onChunk func(SpeechChunk) error) (SpeechStreamResult, error)
}

type SpeechStreamRequest struct {
	RequestID string
	Text      string
	Voice     string
}

type SpeechChunk struct {
	Sequence int
	Audio    []byte
}

type SpeechStreamResult struct {
	ProviderRequestID string
	Encoding          string
	SampleRate        int
	ChunkCount        int
	AudioBytes        int
}

type RealtimeSpeechProvider interface {
	Name() string
	Configured(ctx context.Context, salonID string) bool
	ConnectRealtime(ctx context.Context, salonID string, opts RealtimeSessionOptions) (RealtimeSession, error)
}

type RealtimeSession interface {
	AppendInputAudio(ctx context.Context, base64Audio string) error
	Speak(ctx context.Context, req RealtimeSpeakRequest) error
	CancelResponse(ctx context.Context, responseID string) error
	RequiresResponseIdentity() bool
	TranscriptPolicy() RealtimeTranscriptPolicy
	Events() <-chan RealtimeEvent
	Close() error
}

type RealtimeTranscriptPolicy struct {
	Profile             string
	EffectiveProfile    string
	RequireLogProbs     bool
	MinMeanLogProb      float64
	MinTokenLogProb     float64
	MaxTokensPerSecond  float64
	AdaptiveStrongNoise *RealtimeTranscriptThresholds
}

// RealtimeTranscriptThresholds carries provider-owned confidence bounds that
// a telephony bridge may activate for the remainder of one call after
// structured audio-quality evidence. It contains no transcript text or
// location assumptions.
type RealtimeTranscriptThresholds struct {
	Profile            string
	MinMeanLogProb     float64
	MinTokenLogProb    float64
	MaxTokensPerSecond float64
}

type RealtimeSpeakRequest struct {
	RequestID string
	Text      string
}

type RealtimeSessionOptions struct {
	SessionID           string
	CallID              string
	Voice               string
	Instructions        string
	TranscriptionPrompt string
}

type RealtimeEvent struct {
	Type               string
	ItemID             string
	ResponseID         string
	ResponseRequestID  string
	ResponseStatus     string
	Transcript         string
	TranscriptLogProbs []float64
	AudioStartMS       int
	AudioEndMS         int
	AudioBase64        string
	AudioTranscript    string
	ErrorCode          string
	ErrorParam         string
	Error              string
}

type ConfigResolver interface {
	ResolveTwilioConfig(ctx context.Context, salonID string) (config.TwilioVoiceConfig, string, error)
	ResolveOpenAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error)
}

type ModelRequest struct {
	SalonID              string
	SessionID            string
	Channel              string
	Intent               string
	Outcome              string
	CustomerMessage      string
	SafeReply            string
	SalonName            string
	AITone               string
	BookingConfirmed     bool
	FallbackOrHandoff    bool
	MissingBookingField  string
	KnownBookingFields   []string
	NextRequiredField    string
	SelectedServiceNames []string
	Summary              string
	KnowledgeContext     string
	ReplyPolicy          string
	ConsultationQuestion *conversation.ConsultationQuestionSpec
}

type ModelReply struct {
	Message    string
	Confidence float64
	Handoff    bool
	Reason     string
}

type InboundSalon struct {
	SalonID                 string
	OwnerUserID             string
	SalonName               string
	Phone                   string
	RecordingEnabled        bool
	RecordingConsentMessage string
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
	Provider          string
	ProviderCallID    string
	FromPhone         string
	ToPhone           string
	SpeechText        string
	Audio             []byte
	AudioContentType  string
	InputModeOverride string
	Payload           map[string]string
}

type SpeechToTextRequest struct {
	Audio       []byte
	ContentType string
	Prompt      string
}

type CallReply struct {
	Message            string
	OpeningNotice      string
	Continue           bool
	Session            *conversation.Session
	AudioURL           string
	InputMode          string
	BackendDiagnostics map[string]string
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
	Ready                     bool             `json:"ready"`
	AIEnabled                 bool             `json:"ai_enabled"`
	ActiveProvider            string           `json:"active_provider"`
	ProviderConnected         bool             `json:"provider_connected"`
	ProviderSynced            bool             `json:"provider_synced"`
	SquareConnected           bool             `json:"square_connected"`
	SquareSynced              bool             `json:"square_synced"`
	TestBookingCancelled      bool             `json:"test_booking_cancelled"`
	BookingWriteBlocked       bool             `json:"booking_write_blocked"`
	BookingWriteBlockedCode   string           `json:"booking_write_blocked_code,omitempty"`
	BookingWriteBlockedReason string           `json:"booking_write_blocked_reason,omitempty"`
	BookingWriteBlockedAt     *time.Time       `json:"booking_write_blocked_at,omitempty"`
	ServiceCount              int              `json:"service_count"`
	StaffCount                int              `json:"staff_count"`
	BusinessHoursCount        int              `json:"business_hours_count"`
	Checks                    []ReadinessCheck `json:"checks"`
	BlockedReason             string           `json:"blocked_reason,omitempty"`
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
	Realtime   ProviderCapabilityStatus `json:"realtime"`
}

type SemanticCheckStatus struct {
	Provider          string            `json:"provider"`
	Configured        bool              `json:"configured"`
	Verified          bool              `json:"verified"`
	SchemaFingerprint string            `json:"schema_fingerprint,omitempty"`
	RequestID         string            `json:"request_id,omitempty"`
	Diagnostics       map[string]string `json:"diagnostics,omitempty"`
}

type Status struct {
	Provider              string                `json:"provider"`
	Configured            bool                  `json:"configured"`
	SignatureVerification bool                  `json:"signature_verification"`
	InboundWebhookURL     string                `json:"inbound_webhook_url"`
	TurnWebhookURL        string                `json:"turn_webhook_url"`
	RecordingWebhookURL   string                `json:"recording_webhook_url"`
	StreamWebhookURL      string                `json:"stream_webhook_url"`
	SalonPhone            string                `json:"salon_phone,omitempty"`
	Ready                 bool                  `json:"ready"`
	PhoneBookingReady     bool                  `json:"phone_booking_ready"`
	BlockedReason         string                `json:"blocked_reason,omitempty"`
	AI                    VoiceAIStatus         `json:"ai"`
	Booking               PhoneBookingReadiness `json:"booking"`
	InputMode             string                `json:"input_mode"`
}

type AIProviders struct {
	STT          SpeechToTextProvider
	LLM          LanguageModelProvider
	TurnModel    TurnModelProvider
	TTS          TextToSpeechProvider
	StreamingTTS StreamingSpeechProvider
	Realtime     RealtimeSpeechProvider
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
