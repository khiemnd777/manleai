package conversation

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/conversation-sessions", handler.List)
	group.Post("/:id/conversation-sessions", handler.Start)
	group.Get("/:id/conversation-sessions/:session_id", handler.Get)
	group.Get("/:id/conversation-sessions/:session_id/realtime-events", handler.RealtimeEvents)
	group.Post("/:id/conversation-sessions/:session_id/archive", handler.Archive)
	group.Post("/:id/conversation-sessions/:session_id/redact", handler.Redact)
	group.Post("/:id/conversation-sessions/:session_id/messages", handler.Message)
}
