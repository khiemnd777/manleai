package voice

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestVoiceOperationalRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&Service{}), "test-secret")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/salons/salon-1/voice/status"},
		{method: http.MethodPost, path: "/api/salons/salon-1/voice/semantic-check"},
		{method: http.MethodPost, path: "/api/salons/salon-1/voice/semantic-evaluate"},
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
