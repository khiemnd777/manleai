package access

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/middleware"
)

type fakeStore struct {
	Store
	evaluate              func(context.Context, string, AccessCheck) (bool, error)
	listPlatformUsers     func(context.Context, string, string, int) ([]AccessUser, error)
	listTenantUsers       func(context.Context, string, string, string, int) ([]AccessUser, error)
	listCapabilities      func(context.Context, string) ([]CapabilityDefinition, error)
	createPlatformUser    func(context.Context, string, string, PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error)
	updatePlatformUser    func(context.Context, string, string, string, PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error)
	mutateSalonAssignment func(context.Context, string, string, string, string, SalonAssignmentMutationRequest) (*SalonAssignment, bool, error)
	grantPIIAccess        func(context.Context, string, string, string, PIIGrantRequest) (*PIIGrant, bool, error)
	createSupportAccess   func(context.Context, string, string, string, SupportAccessRequestCreate) (*SupportAccessRequest, bool, error)
	getEffectiveSupport   func(context.Context, string, string) (*EffectiveSupportAccess, error)
	recordSupportAction   func(context.Context, string, string, Capability, PIIScope, string, string) error
}

func (f *fakeStore) CreatePlatformUser(ctx context.Context, actorUserID, fingerprint string, req PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error) {
	return f.createPlatformUser(ctx, actorUserID, fingerprint, req)
}

func (f *fakeStore) UpdatePlatformUser(ctx context.Context, actorUserID, targetUserID, fingerprint string, req PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error) {
	return f.updatePlatformUser(ctx, actorUserID, targetUserID, fingerprint, req)
}

func (f *fakeStore) ListPlatformUsers(ctx context.Context, actorUserID, query string, limit int) ([]AccessUser, error) {
	return f.listPlatformUsers(ctx, actorUserID, query, limit)
}

func (f *fakeStore) ListTenantUsers(ctx context.Context, actorUserID, salonID, query string, limit int) ([]AccessUser, error) {
	return f.listTenantUsers(ctx, actorUserID, salonID, query, limit)
}

func (f *fakeStore) Evaluate(ctx context.Context, actorUserID string, check AccessCheck) (bool, error) {
	if f.evaluate == nil {
		return false, nil
	}
	return f.evaluate(ctx, actorUserID, check)
}

func (f *fakeStore) ListCapabilities(ctx context.Context, actorUserID string) ([]CapabilityDefinition, error) {
	return f.listCapabilities(ctx, actorUserID)
}

func (f *fakeStore) MutateSalonAssignment(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error) {
	return f.mutateSalonAssignment(ctx, actorUserID, salonID, targetUserID, fingerprint, req)
}

func (f *fakeStore) GrantPIIAccess(ctx context.Context, actorUserID, salonID, fingerprint string, req PIIGrantRequest) (*PIIGrant, bool, error) {
	return f.grantPIIAccess(ctx, actorUserID, salonID, fingerprint, req)
}

func (f *fakeStore) CreateSupportAccessRequest(ctx context.Context, actorUserID, salonID, fingerprint string, req SupportAccessRequestCreate) (*SupportAccessRequest, bool, error) {
	return f.createSupportAccess(ctx, actorUserID, salonID, fingerprint, req)
}

func (f *fakeStore) GetEffectiveSupportAccess(ctx context.Context, actorUserID, salonID string) (*EffectiveSupportAccess, error) {
	return f.getEffectiveSupport(ctx, actorUserID, salonID)
}

func (f *fakeStore) RecordPlatformSupportAction(ctx context.Context, actorUserID, salonID string, capability Capability, piiScope PIIScope, method, route string) error {
	if f.recordSupportAction == nil {
		return nil
	}
	return f.recordSupportAction(ctx, actorUserID, salonID, capability, piiScope, method, route)
}

