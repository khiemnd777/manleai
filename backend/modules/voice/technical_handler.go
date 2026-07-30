package voice

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type technicalAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type TechnicalHandler struct {
	service *Service
	access  technicalAuthorizer
}

func NewTechnicalHandler(service *Service, authorizer technicalAuthorizer) *TechnicalHandler {
	return &TechnicalHandler{service: service, access: authorizer}
}

func (h *TechnicalHandler) TwilioVoiceRoutingStatus(c *fiber.Ctx) error {
	if h == nil || h.access == nil || h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: access.CapabilityTechnicalRead,
	}) != nil {
		return respond.Error(c, fiber.StatusForbidden, "TECHNICAL_FORBIDDEN", "This technical action is not permitted.")
	}
	status, err := h.service.TwilioVoiceRoutingStatus(c.UserContext(), c.Params("tenant_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "TWILIO_VOICE_ROUTING_STATUS_INVALID", "Voice routing status request is invalid.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_VOICE_ROUTING_STATUS_FAILED", "Could not load Voice routing status.")
	}
	return respond.JSON(c, fiber.StatusOK, status)
}
