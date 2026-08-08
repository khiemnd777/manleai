package configtransfer

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type transferAuthCall struct {
	salonID    string
	capability access.Capability
}

type transferTestAuthorizer struct {
	calls  []transferAuthCall
	denied string
}

func (a *transferTestAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	a.calls = append(a.calls, transferAuthCall{salonID: check.SalonID, capability: check.Capability})
	if check.SalonID == a.denied {
		return access.ErrForbidden
	}
	return nil
}

func (*transferTestAuthorizer) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return nil
}

func TestPlatformTransferRoutesAreFixedAndRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(nil, nil), "test-secret")
	want := map[string]bool{
		"GET /api/platform/tenants/:tenant_id/configuration-transfer/export":            true,
		"POST /api/platform/tenants/:tenant_id/configuration-transfer/preview":          true,
		"POST /api/platform/tenants/:tenant_id/configuration-transfer/apply":            true,
		"GET /api/platform/tenants/:tenant_id/configuration-transfer/runs":              true,
		"GET /api/v2/platform/tenants/:tenant_id/configuration-transfers/export":        true,
		"POST /api/v2/platform/tenants/:tenant_id/configuration-transfers/previews":     true,
		"POST /api/v2/platform/tenants/:tenant_id/configuration-transfers/applications": true,
		"GET /api/v2/platform/tenants/:tenant_id/configuration-transfers/runs":          true,
	}
	for _, route := range app.GetRoutes() {
		delete(want, route.Method+" "+route.Path)
	}
	for route := range want {
		t.Errorf("missing route %s", route)
	}

	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/api/platform/tenants/tenant-a/configuration-transfer/export?sections=salon_profile"},
		{http.MethodPost, "/api/platform/tenants/tenant-a/configuration-transfer/preview"},
		{http.MethodPost, "/api/platform/tenants/tenant-a/configuration-transfer/apply"},
		{http.MethodGet, "/api/platform/tenants/tenant-a/configuration-transfer/runs"},
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

func TestPlatformPreviewRequiresBothTargetAndSourceSectionAccess(t *testing.T) {
	targetID := "00000000-0000-4000-8000-000000000001"
	sourceID := "00000000-0000-4000-8000-000000000002"
	authorizer := &transferTestAuthorizer{denied: sourceID}
	handler := NewPlatformHandler(nil, authorizer)
	app := fiber.New()
	app.Post("/platform/tenants/:tenant_id/configuration-transfer/preview", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalActorContext, middleware.ActorContext{UserID: "platform-user", PrincipalScope: middleware.PrincipalScopePlatform})
		c.Locals(middleware.LocalUserID, "platform-user")
		return handler.Preview(c)
	})
	body := []byte(`{"source_type":"tenant","source_tenant_id":"` + sourceID + `","included_sections":["salon_profile"]}`)
	request := httptest.NewRequest(http.MethodPost, "/platform/tenants/"+targetID+"/configuration-transfer/preview", bytes.NewReader(body))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status=%d, want 403", response.StatusCode)
	}
	if len(authorizer.calls) != 2 || authorizer.calls[0].salonID != targetID || authorizer.calls[1].salonID != sourceID {
		t.Fatalf("authorization calls=%#v, want target then source", authorizer.calls)
	}
	for _, call := range authorizer.calls {
		if call.capability != access.CapabilityBusinessRead {
			t.Fatalf("capability=%s, want business.read", call.capability)
		}
	}
}

func TestPlatformPreviewRejectsEmptyScopeBeforeService(t *testing.T) {
	handler := NewPlatformHandler(nil, &transferTestAuthorizer{})
	app := fiber.New()
	app.Post("/platform/tenants/:tenant_id/configuration-transfer/preview", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalActorContext, middleware.ActorContext{UserID: "platform-user", PrincipalScope: middleware.PrincipalScopePlatform})
		c.Locals(middleware.LocalUserID, "platform-user")
		return handler.Preview(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/platform/tenants/00000000-0000-4000-8000-000000000001/configuration-transfer/preview", bytes.NewReader([]byte(`{"source_type":"tenant","source_tenant_id":"00000000-0000-4000-8000-000000000002","included_sections":[]}`)))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status=%d, want 400", response.StatusCode)
	}
}
