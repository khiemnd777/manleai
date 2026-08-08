package openairuntimeverification

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants", middleware.RequireAuth(jwtSecret))
	prefix := "/:tenant_id/technical/openai/runtime-verification"
	group.Get(prefix, handler.Status)
	group.Post(prefix, handler.Verify)
	normalized := &PlatformHandler{service: handler.service, access: handler.access, normalized: true}
	v2 := api.Group("/v2/platform/tenants/:tenant_id/integrations/openai/verifications", middleware.RequireAuth(jwtSecret))
	v2.Get("", normalized.Status)
	v2.Post("", normalized.Verify)
}
