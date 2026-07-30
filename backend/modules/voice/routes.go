package voice

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/voice/status", handler.Status)
	group.Post("/:id/voice/semantic-check", handler.SemanticCheck)
	group.Post("/:id/voice/semantic-evaluate", handler.SemanticEvaluate)
	api.Get("/voice/audio", handler.Audio)
	api.Get("/voice/audio/:id", handler.Audio)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/voice/status", handler.Status)
}

func RegisterTechnicalRoutes(api fiber.Router, handler *TechnicalHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	group.Get("/:tenant_id/technical/voice-routing-status", handler.TwilioVoiceRoutingStatus)
}
