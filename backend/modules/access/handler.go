package access

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

func (h *Handler) ListUsers(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "25"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListUsers(c.UserContext(), middleware.Actor(c), c.Query("query"), limit)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListMemberships(c *fiber.Ctx) error {
	result, err := h.service.ListMemberships(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutateMembership(c *fiber.Ctx) error {
	var req MembershipMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateMembership(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListPlatformRoles(c *fiber.Ctx) error {
	result, err := h.service.ListPlatformRoles(c.UserContext(), middleware.Actor(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListCapabilities(c *fiber.Ctx) error {
	result, err := h.service.ListCapabilities(c.UserContext(), middleware.Actor(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutatePlatformRole(c *fiber.Ctx) error {
	var req PlatformRoleMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutatePlatformRole(c.UserContext(), middleware.Actor(c), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListSalonAssignments(c *fiber.Ctx) error {
	result, err := h.service.ListSalonAssignments(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutateSalonAssignment(c *fiber.Ctx) error {
	var req SalonAssignmentMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateSalonAssignment(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListPIIGrants(c *fiber.Ctx) error {
	result, err := h.service.ListPIIGrants(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) GrantPIIAccess(c *fiber.Ctx) error {
	var req PIIGrantRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.GrantPIIAccess(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusCreated, result)
}

func (h *Handler) RevokePIIAccess(c *fiber.Ctx) error {
	var req PIIGrantRevokeRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.RevokePIIAccess(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("grant_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListAuditEvents(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "50"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	offset, err := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListAuditEvents(c.UserContext(), middleware.Actor(c), c.Query("salon_id"), limit, offset)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) handleError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "ACCESS_INVALID", "Access-management request is invalid.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "ACCESS_FORBIDDEN", "This action is not permitted.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "ACCESS_RECORD_NOT_FOUND", "The requested access record was not found.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "ACCESS_VERSION_CONFLICT", "Access state changed. Reload and retry with the current version.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "ACCESS_ACTION_CONFLICT", "The action key was already used with different input.")
	case errors.Is(err, ErrLastAdmin):
		return respond.Error(c, fiber.StatusConflict, "LAST_PLATFORM_ADMIN", "The last active platform administrator cannot be removed.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "ACCESS_OPERATION_FAILED", "Could not complete the access-management operation.")
	}
}

func defaultQuery(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
