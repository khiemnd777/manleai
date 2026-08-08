package business

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/pos"
)

var (
	ErrValidation         = errors.New("business validation failed")
	ErrForbidden          = errors.New("business access forbidden")
	ErrNotFound           = errors.New("business resource not found")
	ErrVersionConflict    = errors.New("business resource version conflict")
	ErrActionConflict     = errors.New("business action conflict")
	ErrProviderReadOnly   = errors.New("business resource is provider read-only")
	ErrDuplicate          = errors.New("business resource already exists")
	ErrPublicationBlocked = errors.New("public catalog is not ready")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Store interface {
	ListTenantSalons(context.Context, string) ([]SalonSummary, error)
	ListPlatformSalons(context.Context, string) ([]SalonSummary, error)
	GetSalonProfile(context.Context, string) (*SalonProfile, error)
	MutateSalonProfile(context.Context, MutationCommand, SalonProfileMutationRequest) (*MutationResult, error)
	ListServices(context.Context, string) ([]Service, error)
	GetService(context.Context, string, string) (*Service, error)
	MutateService(context.Context, MutationCommand, ServiceMutationRequest, bool) (*MutationResult, error)
	ArchiveService(context.Context, MutationCommand) (*MutationResult, error)
	ListServiceCategories(context.Context, string) ([]ServiceCategory, error)
	GetServiceCategory(context.Context, string, string) (*ServiceCategory, error)
	MutateServiceCategory(context.Context, MutationCommand, ServiceCategoryMutationRequest, bool) (*MutationResult, error)
	ArchiveServiceCategory(context.Context, MutationCommand) (*MutationResult, error)
	ListStaff(context.Context, string) ([]StaffMember, error)
	GetStaff(context.Context, string, string) (*StaffMember, error)
	MutateStaff(context.Context, MutationCommand, StaffMutationRequest, bool) (*MutationResult, error)
	ArchiveStaff(context.Context, MutationCommand) (*MutationResult, error)
	ReplaceStaffServiceEligibility(context.Context, MutationCommand, string, []string) (*MutationResult, error)
	GetBusinessHours(context.Context, string) (*BusinessHours, error)
	ReplaceBusinessHours(context.Context, MutationCommand, []BusinessHourPeriodInput) (*MutationResult, error)
	GetPublicCatalogSettings(context.Context, string) (*PublicCatalogSettings, error)
	MutatePublicCatalogSettings(context.Context, MutationCommand, PublicCatalogMutationRequest) (*MutationResult, error)
	ListCustomers(context.Context, string, int, int) ([]Customer, error)
	GetCustomer(context.Context, string, string) (*Customer, error)
	MutateCustomer(context.Context, MutationCommand, CustomerMutationRequest, bool) (*MutationResult, error)
	ArchiveCustomer(context.Context, MutationCommand) (*MutationResult, error)
}

type AccessAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type ServiceLayer struct {
	repo   Store
	access AccessAuthorizer
}

type MutationCommand struct {
	SalonID            string
	ActorUserID        string
	Surface            access.Surface
	ActionKey          string
	ActionType         string
	RequestFingerprint string
	ResourceType       string
	ResourceID         string
	ExpectedVersion    int64
	ChangedFields      []string
	SchedulingFence    bool
}

func NewService(repo Store, authorizer AccessAuthorizer) *ServiceLayer {
	return &ServiceLayer{repo: repo, access: authorizer}
}

func (s *ServiceLayer) ListSalons(ctx context.Context, actor middleware.ActorContext, surface access.Surface) (*SalonDirectoryResponse, error) {
	if surface == access.SurfacePlatform {
		if err := s.authorize(ctx, actor, access.AccessCheck{Surface: surface, Capability: access.CapabilityPlatformTenantsRead}); err != nil {
			return nil, err
		}
		items, err := s.repo.ListPlatformSalons(ctx, actor.UserID)
		return &SalonDirectoryResponse{Salons: nonNil(items)}, err
	}
	if surface != access.SurfaceTenant || strings.TrimSpace(actor.UserID) == "" {
		return nil, ErrForbidden
	}
	items, err := s.repo.ListTenantSalons(ctx, actor.UserID)
	return &SalonDirectoryResponse{Salons: nonNil(items)}, err
}

func (s *ServiceLayer) SalonProfile(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*SalonProfile, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	return s.repo.GetSalonProfile(ctx, strings.TrimSpace(salonID))
}

func (s *ServiceLayer) PlatformTenantContext(ctx context.Context, actor middleware.ActorContext, salonID string) (*PlatformTenantContextResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || actor.PrincipalScope != middleware.PrincipalScopePlatform {
		return nil, ErrForbidden
	}
	if err := s.authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, Capability: access.CapabilityPlatformTenantsRead}); err != nil {
		return nil, err
	}
	profile, err := s.repo.GetSalonProfile(ctx, salonID)
	if err != nil {
		return nil, err
	}
	checks := []access.AccessCheck{
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityBusinessRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityServicesRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityTrainingRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityCallsRead, PIIScope: access.PIIScopeCalls},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityTechnicalRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityOperationsRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityAuditRead},
		{Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityPlatformAccess},
	}
	permissions := PlatformTenantContextPermissions{CanRead: true, AllowedActions: make([]string, 0, len(checks)), PIIScopes: make([]string, 0, 1)}
	for _, check := range checks {
		err := s.access.Authorize(ctx, actor, check)
		if err == nil {
			permissions.AllowedActions = append(permissions.AllowedActions, string(check.Capability))
			if check.PIIScope != "" {
				permissions.PIIScopes = append(permissions.PIIScopes, string(check.PIIScope))
			}
			continue
		}
		if !errors.Is(err, access.ErrForbidden) {
			return nil, err
		}
	}
	return &PlatformTenantContextResponse{
		Data: *profile,
		Meta: PlatformTenantContextMeta{ResourceVersion: profile.Version, Permissions: permissions},
	}, nil
}