func TestAuthorizeUsesServerRouteOwnedSurfaceAndTargetSalon(t *testing.T) {
	var capturedActor string
	var capturedCheck AccessCheck
	store := &fakeStore{evaluate: func(_ context.Context, actorUserID string, check AccessCheck) (bool, error) {
		capturedActor = actorUserID
		capturedCheck = check
		return true, nil
	}}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "platform-user", PrimarySalonID: "unrelated-primary", Roles: []string{RolePlatformAdmin}}
	check := AccessCheck{
		Surface:    SurfacePlatform,
		SalonID:    "target-salon",
		Capability: CapabilityBusinessRead,
		PIIScope:   PIIScopeCustomers,
	}
	if err := service.Authorize(context.Background(), actor, check); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if capturedActor != actor.UserID || capturedCheck != check {
		t.Fatalf("policy input actor=%q check=%#v, want exact server actor and route-owned check %#v", capturedActor, capturedCheck, check)
	}
}

func TestTemporaryOpsCallsAuthorizationAlwaysRequiresLinkedCallsPII(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{
		evaluate: func(context.Context, string, AccessCheck) (bool, error) { return true, nil },
		createSupportAccess: func(_ context.Context, actorUserID, salonID, _ string, req SupportAccessRequestCreate) (*SupportAccessRequest, bool, error) {
			return &SupportAccessRequest{SalonID: salonID, PlatformUserID: req.UserID, Capabilities: req.Capabilities, PIIScopes: req.PIIScopes}, false, nil
		},
	}
	service := NewService(store)
	service.now = func() time.Time { return now }
	actor := middleware.ActorContext{UserID: "platform-admin", PrincipalScope: middleware.PrincipalScopePlatform}
	request := SupportAccessRequestCreate{
		ActionKey: "support-calls-1", UserID: "platform-user", Reason: "SUPPORT-2048",
		Capabilities: []string{string(CapabilityCallsRead)}, ExpiresAt: now.Add(time.Hour),
	}
	if _, _, err := service.CreateSupportAccessRequest(context.Background(), actor, "salon-1", request); !errors.Is(err, ErrValidation) {
		t.Fatalf("Calls request without Calls PII err=%v, want validation failure", err)
	}
	request.PIIScopes = []string{string(PIIScopeCalls)}
	created, _, err := service.CreateSupportAccessRequest(context.Background(), actor, "salon-1", request)
	if err != nil {
		t.Fatalf("Calls request with Calls PII: %v", err)
	}
	if !reflect.DeepEqual(created.PIIScopes, []string{"calls"}) {
		t.Fatalf("PII scopes=%#v, want exact Calls scope", created.PIIScopes)
	}
	request.ActionKey = "support-calls-2"
	request.ExpiresAt = now.Add(25 * time.Hour)
	if _, _, err := service.CreateSupportAccessRequest(context.Background(), actor, "salon-1", request); !errors.Is(err, ErrValidation) {
		t.Fatalf("25-hour Calls request err=%v, want validation failure", err)
	}
}

func TestEffectiveSupportAccessIsSelfScopedToPlatformPrincipal(t *testing.T) {
	want := &EffectiveSupportAccess{Capabilities: []string{"calls.read"}, PIIScopes: []string{"calls"}}
	store := &fakeStore{getEffectiveSupport: func(_ context.Context, actorUserID, salonID string) (*EffectiveSupportAccess, error) {
		if actorUserID != "platform-user" || salonID != "salon-1" {
			t.Fatalf("scope actor=%q salon=%q", actorUserID, salonID)
		}
		return want, nil
	}}
	service := NewService(store)
	got, err := service.GetEffectiveSupportAccess(context.Background(), middleware.ActorContext{
		UserID: "platform-user", PrincipalScope: middleware.PrincipalScopePlatform,
	}, "salon-1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("effective access=%#v err=%v", got, err)
	}
	if _, err := service.GetEffectiveSupportAccess(context.Background(), middleware.ActorContext{
		UserID: "tenant-user", PrincipalScope: middleware.PrincipalScopeTenant,
	}, "salon-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tenant effective access err=%v, want forbidden", err)
	}
}

