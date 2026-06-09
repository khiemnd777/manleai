package pos

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service *ServiceLayer
}

func NewHandler(service *ServiceLayer) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Services(c *fiber.Ctx) error {
	items, err := h.service.Services(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICES_FAILED", "Could not load services.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"services": items})
}

func (h *Handler) UpdateServiceAIBookable(c *fiber.Ctx) error {
	req, err := parseAIBookableRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include ai_bookable.")
	}
	item, err := h.service.UpdateServiceAIBookable(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"), *req.AIBookable)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service cannot be enabled for AI booking.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_NOT_FOUND", "Service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "Could not update service.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) Staff(c *fiber.Ctx) error {
	items, err := h.service.Staff(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_FAILED", "Could not load staff.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff": items})
}

func (h *Handler) UpdateStaffAIBookable(c *fiber.Ctx) error {
	req, err := parseAIBookableRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include ai_bookable.")
	}
	item, err := h.service.UpdateStaffAIBookable(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("staff_id"), *req.AIBookable)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Staff member cannot be enabled for AI booking.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "STAFF_NOT_FOUND", "Staff member was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_UPDATE_FAILED", "Could not update staff member.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff_member": item})
}

type updateAIBookableRequest struct {
	AIBookable *bool `json:"ai_bookable"`
}

func parseAIBookableRequest(c *fiber.Ctx) (*updateAIBookableRequest, error) {
	var req updateAIBookableRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	if req.AIBookable == nil {
		return nil, ErrValidation
	}
	return &req, nil
}
