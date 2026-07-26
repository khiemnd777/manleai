package salon

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	// Tenant onboarding can create the initial salon. All existing-salon
	// Business management lives in modules/business; technical settings are
	// available only through fixed Platform routes.
	group.Post("/", handler.Create)
}
