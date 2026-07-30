package integrationconfig

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

func (h *PlatformHandler) GetAll(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalRead); err != nil {
		return h.respond(c, nil, err, "INTEGRATION_CONFIG_FAILED")
	}
	res, err := h.service.getAllForSalon(c.UserContext(), c.Params("tenant_id"))
	return h.respond(c, res, err, "INTEGRATION_CONFIG_FAILED")
}

func (h *PlatformHandler) UpdateSquare(c *fiber.Ctx) error {
	var req UpdateSquareSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "SQUARE_CONFIG_UPDATE_FAILED")
	}
	res, err := h.service.UpdateSquareForPlatform(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), req)
	return h.respond(c, res, err, "SQUARE_CONFIG_UPDATE_FAILED")
}

func (h *PlatformHandler) UpdateTwilio(c *fiber.Ctx) error {
	var req UpdateTwilioSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "TWILIO_CONFIG_UPDATE_FAILED")
	}
	res, err := h.service.UpdateTwilioForPlatform(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), req)
	return h.respond(c, res, err, "TWILIO_CONFIG_UPDATE_FAILED")
}

func (h *PlatformHandler) UpdateOpenAI(c *fiber.Ctx) error {
	var req UpdateOpenAISettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "OPENAI_CONFIG_UPDATE_FAILED")
	}
	res, err := h.service.UpdateOpenAIForPlatform(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), req)
	return h.respond(c, res, err, "OPENAI_CONFIG_UPDATE_FAILED")
}

func (h *PlatformHandler) respond(c *fiber.Ctx, value any, err error, code string) error {
	if errors.Is(err, access.ErrForbidden) {
		return respond.Error(c, fiber.StatusForbidden, "TECHNICAL_FORBIDDEN", "This technical action is not permitted.")
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "INTEGRATION_CONFIG_INVALID", "Integration config values are invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrVersionConflict) {
		return respond.Error(c, fiber.StatusConflict, "TECHNICAL_VERSION_CONFLICT", "Technical settings changed. Reload before saving again.")
	}
	if errors.Is(err, ErrActionConflict) {
		return respond.Error(c, fiber.StatusConflict, "TECHNICAL_ACTION_CONFLICT", "This technical action key was already used for a different request.")
	}
	if errors.Is(err, ErrTwilioVoiceNumberConflict) {
		return respond.Error(c, fiber.StatusConflict, "TWILIO_VOICE_NUMBER_CONFLICT", "This Voice inbound number is already assigned to another active route.")
	}
	if errors.Is(err, ErrOpenAICredentialConflict) {
		return respond.Error(c, fiber.StatusConflict, "OPENAI_CREDENTIAL_TENANT_CONFLICT", "This OpenAI API key is already assigned to another tenant. Rotate one tenant credential before retrying.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, code, "Could not complete integration configuration.")
	}
	return respond.JSON(c, fiber.StatusOK, value)
}
