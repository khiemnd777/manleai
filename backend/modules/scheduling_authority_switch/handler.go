package scheduling_authority_switch

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

func switchSalonID(c *fiber.Ctx) string {
	if value := c.Params("tenant_id"); value != "" {
		return value
	}
	return c.Params("id")
}

func (h *Handler) Preview(c *fiber.Ctx) error {
	var req PreviewRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR", "Scheduling authority switch preview input is invalid.")
	}
	result, err := h.service.Preview(c.UserContext(), switchSalonID(c), middleware.UserID(c), req)
	return respondSwitch(c, result, err)
}

func (h *Handler) Latest(c *fiber.Ctx) error {
	result, err := h.service.Latest(c.UserContext(), switchSalonID(c), middleware.UserID(c))
	return respondSwitch(c, result, err)
}

func (h *Handler) LatestV2(c *fiber.Ctx) error {
	result, err := h.service.Latest(c.UserContext(), switchSalonID(c), middleware.UserID(c))
	return respondChangeV2(c, result, err)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	result, err := h.service.Get(c.UserContext(), switchSalonID(c), middleware.UserID(c), c.Params("run_id"))
	return respondSwitch(c, result, err)
}

func (h *Handler) Commit(c *fiber.Ctx) error {
	var req CommitRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR", "Scheduling authority switch commit input is invalid.")
	}
	result, err := h.service.Commit(c.UserContext(), switchSalonID(c), middleware.UserID(c), c.Params("run_id"), req)
	return respondSwitch(c, result, err)
}

func (h *Handler) Change(c *fiber.Ctx) error {
	var req ChangeRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR", "Scheduling authority change input is invalid.")
	}
	result, err := h.service.Change(c.UserContext(), switchSalonID(c), middleware.UserID(c), req)
	return respondChangeV2(c, result, err)
}

func (h *Handler) PrepareChange(c *fiber.Ctx) error {
	var req ChangeRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR", "Scheduling authority readiness input is invalid.")
	}
	result, err := h.service.PrepareChange(c.UserContext(), switchSalonID(c), middleware.UserID(c), req)
	return respondChangeV2(c, result, err)
}

func respondChangeV2(c *fiber.Ctx, result *PreviewResponse, err error) error {
	if err != nil {
		return respondSwitch(c, nil, err)
	}
	actions := make([]string, 0, 1)
	if result.SwitchRun.Status == StatusPreviewBlocked {
		actions = append(actions, "retry_after_resolving_blockers")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{
		"data": result.SwitchRun,
		"meta": fiber.Map{
			"replayed":         result.Replayed,
			"resource_version": result.SwitchRun.ExpectedSourceAuthorityVersion,
			"permissions":      fiber.Map{"can_read": true, "allowed_actions": actions},
		},
	})
}

func respondSwitch(c *fiber.Ctx, body *PreviewResponse, err error) error {
	switch {
	case err == nil:
		return respond.JSON(c, fiber.StatusOK, body)
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "SCHEDULING_AUTHORITY_SWITCH_VALIDATION_ERROR", "Scheduling authority switch preview input is invalid.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SCHEDULING_AUTHORITY_SWITCH_NOT_FOUND", "Scheduling authority switch preview was not found.")
	case errors.Is(err, ErrOperationConflict):
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_SWITCH_OPERATION_CONFLICT", "This operation key is already assigned to a different scheduling authority switch preview.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_SWITCH_VERSION_CONFLICT", "Scheduling authority changed. Reload current settings before previewing the switch again.")
	case errors.Is(err, ErrReadinessConflict):
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_SWITCH_READINESS_CONFLICT", "Scheduling readiness changed. Create a fresh preview before committing.")
	case errors.Is(err, ErrLiveExecution):
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_SWITCH_LIVE_EXECUTION", "A provider booking operation is still live. Resolve it before switching scheduling authority.")
	case errors.Is(err, ErrStateConflict):
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_SWITCH_STATE_CONFLICT", "This scheduling authority switch run cannot be committed.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_AUTHORITY_SWITCH_FAILED", "Could not complete the scheduling authority switch preview.")
	}
}
