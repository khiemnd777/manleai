package scheduling_behavior

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type behaviorAuthorizer struct {
	read   bool
	write  bool
	checks []access.Capability
}

func (authorizer *behaviorAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	authorizer.checks = append(authorizer.checks, check.Capability)
	if check.Capability == access.CapabilityTechnicalRead && authorizer.read {
		return nil
	}
	if check.Capability == access.CapabilityTechnicalWrite && authorizer.write {
		return nil
	}
	return access.ErrForbidden
}

func TestPlatformGetUsesTechnicalReadAndReturnsWritePermission(t *testing.T) {
	store := &fakeStore{state: PersistedState{
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityVersion:    2, BookingMode: scheduling.BookingModePendingApproval, PolicyVersion: 5,
	}}
	authorizer := &behaviorAuthorizer{read: true, write: true}
	app := fiber.New()
	app.Get("/tenants/:tenant_id/scheduling/behavior", NewPlatformHandler(NewService(store), authorizer).Get)
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/tenants/salon-1/scheduling/behavior", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var payload struct {
		Meta struct {
			Permissions struct {
				AllowedActions []string `json:"allowed_actions"`
			} `json:"permissions"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Meta.Permissions.AllowedActions) != 2 || payload.Meta.Permissions.AllowedActions[0] != "set_authority" || payload.Meta.Permissions.AllowedActions[1] != "set_booking_mode" {
		t.Fatalf("actions=%v", payload.Meta.Permissions.AllowedActions)
	}
	if len(authorizer.checks) != 2 || authorizer.checks[0] != access.CapabilityTechnicalRead || authorizer.checks[1] != access.CapabilityTechnicalWrite {
		t.Fatalf("checks=%v", authorizer.checks)
	}
}

func TestPlatformUpdateRequiresTechnicalWrite(t *testing.T) {
	store := &fakeStore{}
	authorizer := &behaviorAuthorizer{read: true, write: false}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, "admin-1")
		return c.Next()
	})
	app.Put("/tenants/:tenant_id/scheduling/booking-mode", NewPlatformHandler(NewService(store), authorizer).UpdateBookingMode)
	request := httptest.NewRequest(http.MethodPut, "/tenants/salon-1/scheduling/booking-mode", bytes.NewBufferString(`{"booking_mode":"pending_approval","expected_version":2,"action_key":"save-mode"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status=%d, want %d", response.StatusCode, fiber.StatusForbidden)
	}
	if store.command.ActionKey != "" {
		t.Fatalf("unauthorized command reached store: %#v", store.command)
	}
	if len(authorizer.checks) != 1 || authorizer.checks[0] != access.CapabilityTechnicalWrite {
		t.Fatalf("checks=%v", authorizer.checks)
	}
}

func TestSchedulingBehaviorErrorCodesRemainSanitized(t *testing.T) {
	app := fiber.New()
	app.Get("/error", func(c *fiber.Ctx) error {
		_, response := schedulingBehaviorError(c, errors.New("secret database detail"))
		return response
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/error", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	encoded, _ := json.Marshal(payload)
	if bytes.Contains(encoded, []byte("secret database detail")) {
		t.Fatalf("raw error leaked: %s", encoded)
	}
}
