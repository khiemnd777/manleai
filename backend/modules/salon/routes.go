package salon

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	// Tenant onboarding can create the initial salon. All existing-salon
	// Business management lives in modules/business. These owner-scoped routes
	// remain the compatibility contract used by the established Settings UI.
	group.Post("/", handler.Create)
	group.Get("/:id", handler.Get)
	group.Put("/:id", handler.Update)
	group.Get("/:id/settings", handler.GetSettings)
	group.Put("/:id/settings", handler.UpdateSettings)
	group.Get("/:id/public-catalog", handler.GetPublicCatalogSettings)
	group.Put("/:id/public-catalog", handler.UpdatePublicCatalogSettings)
	group.Get("/:id/business-hours", handler.GetBusinessHours)
	group.Put("/:id/business-hours", handler.UpdateBusinessHours)
}
