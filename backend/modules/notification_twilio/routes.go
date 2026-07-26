package notificationtwilio

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/notifications/twilio")
	group.Post("/status", handler.Status)
	group.Post("/inbound/:salon_id", handler.Inbound)
}
