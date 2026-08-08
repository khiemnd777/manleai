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
	group.Post("/active-provider/activate", handler.ActivateInitialProvider)
	group.Post("/ai-booking/enable", handler.EnableAIBooking)
	group.Post("/ai-booking/disable", handler.DisableAIBooking)
	normalized := &PlatformHandler{
		service: handler.service, access: handler.access, runtimeLimiter: handler.runtimeLimiter, normalized: true,
	}
	v2 := api.Group("/v2/platform/tenants/:tenant_id/integrations/square", middleware.RequireAuth(jwtSecret))
	v2.Get("/connection", normalized.Status)
	v2.Get("/connection/connect-url", normalized.ConnectURL)
	v2.Get("/connection/locations", normalized.Locations)
	v2.Put("/connection/location", normalized.SelectLocation)
	v2.Post("/sync-runs", normalized.Sync)
	v2.Post("/verifications/scheduling-safety", normalized.ReevaluateSchedulingCapability)
	v2.Post("/activation", normalized.ActivateInitialProvider)
	api.Get("/v2/platform/tenants/:tenant_id/services/external-scheduling-readiness", middleware.RequireAuth(jwtSecret), normalized.BusinessReadiness)
	operations := api.Group("/platform/tenants/:tenant_id/operations/square-webhooks", middleware.RequireAuth(jwtSecret))
	operations.Get("", handler.ListWebhookEvents)
	operations.Get("/:webhook_event_id", handler.GetWebhookEvent)
	operations.Post("/:webhook_event_id/requeue", handler.RequeueWebhookEvent)
	v2Operations := api.Group("/v2/platform/tenants/:tenant_id/operations/provider-events/square", middleware.RequireAuth(jwtSecret))
	v2Operations.Get("", normalized.ListWebhookEvents)
	v2Operations.Get("/:webhook_event_id", normalized.GetWebhookEvent)
	v2Operations.Post("/:webhook_event_id/requeue", normalized.RequeueWebhookEvent)
}
