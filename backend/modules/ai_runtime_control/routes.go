package ai_runtime_control

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/v2/platform/tenants/:tenant_id/ai-receptionist", middleware.RequireAuth(jwtSecret))
	group.Get("/runtime", handler.Get)
	group.Put("/runtime", handler.Update)
}
