package pos

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/services", handler.Services)
	group.Post("/:id/services", handler.CreateService)
	group.Put("/:id/services/:service_id", handler.UpdateService)
	group.Patch("/:id/services/:service_id/owner-controls", handler.UpdateServiceOwnerControls)
	group.Post("/:id/services/:service_id/archive", handler.ArchiveService)
	group.Patch("/:id/services/:service_id/ai-bookable", handler.UpdateServiceAIBookable)
	group.Patch("/:id/services/:service_id/category", handler.AssignServiceCategory)
	group.Get("/:id/service-categories", handler.ServiceCategories)
	group.Post("/:id/service-categories/suggestions/refresh", handler.RefreshServiceCategorySuggestions)
	group.Post("/:id/service-categories", handler.CreateServiceCategory)
	group.Put("/:id/service-categories/:category_id", handler.UpdateServiceCategory)
	group.Post("/:id/service-categories/:category_id/archive", handler.ArchiveServiceCategory)
	group.Post("/:id/service-categories/:category_id/restore", handler.RestoreServiceCategory)
	group.Post("/:id/service-categories/:category_id/aliases", handler.UpsertServiceCategoryAlias)
	group.Post("/:id/service-category-aliases/:alias_id/archive", handler.ArchiveServiceCategoryAlias)
	group.Get("/:id/staff", handler.Staff)
	group.Post("/:id/staff", handler.CreateStaff)
	group.Put("/:id/staff/:staff_id", handler.UpdateStaff)
	group.Post("/:id/staff/:staff_id/archive", handler.ArchiveStaff)
	group.Patch("/:id/staff/:staff_id/ai-bookable", handler.UpdateStaffAIBookable)
	group.Get("/:id/pos/provider-switch-readiness", handler.ProviderSwitchReadiness)
	group.Get("/:id/pos/provider-switch-runs/latest", handler.LatestProviderSwitchRun)
	group.Post("/:id/pos/provider-switch-runs", handler.CreateProviderSwitchRun)
	group.Get("/:id/pos/provider-switch-runs/:run_id", handler.GetProviderSwitchRun)
	group.Get("/:id/pos/provider-switch-runs/:run_id/dry-run-readiness", handler.ProviderSwitchDryRunReadiness)
	group.Patch("/:id/pos/provider-switch-runs/:run_id/matches/:match_id", handler.UpdateProviderSwitchMatch)
}

func RegisterPlatformRoutes(api fiber.Router, handler *Handler, authorizer platformAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants/:id", middleware.RequireAuth(jwtSecret))
	read := requirePlatformServices(authorizer, access.CapabilityServicesRead)
	write := requirePlatformServices(authorizer, access.CapabilityServicesWrite)
	group.Get("/services", read, handler.Services)
	group.Post("/services", write, handler.CreateService)
	group.Put("/services/:service_id", write, handler.UpdateService)
	group.Patch("/services/:service_id/owner-controls", write, handler.UpdateServiceOwnerControls)
	group.Post("/services/:service_id/archive", write, handler.ArchiveService)
	group.Patch("/services/:service_id/ai-bookable", write, handler.UpdateServiceAIBookable)
	group.Patch("/services/:service_id/category", write, handler.AssignServiceCategory)
	group.Get("/service-categories", read, handler.ServiceCategories)
	group.Post("/service-categories/suggestions/refresh", write, handler.RefreshServiceCategorySuggestions)
	group.Post("/service-categories", write, handler.CreateServiceCategory)
	group.Put("/service-categories/:category_id", write, handler.UpdateServiceCategory)
	group.Post("/service-categories/:category_id/archive", write, handler.ArchiveServiceCategory)
	group.Post("/service-categories/:category_id/restore", write, handler.RestoreServiceCategory)
	group.Post("/service-categories/:category_id/aliases", write, handler.UpsertServiceCategoryAlias)
	group.Post("/service-category-aliases/:alias_id/archive", write, handler.ArchiveServiceCategoryAlias)
}

func RegisterPlatformCallsCatalogRoutes(api fiber.Router, handler *Handler, authorizer platformAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants/:id/calls", middleware.RequireAuth(jwtSecret))
	read := requirePlatformCalls(authorizer, access.CapabilityCallsRead)
	group.Get("/services", read, handler.Services)
	group.Get("/staff", read, handler.Staff)
}

func requirePlatformServices(authorizer platformAuthorizer, capability access.Capability) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if authorizer == nil || authorizer.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("id"), Capability: capability,
		}) != nil {
			return respond.Error(c, fiber.StatusForbidden, "SERVICES_FORBIDDEN", "This Platform account is not authorized for this salon's Services.")
		}
		if authorizer.RecordPlatformSupportAction(c.UserContext(), middleware.Actor(c), c.Params("id"), capability, "", c.Method(), c.Path()) != nil {
			return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
		}
		return c.Next()
	}
}

func requirePlatformCalls(authorizer platformAuthorizer, capability access.Capability) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if authorizer == nil || authorizer.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("id"), Capability: capability, PIIScope: access.PIIScopeCalls,
		}) != nil {
			return respond.Error(c, fiber.StatusForbidden, "CALLS_ACCESS_FORBIDDEN", "This Platform account is not authorized for the salon Calls view.")
		}
		if authorizer.RecordPlatformSupportAction(c.UserContext(), middleware.Actor(c), c.Params("id"), capability, access.PIIScopeCalls, c.Method(), c.Path()) != nil {
			return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
		}
		return c.Next()
	}
}
