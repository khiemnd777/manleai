package voice_twilio

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/voice/twilio")
	group.Post("/incoming", handler.Incoming)
	group.Post("/turn", handler.Turn)
	group.Post("/recording", handler.Recording)
	group.Get("/stream", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}, websocket.New(handler.Stream))
}
