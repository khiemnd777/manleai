package operationshealth

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type PlatformHandler struct {
	service *Service
	access  platformAuthorizer
}

func NewPlatformHandler(service *Service, authorizer platformAuthorizer) *PlatformHandler {
	return &PlatformHandler{service: service, access: authorizer}
}

func (h *PlatformHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.access == nil {
		return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_FORBIDDEN", "Operations access is not permitted.")
	}
	if err := h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: access.CapabilityOperationsRead,
	}); err != nil {
		return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_FORBIDDEN", "Operations access is not permitted.")
	}
	for _, piiScope := range []access.PIIScope{access.PIIScopeCalls, access.PIIScopeAppointments, access.PIIScopeNotifications} {
		if err := h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"),
			Capability: access.CapabilityOperationsRead, PIIScope: piiScope,
		}); err != nil {
			return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_PII_GRANT_REQUIRED", "Active calls, appointments, and notifications access grants are required for queue health.")
		}
	}
	status, err := h.service.GetForPlatform(c.UserContext(), c.Params("tenant_id"))
	switch {
	case err == nil:
		return respond.JSON(c, fiber.StatusOK, status)
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "OPERATIONS_STATUS_INVALID", "Operations status request is invalid.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "OPERATIONS_STATUS_FAILED", "Could not load operations status.")
	}
}
