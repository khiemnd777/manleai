package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/conversation"
)

type Service struct {
	repo         *Repository
	conversation ConversationEngine
	cfg          config.VoiceConfig
	providers    AIProviders
}

func NewService(repo *Repository, conversation ConversationEngine, cfg config.VoiceConfig, providers AIProviders) *Service {
	return &Service{
		repo:         repo,
		conversation: conversation,
		cfg:          cfg,
		providers:    providers,
	}
}

func (s *Service) Status(ctx context.Context, salonID string, ownerUserID string) (*Status, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	salon, err := s.repo.GetSalonVoiceStatus(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Provider:              defaultProvider(s.cfg.Provider),
		Configured:            s.configured(),
		SignatureVerification: strings.TrimSpace(s.cfg.Twilio.AuthToken) != "",
		InboundWebhookURL:     s.webhookURL(s.cfg.Twilio.IncomingPath),
		TurnWebhookURL:        s.webhookURL(s.cfg.Twilio.TurnPath),
		RecordingWebhookURL:   s.webhookURL(s.cfg.Twilio.RecordingPath),
		SalonPhone:            salon.Phone,
		AI:                    s.aiStatus(),
		InputMode:             s.inputMode(),
	}
	switch {
	case !status.Configured:
		status.BlockedReason = "Twilio voice provider is not configured."
	case strings.TrimSpace(salon.Phone) == "":
		status.BlockedReason = "Salon phone is not configured."
	default:
		status.Ready = true
	}
	return status, nil
}

func (s *Service) Audio(ctx context.Context, id string) (*AudioOutput, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrValidation
	}
	return s.repo.GetAudioOutput(ctx, id)
}

func (s *Service) HandleIncomingCall(ctx context.Context, req IncomingCallRequest) (*CallReply, error) {
	req = normalizeIncomingCall(req)
	if !s.configured() {
		return nil, ErrProviderDisabled
	}
	if req.Provider == "" || req.ProviderCallID == "" || req.ToPhone == "" {
		return nil, ErrValidation
	}
	salon, err := s.repo.FindSalonByPhone(ctx, req.ToPhone)
	if errors.Is(err, ErrNotFound) {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventIncomingCall,
			Payload:        req.Payload,
		})
		return nil, ErrRouteNotFound
	}
	if err != nil {
		return nil, err
	}

	session, err := s.getOrStartPhoneSession(ctx, salon.SalonID, salon.OwnerUserID, req.Provider, req.ProviderCallID, req.FromPhone, req.ToPhone)
	if err != nil {
		return nil, err
	}
	_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
		SalonID:        salon.SalonID,
		CallSessionID:  session.ID,
		Provider:       req.Provider,
		ProviderCallID: req.ProviderCallID,
		EventType:      EventIncomingCall,
		Payload:        req.Payload,
	})
	return s.buildReply(ctx, CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, session, req.Provider, req.ProviderCallID), nil
}

