package voice

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

type failingVoiceSupportAudit struct{}

func (failingVoiceSupportAudit) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (failingVoiceSupportAudit) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return errors.New("audit unavailable")
}

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

func TestPlatformVoiceStatusFailsClosedBeforeServiceWhenAuditFails(t *testing.T) {
	app := fiber.New()
	handler := NewPlatformHandler(&Service{}, failingVoiceSupportAudit{})
	app.Get("/platform/tenants/:id/voice/status", handler.Status)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/platform/tenants/salon-1/voice/status", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", response.StatusCode)
	}
}
