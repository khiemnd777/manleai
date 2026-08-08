package conversation

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

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
