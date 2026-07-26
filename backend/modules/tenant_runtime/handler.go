package tenantruntime

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Get(c *fiber.Ctx) error {
	window, err := strconv.Atoi(strings.TrimSpace(c.Query("window_minutes", "60")))
	if err != nil {
		return respondRuntime(c, nil, false, ErrValidation)
	}
	result, err := h.service.GetProfile(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), window)
	return respondRuntime(c, result, false, err)
}

func (h *Handler) Update(c *fiber.Ctx) error {
	var req UpdateLimitsRequest
	if err := c.BodyParser(&req); err != nil {
		return respondRuntime(c, nil, false, ErrValidation)
	}
	result, replayed, err := h.service.UpdateLimits(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), req)
	return respondRuntime(c, result, replayed, err)
}

func respondRuntime(c *fiber.Ctx, value any, replayed bool, err error) error {
	switch {
	case err == nil:
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
		return respond.JSON(c, fiber.StatusOK, value)
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "TENANT_RUNTIME_INVALID", "Tenant runtime request is invalid.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_FORBIDDEN", "Tenant runtime operations access is not permitted.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "TENANT_RUNTIME_NOT_FOUND", "Tenant runtime limits were not found.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "TENANT_RUNTIME_VERSION_CONFLICT", "Tenant runtime limits changed. Refresh and try again.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "TENANT_RUNTIME_ACTION_CONFLICT", "This action key is already assigned to different limits.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "TENANT_RUNTIME_FAILED", "Could not process tenant runtime controls.")
	}
}
