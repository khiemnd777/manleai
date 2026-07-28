package business

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformSupportAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

func RegisterTenantRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/", handler.ListSalons)
	registerBusinessRoutes(group, "/:id/business", handler, nil)
}

func RegisterPlatformRoutes(api fiber.Router, handler *Handler, authorizer platformSupportAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.ListSalons)
	registerBusinessRoutes(group, "/:tenant_id/business", handler, authorizer)
}

func registerBusinessRoutes(group fiber.Router, prefix string, handler *Handler, platformAuthorizer platformSupportAuthorizer) {
	group.Get(prefix+"/profile", handler.SalonProfile)
	group.Patch(prefix+"/profile", handler.UpdateSalonProfile)
	if platformAuthorizer == nil {
		group.Get(prefix+"/services", handler.Services)
		group.Post(prefix+"/services", handler.CreateService)
		group.Patch(prefix+"/services/:service_id", handler.UpdateService)
		group.Post(prefix+"/services/:service_id/archive", handler.ArchiveService)
		group.Get(prefix+"/service-categories", handler.ServiceCategories)
		group.Post(prefix+"/service-categories", handler.CreateServiceCategory)
		group.Patch(prefix+"/service-categories/:category_id", handler.UpdateServiceCategory)
		group.Post(prefix+"/service-categories/:category_id/archive", handler.ArchiveServiceCategory)
	} else {
		read := requirePlatformBusinessServices(platformAuthorizer, access.CapabilityServicesRead)
		write := requirePlatformBusinessServices(platformAuthorizer, access.CapabilityServicesWrite)
		group.Get(prefix+"/services", read, handler.Services)
		group.Post(prefix+"/services", write, handler.CreateService)
		group.Patch(prefix+"/services/:service_id", write, handler.UpdateService)
		group.Post(prefix+"/services/:service_id/archive", write, handler.ArchiveService)
		group.Get(prefix+"/service-categories", read, handler.ServiceCategories)
		group.Post(prefix+"/service-categories", write, handler.CreateServiceCategory)
		group.Patch(prefix+"/service-categories/:category_id", write, handler.UpdateServiceCategory)
		group.Post(prefix+"/service-categories/:category_id/archive", write, handler.ArchiveServiceCategory)
	}
	group.Get(prefix+"/staff", handler.Staff)
	group.Post(prefix+"/staff", handler.CreateStaff)
	group.Patch(prefix+"/staff/:staff_id", handler.UpdateStaff)
	group.Post(prefix+"/staff/:staff_id/archive", handler.ArchiveStaff)
	group.Put(prefix+"/staff/:staff_id/services", handler.ReplaceStaffServiceEligibility)
	group.Get(prefix+"/business-hours", handler.BusinessHours)
	group.Put(prefix+"/business-hours", handler.ReplaceBusinessHours)
	group.Get(prefix+"/public-catalog", handler.PublicCatalogSettings)
	group.Patch(prefix+"/public-catalog", handler.UpdatePublicCatalogSettings)
	group.Get(prefix+"/customers", handler.Customers)
	group.Post(prefix+"/customers", handler.CreateCustomer)
	group.Patch(prefix+"/customers/:customer_id", handler.UpdateCustomer)
	group.Post(prefix+"/customers/:customer_id/archive", handler.ArchiveCustomer)
}

func requirePlatformBusinessServices(authorizer platformSupportAuthorizer, capability access.Capability) fiber.Handler {
	return func(c *fiber.Ctx) error {
		salonID := c.Params("tenant_id")
		actor := middleware.Actor(c)
		businessCapability := access.CapabilityBusinessRead
		if capability == access.CapabilityServicesWrite {
			businessCapability = access.CapabilityBusinessWrite
		}
		if authorizer == nil || authorizer.Authorize(c.UserContext(), actor, access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: salonID, Capability: capability,
		}) != nil || authorizer.Authorize(c.UserContext(), actor, access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: salonID, Capability: businessCapability,
		}) != nil {
			return respond.Error(c, fiber.StatusForbidden, "BUSINESS_SERVICES_ACCESS_FORBIDDEN", "This Platform account is not authorized for this salon's Services.")
		}
		if authorizer.RecordPlatformSupportAction(c.UserContext(), actor, salonID, capability, "", c.Method(), c.Path()) != nil {
			return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
		}
		return c.Next()
	}
}
