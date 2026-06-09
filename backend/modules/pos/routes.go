package pos

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/services", handler.Services)
	group.Patch("/:id/services/:service_id/ai-bookable", handler.UpdateServiceAIBookable)
	group.Get("/:id/staff", handler.Staff)
	group.Patch("/:id/staff/:staff_id/ai-bookable", handler.UpdateStaffAIBookable)
}
