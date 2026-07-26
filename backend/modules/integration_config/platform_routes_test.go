package integrationconfig

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestPlatformIntegrationConfigRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/api/platform/tenants/salon-1/technical/integration-configs"},
		{http.MethodPut, "/api/platform/tenants/salon-1/technical/integration-configs/square"},
		{http.MethodPut, "/api/platform/tenants/salon-1/technical/integration-configs/twilio"},
		{http.MethodPut, "/api/platform/tenants/salon-1/technical/integration-configs/openai"},
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
