package voice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
)

type failingVoiceSupportAudit struct{}

func (failingVoiceSupportAudit) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (failingVoiceSupportAudit) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return errors.New("audit unavailable")
}

type allowingPlatformCallsAccess struct{}

func (allowingPlatformCallsAccess) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func (allowingPlatformCallsAccess) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return nil
}

type rejectingPlatformCallsAccess struct{}

func (rejectingPlatformCallsAccess) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return errors.New("denied for route ownership test")
}

func (rejectingPlatformCallsAccess) RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error {
	return nil
}

type platformRoutePrincipalResolver struct{}

func (platformRoutePrincipalResolver) ResolveAccessPrincipal(_ context.Context, userID string) (string, string, middleware.PrincipalScope, []string, error) {
	return userID, "", middleware.PrincipalScopePlatform, []string{"platform_admin"}, nil
}

func TestVoiceOperationalRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&Service{}), "test-secret")
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/salons/salon-1/voice/status"},
		{method: http.MethodPost, path: "/api/salons/salon-1/voice/semantic-check"},
		{method: http.MethodPost, path: "/api/salons/salon-1/voice/semantic-evaluate"},
		{method: http.MethodGet, path: "/api/v2/platform/tenants/salon-1/calls/readiness"},
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

func TestPlatformCallsReadinessWinsOverConversationSessionRouteInProductionOrder(t *testing.T) {
	const (
		jwtSecret = "calls-route-composition-secret"
		sessionID = "33333333-3333-4333-8333-333333333333"
	)
	app := fiber.New()
	api := app.Group("/api", middleware.WithAccessPrincipalResolver(platformRoutePrincipalResolver{}))

	// Keep this order aligned with cmd/api/main.go: conversation routes are
	// registered before voice routes in the composed production router.
	conversation.RegisterPlatformV2Routes(api, conversation.NewPlatformHandler(&conversation.Service{}, rejectingPlatformCallsAccess{}), jwtSecret)
	voiceService := newVoiceStatusService(
		newFakeVoiceStore(),
		testVoiceConfig(),
		readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1),
	)
	RegisterPlatformRoutes(api, NewPlatformHandler(voiceService, allowingPlatformCallsAccess{}), jwtSecret)

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{
		UserID: "platform-user-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "platform-user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte(jwtSecret))
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "static readiness reaches voice", path: "/api/v2/platform/tenants/salon-1/calls/readiness", wantStatus: fiber.StatusOK},
		{name: "valid session reaches conversation", path: "/api/v2/platform/tenants/salon-1/calls/" + sessionID, wantStatus: fiber.StatusForbidden},
		{name: "non guid session does not reach conversation", path: "/api/v2/platform/tenants/salon-1/calls/not-a-session", wantStatus: fiber.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status=%d, want %d", response.StatusCode, test.wantStatus)
			}
		})
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
