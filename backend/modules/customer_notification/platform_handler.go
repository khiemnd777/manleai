package customernotification

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type PlatformHandler struct {
	delegate *Handler
	access   platformAuthorizer
}

func NewPlatformHandler(service handlerService, authorizer platformAuthorizer) *PlatformHandler {
	return &PlatformHandler{delegate: NewHandler(service), access: authorizer}
}

func (h *PlatformHandler) guard(capability access.Capability, next fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h == nil || h.access == nil {
			return respond.Error(c, fiber.StatusForbidden, "CUSTOMER_NOTIFICATION_FORBIDDEN", "Customer notification operations access is not permitted.")
		}
		if err := h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: capability,
		}); err != nil {
			return respond.Error(c, fiber.StatusForbidden, "CUSTOMER_NOTIFICATION_FORBIDDEN", "Customer notification operations access is not permitted.")
		}
		if err := h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"),
			Capability: access.CapabilityOperationsRead, PIIScope: access.PIIScopeNotifications,
		}); err != nil {
			return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_PII_GRANT_REQUIRED", "An active notifications access grant is required.")
		}
		return next(c)
	}
}
