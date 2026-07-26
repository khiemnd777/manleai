package access

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/platform/access", middleware.RequireAuth(jwtSecret))

	group.Get("/roles", handler.ListPlatformRoles)
	group.Get("/users", handler.ListUsers)
	group.Get("/capabilities", handler.ListCapabilities)
	group.Put("/users/:user_id/platform-role", handler.MutatePlatformRole)

	group.Get("/salons/:salon_id/memberships", handler.ListMemberships)
	group.Put("/salons/:salon_id/memberships/:user_id", handler.MutateMembership)

	group.Get("/salons/:salon_id/assignments", handler.ListSalonAssignments)
	group.Put("/salons/:salon_id/assignments/:user_id", handler.MutateSalonAssignment)

	group.Get("/salons/:salon_id/pii-grants", handler.ListPIIGrants)
	group.Post("/salons/:salon_id/pii-grants", handler.GrantPIIAccess)
	group.Post("/salons/:salon_id/pii-grants/:grant_id/revoke", handler.RevokePIIAccess)

	group.Get("/audit", handler.ListAuditEvents)
}
