package notificationdelivery

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/owner-notification-deliveries", handler.List)
	group.Get("/:id/owner-notification-deliveries/:notification_id", handler.Get)
	group.Post("/:id/owner-notification-deliveries/:notification_id/requeue", handler.Requeue)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/operations/owner-notification-deliveries", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.guard(access.CapabilityOperationsRead, handler.delegate.List))
	group.Get("/:notification_id", handler.guard(access.CapabilityOperationsRead, handler.delegate.Get))
	group.Post("/:notification_id/requeue", handler.guard(access.CapabilityOperationsWrite, handler.delegate.Requeue))
}
