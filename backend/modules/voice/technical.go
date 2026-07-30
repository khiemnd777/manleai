package voice

import (
	"context"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/twiliovoice"
)

type twilioVoiceVerificationStore interface {
	GetTwilioVoiceVerificationEvidence(context.Context, string, string) (*time.Time, *time.Time, error)
}

func (s *Service) TwilioVoiceRoutingStatus(ctx context.Context, salonID string) (*TwilioVoiceRoutingStatus, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return nil, ErrValidation
	}
	status := &TwilioVoiceRoutingStatus{Blockers: []string{}}
	var cfg config.TwilioVoiceConfig
	var publicBaseURL string
	if s.configResolver == nil {
		status.Blockers = append(status.Blockers, "TWILIO_VOICE_ROUTING_NOT_CONFIGURED")
	} else {
		var err error
		cfg, publicBaseURL, err = s.configResolver.ResolveTwilioConfig(ctx, salonID)
		if err != nil {
			status.Blockers = append(status.Blockers, "TWILIO_VOICE_ROUTING_NOT_CONFIGURED")
		}
	}
	status.RoutingConfigured = cfg.RoutingEnabled && strings.TrimSpace(cfg.RouteID) != "" &&
		twiliovoice.ValidE164(cfg.InboundNumber) && twiliovoice.ValidAccountSID(cfg.AccountSID) &&
		strings.TrimSpace(cfg.AuthToken) != "" && twiliovoice.ValidPublicHTTPSBase(publicBaseURL)
	if !status.RoutingConfigured && len(status.Blockers) == 0 {
		status.Blockers = append(status.Blockers, "TWILIO_VOICE_ROUTING_NOT_CONFIGURED")
	}
	store, ok := s.repo.(twilioVoiceVerificationStore)
	if !ok {
		return status, nil
	}
	route := config.TwilioVoiceRouteConfig{SalonID: salonID, PublicBaseURL: publicBaseURL, TwilioVoiceConfig: cfg}
	fingerprint := ""
	if status.RoutingConfigured {
		fingerprint = twilioRoutingFingerprint(route)
	}
	boundCtx := databasecontext.WithSystemSalon(databasecontext.WithScope(ctx, databasecontext.ScopeProvider), databasecontext.ScopeProvider, salonID)
	matchingAt, anyAt, err := store.GetTwilioVoiceVerificationEvidence(boundCtx, salonID, fingerprint)
	if err != nil {
		return nil, err
	}
	status.LastVerifiedInboundAt = matchingAt
	status.LastObservedInboundAt = anyAt
	status.LiveVerified = status.RoutingConfigured && matchingAt != nil
	status.VerificationStale = status.RoutingConfigured && matchingAt == nil && anyAt != nil
	if status.RoutingConfigured && !status.LiveVerified {
		if status.VerificationStale {
			status.Blockers = append(status.Blockers, "TWILIO_VOICE_LIVE_VERIFICATION_STALE")
		} else {
			status.Blockers = append(status.Blockers, "TWILIO_VOICE_LIVE_VERIFICATION_REQUIRED")
		}
	}
	return status, nil
}
