package operationshealth

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Get(c *fiber.Ctx) error {
	status, err := h.service.Get(c.UserContext(), c.Params("id"), middleware.UserID(c))
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
