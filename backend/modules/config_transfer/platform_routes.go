package configtransfer

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	prefix := "/:tenant_id/configuration-transfer"
	group.Get(prefix+"/export", handler.Export)
	group.Post(prefix+"/preview", handler.Preview)
	group.Post(prefix+"/apply", handler.Apply)
	group.Get(prefix+"/runs", handler.Runs)
	normalized := &PlatformHandler{service: handler.service, access: handler.access, normalized: true}
	v2 := api.Group("/v2/platform/tenants/:tenant_id/configuration-transfers", middleware.RequireAuth(jwtSecret))
	v2.Get("/export", normalized.Export)
	v2.Post("/previews", normalized.Preview)
	v2.Post("/applications", normalized.Apply)
	v2.Get("/runs", normalized.Runs)
}
