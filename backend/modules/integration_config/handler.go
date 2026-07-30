package integrationconfig

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetAll(c *fiber.Ctx) error {
	res, err := h.service.GetAll(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "INTEGRATION_CONFIG_INVALID", "Integration config request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "INTEGRATION_CONFIG_FAILED", "Could not load integration configuration.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) UpdateSquare(c *fiber.Ctx) error {
	var req UpdateSquareSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	res, err := h.service.UpdateSquare(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	return h.respondMutation(c, res, err, "SQUARE_CONFIG_UPDATE_FAILED")
}

func (h *Handler) UpdateTwilio(c *fiber.Ctx) error {
	var req UpdateTwilioSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	res, err := h.service.UpdateTwilio(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	return h.respondMutation(c, res, err, "TWILIO_CONFIG_UPDATE_FAILED")
}

func (h *Handler) UpdateOpenAI(c *fiber.Ctx) error {
	var req UpdateOpenAISettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	res, err := h.service.UpdateOpenAI(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	return h.respondMutation(c, res, err, "OPENAI_CONFIG_UPDATE_FAILED")
}

func (h *Handler) respondMutation(c *fiber.Ctx, res any, err error, code string) error {
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "INTEGRATION_CONFIG_INVALID", "Integration config values are invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrTwilioVoiceNumberConflict) {
		return respond.Error(c, fiber.StatusConflict, "TWILIO_VOICE_NUMBER_CONFLICT", "This Voice inbound number is already assigned to another active route.")
	}
	if errors.Is(err, ErrOpenAICredentialConflict) {
		return respond.Error(c, fiber.StatusConflict, "OPENAI_CREDENTIAL_TENANT_CONFLICT", "This OpenAI API key is already assigned to another tenant.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, code, "Could not update integration configuration.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}
