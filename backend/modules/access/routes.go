package access

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/platform/access", middleware.RequireAuth(jwtSecret))

	group.Get("/roles", handler.ListPlatformRoles)
	group.Get("/platform-users", handler.ListPlatformUsers)
	group.Post("/platform-users", handler.CreatePlatformUser)
	group.Put("/platform-users/:user_id", handler.UpdatePlatformUser)
	group.Get("/capabilities", handler.ListCapabilities)
	group.Put("/platform-users/:user_id/role", handler.MutatePlatformRole)

	group.Get("/salons/:salon_id/tenant-users", handler.ListTenantUsers)
	group.Get("/salons/:salon_id/memberships", handler.ListMemberships)
	group.Put("/salons/:salon_id/memberships/:user_id", handler.MutateMembership)

	group.Get("/salons/:salon_id/assignments", handler.ListSalonAssignments)
	group.Put("/salons/:salon_id/assignments/:user_id", handler.MutateSalonAssignment)

	group.Get("/salons/:salon_id/pii-grants", handler.ListPIIGrants)
	group.Post("/salons/:salon_id/pii-grants", handler.GrantPIIAccess)
	group.Post("/salons/:salon_id/pii-grants/:grant_id/revoke", handler.RevokePIIAccess)
	group.Get("/salons/:salon_id/support-requests", handler.ListPlatformSupportAccessRequests)
	group.Post("/salons/:salon_id/support-requests", handler.CreateSupportAccessRequest)
	group.Post("/salons/:salon_id/support-requests/:request_id/cancel", handler.CancelSupportAccessRequest)
	group.Post("/salons/:salon_id/support-requests/:request_id/revoke", handler.RevokeSupportAccessRequest)

	group.Get("/audit", handler.ListAuditEvents)
	api.Get("/platform/tenants/:id/support-access/effective", middleware.RequireAuth(jwtSecret), handler.GetEffectiveSupportAccess)

}
