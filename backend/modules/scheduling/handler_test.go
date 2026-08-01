package scheduling

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/booking"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
)

type rejectingTenantLimiter struct{}

func (rejectingTenantLimiter) AllowTenant(context.Context, middleware.ActorContext, string, string, int) (tenantruntime.Decision, error) {
	return tenantruntime.Decision{Limit: 1, Remaining: 0, RetryAfterSec: 12}, tenantruntime.ErrQuotaExceeded
}

type fakeHandlerActions struct {
	result  *ActionResult
	err     error
	request ActionRequest
}

func (f *fakeHandlerActions) CheckAvailability(context.Context, string, string, booking.AvailabilityRequest) (*AvailabilityResult, error) {
	return nil, f.err
}

func (f *fakeHandlerActions) ExecuteAction(_ context.Context, _ string, _ string, req ActionRequest) (*ActionResult, error) {
	f.request = req
	return f.result, f.err
}

type fakeHandlerRequests struct {
	list       *ListSchedulingRequestsResponse
	request    *SchedulingRequest
	getErr     error
	transition *SchedulingRequest
	transErr   error
}

func (f *fakeHandlerRequests) List(context.Context, string, string, SchedulingRequestStatus, int, int) (*ListSchedulingRequestsResponse, error) {
	return f.list, nil
}

func (f *fakeHandlerRequests) Get(context.Context, string, string, string) (*SchedulingRequest, error) {
	return f.request, f.getErr
}

func (f *fakeHandlerRequests) Transition(context.Context, string, string, string, TransitionSchedulingRequest) (*SchedulingRequest, bool, error) {
	return f.transition, false, f.transErr
}

func TestHandlerExecuteActionUsesOperationSpecificHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		result     *ActionResult
		wantStatus int
	}{
		{name: "confirmed book created", result: confirmedHandlerResult(OperationKindBook), wantStatus: fiber.StatusCreated},
		{name: "confirmed reschedule ok", result: confirmedHandlerResult(OperationKindReschedule), wantStatus: fiber.StatusOK},
		{name: "confirmed cancel ok", result: confirmedHandlerResult(OperationKindCancel), wantStatus: fiber.StatusOK},
		{name: "owner review accepted", result: &ActionResult{Kind: ActionKindPendingOwnerReview, OperationType: OperationKindBook, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual, PendingOwnerReview: &PendingOwnerReviewResult{SchedulingRequestID: "request-1", Status: string(SchedulingRequestStatusPending), Version: 1}}, wantStatus: fiber.StatusAccepted},
		{name: "external fallback accepted", result: &ActionResult{Kind: ActionKindExternalFallbackPending, OperationType: OperationKindBook, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider, ExternalFallbackPending: &ExternalFallbackPendingResult{ExternalAttemptID: "attempt-1"}}, wantStatus: fiber.StatusAccepted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &fakeHandlerActions{result: test.result}
			handler := NewHandler(actions, &fakeHandlerRequests{})
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error {
				c.Locals(middleware.LocalUserID, "owner-1")
				return c.Next()
			})
			app.Post("/salons/:id/scheduling-actions", handler.ExecuteAction)
			response := executeSchedulingRequest(t, app, http.MethodPost, "/salons/salon-1/scheduling-actions", `{"operation_type":"book","operation_key":"operation-1"}`)
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.wantStatus)
			}
			if actions.request.Source != booking.SourceOwnerDashboard {
				t.Fatalf("source = %q, want trusted owner dashboard source", actions.request.Source)
			}
		})
	}
}

func TestHandlerAvailabilityMapsFailClosedConflictToStale409(t *testing.T) {
	handler := NewHandler(&fakeHandlerActions{err: booking.ErrAvailabilityQuoteStale}, &fakeHandlerRequests{})
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, "owner-1")
		return c.Next()
	})
	app.Post("/salons/:id/scheduling-availability", handler.Availability)
	response := executeSchedulingRequest(t, app, http.MethodPost, "/salons/salon-1/scheduling-availability", `{}`)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "AVAILABILITY_QUOTE_STALE" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}

func TestHandlerExecuteMapsSlotCommitConflictToRefreshable409(t *testing.T) {
	handler := NewHandler(&fakeHandlerActions{err: booking.ErrSlotCommitConflict}, &fakeHandlerRequests{})
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, "owner-1")
		return c.Next()
	})
	app.Post("/salons/:id/scheduling-actions", handler.ExecuteAction)
	response := executeSchedulingRequest(t, app, http.MethodPost, "/salons/salon-1/scheduling-actions", `{"operation_type":"book","operation_key":"operation-1"}`)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status=%d, want 409", response.StatusCode)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "SLOT_COMMIT_CONFLICT" {
		t.Fatalf("error code=%q", payload.Error.Code)
	}
}

func TestHandlerRejectsOverQuotaBeforeSchedulingExecution(t *testing.T) {
	actions := &fakeHandlerActions{}
	handler := NewHandler(actions, &fakeHandlerRequests{}).SetTenantRuntimeLimiter(rejectingTenantLimiter{})
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, "owner-1")
		c.Locals(middleware.LocalActorContext, middleware.ActorContext{UserID: "owner-1"})
		return c.Next()
	})
	app.Post("/salons/:id/scheduling-actions", handler.ExecuteAction)
	response := executeSchedulingRequest(t, app, http.MethodPost, "/salons/salon-1/scheduling-actions", `{"operation_type":"book","operation_key":"over-limit"}`)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusTooManyRequests || response.Header.Get("Retry-After") != "12" {
		t.Fatalf("status=%d retry-after=%q", response.StatusCode, response.Header.Get("Retry-After"))
	}
	if actions.request.OperationType != "" {
		t.Fatalf("scheduling action executed after quota rejection: %#v", actions.request)
	}
}

