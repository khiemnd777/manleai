package operationshealth

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/operations/status", handler.Get)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/operations", middleware.RequireAuth(jwtSecret))
	group.Get("/status", handler.Get)
	v2 := api.Group("/v2/platform/tenants/:tenant_id/operations", middleware.RequireAuth(jwtSecret))
	v2.Get("/overview", (&PlatformHandler{service: handler.service, access: handler.access, normalized: true}).Get)
}
