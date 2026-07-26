package scheduling_manleai_calendar

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

func salonIDFromRequest(c *fiber.Ctx) string {
	if value := c.Params("tenant_id"); value != "" {
		return value
	}
	return c.Params("id")
}

func (h *Handler) GetAggregate(c *fiber.Ctx) error {
	result, err := h.service.GetAggregate(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c))
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) PutConfig(c *fiber.Ctx) error {
	var req CalendarConfigInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.PutConfig(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) PutHours(c *fiber.Ctx) error {
	var req ReplaceBusinessHoursInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.PutHours(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) GetStaff(c *fiber.Ctx) error {
	result, err := h.service.GetStaffProfile(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("staff_id"))
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) PutStaff(c *fiber.Ctx) error {
	var req StaffProfileInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.PutStaffProfile(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("staff_id"), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) GetService(c *fiber.Ctx) error {
	result, err := h.service.GetServicePolicy(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("service_id"))
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) PutService(c *fiber.Ctx) error {
	var req ServicePolicyInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.PutServicePolicy(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("service_id"), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) ListResources(c *fiber.Ctx) error {
	result, err := h.service.ListResources(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c))
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) CreateResource(c *fiber.Ctx) error {
	var req ResourcePoolInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.CreateResource(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), req)
	return respondCalendar(c, fiber.StatusCreated, result, err)
}

func (h *Handler) UpdateResource(c *fiber.Ctx) error {
	var req ResourcePoolInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.UpdateResource(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("resource_id"), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) ArchiveResource(c *fiber.Ctx) error {
	var req MutationMeta
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.ArchiveResource(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("resource_id"), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) CreateException(c *fiber.Ctx) error {
	var req ExceptionInput
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.CreateException(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), req)
	return respondCalendar(c, fiber.StatusCreated, result, err)
}

func (h *Handler) CancelException(c *fiber.Ctx) error {
	var req MutationMeta
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.CancelException(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), c.Params("exception_id"), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func (h *Handler) Activate(c *fiber.Ctx) error {
	var req MutationMeta
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	result, err := h.service.Activate(c.UserContext(), salonIDFromRequest(c), middleware.UserID(c), req)
	return respondCalendar(c, fiber.StatusOK, result, err)
}

func invalidBody(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusBadRequest, "MANLEAI_CALENDAR_VALIDATION_ERROR", "Request body is invalid.")
}

func respondCalendar(c *fiber.Ctx, successStatus int, body any, err error) error {
	switch {
	case err == nil:
		return respond.JSON(c, successStatus, body)
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "MANLEAI_CALENDAR_VALIDATION_ERROR", "ManleAI Calendar input is invalid.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "MANLEAI_CALENDAR_NOT_FOUND", "Salon or calendar resource was not found.")
	case errors.Is(err, ErrConfigRequired):
		return respond.Error(c, fiber.StatusConflict, "MANLEAI_CALENDAR_CONFIG_REQUIRED", "Create the ManleAI Calendar configuration before changing calendar details.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "MANLEAI_CALENDAR_CONFIG_VERSION_CONFLICT", "The calendar configuration changed. Reload it before trying again.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "MANLEAI_CALENDAR_ACTION_CONFLICT", "This action key is already assigned to different calendar data.")
	case errors.Is(err, ErrNotReady):
		return respond.Error(c, fiber.StatusConflict, "MANLEAI_CALENDAR_NOT_READY", "Resolve all configuration blockers before activating the calendar.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "MANLEAI_CALENDAR_FAILED", "Could not complete the ManleAI Calendar request.")
	}
}
