package voice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/twiliovoice"
)

const (
	TwilioEndpointIncoming       = "incoming"
	TwilioEndpointTurn           = "turn"
	TwilioEndpointRecording      = "recording"
	TwilioEndpointStream         = "stream"
	TwilioEndpointStreamStatus   = "stream_status"
	TwilioEndpointStreamFallback = "stream_fallback"

	EventTwilioInboundRouteVerified = "twilio_inbound_route_verified"
)

type TwilioWebhookEnvelope struct {
	ProviderCallID string
	AccountSID     string
	FromPhone      string
	ToPhone        string
}

type ResolvedTwilioVoiceRoute struct {
	config      config.TwilioVoiceRouteConfig
	salon       InboundSalon
	callRoute   *CallRoute
	endpoint    string
	requestPath string
	envelope    TwilioWebhookEnvelope
}

func (r *ResolvedTwilioVoiceRoute) AdapterConfig() config.TwilioVoiceConfig {
	if r == nil {
		return config.TwilioVoiceConfig{}
	}
	return r.config.TwilioVoiceConfig
}

func (r *ResolvedTwilioVoiceRoute) PublicBaseURL() string {
	if r == nil {
		return ""
	}
	return r.config.PublicBaseURL
}

func (r *ResolvedTwilioVoiceRoute) ExactCallbackURL() string {
	if r == nil {
		return ""
	}
	return twiliovoice.HTTPURL(r.config.PublicBaseURL, r.requestPath)
}

func (r *ResolvedTwilioVoiceRoute) exactRequestURL(requestURI string) string {
	if r == nil {
		return ""
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(requestURI))
	if err != nil || parsed.Path != r.requestPath || parsed.Fragment != "" {
		return ""
	}
	return twiliovoice.HTTPURL(r.config.PublicBaseURL, parsed.RequestURI())
}

type VerifiedTwilioVoiceRoute struct {
	resolved *ResolvedTwilioVoiceRoute
}

func (s *Service) ResolveTwilioVoiceRoute(ctx context.Context, routeID, endpoint string, envelope TwilioWebhookEnvelope) (*ResolvedTwilioVoiceRoute, error) {
	resolver, ok := s.configResolver.(TwilioVoiceRouteConfigResolver)
	if !ok {
		return nil, ErrTwilioWebhookRejected
	}
	requestPath, requiresExistingCall := twilioEndpointPath(strings.TrimSpace(routeID), endpoint)
	if requestPath == "" {
		return nil, ErrTwilioWebhookRejected
	}
	envelope.ProviderCallID = strings.TrimSpace(envelope.ProviderCallID)
	envelope.AccountSID = strings.TrimSpace(envelope.AccountSID)
	envelope.FromPhone = strings.TrimSpace(envelope.FromPhone)
	envelope.ToPhone = strings.TrimSpace(envelope.ToPhone)
	if envelope.ProviderCallID == "" || envelope.AccountSID == "" || (endpoint != TwilioEndpointStreamStatus && envelope.ToPhone == "") {
		return nil, ErrTwilioWebhookRejected
	}
	routeConfig, err := resolver.ResolveTwilioVoiceRoute(ctx, strings.TrimSpace(routeID))
	if err != nil || routeConfig.SalonID == "" || !routeConfig.RoutingEnabled {
		return nil, ErrTwilioWebhookRejected
	}
	if routeConfig.RouteID != strings.TrimSpace(routeID) {
		return nil, ErrTwilioWebhookRejected
	}
	if envelope.ToPhone != "" && !hmac.Equal([]byte(routeConfig.InboundNumber), []byte(envelope.ToPhone)) {
		return nil, ErrTwilioWebhookRejected
	}
	if !hmac.Equal([]byte(routeConfig.AccountSID), []byte(envelope.AccountSID)) {
		return nil, ErrTwilioWebhookRejected
	}
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, routeConfig.SalonID)
	salon, err := s.repo.FindInboundSalonByID(boundCtx, routeConfig.SalonID)
	if err != nil || salon == nil || salon.SalonID != routeConfig.SalonID {
		return nil, ErrTwilioWebhookRejected
	}
	callRoute, routeErr := s.repo.FindCallRoute(boundCtx, ProviderTwilio, envelope.ProviderCallID)
	switch {
	case routeErr == nil:
		if callRoute == nil || callRoute.SalonID != routeConfig.SalonID ||
			(strings.TrimSpace(callRoute.FromPhone) != "" && envelope.FromPhone != "" && callRoute.FromPhone != envelope.FromPhone) ||
			(strings.TrimSpace(callRoute.ToPhone) != "" && envelope.ToPhone != "" && callRoute.ToPhone != envelope.ToPhone) {
			return nil, ErrTwilioWebhookRejected
		}
	case errors.Is(routeErr, ErrNotFound) && !requiresExistingCall:
		callRoute = nil
	default:
		return nil, ErrTwilioWebhookRejected
	}
	if envelope.ToPhone == "" && endpoint == TwilioEndpointStreamStatus && callRoute != nil {
		envelope.ToPhone = strings.TrimSpace(callRoute.ToPhone)
	}
	if envelope.FromPhone == "" && endpoint == TwilioEndpointStreamStatus && callRoute != nil {
		envelope.FromPhone = strings.TrimSpace(callRoute.FromPhone)
	}
	if !hmac.Equal([]byte(routeConfig.InboundNumber), []byte(envelope.ToPhone)) {
		return nil, ErrTwilioWebhookRejected
	}
	return &ResolvedTwilioVoiceRoute{
		config: routeConfig, salon: *salon, callRoute: callRoute,
		endpoint: endpoint, requestPath: requestPath, envelope: envelope,
	}, nil
}

