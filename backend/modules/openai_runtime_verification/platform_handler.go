package openairuntimeverification

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

func (h *PlatformHandler) authorize(c *fiber.Ctx, capability access.Capability) error {
	if h == nil || h.access == nil {
		return access.ErrForbidden
	}
	return h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: capability,
	})
}

func (h *PlatformHandler) Verify(c *fiber.Ctx) error {
	var req VerifyRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, false, err)
	}
	status, replayed, err := h.service.Enqueue(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), req)
	return h.respond(c, status, replayed, err)
}

func (h *PlatformHandler) Status(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalRead); err != nil {
		return h.respond(c, nil, false, err)
	}
	status, err := h.service.Latest(c.UserContext(), c.Params("tenant_id"))
	return h.respond(c, status, false, err)
}

func (h *PlatformHandler) respond(c *fiber.Ctx, status *RunStatus, replayed bool, err error) error {
	if errors.Is(err, access.ErrForbidden) {
		return respond.Error(c, fiber.StatusForbidden, "TECHNICAL_FORBIDDEN", "This technical action is not permitted.")
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "OPENAI_VERIFICATION_INVALID", "OpenAI verification preflight failed.")
	}
	if errors.Is(err, ErrVersionConflict) {
		return respond.Error(c, fiber.StatusConflict, "OPENAI_VERIFICATION_VERSION_CONFLICT", "OpenAI settings changed. Reload before verifying.")
	}
	if errors.Is(err, ErrActionConflict) {
		return respond.Error(c, fiber.StatusConflict, "OPENAI_VERIFICATION_ACTION_CONFLICT", "This verification action key was already used for a different request.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "OPENAI_VERIFICATION_NOT_FOUND", "No OpenAI verification run exists for this tenant.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "OPENAI_VERIFICATION_FAILED", "OpenAI verification could not be completed.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"verification": status, "replayed": replayed})
}
