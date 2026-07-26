package integrationconfig

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestIntegrationConfigRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&Service{}), "test-secret")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/salons/salon-1/integration-configs"},
		{method: http.MethodPut, path: "/api/salons/salon-1/integration-configs/square"},
		{method: http.MethodPut, path: "/api/salons/salon-1/integration-configs/twilio"},
		{method: http.MethodPut, path: "/api/salons/salon-1/integration-configs/openai"},
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
