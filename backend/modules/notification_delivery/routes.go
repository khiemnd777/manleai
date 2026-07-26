package notificationdelivery

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/owner-notification-deliveries", handler.List)
	group.Get("/:id/owner-notification-deliveries/:notification_id", handler.Get)
	group.Post("/:id/owner-notification-deliveries/:notification_id/requeue", handler.Requeue)
}
