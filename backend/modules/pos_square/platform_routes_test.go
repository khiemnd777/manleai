package pos_square

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
)

func TestPlatformSquareRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, test := range []struct{ method, path string }{
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
