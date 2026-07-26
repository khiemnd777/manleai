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
	mutateSalonAssignment func(context.Context, string, string, string, string, SalonAssignmentMutationRequest) (*SalonAssignment, bool, error)
	grantPIIAccess        func(context.Context, string, string, string, PIIGrantRequest) (*PIIGrant, bool, error)
}

func (f *fakeStore) Evaluate(ctx context.Context, actorUserID string, check AccessCheck) (bool, error) {
	if f.evaluate == nil {
		return false, nil
	}
	return f.evaluate(ctx, actorUserID, check)
}

func (f *fakeStore) MutateSalonAssignment(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error) {
	return f.mutateSalonAssignment(ctx, actorUserID, salonID, targetUserID, fingerprint, req)
}

func (f *fakeStore) GrantPIIAccess(ctx context.Context, actorUserID, salonID, fingerprint string, req PIIGrantRequest) (*PIIGrant, bool, error) {
	return f.grantPIIAccess(ctx, actorUserID, salonID, fingerprint, req)
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
