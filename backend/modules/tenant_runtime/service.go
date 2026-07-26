package tenantruntime

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

var (
	ErrValidation      = errors.New("tenant runtime validation failed")
	ErrForbidden       = errors.New("tenant runtime access forbidden")
	ErrNotFound        = errors.New("tenant runtime profile not found")
	ErrQuotaExceeded   = errors.New("tenant runtime quota exceeded")
	ErrVersionConflict = errors.New("tenant runtime version conflict")
	ErrActionConflict  = errors.New("tenant runtime action conflict")
)

type authorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type Service struct {
	repo   *Repository
	access authorizer
}

func NewService(repo *Repository, authorizer authorizer) *Service {
	return &Service{repo: repo, access: authorizer}
}

func (s *Service) GetProfile(ctx context.Context, actor middleware.ActorContext, salonID string, windowMinutes int) (*RuntimeProfile, error) {
	salonID = strings.TrimSpace(salonID)
	if windowMinutes <= 0 {
		windowMinutes = 60
	}
	if salonID == "" || windowMinutes > 1440 || s == nil || s.access == nil {
		return nil, ErrValidation
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityOperationsRead}); err != nil {
		return nil, ErrForbidden
	}
	return s.repo.GetProfile(ctx, salonID, actor.UserID, windowMinutes)
}

func (s *Service) UpdateLimits(ctx context.Context, actor middleware.ActorContext, salonID string, req UpdateLimitsRequest) (*Limits, bool, error) {
	salonID = strings.TrimSpace(salonID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if salonID == "" || req.ActionKey == "" || len(req.ActionKey) > 256 || req.ExpectedVersion < 1 || !validLimits(req) || s == nil || s.access == nil {
		return nil, false, ErrValidation
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityOperationsWrite}); err != nil {
		return nil, false, ErrForbidden
	}
	changedFields := []string{
		"expensive_requests_per_minute", "provider_writes_per_minute",
		"scheduling_writes_per_minute", "voice_starts_per_minute", "worker_claims_per_batch",
	}
	sort.Strings(changedFields)
	return s.repo.UpdateLimits(ctx, salonID, actor.UserID, LimitsFingerprint(req), req, changedFields)
}

func (s *Service) AllowTenant(ctx context.Context, actor middleware.ActorContext, salonID, metric string, units int) (Decision, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || !validMetric(metric) || units < 1 || s == nil || s.access == nil {
		return Decision{}, ErrValidation
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfaceTenant, SalonID: salonID, Capability: access.CapabilityBusinessRead}); err != nil {
		return Decision{}, ErrForbidden
	}
	return s.consume(ctx, salonID, metric, units)
}

func (s *Service) AllowPlatform(ctx context.Context, actor middleware.ActorContext, salonID string, capability access.Capability, metric string, units int) (Decision, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || !validMetric(metric) || units < 1 || s == nil || s.access == nil {
		return Decision{}, ErrValidation
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, SalonID: salonID, Capability: capability}); err != nil {
		return Decision{}, ErrForbidden
	}
	return s.consume(ctx, salonID, metric, units)
}

func (s *Service) AllowSystem(ctx context.Context, salonID, metric string, units int) (Decision, error) {
	if strings.TrimSpace(salonID) == "" || !validMetric(metric) || units < 1 {
		return Decision{}, ErrValidation
	}
	return s.consume(ctx, strings.TrimSpace(salonID), metric, units)
}

func (s *Service) consume(ctx context.Context, salonID, metric string, units int) (Decision, error) {
	decision, err := s.repo.Consume(ctx, salonID, metric, units)
	if err != nil {
		return Decision{}, err
	}
	if !decision.Allowed {
		return decision, ErrQuotaExceeded
	}
	return decision, nil
}

func validMetric(metric string) bool {
	switch metric {
	case MetricExpensiveRequest, MetricSchedulingWrite, MetricProviderWrite, MetricVoiceStart:
		return true
	default:
		return false
	}
}

func validLimits(req UpdateLimitsRequest) bool {
	values := []int{req.ExpensiveRequestsPerMinute, req.SchedulingWritesPerMinute, req.ProviderWritesPerMinute, req.VoiceStartsPerMinute}
	for _, value := range values {
		if value < 1 || value > 6000 {
			return false
		}
	}
	return req.WorkerClaimsPerBatch >= 1 && req.WorkerClaimsPerBatch <= 50
}
