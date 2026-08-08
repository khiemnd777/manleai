package ai_runtime_control

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/pos"
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

func (handler *PlatformHandler) authorize(c *fiber.Ctx, capability access.Capability) error {
	if handler == nil || handler.access == nil {
		return access.ErrForbidden
	}
	return handler.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: capability,
	})
}

func (handler *PlatformHandler) Get(c *fiber.Ctx) error {
	if err := handler.authorize(c, access.CapabilityTechnicalRead); err != nil {
		return handler.respond(c, pos.AIRuntimeState{}, false, err, false)
	}
	canWrite := false
	if err := handler.authorize(c, access.CapabilityTechnicalWrite); err == nil {
		canWrite = true
	} else if !errors.Is(err, access.ErrForbidden) {
		return handler.respond(c, pos.AIRuntimeState{}, false, err, false)
	}
	state, err := handler.service.Get(c.UserContext(), c.Params("tenant_id"))
	return handler.respond(c, state, false, err, canWrite)
}

func (handler *PlatformHandler) Update(c *fiber.Ctx) error {
	var request UpdateRequest
	if err := c.BodyParser(&request); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "AI_RUNTIME_INVALID", "AI runtime command is invalid.")
	}
	if err := handler.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return handler.respond(c, pos.AIRuntimeState{}, false, err, true)
	}
	state, replayed, err := handler.service.Update(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), request)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return handler.respond(c, state, replayed, err, true)
}

func (handler *PlatformHandler) respond(c *fiber.Ctx, state pos.AIRuntimeState, replayed bool, err error, canWrite bool) error {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "AI_RUNTIME_FORBIDDEN", "This AI runtime action is not permitted.")
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "AI_RUNTIME_INVALID", "AI runtime command is invalid.")
	case errors.Is(err, pos.ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	case errors.Is(err, pos.ErrTechnicalVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "AI_RUNTIME_VERSION_CONFLICT", "AI runtime changed. Reload before saving again.")
	case errors.Is(err, pos.ErrTechnicalActionConflict):
		return respond.Error(c, fiber.StatusConflict, "AI_RUNTIME_ACTION_CONFLICT", "This action key is already assigned to a different AI runtime command.")
	case err != nil:
		return respond.Error(c, fiber.StatusInternalServerError, "AI_RUNTIME_FAILED", "Could not load or update the AI runtime.")
	}
	actions := []string{}
	if canWrite {
		actions = append(actions, "set_enabled")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{
		"data": state,
		"meta": fiber.Map{
			"replayed":         replayed,
			"resource_version": state.Version,
			"permissions":      fiber.Map{"can_read": true, "allowed_actions": actions},
		},
	})
}
