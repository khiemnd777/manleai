package scheduling_manleai_calendar

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestPlatformV2RoutesUseSchedulingResources(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	want := map[string]bool{
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/scheduling/internal-calendar":             false,
		http.MethodPut + " /api/v2/platform/tenants/:tenant_id/scheduling/internal-calendar/policy":      false,
		http.MethodPost + " /api/v2/platform/tenants/:tenant_id/scheduling/internal-calendar/activation": false,
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
