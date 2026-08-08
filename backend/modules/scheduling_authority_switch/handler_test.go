package scheduling_authority_switch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func TestRegisterRoutesUsesProtectedPhaseFivePreviewPaths(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(NewService(&fakeStore{}, nil, nil, true)), "test-secret")
	want := map[string]bool{
		http.MethodPost + " /api/salons/:id/scheduling-authority-switches/preview":        false,
		http.MethodGet + " /api/salons/:id/scheduling-authority-switches/latest":          false,
		http.MethodGet + " /api/salons/:id/scheduling-authority-switches/:run_id":         false,
		http.MethodPost + " /api/salons/:id/scheduling-authority-switches/:run_id/commit": false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route %s was not registered", route)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/salons/salon-1/scheduling-authority-switches/latest", nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("unauthenticated request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want %d", response.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestRegisterPlatformV2RoutesExposesOneAuthorityCommand(t *testing.T) {
	app := fiber.New()
	RegisterPlatformV2Routes(app.Group("/api"), NewPlatformHandler(NewService(&fakeStore{}, nil, nil, true), nil), "test-secret")
	want := map[string]bool{
		http.MethodPost + " /api/v2/platform/tenants/:tenant_id/scheduling/authority/readiness":     false,
		http.MethodPut + " /api/v2/platform/tenants/:tenant_id/scheduling/authority":                false,
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/scheduling/authority/history/latest": false,
	}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route %s was not registered", route)
		}
	}
}

func TestHandlerPreviewReturnsSanitizedRunAndStableErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		store      *fakeStore
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name: "ready preview", store: &fakeStore{current: authorityState{Authority: TargetExternalProvider, Version: 4}, eligibleServices: 1},
			body:       `{"operation_key":"preview-http-1","source_scheduling_authority":"external_provider","target_scheduling_authority":"owner_manual","expected_source_authority_version":4}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name: "invalid body", store: &fakeStore{}, body: `{`,
			wantStatus: fiber.StatusBadRequest, wantCode: "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR",
		},
		{
			name: "stale version", store: &fakeStore{current: authorityState{Authority: TargetExternalProvider, Version: 5}},
			body:       `{"operation_key":"preview-http-2","source_scheduling_authority":"external_provider","target_scheduling_authority":"owner_manual","expected_source_authority_version":4}`,
			wantStatus: fiber.StatusConflict, wantCode: "SCHEDULING_AUTHORITY_SWITCH_VERSION_CONFLICT",
		},
		{
			name: "changed operation reuse", store: &fakeStore{existing: &SwitchRun{payloadFingerprint: "different"}},
			body:       `{"operation_key":"preview-http-3","source_scheduling_authority":"external_provider","target_scheduling_authority":"owner_manual","expected_source_authority_version":4}`,
			wantStatus: fiber.StatusConflict, wantCode: "SCHEDULING_AUTHORITY_SWITCH_OPERATION_CONFLICT",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error {
				c.Locals(middleware.LocalUserID, "owner-1")
				return c.Next()
			})
			app.Post("/api/salons/:id/scheduling-authority-switches/preview", NewHandler(NewService(test.store, nil, nil, true)).Preview)
			request := httptest.NewRequest(http.MethodPost, "/api/salons/salon-1/scheduling-authority-switches/preview", bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status=%d, want %d", response.StatusCode, test.wantStatus)
			}
			var payload map[string]any
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if test.wantCode == "" {
				run, ok := payload["scheduling_authority_switch"].(map[string]any)
				if !ok || run["status"] != StatusPreviewReady {
					t.Fatalf("success payload=%#v", payload)
				}
				if _, leaked := run["payload_fingerprint"]; leaked {
					t.Fatalf("payload fingerprint leaked in response: %#v", run)
				}
				return
			}
			errorBody, ok := payload["error"].(map[string]any)
			if !ok || errorBody["code"] != test.wantCode {
				t.Fatalf("error payload=%#v, want code %s", payload, test.wantCode)
			}
		})
	}
}
