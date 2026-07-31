package tenant_provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	"golang.org/x/crypto/bcrypt"
)

type provisioningStoreStub struct {
	tokenHash, rawToken, passwordHash string
	searches                          int
	provisionRequest                  ProvisionRequest
	provisionFingerprint              string
	acceptError                       error
}

func (s *provisioningStoreStub) Provision(_ context.Context, _, _, fingerprint string, req ProvisionRequest) (*ProvisionResult, error) {
	s.provisionRequest = req
	s.provisionFingerprint = fingerprint
	return &ProvisionResult{SchedulingAuthority: "owner_manual"}, nil
}
func (s *provisioningStoreStub) CreateInvitation(_ context.Context, _, _, _, hash, raw string, _ InvitationRequest) (*InvitationResult, error) {
	s.tokenHash = hash
	s.rawToken = raw
	return &InvitationResult{RawToken: raw, TokenAvailable: true}, nil
}
func (s *provisioningStoreStub) AcceptInvitation(_ context.Context, _, hash string) error {
	s.passwordHash = hash
	return s.acceptError
}
func (s *provisioningStoreStub) SearchTenantIdentities(context.Context, string, int) ([]TenantIdentity, error) {
	s.searches++
	return []TenantIdentity{{ID: "c96dd442-ac11-4d5b-9837-fdbd2bbcfc73", Email: "owner@example.com", FullName: "Owner", Status: "active"}}, nil
}

type provisioningAuthorizer struct{ allow bool }

func (a provisioningAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	if !a.allow {
		return errors.New("denied")
	}
	if check.Capability != access.CapabilityTenantProvision {
		return errors.New("wrong capability")
	}
	return nil
}
func platformActor() middleware.ActorContext {
	return middleware.ActorContext{UserID: "cc7b2f09-08bc-4561-a8f7-0f6ea99b2467", PrincipalScope: middleware.PrincipalScopePlatform}
}

func validProvisionRequest() ProvisionRequest {
	return ProvisionRequest{
		ActionKey: "provision-1", ExpectedVersion: 2,
		Owner: OwnerIdentityInput{Mode: OwnerModeCreateInvited, Email: " OWNER@EXAMPLE.COM ", FullName: "  Owner Name  ", Phone: "312-555-0148"},
		Salon: SalonProfileInput{Name: "  Prepared Nails  ", Phone: "773-555-0180", City: "Chicago", State: " il ", ZipCode: "60614", Timezone: "America/Chicago", PrimaryLanguage: "vi", SecondaryLanguage: "en", HandoffPhone: "773-555-0180"},
	}
}

func TestProvisionIsAdminCapabilityBoundAndNormalizesReviewedFields(t *testing.T) {
	store := &provisioningStoreStub{}
	service := NewService(store, provisioningAuthorizer{allow: true})
	result, err := service.Provision(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", validProvisionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.SchedulingAuthority != "owner_manual" || len(store.provisionFingerprint) != 64 {
		t.Fatalf("provision result=%#v fingerprint=%q", result, store.provisionFingerprint)
	}
	if store.provisionRequest.Owner.Email != "owner@example.com" || store.provisionRequest.Owner.FullName != "Owner Name" || store.provisionRequest.Salon.Name != "Prepared Nails" || store.provisionRequest.Salon.State != "IL" {
		t.Fatalf("provision request not normalized: %#v", store.provisionRequest)
	}
	tenantActor := platformActor()
	tenantActor.PrincipalScope = middleware.PrincipalScopeTenant
	if _, err := service.Provision(context.Background(), tenantActor, "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", validProvisionRequest()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tenant actor error=%v", err)
	}
	if _, err := NewService(store, provisioningAuthorizer{allow: false}).Provision(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", validProvisionRequest()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin platform actor error=%v", err)
	}
}

