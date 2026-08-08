package openairuntimeverification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type capturePlatformAuthorizer struct {
	checks []access.AccessCheck
	err    error
}

func (a *capturePlatformAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	check.SalonID = strings.Clone(check.SalonID)
	a.checks = append(a.checks, check)
	return a.err
}

func TestPlatformHandlerAuthorizesExactTenantBeforeVerificationService(t *testing.T) {
	authorizer := &capturePlatformAuthorizer{err: access.ErrForbidden}
	handler := NewPlatformHandler(nil, authorizer)
	app := fiber.New()
	app.Post("/:tenant_id", handler.Verify)
	app.Get("/:tenant_id", handler.Status)

	request := httptest.NewRequest(http.MethodPost, "/salon-a", strings.NewReader(`{"action_key":"verify-a","expected_config_version":4}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("write status=%d", response.StatusCode)
	}
	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/salon-b", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("read status=%d", response.StatusCode)
	}
	if len(authorizer.checks) != 2 {
		t.Fatalf("authorization checks=%#v", authorizer.checks)
	}
	if authorizer.checks[0].Surface != access.SurfacePlatform || authorizer.checks[0].SalonID != "salon-a" || authorizer.checks[0].Capability != access.CapabilityTechnicalWrite {
		t.Fatalf("write authorization=%#v", authorizer.checks[0])
	}
	if authorizer.checks[1].Surface != access.SurfacePlatform || authorizer.checks[1].SalonID != "salon-b" || authorizer.checks[1].Capability != access.CapabilityTechnicalRead {
		t.Fatalf("read authorization=%#v", authorizer.checks[1])
	}
}

func TestPlatformVerificationV2RoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response, err := app.Test(httptest.NewRequest(method, "/api/v2/platform/tenants/salon-a/integrations/openai/verifications", nil))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status=%d, want 401", method, response.StatusCode)
		}
	}
}
