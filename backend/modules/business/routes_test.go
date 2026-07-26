package business

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/modules/access"
)

func TestTenantAndPlatformBusinessRoutesAreFixedAndSymmetric(t *testing.T) {
	app := fiber.New()
	api := app.Group("/api")
	RegisterTenantRoutes(api, NewHandler(&ServiceLayer{}, access.SurfaceTenant), "secret")
	RegisterPlatformRoutes(api, NewHandler(&ServiceLayer{}, access.SurfacePlatform), "secret")
	routes := map[string]bool{}
	for _, route := range app.GetRoutes() {
		routes[route.Method+" "+route.Path] = true
	}
	pairs := [][2]string{
		{"GET /api/salons/:id/business/profile", "GET /api/platform/tenants/:tenant_id/business/profile"},
		{"GET /api/salons/:id/business/services", "GET /api/platform/tenants/:tenant_id/business/services"},
		{"PUT /api/salons/:id/business/staff/:staff_id/services", "PUT /api/platform/tenants/:tenant_id/business/staff/:staff_id/services"},
		{"PUT /api/salons/:id/business/business-hours", "PUT /api/platform/tenants/:tenant_id/business/business-hours"},
		{"GET /api/salons/:id/business/customers", "GET /api/platform/tenants/:tenant_id/business/customers"},
	}
	for _, pair := range pairs {
		for _, key := range pair {
			if !routes[key] {
				t.Errorf("missing route %s", key)
			}
		}
	}
}