func (s *Service) HandleSpeechTurn(ctx context.Context, req SpeechTurnRequest) (*CallReply, error) {
	req = normalizeSpeechTurn(req)
	if !s.configured() {
		return nil, ErrProviderDisabled
	}
	if req.Provider == "" || req.ProviderCallID == "" {
		return nil, ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, req.Provider, req.ProviderCallID)
	if errors.Is(err, ErrNotFound) && req.ToPhone != "" {
		salon, routeErr := s.repo.FindSalonByPhone(ctx, req.ToPhone)
		if routeErr != nil {
			err = routeErr
		} else {
			session, startErr := s.getOrStartPhoneSession(ctx, salon.SalonID, salon.OwnerUserID, req.Provider, req.ProviderCallID, req.FromPhone, req.ToPhone)
			if startErr != nil {
				return nil, startErr
			}
			route = &CallRoute{SalonID: salon.SalonID, OwnerUserID: salon.OwnerUserID, SessionID: session.ID}
			err = nil
		}
	}
	if errors.Is(err, ErrNotFound) {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventSpeechTurn,
			Payload:        req.Payload,
		})
		return nil, ErrRouteNotFound
	}
	if err != nil {
		return nil, err
	}

	if req.SpeechText == "" && len(req.Audio) > 0 {
		text, transcribeErr := s.transcribe(ctx, req)
		if transcribeErr != nil {
			_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
				SalonID:        route.SalonID,
				CallSessionID:  route.SessionID,
				Provider:       req.Provider,
				ProviderCallID: req.ProviderCallID,
				EventType:      EventSTTFailed,
				Payload:        req.Payload,
			})
			session, _ := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
			return s.buildReply(ctx, CallReply{
				Message:  "I could not hear that clearly. Please say it again, or the owner can help directly.",
				Continue: true,
				Session:  session,
			}, session, req.Provider, req.ProviderCallID), nil
		}
		req.SpeechText = strings.TrimSpace(text)
	}

	if req.SpeechText == "" {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        route.SalonID,
			CallSessionID:  route.SessionID,
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventNoSpeech,
			Payload:        req.Payload,
		})
		session, _ := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return s.buildReply(ctx, CallReply{
			Message:  "I did not hear that. How can I help you today?",
			Continue: true,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID), nil
	}

	session, err := s.conversation.Message(ctx, route.SalonID, route.OwnerUserID, route.SessionID, conversation.MessageRequest{Message: req.SpeechText})
	if errors.Is(err, conversation.ErrSessionClosed) {
		session, _ = s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return s.buildReply(ctx, CallReply{
			Message:  "This request is already complete. The owner can help with anything else.",
			Continue: false,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID), nil
	}
	if err != nil {
		return nil, err
	}
	_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
		SalonID:        route.SalonID,
		CallSessionID:  route.SessionID,
		Provider:       req.Provider,
		ProviderCallID: req.ProviderCallID,
		EventType:      EventSpeechTurn,
		Payload:        req.Payload,
	})
	return s.buildReply(ctx, CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, session, req.Provider, req.ProviderCallID), nil
}

func (s *Service) configured() bool {
	return defaultProvider(s.cfg.Provider) == ProviderTwilio && strings.TrimSpace(s.cfg.Twilio.AuthToken) != ""
}

func (s *Service) transcribe(ctx context.Context, req SpeechTurnRequest) (string, error) {
	if s.providers.STT == nil || !s.providers.STT.Configured() {
		return "", ErrProviderDisabled
	}
	return s.providers.STT.Transcribe(ctx, req.Audio, req.AudioContentType)
}

func (s *Service) buildReply(ctx context.Context, reply CallReply, session *conversation.Session, provider string, providerCallID string) *CallReply {
	reply.InputMode = s.inputMode()
	if session == nil || strings.TrimSpace(reply.Message) == "" || s.providers.TTS == nil || !s.providers.TTS.Configured() {
		return &reply
	}
	audio, err := s.providers.TTS.Synthesize(ctx, reply.Message, s.ttsVoice())
	if err != nil || len(audio) == 0 {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        session.SalonID,
			CallSessionID:  session.ID,
			Provider:       provider,
			ProviderCallID: providerCallID,
			EventType:      EventTTSFailed,
			Payload:        map[string]string{"provider": s.providers.TTS.Name()},
		})
		return &reply
	}
	output, err := s.repo.SaveAudioOutput(ctx, AudioOutputRecord{
		SalonID:        session.SalonID,
		CallSessionID:  session.ID,
		Provider:       s.providers.TTS.Name(),
		ProviderCallID: providerCallID,
		ContentType:    s.providers.TTS.ContentType(),
		Audio:          audio,
	})
	if err != nil {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        session.SalonID,
			CallSessionID:  session.ID,
			Provider:       provider,
			ProviderCallID: providerCallID,
			EventType:      EventTTSFailed,
			Payload:        map[string]string{"provider": s.providers.TTS.Name()},
		})
		return &reply
	}
	reply.AudioURL = s.audioURL(output.ID)
	return &reply
}

func (s *Service) audioURL(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return s.webhookURL("/api/voice/audio/" + strings.TrimSpace(id))
}

func (s *Service) inputMode() string {
	if s.providers.STT != nil && s.providers.STT.Configured() {
		return InputModeRecording
	}
	return InputModeGather
}

