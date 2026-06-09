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
}

func NewService(repo *Repository, conversation ConversationEngine, cfg config.VoiceConfig) *Service {
	return &Service{
		repo:         repo,
		conversation: conversation,
		cfg:          cfg,
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
		SalonPhone:            salon.Phone,
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
	return &CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, nil
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
		return &CallReply{
			Message:  "I did not hear that. How can I help you today?",
			Continue: true,
			Session:  session,
		}, nil
	}

	session, err := s.conversation.Message(ctx, route.SalonID, route.OwnerUserID, route.SessionID, conversation.MessageRequest{Message: req.SpeechText})
	if errors.Is(err, conversation.ErrSessionClosed) {
		session, _ = s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return &CallReply{
			Message:  "This request is already complete. The owner can help with anything else.",
			Continue: false,
			Session:  session,
		}, nil
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
	return &CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, nil
}

func (s *Service) configured() bool {
	return defaultProvider(s.cfg.Provider) == ProviderTwilio && strings.TrimSpace(s.cfg.Twilio.AuthToken) != ""
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
