package scheduling

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Post("/:id/scheduling-availability", handler.Availability)
	group.Post("/:id/scheduling-actions", handler.ExecuteAction)
	group.Get("/:id/scheduling-requests", handler.ListRequests)
	group.Get("/:id/scheduling-requests/:request_id", handler.GetRequest)
	group.Patch("/:id/scheduling-requests/:request_id", handler.TransitionRequest)
}
