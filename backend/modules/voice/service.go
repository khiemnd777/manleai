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
	repo           Store
	conversation   ConversationEngine
	cfg            config.VoiceConfig
	providers      AIProviders
	configResolver ConfigResolver
}

func NewService(repo Store, conversation ConversationEngine, cfg config.VoiceConfig, providers AIProviders) *Service {
	return &Service{
		repo:         repo,
		conversation: conversation,
		cfg:          cfg,
		providers:    providers,
	}
}

func (s *Service) SetConfigResolver(resolver ConfigResolver) {
	s.configResolver = resolver
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
	bookingReadiness, err := s.repo.GetPhoneBookingReadiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	voiceCfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Provider:              defaultProvider(voiceCfg.Provider),
		Configured:            s.configured(voiceCfg),
		SignatureVerification: strings.TrimSpace(voiceCfg.Twilio.AuthToken) != "",
		InboundWebhookURL:     s.webhookURL(voiceCfg, voiceCfg.Twilio.IncomingPath),
		TurnWebhookURL:        s.webhookURL(voiceCfg, voiceCfg.Twilio.TurnPath),
		RecordingWebhookURL:   s.webhookURL(voiceCfg, voiceCfg.Twilio.RecordingPath),
		StreamWebhookURL:      s.streamWebhookURL(voiceCfg, voiceCfg.Twilio.StreamPath),
		SalonPhone:            salon.Phone,
		AI:                    s.aiStatus(ctx, salonID, voiceCfg),
		Booking:               *bookingReadiness,
		InputMode:             s.inputMode(ctx, salonID),
	}
	switch {
	case !status.Configured:
		status.BlockedReason = "Twilio voice provider is not configured."
	case strings.TrimSpace(salon.Phone) == "":
		status.BlockedReason = "Salon phone is not configured."
	default:
		status.Ready = true
	}
	status.PhoneBookingReady = status.Ready && bookingReadiness.Ready
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
	voiceCfg, err := s.voiceConfig(ctx, salon.SalonID)
	if err != nil {
		return nil, err
	}
	if !s.configured(voiceCfg) {
		return nil, ErrProviderDisabled
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
	voiceCfg, err := s.voiceConfig(ctx, route.SalonID)
	if err != nil {
		return nil, err
	}
	if !s.configured(voiceCfg) {
		return nil, ErrProviderDisabled
	}

	if req.SpeechText == "" && len(req.Audio) > 0 {
		text, transcribeErr := s.transcribe(ctx, route.SalonID, route.OwnerUserID, route.SessionID, req)
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
			return s.buildReplyWithInputMode(ctx, CallReply{
				Message:  "I could not hear that clearly. Please say it again, or the owner can help directly.",
				Continue: true,
				Session:  session,
			}, session, req.Provider, req.ProviderCallID, req.InputModeOverride), nil
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
		return s.buildReplyWithInputMode(ctx, CallReply{
			Message:  "I did not hear that. How can I help you today?",
			Continue: true,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID, req.InputModeOverride), nil
	}

	session, err := s.conversation.Message(ctx, route.SalonID, route.OwnerUserID, route.SessionID, conversation.MessageRequest{
		Message:  req.SpeechText,
		EventKey: speechTurnEventKey(req),
	})
	if errors.Is(err, conversation.ErrSessionClosed) {
		session, _ = s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return s.buildReplyWithInputMode(ctx, CallReply{
			Message:  "This request is already complete. The owner can help with anything else.",
			Continue: false,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID, req.InputModeOverride), nil
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
	return s.buildReplyWithInputMode(ctx, CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, session, req.Provider, req.ProviderCallID, req.InputModeOverride), nil
}

func (s *Service) configured(cfg config.VoiceConfig) bool {
	return defaultProvider(cfg.Provider) == ProviderTwilio && strings.TrimSpace(cfg.Twilio.AuthToken) != ""
}

func (s *Service) transcribe(ctx context.Context, salonID string, ownerUserID string, sessionID string, req SpeechTurnRequest) (string, error) {
	if s.providers.STT == nil || !s.providers.STT.Configured(ctx, salonID) {
		return "", ErrProviderDisabled
	}
	return s.providers.STT.Transcribe(ctx, salonID, SpeechToTextRequest{
		Audio:       req.Audio,
		ContentType: req.AudioContentType,
		Prompt:      s.transcriptionContextPrompt(ctx, salonID, ownerUserID, sessionID),
	})
}

func speechTurnEventKey(req SpeechTurnRequest) string {
	callID := strings.TrimSpace(req.ProviderCallID)
	if callID == "" {
		callID = strings.TrimSpace(req.Payload["CallSid"])
	}
	if callID == "" {
		return ""
	}
	for _, key := range []string{"RealtimeTranscriptID", "RecordingSid", "EventSid", "TwilioIdempotencyToken"} {
		value := strings.TrimSpace(req.Payload[key])
		if value != "" {
			return strings.Join([]string{defaultProvider(req.Provider), callID, strings.ToLower(key), value}, ":")
		}
	}
	return ""
}

func (s *Service) buildReply(ctx context.Context, reply CallReply, session *conversation.Session, provider string, providerCallID string) *CallReply {
	return s.buildReplyWithInputMode(ctx, reply, session, provider, providerCallID, "")
}

func (s *Service) buildReplyWithInputMode(ctx context.Context, reply CallReply, session *conversation.Session, provider string, providerCallID string, inputModeOverride string) *CallReply {
	salonID := ""
	if session != nil {
		salonID = session.SalonID
	}
	reply.InputMode = s.inputMode(ctx, salonID)
	if strings.TrimSpace(inputModeOverride) != "" {
		reply.InputMode = normalizeVoiceTransport(inputModeOverride)
	}
	if reply.InputMode == InputModeRealtimeStream {
		return &reply
	}
	if session == nil || strings.TrimSpace(reply.Message) == "" || s.providers.TTS == nil || !s.providers.TTS.Configured(ctx, session.SalonID) {
		return &reply
	}
	audio, err := s.providers.TTS.Synthesize(ctx, session.SalonID, reply.Message, s.ttsVoice(ctx, session.SalonID))
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
	reply.AudioURL = s.audioURL(ctx, session.SalonID, output.ID)
	return &reply
}

func (s *Service) audioURL(ctx context.Context, salonID string, id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	cfg, _ := s.voiceConfig(ctx, salonID)
	return s.webhookURL(cfg, "/api/voice/audio/"+strings.TrimSpace(id))
}

func (s *Service) inputMode(ctx context.Context, salonID string) string {
	cfg, err := s.voiceConfig(ctx, salonID)
	if err == nil && normalizeVoiceTransport(cfg.Twilio.VoiceTransport) == InputModeRealtimeStream && s.realtimeReady(ctx, salonID, cfg) {
		return InputModeRealtimeStream
	}
	if s.providers.STT != nil && s.providers.STT.Configured(ctx, salonID) {
		return InputModeRecording
	}
	return InputModeGather
}

func (s *Service) ttsVoice(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.SpeechVoice)
	}
	return strings.TrimSpace(cfg.SpeechVoice)
}

func (s *Service) ConnectRealtime(ctx context.Context, salonID string, sessionID string, providerCallID string) (RealtimeSession, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(providerCallID) == "" {
		return nil, ErrValidation
	}
	cfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return nil, err
	}
	if !s.realtimeReady(ctx, salonID, cfg) {
		return nil, ErrProviderDisabled
	}
	ownerUserID := ""
	if route, err := s.repo.FindCallRoute(ctx, ProviderTwilio, providerCallID); err == nil && route.SessionID == strings.TrimSpace(sessionID) {
		ownerUserID = route.OwnerUserID
	}
	return s.providers.Realtime.ConnectRealtime(ctx, salonID, RealtimeSessionOptions{
		SessionID:           strings.TrimSpace(sessionID),
		CallID:              strings.TrimSpace(providerCallID),
		Voice:               s.realtimeVoice(ctx, salonID),
		Instructions:        s.realtimeInstructions(ctx, salonID),
		TranscriptionPrompt: s.transcriptionContextPrompt(ctx, salonID, ownerUserID, sessionID),
	})
}

