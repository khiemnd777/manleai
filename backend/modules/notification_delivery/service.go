package notificationdelivery

import (
	"context"
	"strings"
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context, salonID, ownerUserID, status string, limit, offset int) (*ListResponse, error) {
	salonID, ownerUserID, status = strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(status)
	if salonID == "" || ownerUserID == "" || !ValidListStatus(status) || ValidateListBounds(limit, offset) != nil {
		return nil, ErrValidation
	}
	items, metrics, err := s.repo.ListForOwner(ctx, salonID, ownerUserID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return &ListResponse{Deliveries: items, Metrics: metrics, Limit: limit, Offset: offset, HasMore: hasMore}, nil
}

func (s *Service) Get(ctx context.Context, salonID, ownerUserID, notificationID string) (*DetailResponse, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(notificationID) == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.GetForOwner(ctx, salonID, ownerUserID, notificationID)
	if err != nil {
		return nil, err
	}
	return &DetailResponse{Delivery: *item}, nil
}

func (s *Service) Requeue(ctx context.Context, salonID, ownerUserID, notificationID string, req RequeueRequest) (*DetailResponse, bool, error) {
	actionKey := strings.TrimSpace(req.ActionKey)
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(notificationID) == "" || actionKey == "" || len(actionKey) > 256 {
		return nil, false, ErrValidation
	}
	item, replayed, err := s.repo.RequeueForOwner(ctx, salonID, ownerUserID, notificationID, actionKey, RequeueFingerprint(notificationID))
	if err != nil {
		return nil, false, err
	}
	return &DetailResponse{Delivery: *item}, replayed, nil
}

func (s *Service) ApplyProviderCallback(ctx context.Context, callback ProviderCallback) error {
	if strings.TrimSpace(callback.Provider) == "" || strings.TrimSpace(callback.ProviderMessageID) == "" || strings.TrimSpace(callback.ProviderStatus) == "" || callback.StatusRank <= 0 || strings.TrimSpace(callback.EventKey) == "" || strings.TrimSpace(callback.EventFingerprint) == "" {
		return ErrValidation
	}
	return s.repo.ApplyProviderCallback(ctx, callback)
}

func (s *Service) SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error) {
	return s.repo.SalonIDForProviderMessage(ctx, provider, providerMessageID)
}
