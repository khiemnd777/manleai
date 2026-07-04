package pos

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/services", handler.Services)
	group.Post("/:id/services", handler.CreateService)
	group.Put("/:id/services/:service_id", handler.UpdateService)
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