func (s *Service) RecordRealtimeEvent(ctx context.Context, provider string, providerCallID string, sessionID string, eventType string, payload map[string]string) error {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	sessionID = strings.TrimSpace(sessionID)
	eventType = strings.TrimSpace(eventType)
	if providerCallID == "" || eventType == "" {
		return ErrValidation
	}
	event := WebhookEvent{
		Provider:       provider,
		ProviderCallID: providerCallID,
		EventType:      eventType,
		Payload:        payload,
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err == nil {
		if sessionID != "" && route.SessionID != sessionID {
			return ErrRouteNotFound
		}
		event.SalonID = route.SalonID
		event.CallSessionID = route.SessionID
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.repo.RecordWebhookEvent(ctx, event)
}

func (s *Service) RealtimeFallbackMessage(ctx context.Context, provider string, providerCallID string) (string, error) {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	if providerCallID == "" {
		return "", ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "The live phone connection had a problem. Please call again or wait for the owner.", nil
		}
		return "", err
	}
	session, err := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
	if err != nil {
		return "", err
	}
	if session == nil || session.Status != conversation.StatusActive {
		return "", nil
	}
	return "The live phone connection had a problem. Please call again or wait for the owner.", nil
}

func (s *Service) realtimeVoice(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.RealtimeVoice)
	}
	return strings.TrimSpace(cfg.RealtimeVoice)
}

