package conversation

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

type failingSupportAudit struct{}

func (failingSupportAudit) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (failingSupportAudit) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return errors.New("audit unavailable")
}

func TestPlatformCallsGuardFailsClosedBeforeDomainHandlerWhenAuditFails(t *testing.T) {
	handler := &PlatformHandler{access: failingSupportAudit{}}
	app := fiber.New()
	reached := false
	app.Get("/platform/tenants/:id/conversation-sessions", handler.guard(access.CapabilityCallsRead, access.PIIScopeCalls), func(c *fiber.Ctx) error {
		reached = true
		return c.SendStatus(fiber.StatusOK)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/conversation-sessions", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusInternalServerError || reached {
		t.Fatalf("status/reached = %d/%v, want 500/false", response.StatusCode, reached)
	}
}
