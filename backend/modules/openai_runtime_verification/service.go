package openairuntimeverification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type CapabilityProber interface {
	VerifyCapability(context.Context, string, string) (string, error)
}

type Service struct {
	repo     *Repository
	resolver openairuntime.Resolver
	prober   CapabilityProber
}

func NewService(repo *Repository, resolver openairuntime.Resolver, prober CapabilityProber) *Service {
	return &Service{repo: repo, resolver: resolver, prober: prober}
}

func (s *Service) Enqueue(ctx context.Context, salonID, actorUserID string, req VerifyRequest) (*RunStatus, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if s == nil || s.repo == nil || s.resolver == nil || salonID == "" || actorUserID == "" || req.ActionKey == "" || len(req.ActionKey) > 256 || req.ExpectedConfigVersion <= 0 {
		return nil, false, ErrValidation
	}
	resolved, err := s.resolver.ResolveOpenAIRuntimeConfig(ctx, salonID)
	if err != nil || strings.TrimSpace(resolved.SalonID) != salonID || !openairuntime.Validate(resolved).Ready {
		return nil, false, ErrValidation
	}
	plans := verificationPlan(resolved)
	fingerprint, err := requestFingerprint(req, resolved, plans)
	if err != nil {
		return nil, false, err
	}
	return s.repo.Enqueue(ctx, resolved, actorUserID, req, plans, fingerprint)
}

func (s *Service) Latest(ctx context.Context, salonID string) (*RunStatus, error) {
	if s == nil || s.repo == nil || strings.TrimSpace(salonID) == "" {
		return nil, ErrValidation
	}
	return s.repo.Latest(ctx, strings.TrimSpace(salonID))
}

func (s *Service) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if s == nil || s.repo == nil || s.resolver == nil || s.prober == nil {
		return 0, ErrValidation
	}
	claims, err := s.repo.Claim(ctx, limit, 5*time.Minute)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, claim := range claims {
		itemCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, claim.SalonID)
		if err := s.processClaim(itemCtx, claim); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (s *Service) processClaim(ctx context.Context, claim Claim) error {
	run, err := s.repo.LoadClaimed(ctx, claim)
	if err != nil {
		return err
	}
	_ = s.repo.InsertEvent(ctx, run.SalonID, run.ID, "claimed:"+run.ClaimToken, "claimed", "claimed", "", "")
	resolved, err := s.resolver.ResolveOpenAIRuntimeConfig(ctx, run.SalonID)
	if err != nil {
		return s.repo.Finish(ctx, run, "failed", "config_unresolvable")
	}
	if !runMatchesResolved(run, resolved) {
		return s.repo.Finish(ctx, run, "stale", "config_fence_changed")
	}
	failed := false
	for _, capability := range run.Capabilities {
		if !capability.Required || capability.Status == "verified" {
			continue
		}
		started := time.Now()
		requestID, probeErr := s.prober.VerifyCapability(ctx, run.SalonID, capability.Capability)
		status, code := "verified", ""
		if probeErr != nil {
			status, code, failed = "failed", classifyError(probeErr), true
		}
		if err := s.repo.CompleteCapability(ctx, run, capability.Capability, status, time.Since(started), requestID, code); err != nil {
			return err
		}
	}
	resolved, err = s.resolver.ResolveOpenAIRuntimeConfig(ctx, run.SalonID)
	if err != nil || !runMatchesResolved(run, resolved) {
		return s.repo.Finish(ctx, run, "stale", "config_fence_changed")
	}
	if failed {
		return s.repo.Finish(ctx, run, "failed", "capability_failed")
	}
	return s.repo.Finish(ctx, run, "succeeded", "")
}

func verificationPlan(resolved openairuntime.ResolvedConfig) []capabilityPlan {
	plans := make([]capabilityPlan, 0, len(openairuntime.CapabilityOrder))
	for _, capability := range openairuntime.CapabilityOrder {
		required := true
		if capability == openairuntime.CapabilityRealtime {
			required = resolved.Config.RealtimeEnabled
		}
		plans = append(plans, capabilityPlan{Capability: capability, Required: required})
	}
	return plans
}

func requestFingerprint(req VerifyRequest, resolved openairuntime.ResolvedConfig, plans []capabilityPlan) (string, error) {
	raw, err := json.Marshal(struct {
		Request              VerifyRequest
		SalonID              string
		IntegrationConfigID  string
		CredentialRevision   int64
		DestinationPolicy    string
		VerificationContract string
		Plans                []capabilityPlan
	}{req, resolved.SalonID, resolved.IntegrationConfigID, resolved.CredentialRevision,
		openairuntime.DestinationPolicyVersion, openairuntime.VerificationContract, plans})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func runMatchesResolved(run *claimedRun, resolved openairuntime.ResolvedConfig) bool {
	return run != nil && strings.TrimSpace(resolved.SalonID) == run.SalonID &&
		resolved.IntegrationConfigID == run.IntegrationConfigID && resolved.ConfigVersion == run.ConfigVersion &&
		resolved.CredentialRevision == run.CredentialRevision && resolved.DestinationProfile == openairuntime.DestinationProfile &&
		run.DestinationPolicyVersion == openairuntime.DestinationPolicyVersion &&
		run.VerificationContractVersion == openairuntime.VerificationContract && openairuntime.Validate(resolved).Ready
}

func classifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "provider_timeout"
	}
	var providerErr *voice.ProviderRequestError
	if errors.As(err, &providerErr) {
		if providerErr.StatusCode == 401 || providerErr.StatusCode == 403 {
			return "credential_rejected"
		}
		if providerErr.StatusCode == 429 {
			return "provider_rate_limited"
		}
		if providerErr.StatusCode >= 500 {
			return "provider_unavailable"
		}
		return "provider_request_rejected"
	}
	return "capability_probe_failed"
}
