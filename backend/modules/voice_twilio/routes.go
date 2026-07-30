package voice_twilio

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/voice/twilio", middleware.DatabaseScope(databasecontext.ScopeProvider))
	group.Post("/incoming", handler.Incoming)
	group.Post("/turn", handler.Turn)
	group.Post("/recording", handler.Recording)
	group.Post("/stream/status", handler.StreamStatus)
	group.Post("/stream/fallback", handler.StreamFallback)
	group.Get("/stream", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}, websocket.New(handler.Stream))

	tenant := group.Group("/:route_id")
	tenant.Post("/incoming", handler.TenantIncoming)
	tenant.Post("/turn", handler.TenantTurn)
	tenant.Post("/recording", handler.TenantRecording)
	tenant.Post("/stream/status", handler.TenantStreamStatus)
	tenant.Post("/stream/fallback", handler.TenantStreamFallback)
	tenant.Get("/stream", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}, websocket.New(handler.Stream))
}
