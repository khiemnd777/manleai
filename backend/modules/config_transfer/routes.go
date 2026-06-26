package configtransfer

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/configuration-export", handler.Get)
	group.Post("/:id/configuration-import/preview", handler.PreviewImport)
	group.Post("/:id/configuration-import", handler.ApplyImport)

	onboarding := api.Group("/onboarding", middleware.RequireAuth(jwtSecret))
	onboarding.Post("/configuration-import/preview", handler.PreviewOnboardingImport)
	onboarding.Post("/configuration-import", handler.ApplyOnboardingImport)
}
