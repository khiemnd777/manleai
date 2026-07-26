package operationshealth

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestOperationsStatusRouteRequiresAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(NewService(fakeStatusStore{})), "test-secret")
	response, err := app.Test(httptest.NewRequest("GET", "/api/salons/00000000-0000-0000-0000-000000000001/operations/status", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", response.StatusCode)
	}
}