func TestPlatformSupportActionAuditPreservesExactAuthorizedRouteContext(t *testing.T) {
	var captured struct {
		actor, salon, method, route string
		capability                  Capability
		pii                         PIIScope
	}
	store := &fakeStore{recordSupportAction: func(_ context.Context, actorUserID, salonID string, capability Capability, piiScope PIIScope, method, route string) error {
		captured.actor, captured.salon = actorUserID, salonID
		captured.capability, captured.pii = capability, piiScope
		captured.method, captured.route = method, route
		return nil
	}}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "platform-ops", PrincipalScope: middleware.PrincipalScopePlatform}
	if err := service.RecordPlatformSupportAction(context.Background(), actor, " salon-1 ", CapabilityCallsRead, PIIScopeCalls, " GET ", " /api/platform/tenants/:id/conversation-sessions "); err != nil {
		t.Fatalf("record support action: %v", err)
	}
	if captured.actor != "platform-ops" || captured.salon != "salon-1" || captured.capability != CapabilityCallsRead || captured.pii != PIIScopeCalls || captured.method != "GET" || captured.route != "/api/platform/tenants/:id/conversation-sessions" {
		t.Fatalf("captured audit context = %#v", captured)
	}
	if err := service.RecordPlatformSupportAction(context.Background(), middleware.ActorContext{UserID: "tenant-owner", PrincipalScope: middleware.PrincipalScopeTenant}, "salon-1", CapabilityCallsRead, PIIScopeCalls, "GET", "/calls"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tenant audit write err=%v, want forbidden", err)
	}
}

func TestAccountDirectoriesHaveFixedPrincipalScopes(t *testing.T) {
	store := &fakeStore{}
	store.evaluate = func(_ context.Context, _ string, check AccessCheck) (bool, error) {
		return check.Surface == SurfacePlatform && check.Capability == CapabilityPlatformAccess, nil
	}
	store.listPlatformUsers = func(_ context.Context, actorID, query string, limit int) ([]AccessUser, error) {
		if actorID != "admin" || query != "ops" || limit != 25 {
			t.Fatalf("platform directory args actor=%q query=%q limit=%d", actorID, query, limit)
		}
		return []AccessUser{{ID: "platform-1", PrincipalScope: PrincipalScopePlatform}}, nil
	}
	store.listTenantUsers = func(_ context.Context, actorID, salonID, query string, limit int) ([]AccessUser, error) {
		if actorID != "admin" || salonID != "salon-1" || query != "manager" || limit != 25 {
			t.Fatalf("tenant directory args actor=%q salon=%q query=%q limit=%d", actorID, salonID, query, limit)
		}
		return []AccessUser{{ID: "tenant-1", PrincipalScope: PrincipalScopeTenant}}, nil
	}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "admin"}

	platform, err := service.ListPlatformUsers(context.Background(), actor, " ops ", 25)
	if err != nil || len(platform.Users) != 1 || platform.Users[0].PrincipalScope != PrincipalScopePlatform {
		t.Fatalf("platform directory=%#v err=%v", platform, err)
	}
	tenant, err := service.ListTenantUsers(context.Background(), actor, " salon-1 ", " manager ", 25)
	if err != nil || len(tenant.Users) != 1 || tenant.Users[0].PrincipalScope != PrincipalScopeTenant {
		t.Fatalf("tenant directory=%#v err=%v", tenant, err)
	}
}

