package scheduling_manleai_calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type fakeCalendarStore struct {
	aggregate *Aggregate
	err       error
	replayed  bool
}

func (f *fakeCalendarStore) GetAggregate(context.Context, string, string) (*Aggregate, error) {
	return f.aggregate, f.err
}
func (f *fakeCalendarStore) PutConfig(context.Context, string, string, CalendarConfigInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) PutHours(context.Context, string, string, ReplaceBusinessHoursInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) PutStaffProfile(context.Context, string, string, string, StaffProfileInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) PutServicePolicy(context.Context, string, string, string, ServicePolicyInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) CreateResource(context.Context, string, string, ResourcePoolInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) UpdateResource(context.Context, string, string, string, ResourcePoolInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) ArchiveResource(context.Context, string, string, string, MutationMeta, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) CreateException(context.Context, string, string, ExceptionInput, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) CancelException(context.Context, string, string, string, MutationMeta, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}
func (f *fakeCalendarStore) Activate(context.Context, string, string, MutationMeta, string) (*Aggregate, bool, error) {
	return f.aggregate, f.replayed, f.err
}

func TestRegisterRoutesUsesExactPhaseThreePaths(t *testing.T) {
	app := fiber.New()
	store := &fakeCalendarStore{aggregate: readyAggregateFixture()}
	RegisterRoutes(app.Group("/api"), NewHandler(NewService(store)), "test-secret")
	want := map[string]int{
		http.MethodGet + " /api/salons/:id/manleai-calendar/":                                 1,
		http.MethodPut + " /api/salons/:id/manleai-calendar/config":                           1,
		http.MethodPut + " /api/salons/:id/manleai-calendar/hours":                            1,
		http.MethodGet + " /api/salons/:id/manleai-calendar/staff/:staff_id":                  1,
		http.MethodPut + " /api/salons/:id/manleai-calendar/staff/:staff_id":                  1,
		http.MethodGet + " /api/salons/:id/manleai-calendar/services/:service_id":             1,
		http.MethodPut + " /api/salons/:id/manleai-calendar/services/:service_id":             1,
		http.MethodGet + " /api/salons/:id/manleai-calendar/resources":                        1,
		http.MethodPost + " /api/salons/:id/manleai-calendar/resources":                       1,
		http.MethodPut + " /api/salons/:id/manleai-calendar/resources/:resource_id":           1,
		http.MethodPost + " /api/salons/:id/manleai-calendar/resources/:resource_id/archive":  1,
		http.MethodPost + " /api/salons/:id/manleai-calendar/exceptions":                      1,
		http.MethodPost + " /api/salons/:id/manleai-calendar/exceptions/:exception_id/cancel": 1,
		http.MethodPost + " /api/salons/:id/manleai-calendar/activate":                        1,
	}
	got := map[string]int{}
	for _, route := range app.GetRoutes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			got[key]++
		}
		if route.Path == "/api/salons/:id/manleai-calendar/staff/:staff_id/profile" || route.Path == "/api/salons/:id/manleai-calendar/services/:service_id/policy" {
			t.Fatalf("obsolete suffix route registered: %s", route.Path)
		}
	}
	for route, count := range want {
		if got[route] != count {
			t.Fatalf("route %s count = %d, want %d", route, got[route], count)
		}
	}
}

func TestHandlerUsesUniformMutationResponse(t *testing.T) {
	aggregate := readyAggregateFixture()
	store := &fakeCalendarStore{aggregate: aggregate, replayed: true}
	handler := NewHandler(NewService(store))
	ownerID := uuid.NewString()
	salonID := aggregate.SalonID
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, ownerID)
		return c.Next()
	})
	app.Put("/salons/:id/manleai-calendar/config", handler.PutConfig)
	body, err := json.Marshal(validConfigRequest("handler-config", aggregate.ConfigVersion))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response := executeCalendarRequest(t, app, http.MethodPut, "/salons/"+salonID+"/manleai-calendar/config", body)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 || payload["manleai_calendar"] == nil || payload["replayed"] == nil {
		t.Fatalf("mutation wrapper keys = %#v", payload)
	}
}

func TestHandlerMapsStableCalendarErrors(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{err: ErrValidation, wantStatus: fiber.StatusBadRequest, wantCode: "MANLEAI_CALENDAR_VALIDATION_ERROR"},
		{err: ErrNotFound, wantStatus: fiber.StatusNotFound, wantCode: "MANLEAI_CALENDAR_NOT_FOUND"},
		{err: ErrConfigRequired, wantStatus: fiber.StatusConflict, wantCode: "MANLEAI_CALENDAR_CONFIG_REQUIRED"},
		{err: fmt.Errorf("wrapped: %w", ErrVersionConflict), wantStatus: fiber.StatusConflict, wantCode: "MANLEAI_CALENDAR_CONFIG_VERSION_CONFLICT"},
		{err: ErrActionConflict, wantStatus: fiber.StatusConflict, wantCode: "MANLEAI_CALENDAR_ACTION_CONFLICT"},
		{err: ErrNotReady, wantStatus: fiber.StatusConflict, wantCode: "MANLEAI_CALENDAR_NOT_READY"},
		{err: fmt.Errorf("database unavailable"), wantStatus: fiber.StatusInternalServerError, wantCode: "MANLEAI_CALENDAR_FAILED"},
	}
	for _, test := range tests {
		t.Run(test.wantCode, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error { return respondCalendar(c, fiber.StatusOK, nil, test.err, false) })
			response := executeCalendarRequest(t, app, http.MethodGet, "/", nil)
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.wantStatus)
			}
			var payload respond.ErrorResponse
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", payload.Error.Code, test.wantCode)
			}
		})
	}
}

func executeCalendarRequest(t *testing.T, app *fiber.App, method string, path string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, path, bytes.NewReader(body))
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