func (s *ServiceLayer) UpdateSalonProfile(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req SalonProfileMutationRequest) (*MutationResponse[SalonProfile], error) {
	salonID = strings.TrimSpace(salonID)
	req = normalizeSalonProfile(req)
	if req.Name == "" || req.Phone == "" || !validTimezone(req.Timezone) {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "salon_profile.updated", "salon_profile", salonID, req.MutationControl, req, []string{"name", "phone", "address", "city", "state", "zip_code", "timezone", "primary_language", "secondary_language", "handoff_phone"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutateSalonProfile(ctx, command, req)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetSalonProfile(ctx, salonID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) Services(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*ServicesResponse, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	items, err := s.repo.ListServices(ctx, strings.TrimSpace(salonID))
	return &ServicesResponse{Services: nonNil(items)}, err
}

func (s *ServiceLayer) CreateService(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req ServiceMutationRequest) (*MutationResponse[Service], error) {
	if err := normalizeServiceRequest(&req); err != nil {
		return nil, err
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" || req.DurationMinutes == nil || *req.DurationMinutes <= 0 {
		return nil, ErrValidation
	}
	return s.mutateService(ctx, actor, surface, salonID, uuid.NewString(), req, true)
}

func (s *ServiceLayer) UpdateService(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, serviceID string, req ServiceMutationRequest) (*MutationResponse[Service], error) {
	if err := normalizeServiceRequest(&req); err != nil {
		return nil, err
	}
	if !hasServiceMutation(req) {
		return nil, ErrValidation
	}
	return s.mutateService(ctx, actor, surface, salonID, strings.TrimSpace(serviceID), req, false)
}

func (s *ServiceLayer) mutateService(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, serviceID string, req ServiceMutationRequest, create bool) (*MutationResponse[Service], error) {
	salonID = strings.TrimSpace(salonID)
	if serviceID == "" || req.DurationMinutes != nil && *req.DurationMinutes <= 0 || req.PriceFrom != nil && *req.PriceFrom < 0 {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	action := "service.updated"
	if create {
		action = "service.created"
	}
	fields := serviceChangedFields(req)
	command, err := mutationCommand(actor, surface, salonID, action, "service", serviceID, req.MutationControl, req, fields, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutateService(ctx, command, req, create)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetService(ctx, salonID, result.ResourceID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ArchiveService(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, serviceID string, control MutationControl) (*MutationResponse[Service], error) {
	return s.archiveService(ctx, actor, surface, strings.TrimSpace(salonID), strings.TrimSpace(serviceID), control)
}

func (s *ServiceLayer) archiveService(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, serviceID string, control MutationControl) (*MutationResponse[Service], error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "service.archived", "service", serviceID, control, control, []string{"active", "archived_at"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ArchiveService(ctx, command)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetService(ctx, salonID, serviceID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ServiceCategories(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*ServiceCategoriesResponse, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	items, err := s.repo.ListServiceCategories(ctx, strings.TrimSpace(salonID))
	return &ServiceCategoriesResponse{Categories: nonNil(items)}, err
}

func (s *ServiceLayer) CreateServiceCategory(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req ServiceCategoryMutationRequest) (*MutationResponse[ServiceCategory], error) {
	return s.mutateServiceCategory(ctx, actor, surface, salonID, uuid.NewString(), req, true)
}

func (s *ServiceLayer) UpdateServiceCategory(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, categoryID string, req ServiceCategoryMutationRequest) (*MutationResponse[ServiceCategory], error) {
	return s.mutateServiceCategory(ctx, actor, surface, salonID, strings.TrimSpace(categoryID), req, false)
}

func (s *ServiceLayer) mutateServiceCategory(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, categoryID string, req ServiceCategoryMutationRequest, create bool) (*MutationResponse[ServiceCategory], error) {
	salonID = strings.TrimSpace(salonID)
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Description = strings.TrimSpace(req.Description)
	if categoryID == "" || req.Name == "" || !slugPattern.MatchString(req.Slug) {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	action := "service_category.updated"
	if create {
		action = "service_category.created"
	}
	command, err := mutationCommand(actor, surface, salonID, action, "service_category", categoryID, req.MutationControl, req, []string{"name", "slug", "description", "sort_order"}, false)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutateServiceCategory(ctx, command, req, create)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetServiceCategory(ctx, salonID, result.ResourceID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ArchiveServiceCategory(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, categoryID string, control MutationControl) (*MutationResponse[ServiceCategory], error) {
	salonID = strings.TrimSpace(salonID)
	categoryID = strings.TrimSpace(categoryID)
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "service_category.archived", "service_category", categoryID, control, control, []string{"status", "archived_at"}, false)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ArchiveServiceCategory(ctx, command)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetServiceCategory(ctx, salonID, categoryID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) Staff(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*StaffResponse, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	items, err := s.repo.ListStaff(ctx, strings.TrimSpace(salonID))
	if surface == access.SurfacePlatform {
		for i := range items {
			items[i].Phone = ""
			items[i].Email = ""
		}
	}
	return &StaffResponse{Staff: nonNil(items)}, err
}

func (s *ServiceLayer) CreateStaff(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req StaffMutationRequest) (*MutationResponse[StaffMember], error) {
	normalizeStaffRequest(&req)
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return nil, ErrValidation
	}
	return s.mutateStaff(ctx, actor, surface, salonID, uuid.NewString(), req, true)
}

func (s *ServiceLayer) UpdateStaff(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, staffID string, req StaffMutationRequest) (*MutationResponse[StaffMember], error) {
	normalizeStaffRequest(&req)
	if !hasStaffMutation(req) {
		return nil, ErrValidation
	}
	return s.mutateStaff(ctx, actor, surface, salonID, strings.TrimSpace(staffID), req, false)
}

func (s *ServiceLayer) mutateStaff(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, staffID string, req StaffMutationRequest, create bool) (*MutationResponse[StaffMember], error) {
	salonID = strings.TrimSpace(salonID)
	if staffID == "" || surface == access.SurfacePlatform && (req.Phone != nil || req.Email != nil) {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	action := "staff.updated"
	if create {
		action = "staff.created"
	}
	command, err := mutationCommand(actor, surface, salonID, action, "staff", staffID, req.MutationControl, req, staffChangedFields(req), true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutateStaff(ctx, command, req, create)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetStaff(ctx, salonID, result.ResourceID)
	if item != nil && surface == access.SurfacePlatform {
		item.Phone = ""
		item.Email = ""
	}
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ArchiveStaff(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, staffID string, control MutationControl) (*MutationResponse[StaffMember], error) {
	salonID = strings.TrimSpace(salonID)
	staffID = strings.TrimSpace(staffID)
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "staff.archived", "staff", staffID, control, control, []string{"active", "archived_at"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ArchiveStaff(ctx, command)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetStaff(ctx, salonID, staffID)
	if item != nil && surface == access.SurfacePlatform {
		item.Phone = ""
		item.Email = ""
	}
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ReplaceStaffServiceEligibility(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req StaffServiceEligibilityMutationRequest) (*MutationResponse[StaffMember], error) {
	salonID = strings.TrimSpace(salonID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.ServiceIDs = normalizedIDs(req.ServiceIDs)
	if req.StaffID == "" {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "staff_service_eligibility.replaced", "staff_service_eligibility", salonID, req.MutationControl, req, []string{"service_ids"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ReplaceStaffServiceEligibility(ctx, command, req.StaffID, req.ServiceIDs)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetStaff(ctx, salonID, req.StaffID)
	if item != nil {
		item.EligibilityVersion = result.Version
		if surface == access.SurfacePlatform {
			item.Phone = ""
			item.Email = ""
		}
	}
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) BusinessHours(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*BusinessHours, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	return s.repo.GetBusinessHours(ctx, strings.TrimSpace(salonID))
}

func (s *ServiceLayer) ReplaceBusinessHours(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req BusinessHoursMutationRequest) (*MutationResponse[BusinessHours], error) {
	salonID = strings.TrimSpace(salonID)
	normalizeHourPeriods(req.Periods)
	if !validHourPeriods(req.Periods) {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "business_hours.replaced", "business_hours", salonID, req.MutationControl, req, []string{"periods"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ReplaceBusinessHours(ctx, command, req.Periods)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetBusinessHours(ctx, salonID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) PublicCatalogSettings(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string) (*PublicCatalogSettings, error) {
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, ""); err != nil {
		return nil, err
	}
	return s.repo.GetPublicCatalogSettings(ctx, strings.TrimSpace(salonID))
}

func (s *ServiceLayer) UpdatePublicCatalogSettings(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req PublicCatalogMutationRequest) (*MutationResponse[PublicCatalogSettings], error) {
	salonID = strings.TrimSpace(salonID)
	req.PublicSlug = strings.ToLower(strings.TrimSpace(req.PublicSlug))
	if req.PublicSlug != "" && !slugPattern.MatchString(req.PublicSlug) {
		return nil, ErrValidation
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	command, err := mutationCommand(actor, surface, salonID, "public_catalog.updated", "public_catalog", salonID, req.MutationControl, req, []string{"public_slug", "public_catalog_enabled"}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutatePublicCatalogSettings(ctx, command, req)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetPublicCatalogSettings(ctx, salonID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) Customers(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, limit, offset int) (*CustomersResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 || offset < 0 {
		return nil, ErrValidation
	}
	pii := access.PIIScope("")
	if surface == access.SurfacePlatform {
		pii = access.PIIScopeCustomers
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, pii); err != nil {
		return nil, err
	}
	items, err := s.repo.ListCustomers(ctx, strings.TrimSpace(salonID), limit, offset)
	return &CustomersResponse{Customers: nonNil(items)}, err
}

func (s *ServiceLayer) CreateCustomer(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, req CustomerMutationRequest) (*MutationResponse[Customer], error) {
	normalizeCustomerRequest(&req)
	if req.Name == nil || *req.Name == "" || (req.Phone == nil || *req.Phone == "") && (req.Email == nil || *req.Email == "") {
		return nil, ErrValidation
	}
	return s.mutateCustomer(ctx, actor, surface, salonID, uuid.NewString(), req, true)
}

func (s *ServiceLayer) UpdateCustomer(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, customerID string, req CustomerMutationRequest) (*MutationResponse[Customer], error) {
	normalizeCustomerRequest(&req)
	if !hasCustomerMutation(req) {
		return nil, ErrValidation
	}
	return s.mutateCustomer(ctx, actor, surface, salonID, strings.TrimSpace(customerID), req, false)
}

func (s *ServiceLayer) mutateCustomer(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, customerID string, req CustomerMutationRequest, create bool) (*MutationResponse[Customer], error) {
	salonID = strings.TrimSpace(salonID)
	if customerID == "" {
		return nil, ErrValidation
	}
	pii := access.PIIScope("")
	if surface == access.SurfacePlatform {
		pii = access.PIIScopeCustomers
	}
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	if pii != "" {
		if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, pii); err != nil {
			return nil, err
		}
	}
	action := "customer.updated"
	if create {
		action = "customer.created"
	}
	command, err := mutationCommand(actor, surface, salonID, action, "customer", customerID, req.MutationControl, req, customerChangedFields(req), false)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.MutateCustomer(ctx, command, req, create)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetCustomer(ctx, salonID, result.ResourceID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) ArchiveCustomer(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID, customerID string, control MutationControl) (*MutationResponse[Customer], error) {
	salonID = strings.TrimSpace(salonID)
	customerID = strings.TrimSpace(customerID)
	if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessWrite, ""); err != nil {
		return nil, err
	}
	if surface == access.SurfacePlatform {
		if err := s.authorizeBusiness(ctx, actor, surface, salonID, access.CapabilityBusinessRead, access.PIIScopeCustomers); err != nil {
			return nil, err
		}
	}
	command, err := mutationCommand(actor, surface, salonID, "customer.archived", "customer", customerID, control, control, []string{"active", "archived_at"}, false)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.ArchiveCustomer(ctx, command)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.GetCustomer(ctx, salonID, customerID)
	return mutationResponse(item, result), err
}

func (s *ServiceLayer) authorizeBusiness(ctx context.Context, actor middleware.ActorContext, surface access.Surface, salonID string, capability access.Capability, pii access.PIIScope) error {
	return s.authorize(ctx, actor, access.AccessCheck{Surface: surface, SalonID: strings.TrimSpace(salonID), Capability: capability, PIIScope: pii})
}

func (s *ServiceLayer) authorize(ctx context.Context, actor middleware.ActorContext, check access.AccessCheck) error {
	if s == nil || s.access == nil {
		return ErrForbidden
	}
	if err := s.access.Authorize(ctx, actor, check); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			return ErrForbidden
		}
		return err
	}
	return nil
}

func mutationCommand(actor middleware.ActorContext, surface access.Surface, salonID, actionType, resourceType, resourceID string, control MutationControl, payload any, fields []string, schedulingFence bool) (MutationCommand, error) {
	actionKey := strings.TrimSpace(control.ActionKey)
	if strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(salonID) == "" || strings.TrimSpace(resourceID) == "" || len(actionKey) == 0 || len(actionKey) > 256 || control.ExpectedVersion < 0 || surface != access.SurfaceTenant && surface != access.SurfacePlatform {
		return MutationCommand{}, ErrValidation
	}
	fingerprint, err := fingerprint(payload)
	if err != nil {
		return MutationCommand{}, err
	}
	sort.Strings(fields)
	return MutationCommand{SalonID: salonID, ActorUserID: actor.UserID, Surface: surface, ActionKey: actionKey, ActionType: actionType, RequestFingerprint: fingerprint, ResourceType: resourceType, ResourceID: resourceID, ExpectedVersion: control.ExpectedVersion, ChangedFields: fields, SchedulingFence: schedulingFence}, nil
}

func fingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func mutationResponse[T any](item *T, result *MutationResult) *MutationResponse[T] {
	if item == nil || result == nil {
		return nil
	}
	return &MutationResponse[T]{Data: *item, Replayed: result.Replayed}
}

func normalizeSalonProfile(req SalonProfileMutationRequest) SalonProfileMutationRequest {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = validation.NormalizePhone(req.Phone)
	req.Address = strings.TrimSpace(req.Address)
	req.City = strings.TrimSpace(req.City)
	req.State = strings.TrimSpace(req.State)
	req.ZipCode = strings.TrimSpace(req.ZipCode)
	req.Timezone = strings.TrimSpace(req.Timezone)
	req.PrimaryLanguage = strings.ToLower(strings.TrimSpace(req.PrimaryLanguage))
	req.SecondaryLanguage = strings.ToLower(strings.TrimSpace(req.SecondaryLanguage))
	req.HandoffPhone = validation.NormalizePhone(req.HandoffPhone)
	return req
}

func validTimezone(value string) bool { _, err := time.LoadLocation(value); return err == nil }

func normalizeServiceRequest(req *ServiceMutationRequest) error {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	trimStringPointer(&req.Name)
	trimStringPointer(&req.Description)
	trimStringPointer(&req.AIDescription)
	trimStringPointer(&req.PriceDisplay)
	trimStringPointer(&req.ServiceCategoryID)
	if req.ConsultationProfile != nil {
		normalized, err := pos.NormalizeConsultationProfileWriteRequest(&pos.ServiceConsultationProfileWriteRequest{
			Status:                   req.ConsultationProfile.Status,
			RecommendedOutcomes:      req.ConsultationProfile.RecommendedOutcomes,
			CompatibleCurrentSystems: req.ConsultationProfile.CompatibleCurrentSystems,
			LengthCapabilities:       req.ConsultationProfile.LengthCapabilities,
			PriorityTags:             req.ConsultationProfile.PriorityTags,
			FinishOptions:            req.ConsultationProfile.FinishOptions,
			MaintenanceNote:          req.ConsultationProfile.MaintenanceNote,
			OwnerApprovedSummary:     req.ConsultationProfile.OwnerApprovedSummary,
		})
		if err != nil || normalized == nil {
			return ErrValidation
		}
		req.ConsultationProfile = &ConsultationProfile{
			Status: normalized.Status, RecommendedOutcomes: normalized.RecommendedOutcomes,
			CompatibleCurrentSystems: normalized.CompatibleCurrentSystems,
			LengthCapabilities:       normalized.LengthCapabilities, PriorityTags: normalized.PriorityTags,
			FinishOptions: normalized.FinishOptions, MaintenanceNote: normalized.MaintenanceNote,
			OwnerApprovedSummary: normalized.OwnerApprovedSummary,
		}
	}
	return nil
}

func normalizeStaffRequest(req *StaffMutationRequest) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	trimStringPointer(&req.Name)
	trimStringPointer(&req.Phone)
	trimStringPointer(&req.Email)
	if req.Phone != nil {
		value := validation.NormalizePhone(*req.Phone)
		req.Phone = &value
	}
	if req.Email != nil {
		value := strings.ToLower(*req.Email)
		req.Email = &value
	}
}

func normalizeCustomerRequest(req *CustomerMutationRequest) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	trimStringPointer(&req.Name)
	trimStringPointer(&req.Phone)
	trimStringPointer(&req.Email)
	trimStringPointer(&req.Notes)
	if req.Phone != nil {
		value := validation.NormalizePhone(*req.Phone)
		req.Phone = &value
	}
	if req.Email != nil {
		value := strings.ToLower(*req.Email)
		req.Email = &value
	}
}

func trimStringPointer(value **string) {
	if *value != nil {
		normalized := strings.TrimSpace(**value)
		*value = &normalized
	}
}

func hasServiceMutation(req ServiceMutationRequest) bool {
	return req.Name != nil || req.Description != nil || req.AIDescription != nil || req.DurationMinutes != nil || req.PriceFrom != nil || req.PriceDisplay != nil || req.AIBookable != nil || req.Active != nil || req.ServiceCategoryID != nil || req.ConsultationProfile != nil
}
func hasStaffMutation(req StaffMutationRequest) bool {
	return req.Name != nil || req.Phone != nil || req.Email != nil || req.AIBookable != nil || req.Active != nil
}
func hasCustomerMutation(req CustomerMutationRequest) bool {
	return req.Name != nil || req.Phone != nil || req.Email != nil || req.Notes != nil || req.Active != nil
}

func serviceChangedFields(req ServiceMutationRequest) []string {
	fields := []string{}
	if req.Name != nil {
		fields = append(fields, "name")
	}
	if req.Description != nil {
		fields = append(fields, "description")
	}
	if req.AIDescription != nil {
		fields = append(fields, "ai_description")
	}
	if req.DurationMinutes != nil {
		fields = append(fields, "duration_minutes")
	}
	if req.PriceFrom != nil {
		fields = append(fields, "price_from")
	}
	if req.PriceDisplay != nil {
		fields = append(fields, "price_display")
	}
	if req.AIBookable != nil {
		fields = append(fields, "ai_bookable")
	}
	if req.Active != nil {
		fields = append(fields, "active")
	}
	if req.ServiceCategoryID != nil {
		fields = append(fields, "service_category_id")
	}
	if req.ConsultationProfile != nil {
		fields = append(fields, "consultation_profile")
	}
	return fields
}
func staffChangedFields(req StaffMutationRequest) []string {
	fields := []string{}
	if req.Name != nil {
		fields = append(fields, "name")
	}
	if req.Phone != nil {
		fields = append(fields, "phone")
	}
	if req.Email != nil {
		fields = append(fields, "email")
	}
	if req.AIBookable != nil {
		fields = append(fields, "ai_bookable")
	}
	if req.Active != nil {
		fields = append(fields, "active")
	}
	return fields
}
func customerChangedFields(req CustomerMutationRequest) []string {
	fields := []string{}
	if req.Name != nil {
		fields = append(fields, "name")
	}
	if req.Phone != nil {
		fields = append(fields, "phone")
	}
	if req.Email != nil {
		fields = append(fields, "email")
	}
	if req.Notes != nil {
		fields = append(fields, "notes")
	}
	if req.Active != nil {
		fields = append(fields, "active")
	}
	return fields
}

func normalizedIDs(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
func normalizeHourPeriods(values []BusinessHourPeriodInput) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].DayOfWeek != values[j].DayOfWeek {
			return values[i].DayOfWeek < values[j].DayOfWeek
		}
		return values[i].StartLocalTime < values[j].StartLocalTime
	})
}
func validHourPeriods(values []BusinessHourPeriodInput) bool {
	for _, value := range values {
		if value.DayOfWeek < 0 || value.DayOfWeek > 6 || !validClock(value.StartLocalTime) || !validClock(value.EndLocalTime) || value.EndAtMidnight && value.EndLocalTime != "00:00" || !value.EndAtMidnight && value.EndLocalTime <= value.StartLocalTime {
			return false
		}
	}
	return true
}
func validClock(value string) bool { _, err := time.Parse("15:04", value); return err == nil }

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
