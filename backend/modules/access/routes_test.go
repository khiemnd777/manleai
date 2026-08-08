package access

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
)

func TestPlatformAccessRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(NewService(&fakeStore{})), "test-secret")
	for _, path := range []string{
		"/api/platform/access/platform-users",
		"/api/platform/access/roles",
		"/api/platform/access/capabilities",
		"/api/platform/access/salons/salon-1/tenant-users",
		"/api/platform/access/salons/salon-1/memberships",
		"/api/platform/access/salons/salon-1/assignments",
		"/api/platform/access/salons/salon-1/pii-grants",
		"/api/platform/access/audit",
		"/api/platform/tenants/salon-1/support-access/effective",
		"/api/v2/platform/tenants/salon-1/access",
		"/api/v2/platform/tenants/salon-1/access/effective",
	} {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var body respond.ErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			response.Body.Close()
			t.Fatalf("decode %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != fiber.StatusUnauthorized || body.Error.Code != "UNAUTHENTICATED" {
			t.Fatalf("GET %s status=%d error=%#v, want generic 401", path, response.StatusCode, body.Error)
		}
	}
}