func (s *Service) ttsVoice() string {
	return strings.TrimSpace(s.cfg.AI.OpenAI.SpeechVoice)
}

func (s *Service) aiStatus() VoiceAIStatus {
	provider := defaultAIProvider(s.cfg.AI.Provider)
	status := VoiceAIStatus{
		Provider: provider,
		STT:      s.capabilityStatus(provider, "stt"),
		LLM:      s.capabilityStatus(provider, "llm"),
		TTS:      s.capabilityStatus(provider, "tts"),
	}
	status.Configured = status.STT.Configured || status.LLM.Configured || status.TTS.Configured
	status.Ready = status.STT.Ready && status.LLM.Ready && status.TTS.Ready
	return status
}

func (s *Service) capabilityStatus(provider string, capability string) ProviderCapabilityStatus {
	status := ProviderCapabilityStatus{Provider: provider}
	if provider != ProviderOpenAI {
		status.BlockedReason = "External AI voice provider is not configured."
		return status
	}
	status.Configured = strings.TrimSpace(s.cfg.AI.OpenAI.APIKey) != ""
	switch capability {
	case "stt":
		status.Model = strings.TrimSpace(s.cfg.AI.OpenAI.TranscriptionModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.STT != nil && s.providers.STT.Configured()
	case "llm":
		status.Model = strings.TrimSpace(s.cfg.AI.OpenAI.ReplyModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.LLM != nil && s.providers.LLM.Configured()
	case "tts":
		status.Model = strings.TrimSpace(s.cfg.AI.OpenAI.SpeechModel)
		status.Voice = strings.TrimSpace(s.cfg.AI.OpenAI.SpeechVoice)
		status.Ready = status.Configured && status.Model != "" && status.Voice != "" && s.providers.TTS != nil && s.providers.TTS.Configured()
	}
	if !status.Configured {
		status.BlockedReason = "OpenAI API key is not configured."
	} else if !status.Ready {
		status.BlockedReason = "OpenAI model configuration is incomplete."
	}
	return status
}

func (s *Service) webhookURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if s.cfg.PublicBaseURL == "" {
		return path
	}
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (s *Service) getOrStartPhoneSession(ctx context.Context, salonID string, ownerUserID string, provider string, providerCallID string, fromPhone string, toPhone string) (*conversation.Session, error) {
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err == nil {
		return s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return s.conversation.StartPhoneCall(ctx, salonID, ownerUserID, conversation.StartPhoneCallRequest{
		Provider:       provider,
		ProviderCallID: providerCallID,
		FromPhone:      fromPhone,
		ToPhone:        toPhone,
	})
}

func normalizeIncomingCall(req IncomingCallRequest) IncomingCallRequest {
	req.Provider = defaultProvider(req.Provider)
	req.ProviderCallID = strings.TrimSpace(req.ProviderCallID)
	req.FromPhone = validation.NormalizePhone(req.FromPhone)
	req.ToPhone = validation.NormalizePhone(req.ToPhone)
	if req.Payload == nil {
		req.Payload = map[string]string{}
	}
	return req
}

func normalizeSpeechTurn(req SpeechTurnRequest) SpeechTurnRequest {
	req.Provider = defaultProvider(req.Provider)
	req.ProviderCallID = strings.TrimSpace(req.ProviderCallID)
	req.FromPhone = validation.NormalizePhone(req.FromPhone)
	req.ToPhone = validation.NormalizePhone(req.ToPhone)
	req.SpeechText = strings.TrimSpace(req.SpeechText)
	req.AudioContentType = strings.TrimSpace(req.AudioContentType)
	if req.Payload == nil {
		req.Payload = map[string]string{}
	}
	return req
}

func defaultProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ProviderTwilio
	}
	return value
}

func defaultAIProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not_configured"
	}
	return value
}

func lastAIMessage(session *conversation.Session) string {
	if session == nil {
		return "Thank you for calling. How can I help you today?"
	}
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		if session.Transcript[i].Speaker == conversation.SpeakerAI {
			return session.Transcript[i].Body
		}
	}
	return "Thank you for calling. How can I help you today?"
}
