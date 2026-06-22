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
	group.Get("/:id/staff", handler.Staff)
	group.Post("/:id/staff", handler.CreateStaff)
	group.Put("/:id/staff/:staff_id", handler.UpdateStaff)
	group.Post("/:id/staff/:staff_id/archive", handler.ArchiveStaff)
	group.Patch("/:id/staff/:staff_id/ai-bookable", handler.UpdateStaffAIBookable)
	group.Get("/:id/pos/provider-switch-readiness", handler.ProviderSwitchReadiness)
	group.Get("/:id/pos/provider-switch-runs/latest", handler.LatestProviderSwitchRun)
	group.Post("/:id/pos/provider-switch-runs", handler.CreateProviderSwitchRun)
	group.Get("/:id/pos/provider-switch-runs/:run_id", handler.GetProviderSwitchRun)
	group.Patch("/:id/pos/provider-switch-runs/:run_id/matches/:match_id", handler.UpdateProviderSwitchMatch)
}