func TestProvisionRequiresExplicitMatchingExistingTenantIdentity(t *testing.T) {
	service := NewService(&provisioningStoreStub{}, provisioningAuthorizer{allow: true})
	request := validProvisionRequest()
	request.Owner.Mode = OwnerModeUseExisting
	request.Owner.UserID = ""
	if _, err := service.Provision(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", request); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing existing identity error=%v", err)
	}
	request.Owner.UserID = "c96dd442-ac11-4d5b-9837-fdbd2bbcfc73"
	if _, err := service.Provision(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", request); err != nil {
		t.Fatal(err)
	}
	request.Owner.Mode = "platform_identity"
	if _, err := service.Provision(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", request); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown owner mode error=%v", err)
	}
}

func TestInvitationReturnsRawOnceButPassesOnlyHashForPersistence(t *testing.T) {
	store := &provisioningStoreStub{}
	service := NewService(store, provisioningAuthorizer{allow: true})
	result, err := service.CreateInvitation(context.Background(), platformActor(), "891bb002-cfd8-48ed-adfb-a2ebbf4274ad", InvitationRequest{ActionKey: "invite-1", ExpectedVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TokenAvailable || result.RawToken == "" || store.rawToken != result.RawToken {
		t.Fatalf("missing one-time token: %#v", result)
	}
	if len(store.tokenHash) != 64 || store.tokenHash == store.rawToken || strings.Contains(store.tokenHash, store.rawToken) {
		t.Fatal("repository did not receive a one-way token hash")
	}
}

func TestInvitationAcceptanceHashesPasswordAndEnforcesLength(t *testing.T) {
	store := &provisioningStoreStub{}
	service := NewService(store, provisioningAuthorizer{})
	if _, err := service.AcceptInvitation(context.Background(), AcceptInvitationRequest{Token: "raw-token", Password: "short"}); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("short password accepted: %v", err)
	}
	password := "a sufficiently long passphrase"
	if _, err := service.AcceptInvitation(context.Background(), AcceptInvitationRequest{Token: "raw-token", Password: password}); err != nil {
		t.Fatal(err)
	}
	if store.passwordHash == password || bcrypt.CompareHashAndPassword([]byte(store.passwordHash), []byte(password)) != nil {
		t.Fatal("password was not bcrypt hashed")
	}
}

func TestInvitationAcceptancePreservesDependencyFailuresAndSanitizesInvalidTokens(t *testing.T) {
	dependencyFailure := errors.New("database unavailable")
	store := &provisioningStoreStub{acceptError: dependencyFailure}
	service := NewService(store, provisioningAuthorizer{})
	if _, err := service.AcceptInvitation(context.Background(), AcceptInvitationRequest{Token: "raw-token", Password: "a sufficiently long passphrase"}); !errors.Is(err, dependencyFailure) {
		t.Fatalf("dependency failure was collapsed: %v", err)
	}
	store.acceptError = ErrInvitationInvalid
	if _, err := service.AcceptInvitation(context.Background(), AcceptInvitationRequest{Token: "raw-token", Password: "a sufficiently long passphrase"}); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("invalid invitation error=%v", err)
	}
}

func TestTenantIdentitySearchIsAdminAuthorizedAndBounded(t *testing.T) {
	store := &provisioningStoreStub{}
	service := NewService(store, provisioningAuthorizer{allow: true})
	result, err := service.SearchTenantIdentities(context.Background(), platformActor(), " owner@example ")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Users) != 1 || store.searches != 1 {
		t.Fatalf("unexpected search %#v", result)
	}
	if _, err := service.SearchTenantIdentities(context.Background(), platformActor(), "x"); !errors.Is(err, ErrValidation) {
		t.Fatalf("short query should fail: %v", err)
	}
	tenant := platformActor()
	tenant.PrincipalScope = middleware.PrincipalScopeTenant
	if _, err := service.SearchTenantIdentities(context.Background(), tenant, "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tenant identity should fail: %v", err)
	}
}
