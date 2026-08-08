package scheduling_behavior

import (
	"context"
	"errors"
	"strconv"

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
		return handler.respondState(c, State{}, err, false)
	}
	canWrite := false
	if err := handler.authorize(c, access.CapabilityTechnicalWrite); err == nil {
		canWrite = true
	} else if !errors.Is(err, access.ErrForbidden) {
		return handler.respondState(c, State{}, err, false)
	}
	state, err := handler.service.Get(c.UserContext(), c.Params("tenant_id"))
	return handler.respondState(c, state, err, canWrite)
}

func (handler *PlatformHandler) UpdateBookingMode(c *fiber.Ctx) error {
	var request UpdateBookingModeRequest
	if err := c.BodyParser(&request); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_BEHAVIOR_INVALID", "The AI booking mode command is invalid.")
	}
	if err := handler.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return handler.respondMutation(c, BookingModeMutationResult{}, false, err)
	}
	result, replayed, err := handler.service.UpdateBookingMode(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), request)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return handler.respondMutation(c, result, replayed, err)
}

func (handler *PlatformHandler) respondState(c *fiber.Ctx, state State, err error, canWrite bool) error {
	if handled, response := schedulingBehaviorError(c, err); handled {
		return response
	}
	actions := []string{}
	if canWrite {
		actions = append(actions, "set_authority", "set_booking_mode")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{
		"data": state,
		"meta": fiber.Map{
			"replayed": false, "resource_version": state.PolicyVersion,
			"permissions": fiber.Map{"can_read": true, "allowed_actions": actions},
		},
	})
}

func (handler *PlatformHandler) respondMutation(c *fiber.Ctx, result BookingModeMutationResult, replayed bool, err error) error {
	if handled, response := schedulingBehaviorError(c, err); handled {
		return response
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{
		"data": result,
		"meta": fiber.Map{
			"replayed": replayed, "resource_version": result.Version,
			"permissions": fiber.Map{"can_read": true, "allowed_actions": []string{"set_booking_mode"}},
		},
	})
}

func schedulingBehaviorError(c *fiber.Ctx, err error) (bool, error) {
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, access.ErrForbidden):
		return true, respond.Error(c, fiber.StatusForbidden, "SCHEDULING_BEHAVIOR_FORBIDDEN", "This scheduling behavior action is not permitted.")
	case errors.Is(err, ErrValidation):
		return true, respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_BEHAVIOR_INVALID", "The scheduling behavior command is invalid.")
	case errors.Is(err, ErrNotFound):
		return true, respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	case errors.Is(err, ErrVersionConflict):
		return true, respond.Error(c, fiber.StatusConflict, "SCHEDULING_BEHAVIOR_VERSION_CONFLICT", "Scheduling behavior changed. Reload before saving again.")
	case errors.Is(err, ErrActionConflict):
		return true, respond.Error(c, fiber.StatusConflict, "SCHEDULING_BEHAVIOR_ACTION_CONFLICT", "This action key is already assigned to a different scheduling behavior command.")
	case errors.Is(err, ErrIncompatibleMode):
		return true, respond.Error(c, fiber.StatusConflict, "SCHEDULING_BEHAVIOR_INCOMPATIBLE", "This AI booking mode is not compatible with the selected scheduling authority.")
	default:
		return true, respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_BEHAVIOR_FAILED", "Could not load or update scheduling behavior.")
	}
}
