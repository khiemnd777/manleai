package pos

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	// Temporary read-only compatibility for the Calls, Appointments, and
	// Training business views while they migrate to the shared Business API.
	// Provider switching and catalog writes are intentionally not registered.
	group.Get("/:id/services", handler.Services)
	group.Get("/:id/service-categories", handler.ServiceCategories)
	group.Get("/:id/staff", handler.Staff)
}
