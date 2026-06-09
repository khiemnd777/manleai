package salon

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.List)
	group.Post("/", handler.Create)
	group.Get("/:id", handler.Get)
	group.Put("/:id", handler.Update)
	group.Get("/:id/settings", handler.GetSettings)
	group.Put("/:id/settings", handler.UpdateSettings)
	group.Get("/:id/business-hours", handler.GetBusinessHours)
	group.Put("/:id/business-hours", handler.UpdateBusinessHours)
}
