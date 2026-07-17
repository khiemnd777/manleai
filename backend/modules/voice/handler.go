package voice

import (
	"context"
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

func (h *Handler) SemanticCheck(c *fiber.Ctx) error {
	status, err := h.service.SemanticCheck(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_CHECK_INVALID", "Semantic check request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_SEMANTIC_CHECK_UNAVAILABLE", "Semantic contract verification is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_SEMANTIC_CHECK_FAILED", "Could not verify the semantic contract.")
	}
	return respond.JSON(c, fiber.StatusOK, status)
}

func (h *Handler) SemanticEvaluate(c *fiber.Ctx) error {
	var req SemanticEvaluationRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_EVALUATION_INVALID", "Semantic evaluation request is invalid.")
	}
	result, err := h.service.SemanticEvaluate(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_EVALUATION_INVALID", "Semantic evaluation request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_SEMANTIC_EVALUATION_UNAVAILABLE", "Semantic evaluation is unavailable.")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return respond.Error(c, fiber.StatusGatewayTimeout, "VOICE_SEMANTIC_EVALUATION_TIMEOUT", "Semantic evaluation timed out.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "VOICE_SEMANTIC_EVALUATION_FAILED", "Could not evaluate the semantic turn.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
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
