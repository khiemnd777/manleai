package pos

import (
	"context"
	"errors"
	"strings"
)

var ErrValidation = errors.New("pos validation failed")

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	ListServices(ctx context.Context, salonID string, provider string) ([]Service, error)
	ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error)
	UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error)
	UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error)
}

type ServiceLayer struct {
	repo Store
}

func NewService(repo Store) *ServiceLayer {
	return &ServiceLayer{repo: repo}
}

func (s *ServiceLayer) Services(ctx context.Context, salonID string, ownerUserID string) ([]Service, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.repo.ListServices(ctx, salonID, ProviderSquare)
}

func (s *ServiceLayer) Staff(ctx context.Context, salonID string, ownerUserID string) ([]StaffMember, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.repo.ListStaff(ctx, salonID, ProviderSquare)
}

func (s *ServiceLayer) UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	return s.repo.UpdateServiceAIBookable(ctx, salonID, ownerUserID, serviceID, aiBookable)
}

func (s *ServiceLayer) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return nil, ErrValidation
	}
	return s.repo.UpdateStaffAIBookable(ctx, salonID, ownerUserID, staffID, aiBookable)
}
