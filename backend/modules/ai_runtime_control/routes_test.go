package ai_runtime_control

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRegisterPlatformRoutesExposesOneRuntimeResource(t *testing.T) {
	app := fiber.New()
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(NewService(&fakeRuntimeStore{}), nil), "test-secret")
	want := map[string]bool{
		http.MethodGet + " /api/v2/platform/tenants/:tenant_id/ai-receptionist/runtime": false,
		http.MethodPut + " /api/v2/platform/tenants/:tenant_id/ai-receptionist/runtime": false,
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
