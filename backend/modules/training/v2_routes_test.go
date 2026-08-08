package training

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestPlatformV2RoutesUseWorkflowResources(t *testing.T) {
	app := fiber.New()
	RegisterPlatformV2Routes(app.Group("/api"), NewNormalizedHandler(&Service{}), nil, "test-secret")
	RegisterPlatformCallsCorrectionRoute(app.Group("/api"), NewNormalizedHandler(&Service{}), nil, "test-secret")
	want := map[string]bool{
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/knowledge":          false,
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/corrections":        false,
		http.MethodPost + " /api/v2/platform/tenants/:tenant_id/evaluations":       false,
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/service-aliases":    false,
		http.MethodPost + " /api/v2/platform/tenants/:tenant_id/calls/corrections": false,
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