func (s *Service) RecordVerifiedRealtimeEvent(ctx context.Context, proof *VerifiedTwilioVoiceRoute, sessionID, eventType string, payload map[string]string) error {
	if proof == nil || proof.resolved == nil || proof.resolved.endpoint != TwilioEndpointStreamStatus || proof.resolved.callRoute == nil ||
		(strings.TrimSpace(sessionID) != "" && strings.TrimSpace(sessionID) != proof.resolved.callRoute.SessionID) {
		return ErrTwilioWebhookRejected
	}
	route := proof.resolved.callRoute
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, route.SalonID)
	return s.repo.RecordWebhookEvent(boundCtx, WebhookEvent{
		SalonID: route.SalonID, CallSessionID: route.SessionID, Provider: ProviderTwilio,
		ProviderCallID: proof.resolved.envelope.ProviderCallID, EventType: strings.TrimSpace(eventType), Payload: payload,
	})
}

func (s *Service) VerifiedRealtimeFallbackMessage(ctx context.Context, proof *VerifiedTwilioVoiceRoute) (string, error) {
	if proof == nil || proof.resolved == nil || proof.resolved.endpoint != TwilioEndpointStreamFallback || proof.resolved.callRoute == nil {
		return "", ErrTwilioWebhookRejected
	}
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, proof.resolved.callRoute.SalonID)
	return s.RealtimeFallbackMessage(boundCtx, ProviderTwilio, proof.resolved.envelope.ProviderCallID)
}

func (s *Service) StreamRouteForResolvedTwilioVoiceRoute(resolved *ResolvedTwilioVoiceRoute, sessionID string) (*CallRoute, error) {
	if resolved == nil || resolved.endpoint != TwilioEndpointStream || resolved.callRoute == nil ||
		strings.TrimSpace(sessionID) == "" || resolved.callRoute.SessionID != strings.TrimSpace(sessionID) {
		return nil, ErrTwilioWebhookRejected
	}
	copyValue := *resolved.callRoute
	return &copyValue, nil
}

func (s *Service) VerifyTwilioVoiceRoute(resolved *ResolvedTwilioVoiceRoute, requestURI string, params map[string]string, signature string, verifier func(config.TwilioVoiceConfig, string, map[string]string, string) bool) (*VerifiedTwilioVoiceRoute, error) {
	exactURL := resolved.exactRequestURL(requestURI)
	if resolved == nil || verifier == nil || exactURL == "" || !verifier(resolved.config.TwilioVoiceConfig, exactURL, params, signature) {
		return nil, ErrTwilioWebhookRejected
	}
	return &VerifiedTwilioVoiceRoute{resolved: resolved}, nil
}

