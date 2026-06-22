package customer

import (
	"context"
	"strings"

	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/pos"
)

const defaultCustomerLimit = 50
const maxCustomerLimit = 100

type Service struct {
	store     Store
	providers map[string]pos.POSProvider
}

func NewService(store Store, providers []pos.POSProvider) *Service {
	byName := make(map[string]pos.POSProvider, len(providers))
	for _, provider := range providers {
		if provider != nil {
			byName[provider.Name()] = provider
		}
	}
	return &Service{store: store, providers: byName}
}

func (s *Service) List(ctx context.Context, salonID string, ownerUserID string, limit int) (*ListResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	items, err := s.store.ListCustomers(ctx, salonID, ownerUserID, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	return &ListResponse{
		Customers: items,
		Summary:   summarize(items),
	}, nil
}

func (s *Service) Create(ctx context.Context, salonID string, ownerUserID string, req WriteRequest) (*MutationResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	input, err := normalizeWriteRequest(req, true)
	if err != nil {
		return nil, err
	}
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	item, err := s.store.CreateCustomer(ctx, salonID, ownerUserID, input)
	if err != nil {
		return nil, err
	}
	return &MutationResponse{Customer: *item}, nil
}

func (s *Service) Update(ctx context.Context, salonID string, ownerUserID string, customerID string, req WriteRequest) (*MutationResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	customerID = strings.TrimSpace(customerID)
	input, err := normalizeWriteRequest(req, false)
	if err != nil {
		return nil, err
	}
	if salonID == "" || ownerUserID == "" || customerID == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	item, err := s.store.UpdateCustomer(ctx, salonID, ownerUserID, customerID, input)
	if err != nil {
		return nil, err
	}
	return &MutationResponse{Customer: *item}, nil
}

func (s *Service) Archive(ctx context.Context, salonID string, ownerUserID string, customerID string) (*MutationResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	customerID = strings.TrimSpace(customerID)
	if salonID == "" || ownerUserID == "" || customerID == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	item, err := s.store.ArchiveCustomer(ctx, salonID, ownerUserID, customerID)
	if err != nil {
		return nil, err
	}
	return &MutationResponse{Customer: *item}, nil
}

func (s *Service) SearchPOS(ctx context.Context, salonID string, ownerUserID string, providerName string, phone string) (*SearchResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		providerName = pos.ProviderSquare
	}
	phone = validation.NormalizePhone(phone)
	if salonID == "" || ownerUserID == "" || phone == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	provider := s.providers[providerName]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}
	item, err := provider.SearchCustomerByPhone(ctx, salonID, phone)
	if err != nil {
		return nil, err
	}
	return &SearchResponse{
		Customer: item,
		Found:    item != nil,
		Provider: provider.Name(),
	}, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultCustomerLimit
	}
	if limit > maxCustomerLimit {
		return maxCustomerLimit
	}
	return limit
}

func summarize(items []Record) Summary {
	summary := Summary{TotalKnownCustomers: len(items)}
	for i := range items {
		item := items[i]
		summary.ConfirmedAppointments += item.ConfirmedAppointments
		summary.PendingRequests += item.PendingRequests
		if item.CallCount > 0 {
			summary.CustomersWithCalls++
		}
		if summary.LastCustomerActivityAt == nil || item.LastActivityAt.After(*summary.LastCustomerActivityAt) {
			activityAt := item.LastActivityAt
			summary.LastCustomerActivityAt = &activityAt
		}
	}
	return summary
}

func normalizeWriteRequest(req WriteRequest, create bool) (Mutation, error) {
	name := strings.TrimSpace(req.Name)
	phone := validation.NormalizePhone(req.Phone)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	notes := strings.TrimSpace(req.Notes)
	if name == "" || (phone == "" && email == "") {
		return Mutation{}, ErrValidation
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	} else if !create {
		active = true
	}
	return Mutation{
		Name:            name,
		Phone:           phone,
		NormalizedPhone: phone,
		Email:           email,
		NormalizedEmail: email,
		Notes:           notes,
		Active:          active,
	}, nil
}
