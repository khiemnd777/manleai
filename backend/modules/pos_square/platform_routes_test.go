package pos_square

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type failingReadinessSupportAudit struct{}

func (failingReadinessSupportAudit) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (failingReadinessSupportAudit) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return errors.New("audit unavailable")
}

func TestPlatformSquareRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/api/platform/tenants/salon-1/services/external-scheduling-readiness"},
		{http.MethodGet, "/api/platform/tenants/salon-1/technical/square/status"},
		{http.MethodPost, "/api/platform/tenants/salon-1/technical/square/sync"},
		{http.MethodPost, "/api/platform/tenants/salon-1/technical/square/ai-booking/enable"},
		{http.MethodPost, "/api/platform/tenants/salon-1/technical/square/ai-booking/disable"},
	} {
		response, err := app.Test(httptest.NewRequest(test.method, test.path, nil))
		if err != nil {
			t.Fatalf("%s %s: %v", test.method, test.path, err)
		}
		if response.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want 401", test.method, test.path, response.StatusCode)
		}
	}
}

func TestTenantSquareRoutesDoNotExposeTechnicalControls(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&Service{}, config.Config{}), "test-secret")
	for _, path := range []string{
		"/api/integrations/square/status",
		"/api/integrations/square/sync",
		"/api/integrations/square/enable-ai-booking",
		"/api/integrations/square/test-booking",
	} {
		response, err := app.Test(httptest.NewRequest(http.MethodPost, path, nil))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		if response.StatusCode != fiber.StatusNotFound {
			t.Fatalf("POST %s status=%d, want 404", path, response.StatusCode)
		}
	}
}

func TestPlatformBusinessReadinessFailsClosedBeforeServiceWhenAuditFails(t *testing.T) {
	app := fiber.New()
	handler := NewPlatformHandler(&Service{}, failingReadinessSupportAudit{})
	app.Get("/platform/tenants/:tenant_id/services/external-scheduling-readiness", handler.BusinessReadiness)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/services/external-scheduling-readiness", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", response.StatusCode)
	}
}
