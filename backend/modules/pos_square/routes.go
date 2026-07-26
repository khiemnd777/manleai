package pos_square

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/integrations/square")
	group.Get("/callback", handler.Callback)
	group.Post("/webhook", handler.Webhook)

	protected := group.Group("", middleware.RequireAuth(jwtSecret))
	protected.Get("/connect-url", handler.ConnectURL)
	protected.Get("/status", handler.Status)
	protected.Get("/locations", handler.Locations)
	protected.Post("/select-location", handler.SelectLocation)
	protected.Post("/sync", handler.Sync)
	protected.Post("/test-booking", handler.TestBooking)
	protected.Post("/cancel-test-booking", handler.CancelTestBooking)
	protected.Post("/enable-ai-booking", handler.EnableAIBooking)
	protected.Post("/disable-ai-booking", handler.DisableAIBooking)

	salonProtected := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	salonProtected.Get("/:id/square-webhook-events", handler.ListWebhookEvents)
	salonProtected.Get("/:id/square-webhook-events/:webhook_event_id", handler.GetWebhookEvent)
	salonProtected.Post("/:id/square-webhook-events/:webhook_event_id/requeue", handler.RequeueWebhookEvent)
}