func TestHandlerDetailAndTransitionUseSchedulingRequestWrapper(t *testing.T) {
	request := &SchedulingRequest{ID: "request-1", Status: SchedulingRequestStatusPending, Version: 1}
	requests := &fakeHandlerRequests{request: request, transition: request}
	handler := NewHandler(&fakeHandlerActions{}, requests)
	tests := []struct {
		method string
		path   string
		body   string
		fn     fiber.Handler
	}{
		{method: http.MethodGet, path: "/salons/salon-1/scheduling-requests/request-1", fn: handler.GetRequest},
		{method: http.MethodPatch, path: "/salons/salon-1/scheduling-requests/request-1", body: `{"action_key":"contact-1","expected_version":1,"status":"contacted"}`, fn: handler.TransitionRequest},
	}
	for _, test := range tests {
		app := fiber.New()
		if test.method == http.MethodGet {
			app.Get(test.path, test.fn)
		} else {
			app.Patch(test.path, test.fn)
		}
		response := executeSchedulingRequest(t, app, test.method, test.path, test.body)
		defer response.Body.Close()
		if response.StatusCode != fiber.StatusOK {
			t.Fatalf("%s status = %d", test.method, response.StatusCode)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(payload) != 1 || payload["scheduling_request"] == nil {
			t.Fatalf("%s response keys = %#v, want exact wrapper", test.method, payload)
		}
	}
}

func TestHandlerListUsesExactSchedulingRequestsEnvelope(t *testing.T) {
	handler := NewHandler(&fakeHandlerActions{}, &fakeHandlerRequests{list: &ListSchedulingRequestsResponse{
		SchedulingRequests: []SchedulingRequest{{ID: "request-1"}},
		Limit:              25,
		Offset:             5,
		HasMore:            true,
	}})
	app := fiber.New()
	app.Get("/salons/:id/scheduling-requests", handler.ListRequests)
	response := executeSchedulingRequest(t, app, http.MethodGet, "/salons/salon-1/scheduling-requests?limit=25&offset=5", "")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantKeys := []string{"scheduling_requests", "limit", "offset", "has_more"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("response keys = %#v", payload)
	}
	for _, key := range wantKeys {
		if payload[key] == nil {
			t.Fatalf("response missing %q: %#v", key, payload)
		}
	}
}

func TestHandlerMapsStaleTransitionToStableConflictCode(t *testing.T) {
	handler := NewHandler(&fakeHandlerActions{}, &fakeHandlerRequests{transErr: ErrSchedulingRequestVersion})
	app := fiber.New()
	app.Patch("/salons/:id/scheduling-requests/:request_id", handler.TransitionRequest)
	response := executeSchedulingRequest(t, app, http.MethodPatch, "/salons/salon-1/scheduling-requests/request-1", `{"action_key":"contact-1","expected_version":1,"status":"contacted"}`)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	var payload respond.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "SCHEDULING_REQUEST_VERSION_CONFLICT" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}

func TestHandlerMapsActionOperationConflict(t *testing.T) {
	handler := NewHandler(&fakeHandlerActions{err: booking.ErrOperationConflict}, &fakeHandlerRequests{})
	app := fiber.New()
	app.Post("/salons/:id/scheduling-actions", handler.ExecuteAction)
	response := executeSchedulingRequest(t, app, http.MethodPost, "/salons/salon-1/scheduling-actions", `{"operation_type":"book","operation_key":"operation-1"}`)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	var payload respond.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "SCHEDULING_OPERATION_CONFLICT" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}

func TestSchedulingRequestNullableTimesAreOmitted(t *testing.T) {
	payload, err := json.Marshal(SchedulingRequest{ID: "request-1", Segments: []SchedulingRequestSegment{{ID: "segment-1"}}})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, found := value["requested_start_time"]; found {
		t.Fatalf("request zero start timestamp was serialized: %s", payload)
	}
	segments := value["segments"].([]any)
	segment := segments[0].(map[string]any)
	if _, found := segment["requested_start_time"]; found {
		t.Fatalf("segment zero start timestamp was serialized: %s", payload)
	}
}

func TestRegisterRoutesUsesAdditiveAuthorityAwarePathsWithoutDuplicates(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&fakeHandlerActions{}, &fakeHandlerRequests{}), "test-secret")
	want := map[string]int{
		http.MethodPost + " /api/salons/:id/scheduling-availability":          1,
		http.MethodPost + " /api/salons/:id/scheduling-actions":               1,
		http.MethodGet + " /api/salons/:id/scheduling-requests":               1,
		http.MethodGet + " /api/salons/:id/scheduling-requests/:request_id":   1,
		http.MethodPatch + " /api/salons/:id/scheduling-requests/:request_id": 1,
	}
	got := make(map[string]int)
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, expected := want[key]; expected {
			got[key]++
		}
		if route.Path == "/api/salons/:id/availability" || route.Path == "/api/salons/:id/booking-attempts" {
			t.Fatalf("neutral route registration claimed legacy path %s", route.Path)
		}
	}
	for route, count := range want {
		if got[route] != count {
			t.Fatalf("route %s count = %d, want %d", route, got[route], count)
		}
	}
}

func confirmedHandlerResult(operation OperationKind) *ActionResult {
	return &ActionResult{
		Kind:                ActionKindConfirmedAppointment,
		OperationType:       operation,
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		ConfirmedAppointment: &ConfirmedAppointmentResult{
			AppointmentID: "appointment-1",
		},
	}
}

func executeSchedulingRequest(t *testing.T, app *fiber.App, method string, path string, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	return response
}
