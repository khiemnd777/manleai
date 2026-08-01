package pos_square

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/integrations/square", middleware.DatabaseScope(databasecontext.ScopeProvider))
	group.Get("/callback", handler.Callback)
	group.Post("/webhook", handler.Webhook)
	business := api.Group("/salons/:id/business", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	business.Get("/external-scheduling-readiness", handler.BusinessReadiness)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	business := api.Group("/platform/tenants/:tenant_id/services", middleware.RequireAuth(jwtSecret))
	business.Get("/external-scheduling-readiness", handler.BusinessReadiness)
	group := api.Group("/platform/tenants/:tenant_id/technical/square", middleware.RequireAuth(jwtSecret))
	group.Get("/connect-url", handler.ConnectURL)
	group.Get("/status", handler.Status)
	group.Get("/locations", handler.Locations)
	group.Post("/select-location", handler.SelectLocation)
	group.Post("/sync", handler.Sync)
	group.Post("/scheduling-capability/re-evaluate", handler.ReevaluateSchedulingCapability)
	group.Post("/ai-booking/enable", handler.EnableAIBooking)
	group.Post("/ai-booking/disable", handler.DisableAIBooking)
	operations := api.Group("/platform/tenants/:tenant_id/operations/square-webhooks", middleware.RequireAuth(jwtSecret))
	operations.Get("", handler.ListWebhookEvents)
	operations.Get("/:webhook_event_id", handler.GetWebhookEvent)
	operations.Post("/:webhook_event_id/requeue", handler.RequeueWebhookEvent)
}
