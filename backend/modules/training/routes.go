package training

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/knowledge-items", handler.ListKnowledge)
	group.Post("/:id/knowledge-items", handler.CreateKnowledge)
	group.Put("/:id/knowledge-items/:item_id", handler.UpdateKnowledge)
	group.Delete("/:id/knowledge-items/:item_id", handler.DeleteKnowledge)
	group.Get("/:id/service-aliases", handler.ListServiceAliases)
	group.Post("/:id/service-aliases", handler.UpsertServiceAlias)
	group.Get("/:id/owner-corrections", handler.ListCorrections)
	group.Post("/:id/owner-corrections", handler.CreateCorrection)
	group.Post("/:id/owner-corrections/:correction_id/apply", handler.ApplyCorrection)
	group.Post("/:id/owner-corrections/:correction_id/apply-service-alias", handler.ApplyServiceAliasCorrection)
	group.Post("/:id/owner-corrections/:correction_id/dismiss", handler.DismissCorrection)
	group.Post("/:id/training/evaluate", handler.Evaluate)
}

func RegisterPlatformServiceAliasRoutes(api fiber.Router, handler *Handler, authorizer platformAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants/:id", middleware.RequireAuth(jwtSecret))
	group.Get("/service-aliases", requirePlatformCapability(authorizer, access.CapabilityServicesRead), handler.ListServiceAliases)
	group.Post("/service-aliases", requirePlatformCapability(authorizer, access.CapabilityServicesWrite), handler.UpsertServiceAlias)
}

func RegisterPlatformRoutes(api fiber.Router, handler *Handler, authorizer platformAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants/:id", middleware.RequireAuth(jwtSecret))
	read := requirePlatformCapability(authorizer, access.CapabilityTrainingRead)
	write := requirePlatformCapability(authorizer, access.CapabilityTrainingWrite)
	correctionRead := requirePlatformCapabilityWithPII(authorizer, access.CapabilityTrainingRead, access.PIIScopeCalls)
	correctionWrite := requirePlatformCapabilityWithPII(authorizer, access.CapabilityTrainingWrite, access.PIIScopeCalls)
	group.Get("/knowledge-items", read, handler.ListKnowledge)
	group.Post("/knowledge-items", write, handler.CreateKnowledge)
	group.Put("/knowledge-items/:item_id", write, handler.UpdateKnowledge)
	group.Delete("/knowledge-items/:item_id", write, handler.DeleteKnowledge)
	group.Get("/owner-corrections", correctionRead, handler.ListCorrections)
	group.Post("/owner-corrections", correctionWrite, handler.CreateCorrection)
	group.Post("/owner-corrections/:correction_id/apply", correctionWrite, handler.ApplyCorrection)
	group.Post("/owner-corrections/:correction_id/apply-service-alias", correctionWrite, handler.ApplyServiceAliasCorrection)
	group.Post("/owner-corrections/:correction_id/dismiss", correctionWrite, handler.DismissCorrection)
	group.Post("/training/evaluate", read, handler.Evaluate)
}

func RegisterPlatformCallsCorrectionRoute(api fiber.Router, handler *Handler, authorizer platformAuthorizer, jwtSecret string) {
	group := api.Group("/platform/tenants/:id/calls", middleware.RequireAuth(jwtSecret))
	group.Post("/owner-corrections", requirePlatformCapabilityWithPII(authorizer, access.CapabilityCallsManage, access.PIIScopeCalls), handler.CreateCorrection)
}

func requirePlatformCapability(authorizer platformAuthorizer, capability access.Capability) fiber.Handler {
	return requirePlatformCapabilityWithPII(authorizer, capability, "")
}

func requirePlatformCapabilityWithPII(authorizer platformAuthorizer, capability access.Capability, piiScope access.PIIScope) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if authorizer == nil || authorizer.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("id"), Capability: capability, PIIScope: piiScope,
		}) != nil {
			return respond.Error(c, fiber.StatusForbidden, "SALON_FEATURE_ACCESS_FORBIDDEN", "This Platform account is not authorized for this salon feature.")
		}
		if authorizer.RecordPlatformSupportAction(c.UserContext(), middleware.Actor(c), c.Params("id"), capability, piiScope, c.Method(), c.Path()) != nil {
			return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
		}
		return c.Next()
	}
}
