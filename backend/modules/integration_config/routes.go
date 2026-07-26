package integrationconfig

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/integration-configs", handler.GetAll)
	group.Put("/:id/integration-configs/square", handler.UpdateSquare)
	group.Put("/:id/integration-configs/twilio", handler.UpdateTwilio)
	group.Put("/:id/integration-configs/openai", handler.UpdateOpenAI)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	prefix := "/:tenant_id/technical/integration-configs"
	group.Get(prefix, handler.GetAll)
	group.Put(prefix+"/square", handler.UpdateSquare)
	group.Put(prefix+"/twilio", handler.UpdateTwilio)
	group.Put(prefix+"/openai", handler.UpdateOpenAI)
}