func (s *Service) realtimeInstructions(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.RealtimeInstructions)
	}
	return strings.TrimSpace(cfg.RealtimeInstructions)
}

func (s *Service) transcriptionContextPrompt(ctx context.Context, salonID string, ownerUserID string, sessionID string) string {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	context, err := s.conversation.TranscriptionContext(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(context.Prompt)
}

func (s *Service) aiStatus(ctx context.Context, salonID string, cfg config.VoiceConfig) VoiceAIStatus {
	provider := defaultAIProvider(cfg.AI.Provider)
	status := VoiceAIStatus{
		Provider: provider,
		STT:      s.capabilityStatus(ctx, salonID, cfg, provider, "stt"),
		LLM:      s.capabilityStatus(ctx, salonID, cfg, provider, "llm"),
		TTS:      s.capabilityStatus(ctx, salonID, cfg, provider, "tts"),
		Realtime: s.capabilityStatus(ctx, salonID, cfg, provider, "realtime"),
	}
	status.Configured = status.STT.Configured || status.LLM.Configured || status.TTS.Configured || status.Realtime.Configured
	status.Ready = status.STT.Ready && status.LLM.Ready && status.TTS.Ready
	return status
}

func (s *Service) capabilityStatus(ctx context.Context, salonID string, cfg config.VoiceConfig, provider string, capability string) ProviderCapabilityStatus {
	status := ProviderCapabilityStatus{Provider: provider}
	if provider != ProviderOpenAI {
		status.BlockedReason = "External AI voice provider is not configured."
		return status
	}
	status.Configured = strings.TrimSpace(cfg.AI.OpenAI.APIKey) != ""
	switch capability {
	case "stt":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.TranscriptionModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.STT != nil && s.providers.STT.Configured(ctx, salonID)
	case "llm":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.ReplyModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.LLM != nil && s.providers.LLM.Configured(ctx, salonID)
	case "tts":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.SpeechModel)
		status.Voice = strings.TrimSpace(cfg.AI.OpenAI.SpeechVoice)
		status.Ready = status.Configured && status.Model != "" && status.Voice != "" && s.providers.TTS != nil && s.providers.TTS.Configured(ctx, salonID)
	case "realtime":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.RealtimeModel)
		status.Voice = strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice)
		if !cfg.AI.OpenAI.RealtimeEnabled {
			status.BlockedReason = "OpenAI Realtime is disabled."
			return status
		}
		status.Configured = status.Configured && cfg.AI.OpenAI.RealtimeEnabled
		status.Ready = status.Configured && status.Model != "" && status.Voice != "" && s.providers.Realtime != nil && s.providers.Realtime.Configured(ctx, salonID)
	}
	if !status.Configured {
		status.BlockedReason = "OpenAI API key is not configured."
	} else if !status.Ready {
		status.BlockedReason = "OpenAI model configuration is incomplete."
	}
	return status
}

func (s *Service) webhookURL(cfg config.VoiceConfig, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if cfg.PublicBaseURL == "" {
		return path
	}
	return strings.TrimRight(cfg.PublicBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (s *Service) streamWebhookURL(cfg config.VoiceConfig, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "ws://") || strings.HasPrefix(path, "wss://") {
		return path
	}
	webhookURL := s.webhookURL(cfg, path)
	switch {
	case strings.HasPrefix(webhookURL, "https://"):
		return "wss://" + strings.TrimPrefix(webhookURL, "https://")
	case strings.HasPrefix(webhookURL, "http://"):
		return "ws://" + strings.TrimPrefix(webhookURL, "http://")
	default:
		return webhookURL
	}
}

func (s *Service) TwilioWebhookConfig(ctx context.Context, providerCallID string, toPhone string) (config.TwilioVoiceConfig, string, error) {
	salonID := ""
	providerCallID = strings.TrimSpace(providerCallID)
	if providerCallID != "" {
		route, err := s.repo.FindCallRoute(ctx, ProviderTwilio, providerCallID)
		if err == nil {
			salonID = route.SalonID
		} else if !errors.Is(err, ErrNotFound) {
			return config.TwilioVoiceConfig{}, "", err
		}
	}
	if salonID == "" && strings.TrimSpace(toPhone) != "" {
		salon, err := s.repo.FindSalonByPhone(ctx, validation.NormalizePhone(toPhone))
		if err == nil {
			salonID = salon.SalonID
		} else if !errors.Is(err, ErrNotFound) {
			return config.TwilioVoiceConfig{}, "", err
		}
	}
	voiceCfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return config.TwilioVoiceConfig{}, "", err
	}
	return voiceCfg.Twilio, voiceCfg.PublicBaseURL, nil
}