func TestCreateAndUpdatePlatformUserHashDirectPasswords(t *testing.T) {
	store := &fakeStore{}
	store.evaluate = func(_ context.Context, _ string, check AccessCheck) (bool, error) {
		return check.Surface == SurfacePlatform && check.Capability == CapabilityPlatformAccess, nil
	}
	store.createPlatformUser = func(_ context.Context, actorID, fingerprint string, req PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error) {
		if actorID != "admin" || len(fingerprint) != 64 || req.Email != "ops@example.com" || req.PasswordHash == "" || req.PasswordHash == req.Password {
			t.Fatalf("unexpected create payload actor=%q fingerprint_length=%d", actorID, len(fingerprint))
		}
		return &PlatformRoleAssignment{UserID: "ops-1", Role: RolePlatformOps, Status: "active", Version: 1}, false, nil
	}
	store.updatePlatformUser = func(_ context.Context, actorID, targetID, fingerprint string, req PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error) {
		if actorID != "admin" || targetID != "ops-1" || len(fingerprint) != 64 || req.PasswordHash == "" || req.PasswordHash == req.Password || req.ExpectedVersion != 1 {
			t.Fatalf("unexpected update payload actor=%q target=%q fingerprint_length=%d", actorID, targetID, len(fingerprint))
		}
		return &PlatformRoleAssignment{UserID: targetID, Role: req.Role, Status: req.Status, Version: 2}, false, nil
	}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "admin", PrincipalScope: middleware.PrincipalScopePlatform}
	created, _, err := service.CreatePlatformUser(context.Background(), actor, PlatformUserCreateRequest{ActionKey: "create-ops", Email: " OPS@Example.com ", FullName: " Ops User ", Password: "temporary-pass", Role: RolePlatformOps, Status: "active"})
	if err != nil || created.UserID != "ops-1" {
		t.Fatalf("create=%#v err=%v", created, err)
	}
	updated, _, err := service.UpdatePlatformUser(context.Background(), actor, "ops-1", PlatformUserUpdateRequest{ActionKey: "update-ops", Email: "ops@example.com", FullName: "Ops User", Password: "replacement-pass", Role: RolePlatformOps, Status: "active", ExpectedVersion: 1})
	if err != nil || updated.Version != 2 {
		t.Fatalf("update=%#v err=%v", updated, err)
	}
}

func TestAuthorizeFailsClosedForInvalidOrDeniedChecks(t *testing.T) {
	calls := 0
	store := &fakeStore{evaluate: func(context.Context, string, AccessCheck) (bool, error) {
		calls++
		return false, nil
	}}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "actor"}
	if err := service.Authorize(context.Background(), actor, AccessCheck{Surface: Surface("caller-supplied"), Capability: CapabilityBusinessRead}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("invalid surface error=%v, want forbidden", err)
	}
	if calls != 0 {
		t.Fatalf("repository calls=%d, want 0 for invalid access contract", calls)
	}
	if err := service.Authorize(context.Background(), actor, AccessCheck{Surface: SurfacePlatform, SalonID: "salon", Capability: CapabilityBusinessRead}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("denied access error=%v, want forbidden", err)
	}
	if calls != 1 {
		t.Fatalf("repository calls=%d, want 1 for valid denied check", calls)
	}
}

func TestPlatformAppointmentPIIScopeSupportsBusinessAndOperationsReadsOnly(t *testing.T) {
	for _, capability := range []Capability{CapabilityBusinessRead, CapabilityOperationsRead} {
		if !validAccessCheck(AccessCheck{Surface: SurfacePlatform, SalonID: "salon", Capability: capability, PIIScope: PIIScopeAppointments}) {
			t.Fatalf("appointment PII check rejected %q", capability)
		}
	}
	for _, capability := range []Capability{CapabilityBusinessWrite, CapabilityOperationsWrite, CapabilityTechnicalRead} {
		if validAccessCheck(AccessCheck{Surface: SurfacePlatform, SalonID: "salon", Capability: capability, PIIScope: PIIScopeAppointments}) {
			t.Fatalf("appointment PII check accepted %q", capability)
		}
	}
}

