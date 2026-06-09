package booking

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateBookingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	attempt, err := h.service.Create(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Booking request is missing required customer, service, staff, or start time data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "BOOKING_RESOURCE_NOT_FOUND", "Salon, service, or staff was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOKING_CREATE_FAILED", "Could not create booking request.")
	}
	status := fiber.StatusCreated
	if attempt.Status == StatusFallbackPending {
		status = fiber.StatusAccepted
	}
	return respond.JSON(c, status, attempt)
}

func (h *Handler) Appointments(c *fiber.Ctx) error {
	items, err := h.service.Appointments(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "APPOINTMENTS_FAILED", "Could not load appointments.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"appointments": items})
}

func (h *Handler) Reschedule(c *fiber.Ctx) error {
	var req RescheduleRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	appointment, fallback, err := h.service.Reschedule(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("appointment_id"), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Reschedule request is missing required appointment, POS version, staff, service, or start time data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "APPOINTMENT_NOT_FOUND", "Appointment was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "APPOINTMENT_RESCHEDULE_FAILED", "Could not reschedule appointment.")
	}
	if fallback != nil {
		return respond.JSON(c, fiber.StatusAccepted, fallback)
	}
	return respond.JSON(c, fiber.StatusOK, appointment)
}

func (h *Handler) Cancel(c *fiber.Ctx) error {
	var req CancelRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
		}
	}
	appointment, fallback, err := h.service.Cancel(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("appointment_id"), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Cancel request is missing required appointment or POS version data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "APPOINTMENT_NOT_FOUND", "Appointment was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "APPOINTMENT_CANCEL_FAILED", "Could not cancel appointment.")
	}
	if fallback != nil {
		return respond.JSON(c, fiber.StatusAccepted, fallback)
	}
	return respond.JSON(c, fiber.StatusOK, appointment)
}

func (h *Handler) Attempts(c *fiber.Ctx) error {
	items, err := h.service.Attempts(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOKING_ATTEMPTS_FAILED", "Could not load booking attempts.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"booking_attempts": items})
}

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}
