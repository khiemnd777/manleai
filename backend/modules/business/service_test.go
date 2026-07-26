package business

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func TestPlatformStaffProjectionOmitsContactPII(t *testing.T) {
	store := &fakeBusinessStore{staff: []StaffMember{{ID: "staff-1", Name: "Kim", Phone: "+15551234567", Email: "kim@example.com"}}}
	authorizer := &fakeBusinessAuthorizer{}
	service := NewService(store, authorizer)

	result, err := service.Staff(context.Background(), middleware.ActorContext{UserID: "platform-user"}, access.SurfacePlatform, "salon-1")
	if err != nil {
		t.Fatalf("Staff: %v", err)
	}
	if len(result.Staff) != 1 || result.Staff[0].Phone != "" || result.Staff[0].Email != "" {
		t.Fatalf("platform staff projection leaked contact fields: %#v", result.Staff)
	}
	if len(authorizer.checks) != 1 || authorizer.checks[0].Surface != access.SurfacePlatform || authorizer.checks[0].Capability != access.CapabilityBusinessRead {
		t.Fatalf("checks = %#v", authorizer.checks)
	}
}

func TestPlatformCustomerReadRequiresExactPIIGrant(t *testing.T) {
	authorizer := &fakeBusinessAuthorizer{authorize: func(check access.AccessCheck) error {
		if check.PIIScope == access.PIIScopeCustomers {
			return access.ErrForbidden
		}
		return nil
	}}
	service := NewService(&fakeBusinessStore{}, authorizer)

	_, err := service.Customers(context.Background(), middleware.ActorContext{UserID: "platform-user"}, access.SurfacePlatform, "salon-1", 50, 0)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Customers error = %v, want forbidden", err)
	}
	if len(authorizer.checks) != 1 || authorizer.checks[0].PIIScope != access.PIIScopeCustomers {
		t.Fatalf("checks = %#v", authorizer.checks)
	}
}

func TestTenantCrossSalonAccessFailsBeforeRepository(t *testing.T) {
	store := &fakeBusinessStore{}
	authorizer := &fakeBusinessAuthorizer{authorize: func(check access.AccessCheck) error {
		if check.SalonID == "other-salon" {
			return access.ErrForbidden
		}
		return nil
	}}
	service := NewService(store, authorizer)

	_, err := service.Services(context.Background(), middleware.ActorContext{UserID: "tenant-user"}, access.SurfaceTenant, "other-salon")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Services error = %v, want forbidden", err)
	}
	if store.listServicesCalls != 0 {
		t.Fatalf("repository called %d times after authorization denial", store.listServicesCalls)
	}
}

func TestPlatformCannotWriteStaffContactWithoutImpersonation(t *testing.T) {
	store := &fakeBusinessStore{}
	service := NewService(store, &fakeBusinessAuthorizer{})
	phone := "+15551234567"

	_, err := service.UpdateStaff(context.Background(), middleware.ActorContext{UserID: "platform-user"}, access.SurfacePlatform, "salon-1", "staff-1", StaffMutationRequest{MutationControl: MutationControl{ActionKey: "update-staff", ExpectedVersion: 1}, Phone: &phone})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateStaff error = %v, want validation", err)
	}
	if store.mutateStaffCalls != 0 {
		t.Fatalf("repository called %d times", store.mutateStaffCalls)
	}
}

func TestTenantMutationPreservesActualActorAndFixedSurface(t *testing.T) {
	name := "Classic Manicure"
	duration := 45
	store := &fakeBusinessStore{service: &Service{ID: "service-1", Name: name, Version: 1}}
	service := NewService(store, &fakeBusinessAuthorizer{})

	result, err := service.CreateService(context.Background(), middleware.ActorContext{UserID: "business-manager"}, access.SurfaceTenant, "salon-1", ServiceMutationRequest{MutationControl: MutationControl{ActionKey: "create-service", ExpectedVersion: 0}, Name: &name, DurationMinutes: &duration})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if result.Data.ID != "service-1" {
		t.Fatalf("service = %#v", result.Data)
	}
	if store.lastCommand.ActorUserID != "business-manager" || store.lastCommand.Surface != access.SurfaceTenant || store.lastCommand.SalonID != "salon-1" {
		t.Fatalf("command = %#v", store.lastCommand)
	}
}

type fakeBusinessAuthorizer struct {
	checks    []access.AccessCheck
	authorize func(access.AccessCheck) error
}

func (f *fakeBusinessAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	f.checks = append(f.checks, check)
	if f.authorize != nil {
		return f.authorize(check)
	}
	return nil
}

type fakeBusinessStore struct {
	Store
	staff             []StaffMember
	service           *Service
	lastCommand       MutationCommand
	listServicesCalls int
	mutateStaffCalls  int
}

func (f *fakeBusinessStore) ListPlatformSalons(context.Context, string) ([]SalonSummary, error) {
	return []SalonSummary{}, nil
}

func (f *fakeBusinessStore) ListStaff(context.Context, string) ([]StaffMember, error) {
	return append([]StaffMember(nil), f.staff...), nil
}
func (f *fakeBusinessStore) ListServices(context.Context, string) ([]Service, error) {
	f.listServicesCalls++
	return []Service{}, nil
}
func (f *fakeBusinessStore) MutateStaff(context.Context, MutationCommand, StaffMutationRequest, bool) (*MutationResult, error) {
	f.mutateStaffCalls++
	return nil, nil
}
func (f *fakeBusinessStore) MutateService(_ context.Context, command MutationCommand, _ ServiceMutationRequest, _ bool) (*MutationResult, error) {
	f.lastCommand = command
	return &MutationResult{ResourceType: "service", ResourceID: "service-1", Version: 1}, nil
}
func (f *fakeBusinessStore) GetService(context.Context, string, string) (*Service, error) {
	return f.service, nil
}