func TestMutateSalonAssignmentNormalizesPermissionsAndRequiresReadForWrite(t *testing.T) {
	store := &fakeStore{}
	store.evaluate = func(_ context.Context, _ string, check AccessCheck) (bool, error) {
		return check.Surface == SurfacePlatform && check.Capability == CapabilityPlatformAccess, nil
	}
	var captured SalonAssignmentMutationRequest
	store.mutateSalonAssignment = func(_ context.Context, _, _, _, fingerprint string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error) {
		if len(fingerprint) != 64 {
			t.Fatalf("fingerprint length=%d, want 64", len(fingerprint))
		}
		captured = req
		return &SalonAssignment{ID: "assignment"}, false, nil
	}
	service := NewService(store)
	actor := middleware.ActorContext{UserID: "admin"}
	_, _, err := service.MutateSalonAssignment(context.Background(), actor, "salon", "ops", SalonAssignmentMutationRequest{
		ActionKey:       "assign-1",
		Status:          "active",
		ExpectedVersion: 0,
		Permissions: []string{
			string(CapabilityTechnicalWrite),
			string(CapabilityTechnicalRead),
			string(CapabilityTechnicalRead),
			string(CapabilityBusinessRead),
		},
	})
	if err != nil {
		t.Fatalf("mutate assignment: %v", err)
	}
	want := []string{string(CapabilityBusinessRead), string(CapabilityTechnicalRead), string(CapabilityTechnicalWrite)}
	if !reflect.DeepEqual(captured.Permissions, want) {
		t.Fatalf("permissions=%v, want normalized %v", captured.Permissions, want)
	}

	_, _, err = service.MutateSalonAssignment(context.Background(), actor, "salon", "ops", SalonAssignmentMutationRequest{
		ActionKey:   "assign-2",
		Status:      "active",
		Permissions: []string{string(CapabilityOperationsWrite)},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("write-without-read error=%v, want validation", err)
	}
}

func TestListCapabilitiesPublishesAssignmentDependencies(t *testing.T) {
	store := &fakeStore{
		evaluate: func(_ context.Context, _ string, check AccessCheck) (bool, error) {
			return check.Surface == SurfacePlatform && check.Capability == CapabilityPlatformAccess, nil
		},
		listCapabilities: func(context.Context, string) ([]CapabilityDefinition, error) {
			return []CapabilityDefinition{
				{Name: string(CapabilityBusinessRead), DisplayName: "Read tenant business data", DelegationScope: "salon"},
				{Name: string(CapabilityBusinessWrite), DisplayName: "Manage tenant business data", DelegationScope: "salon"},
				{Name: string(CapabilityAuditRead), DisplayName: "Read tenant audit", DelegationScope: "salon"},
			}, nil
		},
	}
	result, err := NewService(store).ListCapabilities(context.Background(), middleware.ActorContext{UserID: "admin"})
	if err != nil {
		t.Fatalf("list capabilities: %v", err)
	}
	if got := result.Capabilities[1].Requires; !reflect.DeepEqual(got, []string{string(CapabilityBusinessRead)}) {
		t.Fatalf("business.write requires=%v, want business.read", got)
	}
	if got := result.Capabilities[0].Requires; len(got) != 0 {
		t.Fatalf("business.read requires=%v, want none", got)
	}
}

func TestGrantPIIAccessEnforcesBoundedExpiryBeforeRepositoryWrite(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	calls := 0
	store := &fakeStore{
		evaluate: func(context.Context, string, AccessCheck) (bool, error) { return true, nil },
		grantPIIAccess: func(context.Context, string, string, string, PIIGrantRequest) (*PIIGrant, bool, error) {
			calls++
			return &PIIGrant{}, false, nil
		},
	}
	service := NewService(store)
	service.now = func() time.Time { return now }
	actor := middleware.ActorContext{UserID: "admin"}
	base := PIIGrantRequest{ActionKey: "pii-1", UserID: "ops", Scope: string(PIIScopeCustomers), Reason: "support-ticket-123"}
	base.ExpiresAt = now.Add(24*time.Hour + time.Second)
	if _, _, err := service.GrantPIIAccess(context.Background(), actor, "salon", base); !errors.Is(err, ErrValidation) {
		t.Fatalf("overlong grant error=%v, want validation", err)
	}
	if calls != 0 {
		t.Fatalf("repository calls=%d, want 0 for invalid grant", calls)
	}
	base.ExpiresAt = now.Add(time.Hour)
	base.Reason = "Customer Jane requested support"
	if _, _, err := service.GrantPIIAccess(context.Background(), actor, "salon", base); !errors.Is(err, ErrValidation) {
		t.Fatalf("free-text change reference error=%v, want validation", err)
	}
	if calls != 0 {
		t.Fatalf("repository calls=%d, want 0 for unsafe change reference", calls)
	}
	base.Reason = "support-ticket-123"
	if _, _, err := service.GrantPIIAccess(context.Background(), actor, "salon", base); err != nil {
		t.Fatalf("valid grant: %v", err)
	}
	if calls != 1 {
		t.Fatalf("repository calls=%d, want 1", calls)
	}
}
