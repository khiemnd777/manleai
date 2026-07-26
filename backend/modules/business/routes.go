package business

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterTenantRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.ListSalons)
	registerBusinessRoutes(group, "/:id/business", handler)
}

func RegisterPlatformRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.ListSalons)
	registerBusinessRoutes(group, "/:tenant_id/business", handler)
}

func registerBusinessRoutes(group fiber.Router, prefix string, handler *Handler) {
	group.Get(prefix+"/profile", handler.SalonProfile)
	group.Patch(prefix+"/profile", handler.UpdateSalonProfile)
	group.Get(prefix+"/services", handler.Services)
	group.Post(prefix+"/services", handler.CreateService)
	group.Patch(prefix+"/services/:service_id", handler.UpdateService)
	group.Post(prefix+"/services/:service_id/archive", handler.ArchiveService)
	group.Get(prefix+"/service-categories", handler.ServiceCategories)
	group.Post(prefix+"/service-categories", handler.CreateServiceCategory)
	group.Patch(prefix+"/service-categories/:category_id", handler.UpdateServiceCategory)
	group.Post(prefix+"/service-categories/:category_id/archive", handler.ArchiveServiceCategory)
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
