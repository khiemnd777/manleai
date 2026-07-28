package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type fakeAccessPrincipalResolver struct {
	userID            string
	salonID           string
	scope             PrincipalScope
	roles             []string
	err               error
	calls             int
	membershipAllowed bool
	membershipErr     error
	membershipSalonID string
}

func (f *fakeAccessPrincipalResolver) HasActiveTenantSalonMembership(_ context.Context, _ string, salonID string) (bool, error) {
	f.membershipSalonID = salonID
	return f.membershipAllowed, f.membershipErr
}

func (f *fakeAccessPrincipalResolver) ResolveAccessPrincipal(context.Context, string) (string, string, PrincipalScope, []string, error) {
	f.calls++
	return f.userID, f.salonID, f.scope, f.roles, f.err
}

func TestRequireAuthUsesCurrentServerOwnedPrincipal(t *testing.T) {
	const secret = "current-principal-secret"
	resolver := &fakeAccessPrincipalResolver{
		userID:  "user-1",
		salonID: "current-salon",
		scope:   PrincipalScopeTenant,
		roles:   []string{"staff"},
	}
	app := fiber.New()
	api := app.Group("/api", WithAccessPrincipalResolver(resolver))
	api.Get("/protected", RequireAuth(secret), func(c *fiber.Ctx) error {
		actor := Actor(c)
		return c.JSON(fiber.Map{
			"user_id":         UserID(c),
			"salon_id":        SalonID(c),
			"roles":           Roles(c),
			"actor_user_id":   actor.UserID,
			"actor_salon_id":  actor.PrimarySalonID,
			"principal_scope": actor.PrincipalScope,
			"actor_has_staff": actor.HasRole("staff"),
			"actor_has_owner": actor.HasRole("salon_owner"),
		})
	})

	signed := signAccessToken(t, secret, Claims{
		UserID:  "user-1",
		SalonID: "stale-token-salon",
		Roles:   []string{"salon_owner", "super_admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	request := httptest.NewRequest("GET", "/api/protected", nil)
	request.Header.Set("Authorization", "Bearer "+signed)
	request.Header.Set("X-Salon-ID", "caller-selected-salon")
	request.Header.Set("X-Platform-Role", "platform_admin")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("protected request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var body struct {
		UserID         string   `json:"user_id"`
		SalonID        string   `json:"salon_id"`
		Roles          []string `json:"roles"`
		ActorUserID    string   `json:"actor_user_id"`
		ActorSalonID   string   `json:"actor_salon_id"`
		PrincipalScope string   `json:"principal_scope"`
		ActorHasStaff  bool     `json:"actor_has_staff"`
		ActorHasOwner  bool     `json:"actor_has_owner"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.UserID != "user-1" || body.SalonID != "current-salon" || len(body.Roles) != 1 || body.Roles[0] != "staff" {
		t.Fatalf("resolved principal=%#v, want current database-owned tenant and roles", body)
	}
	if body.ActorUserID != body.UserID || body.ActorSalonID != body.SalonID || body.PrincipalScope != string(PrincipalScopeTenant) || !body.ActorHasStaff || body.ActorHasOwner {
		t.Fatalf("actor context=%#v, want server-owned principal projection", body)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls=%d, want 1", resolver.calls)
	}
}

func TestRequireAuthRejectsSignedTokenWhenPrincipalIsInactiveOrUnavailable(t *testing.T) {
	const secret = "fail-closed-principal-secret"
	signed := signAccessToken(t, secret, Claims{
		UserID: "user-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	tests := []struct {
		name     string
		resolver AccessPrincipalResolver
	}{
		{name: "disabled or deleted user", resolver: &fakeAccessPrincipalResolver{err: errors.New("principal is inactive")}},
		{name: "repository failure", resolver: &fakeAccessPrincipalResolver{err: errors.New("database unavailable")}},
		{name: "missing resolver"},
		{name: "mismatched resolved identity", resolver: &fakeAccessPrincipalResolver{userID: "user-2", scope: PrincipalScopeTenant}},
		{name: "invalid principal scope", resolver: &fakeAccessPrincipalResolver{userID: "user-1", scope: PrincipalScope("mixed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			api := app.Group("/api")
			if test.resolver != nil {
				api.Use(WithAccessPrincipalResolver(test.resolver))
			}
			api.Get("/protected", RequireAuth(secret), func(c *fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})
			request := httptest.NewRequest("GET", "/api/protected", nil)
			request.Header.Set("Authorization", "Bearer "+signed)
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("protected request: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != fiber.StatusUnauthorized {
				t.Fatalf("status=%d, want %d", response.StatusCode, fiber.StatusUnauthorized)
			}
			var body respond.ErrorResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Code != "UNAUTHENTICATED" || body.Error.Message != "Invalid or expired access token." {
				t.Fatalf("error=%#v, want generic unauthenticated response", body)
			}
		})
	}
}

func TestRequireAuthRejectsMismatchedSubjectBeforePrincipalLookup(t *testing.T) {
	const secret = "subject-secret"
	resolver := &fakeAccessPrincipalResolver{userID: "user-1", scope: PrincipalScopeTenant}
	app := fiber.New()
	api := app.Group("/api", WithAccessPrincipalResolver(resolver))
	api.Get("/protected", RequireAuth(secret), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	signed := signAccessToken(t, secret, Claims{
		UserID: "user-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-2",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	request := httptest.NewRequest("GET", "/api/protected", nil)
	request.Header.Set("Authorization", "Bearer "+signed)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("protected request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.StatusCode, fiber.StatusUnauthorized)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls=%d, want 0 for conflicting signed identity", resolver.calls)
	}
}

func TestRequireTenantSalonAccessUsesExactRouteSalonAndCurrentMembership(t *testing.T) {
	const secret = "tenant-membership-secret"
	signed := signAccessToken(t, secret, Claims{UserID: "owner-1", RegisteredClaims: jwt.RegisteredClaims{Subject: "owner-1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
	for _, test := range []struct {
		name    string
		allowed bool
		status  int
	}{
		{name: "active membership", allowed: true, status: fiber.StatusOK},
		{name: "revoked membership", allowed: false, status: fiber.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeAccessPrincipalResolver{userID: "owner-1", salonID: "salon-primary", scope: PrincipalScopeTenant, membershipAllowed: test.allowed}
			app := fiber.New()
			api := app.Group("/api", WithAccessPrincipalResolver(resolver))
			group := api.Group("/salons", RequireAuth(secret), RequireTenantSalonAccess())
			group.Get("/:id/services", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
			request := httptest.NewRequest("GET", "/api/salons/salon-target/services", nil)
			request.Header.Set("Authorization", "Bearer "+signed)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, test.status)
			}
			if resolver.membershipSalonID != "salon-target" {
				t.Fatalf("membership salon=%q, want exact route target", resolver.membershipSalonID)
			}
		})
	}
}

func signAccessToken(t *testing.T, secret string, claims Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	return signed
}
