package scheduling_behavior

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRegisterPlatformRoutesExposesSchedulingBehaviorResources(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(NewService(&fakeStore{}), nil), "test-secret")
	want := map[string]bool{
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/scheduling/behavior":     false,
		http.MethodPut + " /api/v2/platform/tenants/:tenant_id/scheduling/booking-mode": false,
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
