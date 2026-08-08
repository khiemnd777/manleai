package scheduling_manleai_calendar

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons/:id/manleai-calendar", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/", handler.GetAggregate)
	group.Put("/config", handler.PutConfig)
	group.Put("/hours", handler.PutHours)
	group.Get("/staff/:staff_id", handler.GetStaff)
	group.Put("/staff/:staff_id", handler.PutStaff)
	group.Get("/services/:service_id", handler.GetService)
	group.Put("/services/:service_id", handler.PutService)
	group.Get("/resources", handler.ListResources)
	group.Post("/resources", handler.CreateResource)
	group.Put("/resources/:resource_id", handler.UpdateResource)
	group.Post("/resources/:resource_id/archive", handler.ArchiveResource)
	group.Post("/exceptions", handler.CreateException)
	group.Post("/exceptions/:exception_id/cancel", handler.CancelException)
	group.Post("/activate", handler.Activate)
}

// RegisterPlatformRoutes exposes calendar configuration only through the
// fixed Platform tenant detail surface. Tenant routes keep business
// scheduling operations but do not expose these technical controls.
func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/technical/manleai-calendar", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.guard(access.CapabilityTechnicalRead, handler.delegate.GetAggregate))
	group.Put("/config", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.PutConfig))
	group.Put("/hours", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.PutHours))
	group.Get("/staff/:staff_id", handler.guard(access.CapabilityTechnicalRead, handler.delegate.GetStaff))
	group.Put("/staff/:staff_id", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.PutStaff))
	group.Get("/services/:service_id", handler.guard(access.CapabilityTechnicalRead, handler.delegate.GetService))
	group.Put("/services/:service_id", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.PutService))
	group.Get("/resources", handler.guard(access.CapabilityTechnicalRead, handler.delegate.ListResources))
	group.Post("/resources", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.CreateResource))
	group.Put("/resources/:resource_id", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.UpdateResource))
	group.Post("/resources/:resource_id/archive", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.ArchiveResource))
	group.Post("/exceptions", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.CreateException))
	group.Post("/exceptions/:exception_id/cancel", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.CancelException))
	group.Post("/activate", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.Activate))
	v2 := api.Group("/v2/platform/tenants/:tenant_id/scheduling/internal-calendar", middleware.RequireAuth(jwtSecret))
	v2.Get("", handler.guard(access.CapabilityTechnicalRead, handler.normalizedDelegate.GetAggregate))
	v2.Put("/policy", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.PutConfig))
	v2.Put("/hours", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.PutHours))
	v2.Get("/staff/:staff_id", handler.guard(access.CapabilityTechnicalRead, handler.normalizedDelegate.GetStaff))
	v2.Put("/staff/:staff_id", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.PutStaff))
	v2.Get("/services/:service_id", handler.guard(access.CapabilityTechnicalRead, handler.normalizedDelegate.GetService))
	v2.Put("/services/:service_id", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.PutService))
	v2.Get("/resources", handler.guard(access.CapabilityTechnicalRead, handler.normalizedDelegate.ListResources))
	v2.Post("/resources", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.CreateResource))
	v2.Put("/resources/:resource_id", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.UpdateResource))
	v2.Post("/resources/:resource_id/archive", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.ArchiveResource))
	v2.Post("/exceptions", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.CreateException))
	v2.Post("/exceptions/:exception_id/cancel", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.CancelException))
	v2.Post("/activation", handler.guard(access.CapabilityTechnicalWrite, handler.normalizedDelegate.Activate))
}
