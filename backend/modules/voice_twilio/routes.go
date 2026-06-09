package voice_twilio

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/voice/twilio")
	group.Post("/incoming", handler.Incoming)
	group.Post("/turn", handler.Turn)
}
