package pos

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

type capturingCallsSupportAudit struct {
	check      access.AccessCheck
	auditScope access.PIIScope
}

func (authorizer *capturingCallsSupportAudit) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	authorizer.check = check
	return nil
}

func (authorizer *capturingCallsSupportAudit) RecordPlatformSupportAction(_ context.Context, _ middleware.ActorContext, _ string, _ access.Capability, piiScope access.PIIScope, _ string, _ string) error {
	authorizer.auditScope = piiScope
	return nil
}

func TestPlatformServicesGuardFailsClosedBeforeDomainHandlerWhenAuditFails(t *testing.T) {
	app := fiber.New()
	reached := false
	app.Get("/platform/tenants/:id/services", requirePlatformServices(failingSupportAudit{}, access.CapabilityServicesRead), func(c *fiber.Ctx) error {
		reached = true
		return c.SendStatus(fiber.StatusOK)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/services", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusInternalServerError || reached {
		t.Fatalf("status/reached = %d/%v, want 500/false", response.StatusCode, reached)
	}
}

func TestPlatformCallsCatalogGuardRequiresAndAuditsCallsPII(t *testing.T) {
	authorizer := &capturingCallsSupportAudit{}
	app := fiber.New()
	app.Get("/platform/tenants/:id/calls/services", requirePlatformCalls(authorizer, access.CapabilityCallsRead), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/calls/services", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if authorizer.check.PIIScope != access.PIIScopeCalls || authorizer.auditScope != access.PIIScopeCalls {
		t.Fatalf("authorize/audit PII scopes = %q/%q, want calls/calls", authorizer.check.PIIScope, authorizer.auditScope)
	}
}
