package notificationtwilio

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/notifications/twilio", middleware.DatabaseScope(databasecontext.ScopeProvider))
	group.Post("/status", handler.Status)
	group.Post("/inbound/:salon_id", handler.Inbound)
}
