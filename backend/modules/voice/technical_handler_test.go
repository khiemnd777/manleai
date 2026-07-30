package voice

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type fakeVoiceTechnicalAuthorizer struct {
	err   error
	check access.AccessCheck
}

func (f *fakeVoiceTechnicalAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	f.check = check
	return f.err
}

func TestTechnicalVoiceRoutingStatusRequiresExactTechnicalRead(t *testing.T) {
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{})
	authorizer := &fakeVoiceTechnicalAuthorizer{err: errors.New("denied")}
	handler := NewTechnicalHandler(service, authorizer)
	app := fiber.New()
	app.Get("/api/platform/tenants/:tenant_id/technical/voice-routing-status", handler.TwilioVoiceRoutingStatus)

	response, err := app.Test(httptest.NewRequest("GET", "/api/platform/tenants/salon_1/technical/voice-routing-status", nil))
	if err != nil {
		t.Fatalf("request status: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status=%d, want 403", response.StatusCode)
	}
	if authorizer.check.Surface != access.SurfacePlatform || authorizer.check.SalonID != "salon_1" || authorizer.check.Capability != access.CapabilityTechnicalRead {
		t.Fatalf("authorization check=%#v", authorizer.check)
	}
}
