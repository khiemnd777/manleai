package conversation

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type normalizedConversationResponse struct {
	Data Session `json:"data"`
	Meta struct {
		ResourceVersion int64 `json:"resource_version"`
	} `json:"meta"`
}

type conversationErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestPlatformV2RoutesUseCallsResource(t *testing.T) {
	app := fiber.New()
	RegisterPlatformV2Routes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	want := map[string]bool{
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/calls":                false,
		http.MethodPost + " /api/v2/platform/tenants/:tenant_id/calls":               false,
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/calls/:session_id":    false,
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/calls/party-requests": false,
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

func TestPlatformV2CallDetailReturnsNormalizedSession(t *testing.T) {
	store := newFakeConversationStore()
	store.session.StateRevision = 7
	store.session.Transcript = []TranscriptMessage{{ID: "message-1", Body: "Hello"}}
	store.session.DetailWarnings = []ConversationDetailWarning{{
		Code:    "TRANSCRIPT_METADATA_SHAPE_UNSUPPORTED",
		Section: "transcript",
		Message: "Unsupported legacy transcript metadata was omitted from this projection.",
	}}
	app := fiber.New()
	app.Get("/api/v2/platform/tenants/:tenant_id/calls/:session_id", NewNormalizedHandler(NewService(store, nil)).Get)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v2/platform/tenants/salon-1/calls/session-1", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body normalizedConversationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != store.session.ID || body.Meta.ResourceVersion != 7 || len(body.Data.Transcript) != 1 || len(body.Data.DetailWarnings) != 1 {
		t.Fatalf("response = %#v", body)
	}
}

func TestPlatformV2CallDetailReturnsSafeStageError(t *testing.T) {
	store := newFakeConversationStore()
	store.getSessionErr = newConversationDetailReadError(conversationDetailStageTranscript, errors.New("private database detail"))
	app := fiber.New()
	app.Get("/api/v2/platform/tenants/:tenant_id/calls/:session_id", NewNormalizedHandler(NewService(store, nil)).Get)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v2/platform/tenants/salon-1/calls/session-1", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.StatusCode)
	}
	var body conversationErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "CONVERSATION_TRANSCRIPT_FAILED" || body.Error.Message != "Could not load the conversation transcript." {
		t.Fatalf("error response = %#v", body.Error)
	}
}
