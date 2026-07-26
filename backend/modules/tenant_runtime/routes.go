package tenantruntime

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/operations/runtime", middleware.RequireAuth(jwtSecret))
	group.Get("", handler.Get)
	group.Put("/limits", handler.Update)
}
