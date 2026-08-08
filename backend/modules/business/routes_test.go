package business

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type testPlatformSupportAuthorizer struct {
	auditErr error
}

func (testPlatformSupportAuthorizer) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (authorizer testPlatformSupportAuthorizer) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return authorizer.auditErr
}

func TestTenantAndPlatformBusinessRoutesAreFixedAndSymmetric(t *testing.T) {
	app := fiber.New()
	api := app.Group("/api")
	RegisterTenantRoutes(api, NewHandler(&ServiceLayer{}, access.SurfaceTenant), "secret")
	RegisterPlatformRoutes(api, NewHandler(&ServiceLayer{}, access.SurfacePlatform), testPlatformSupportAuthorizer{}, "secret")
	routes := map[string]bool{}
	for _, route := range app.GetRoutes() {
		routes[route.Method+" "+route.Path] = true
	}
	pairs := [][2]string{
		{"GET /api/salons/:id/business/profile", "GET /api/platform/tenants/:tenant_id/business/profile"},
		{"GET /api/salons/:id/business/services", "GET /api/platform/tenants/:tenant_id/business/services"},
		{"PUT /api/salons/:id/business/staff/:staff_id/services", "PUT /api/platform/tenants/:tenant_id/business/staff/:staff_id/services"},
		{"PUT /api/salons/:id/business/business-hours", "PUT /api/platform/tenants/:tenant_id/business/business-hours"},
		{"GET /api/salons/:id/business/customers", "GET /api/platform/tenants/:tenant_id/business/customers"},
	}
	for _, pair := range pairs {
		for _, key := range pair {
			if !routes[key] {
				t.Errorf("missing route %s", key)
			}
		}
	}
	if !routes["GET /api/v2/platform/tenants/:tenant_id/context"] {
		t.Error("missing normalized Platform tenant context route")
	}
	if !routes["GET /api/v2/platform/tenants"] {
		t.Error("missing normalized Platform tenant directory route")
	}
	for _, route := range []string{
		"GET /api/v2/platform/tenants/:tenant_id/business/profile",
		"GET /api/v2/platform/tenants/:tenant_id/business/hours",
		"GET /api/v2/platform/tenants/:tenant_id/business/public-page",
		"GET /api/v2/platform/tenants/:tenant_id/staff",
		"GET /api/v2/platform/tenants/:tenant_id/customers",
	} {
		if !routes[route] {
			t.Errorf("missing normalized route %s", route)
		}
	}
}

func TestPlatformBusinessServicesGuardFailsClosedWhenSupportAuditFails(t *testing.T) {
	app := fiber.New()
	reached := false
	app.Get("/platform/tenants/:tenant_id/business/services", requirePlatformBusinessServices(testPlatformSupportAuthorizer{auditErr: errors.New("audit unavailable")}, access.CapabilityServicesRead), func(c *fiber.Ctx) error {
		reached = true
		return c.SendStatus(fiber.StatusOK)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/business/services", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusInternalServerError || reached {
		t.Fatalf("status/reached = %d/%v, want 500/false", response.StatusCode, reached)
	}
}
