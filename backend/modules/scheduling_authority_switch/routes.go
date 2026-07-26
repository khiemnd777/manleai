package scheduling_authority_switch

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons/:id/scheduling-authority-switches", middleware.RequireAuth(jwtSecret))
	group.Post("/preview", handler.Preview)
	group.Get("/latest", handler.Latest)
	group.Get("/:run_id", handler.Get)
	group.Post("/:run_id/commit", handler.Commit)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/technical/scheduling-authority-switches", middleware.RequireAuth(jwtSecret))
	group.Post("/preview", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.Preview))
	group.Get("/latest", handler.guard(access.CapabilityTechnicalRead, handler.delegate.Latest))
	group.Get("/:run_id", handler.guard(access.CapabilityTechnicalRead, handler.delegate.Get))
	group.Post("/:run_id/commit", handler.guard(access.CapabilityTechnicalWrite, handler.delegate.Commit))
}
