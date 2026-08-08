package scheduling_behavior

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/v2/platform/tenants/:tenant_id/scheduling", middleware.RequireAuth(jwtSecret))
	group.Get("/behavior", handler.Get)
	group.Put("/booking-mode", handler.UpdateBookingMode)
}