func (s *Service) HandleVerifiedIncomingCall(ctx context.Context, proof *VerifiedTwilioVoiceRoute, req IncomingCallRequest) (*CallReply, error) {
	if !validVerifiedRoute(proof, TwilioEndpointIncoming, req.ProviderCallID, req.ToPhone) {
		return nil, ErrTwilioWebhookRejected
	}
	req.verifiedRoute = proof
	req.Payload = verifiedWebhookAuditPayload(proof)
	reply, err := s.HandleIncomingCall(ctx, req)
	if err != nil {
		return nil, err
	}
	if reply == nil || reply.Session == nil {
		return nil, ErrTwilioWebhookRejected
	}
	boundCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, proof.resolved.salon.SalonID)
	if err := s.repo.RecordWebhookEvent(boundCtx, WebhookEvent{
		SalonID: proof.resolved.salon.SalonID, CallSessionID: reply.Session.ID,
		Provider: ProviderTwilio, ProviderCallID: req.ProviderCallID,
		EventType: EventTwilioInboundRouteVerified, Payload: verifiedWebhookAuditPayload(proof),
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *Service) HandleVerifiedSpeechTurn(ctx context.Context, proof *VerifiedTwilioVoiceRoute, req SpeechTurnRequest) (*CallReply, error) {
	if proof == nil || proof.resolved == nil || proof.resolved.callRoute == nil ||
		(proof.resolved.endpoint != TwilioEndpointTurn && proof.resolved.endpoint != TwilioEndpointRecording) ||
		strings.TrimSpace(req.ProviderCallID) != proof.resolved.envelope.ProviderCallID {
		return nil, ErrTwilioWebhookRejected
	}
	req.verifiedRoute = proof
	req.Payload = verifiedWebhookRuntimePayload(proof, req.Payload)
	return s.HandleSpeechTurn(ctx, req)
}

func validVerifiedRoute(proof *VerifiedTwilioVoiceRoute, endpoint, providerCallID, toPhone string) bool {
	return proof != nil && proof.resolved != nil && proof.resolved.endpoint == endpoint &&
		strings.TrimSpace(providerCallID) == proof.resolved.envelope.ProviderCallID &&
		strings.TrimSpace(toPhone) == proof.resolved.envelope.ToPhone
}

func twilioEndpointPath(routeID, endpoint string) (string, bool) {
	paths := twiliovoice.CanonicalPaths(routeID)
	switch endpoint {
	case TwilioEndpointIncoming:
		return paths.Incoming, false
	case TwilioEndpointTurn:
		return paths.Turn, true
	case TwilioEndpointRecording:
		return paths.Recording, true
	case TwilioEndpointStream:
		return paths.Stream, true
	case TwilioEndpointStreamStatus:
		return paths.StreamStatus, true
	case TwilioEndpointStreamFallback:
		return paths.StreamFallback, true
	default:
		return "", true
	}
}

func verifiedWebhookAuditPayload(proof *VerifiedTwilioVoiceRoute) map[string]string {
	if proof == nil || proof.resolved == nil {
		return map[string]string{}
	}
	return map[string]string{
		"schema_version":      "1",
		"route_id":            proof.resolved.config.RouteID,
		"routing_fingerprint": twilioRoutingFingerprint(proof.resolved.config),
		"identity_verified":   "true",
		"endpoint":            proof.resolved.endpoint,
		"existing_call_route": boolString(proof.resolved.callRoute != nil),
	}
}

func verifiedWebhookRuntimePayload(proof *VerifiedTwilioVoiceRoute, incoming map[string]string) map[string]string {
	payload := verifiedWebhookAuditPayload(proof)
	if proof == nil || proof.resolved == nil {
		return payload
	}
	for _, key := range []string{"RealtimeTranscriptID", "RecordingSid", "EventSid", "TwilioIdempotencyToken"} {
		value := strings.TrimSpace(incoming[key])
		if value == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(proof.resolved.config.AuthToken))
		_, _ = mac.Write([]byte("twilio-event:" + key + ":" + value))
		payload[key] = hex.EncodeToString(mac.Sum(nil))
	}
	return payload
}

func twilioRoutingFingerprint(route config.TwilioVoiceRouteConfig) string {
	paths := twiliovoice.CanonicalPaths(route.RouteID)
	value := strings.Join([]string{route.RouteID, route.InboundNumber, route.AccountSID, route.PublicBaseURL, route.VoiceTransport, paths.Incoming, paths.Turn, paths.Recording, paths.Stream}, "|")
	mac := hmac.New(sha256.New, []byte(route.AuthToken))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
