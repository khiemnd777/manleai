package pos_square

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/integrations/square")
	group.Get("/callback", handler.Callback)

	protected := group.Group("", middleware.RequireAuth(jwtSecret))
	protected.Get("/connect-url", handler.ConnectURL)
	protected.Get("/status", handler.Status)
	protected.Get("/locations", handler.Locations)
	protected.Post("/select-location", handler.SelectLocation)
	protected.Post("/sync", handler.Sync)
	protected.Post("/test-booking", handler.NotYet)
	protected.Post("/cancel-test-booking", handler.NotYet)
	protected.Post("/enable-ai-booking", handler.NotYet)
	protected.Post("/disable-ai-booking", handler.NotYet)
}
