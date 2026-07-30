package openairuntimeverification

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	prefix := "/:tenant_id/technical/openai/runtime-verification"
	group.Get(prefix, handler.Status)
	group.Post(prefix, handler.Verify)
}
