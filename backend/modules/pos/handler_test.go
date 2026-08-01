package pos

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func TestUpdateServiceMapsProviderManagedFieldsToConflict(t *testing.T) {
	store := &fakePOSStore{currentService: &Service{
		ID: "service_1", POSProvider: ProviderSquare, POSServiceID: "sq_service_1", Source: EntitySourceImported,
	}}
	handler := NewHandler(NewService(store, fakeCapabilityProvider{name: ProviderSquare}))
	app := fiber.New()
	app.Put("/salons/:id/services/:service_id", withOwner("owner_1"), handler.UpdateService)

	req := httptest.NewRequest("PUT", "/salons/salon_1/services/service_1", bytes.NewBufferString(`{"name":"Changed","duration_minutes":45,"active":true}`))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	assertErrorCode(t, response, "PROVIDER_MANAGED_FIELDS")
}

func TestUpdateStaffMapsProviderManagedFieldsToConflict(t *testing.T) {
	store := &fakePOSStore{currentStaff: &StaffMember{
		ID: "staff_1", POSProvider: ProviderSquare, POSStaffID: "sq_staff_1", Source: EntitySourceImported,
	}}
	handler := NewHandler(NewService(store, fakeCapabilityProvider{name: ProviderSquare}))
	app := fiber.New()
	app.Put("/salons/:id/staff/:staff_id", withOwner("owner_1"), handler.UpdateStaff)

	req := httptest.NewRequest("PUT", "/salons/salon_1/staff/staff_1", bytes.NewBufferString(`{"name":"Changed","active":true}`))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	assertErrorCode(t, response, "PROVIDER_MANAGED_FIELDS")
}

func TestServicesMapsUnconfiguredActiveProviderToConflict(t *testing.T) {
	store := &fakePOSStore{returnBlankActiveProvider: true}
	handler := NewHandler(NewService(store, fakeCapabilityProvider{name: ProviderSquare}))
	app := fiber.New()
	app.Get("/salons/:id/services", withOwner("owner_1"), handler.Services)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/salons/salon_1/services", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	assertErrorCode(t, response, "POS_ACTIVE_PROVIDER_NOT_CONFIGURED")
}

func withOwner(ownerUserID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, ownerUserID)
		return c.Next()
	}
}

func assertErrorCode(t *testing.T, response *http.Response, want string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != want {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, want)
	}
}
