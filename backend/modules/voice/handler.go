package voice

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

func (h *Handler) Status(c *fiber.Ctx) error {
	status, err := h.service.Status(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_STATUS_INVALID", "Voice status request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_STATUS_FAILED", "Could not load voice status.")
	}
	return respond.JSON(c, fiber.StatusOK, status)
}

func (h *Handler) Audio(c *fiber.Ctx) error {
	output, err := h.service.Audio(c.UserContext(), c.Params("id"))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "VOICE_AUDIO_NOT_FOUND", "Voice audio was not found.")
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_AUDIO_INVALID", "Voice audio request is invalid.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_AUDIO_FAILED", "Could not load voice audio.")
	}
	c.Set(fiber.HeaderContentType, output.ContentType)
	return c.Status(fiber.StatusOK).Send(output.Audio)
}
