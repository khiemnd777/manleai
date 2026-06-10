package customer

import (
	"errors"
	"strconv"

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

func (h *Handler) List(c *fiber.Ctx) error {
	res, err := h.service.List(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Customer request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CUSTOMERS_FAILED", "Could not load customers.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) SearchPOS(c *fiber.Ctx) error {
	res, err := h.service.SearchPOS(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("provider"), c.Query("phone"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Phone number is required for customer lookup.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The requested POS provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "POS_CUSTOMER_LOOKUP_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}