func (s *Service) StreamRoute(ctx context.Context, provider string, providerCallID string, sessionID string) (*CallRoute, error) {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	sessionID = strings.TrimSpace(sessionID)
	if providerCallID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err != nil {
		return nil, err
	}
	if route.SessionID != sessionID {
		return nil, ErrRouteNotFound
	}
	return route, nil
}

func (s *Service) voiceConfig(ctx context.Context, salonID string) (config.VoiceConfig, error) {
	cfg := s.cfg
	cfg.Provider = defaultProvider(cfg.Provider)
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	cfg.Twilio.IncomingPath = defaultString(strings.TrimSpace(cfg.Twilio.IncomingPath), "/api/voice/twilio/incoming")
	cfg.Twilio.TurnPath = defaultString(strings.TrimSpace(cfg.Twilio.TurnPath), "/api/voice/twilio/turn")
	cfg.Twilio.RecordingPath = defaultString(strings.TrimSpace(cfg.Twilio.RecordingPath), "/api/voice/twilio/recording")
	cfg.Twilio.StreamPath = defaultString(strings.TrimSpace(cfg.Twilio.StreamPath), "/api/voice/twilio/stream")
	cfg.Twilio.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.Twilio.VoiceTransport, InputModeRecording))
	cfg.AI.OpenAI.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(cfg.AI.OpenAI.BaseURL), "/"), "https://api.openai.com/v1")
	cfg.AI.OpenAI.TranscriptionModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.TranscriptionModel), "gpt-4o-mini-transcribe")
	cfg.AI.OpenAI.ReplyModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.ReplyModel), "gpt-4.1-mini")
	cfg.AI.OpenAI.SpeechModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.SpeechModel), "gpt-4o-mini-tts")
	cfg.AI.OpenAI.SpeechVoice = defaultString(strings.TrimSpace(cfg.AI.OpenAI.SpeechVoice), "alloy")
	cfg.AI.OpenAI.RealtimeModel = config.NormalizeOpenAIRealtimeModel(cfg.AI.OpenAI.RealtimeModel)
	cfg.AI.OpenAI.RealtimeVoice = defaultString(strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice), cfg.AI.OpenAI.SpeechVoice)
	cfg.AI.OpenAI.RealtimeNoiseProfile = config.NormalizeOpenAIRealtimeNoiseProfile(cfg.AI.OpenAI.RealtimeNoiseProfile)
	cfg.AI.OpenAI.RealtimeInstructions = strings.TrimSpace(cfg.AI.OpenAI.RealtimeInstructions)
	if s.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return cfg, nil
	}
	twilioCfg, publicBaseURL, err := s.configResolver.ResolveTwilioConfig(ctx, salonID)
	if err != nil {
		return config.VoiceConfig{}, err
	}
	openAICfg, openAIEnabled, err := s.configResolver.ResolveOpenAIConfig(ctx, salonID)
	if err != nil {
		return config.VoiceConfig{}, err
	}
	cfg.Twilio = twilioCfg
	cfg.Twilio.StreamPath = defaultString(strings.TrimSpace(cfg.Twilio.StreamPath), "/api/voice/twilio/stream")
	cfg.Twilio.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.Twilio.VoiceTransport, InputModeRecording))
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	cfg.AI.OpenAI = openAICfg
	if openAIEnabled {
		cfg.AI.Provider = ProviderOpenAI
	} else {
		cfg.AI.Provider = ""
	}
	return cfg, nil
}

func (s *Service) openAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	if s.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return s.cfg.AI.OpenAI, strings.TrimSpace(s.cfg.AI.Provider) == ProviderOpenAI, nil
	}
	return s.configResolver.ResolveOpenAIConfig(ctx, salonID)
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

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeVoiceTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case InputModeRealtimeStream:
		return InputModeRealtimeStream
	default:
		return InputModeRecording
	}
}

func (s *Service) realtimeReady(ctx context.Context, salonID string, cfg config.VoiceConfig) bool {
	return cfg.AI.OpenAI.RealtimeEnabled &&
		strings.TrimSpace(cfg.AI.OpenAI.APIKey) != "" &&
		strings.TrimSpace(cfg.AI.OpenAI.RealtimeModel) != "" &&
		strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice) != "" &&
		s.providers.Realtime != nil &&
		s.providers.Realtime.Configured(ctx, salonID)
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
