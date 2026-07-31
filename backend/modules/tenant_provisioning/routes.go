package tenant_provisioning

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	platform := api.Group("/platform/registration-requests", middleware.RequireAuth(jwtSecret))
	platform.Post("/:request_id/provision", handler.Provision)
	platform.Post("/:request_id/owner-invitation", handler.CreateInvitation)
	api.Get("/platform/tenant-identities", middleware.RequireAuth(jwtSecret), handler.SearchTenantIdentities)
	api.Post("/auth/owner-invitations/accept", middleware.DatabaseScope(databasecontext.ScopePublic), handler.AcceptInvitation)
}
