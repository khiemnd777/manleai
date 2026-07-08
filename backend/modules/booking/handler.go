package booking

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

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

func (h *Handler) Availability(c *fiber.Ctx) error {
	var req AvailabilityRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	result, err := h.service.AvailableSlots(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Availability request is missing required service, date, staff, or salon schedule data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "AVAILABILITY_RESOURCE_NOT_FOUND", "Salon, service, or staff was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "AVAILABILITY_CHECK_FAILED", "Could not load available booking slots from the active POS provider.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) Calendar(c *fiber.Ctx) error {
	req, err := parseCalendarRangeQuery(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Calendar range is invalid.")
	}
	res, err := h.service.Calendar(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Calendar range is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CALENDAR_FAILED", "Could not load calendar.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) CalendarEventStream(c *fiber.Ctx) error {
	cursor, err := parseCalendarEventCursor(c.Query("cursor"))
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Calendar event cursor is invalid.")
	}
	if cursor.CreatedAt.IsZero() {
		cursor.CreatedAt = time.Now().UTC()
	}

	salonID := c.Params("id")
	ownerUserID := middleware.UserID(c)
	if err := h.service.EnsureCalendarEventAccess(c.UserContext(), salonID, ownerUserID); err != nil {
		if errors.Is(err, pos.ErrNotFound) {
			return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
		}
		return respond.Error(c, fiber.StatusInternalServerError, "CALENDAR_EVENTS_FAILED", "Could not open calendar event stream.")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		streamCalendarEvents(w, h.service, salonID, ownerUserID, cursor)
	})
	return nil
}

func (h *Handler) SyncCalendar(c *fiber.Ctx) error {
	var req CalendarSyncRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
		}
	}
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		rangeReq, err := parseCalendarRangeQuery(c)
		if err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Calendar sync range is invalid.")
		}
		req.StartTime = rangeReq.StartTime
		req.EndTime = rangeReq.EndTime
	}
	res, err := h.service.SyncCalendar(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Calendar sync range is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider cannot sync calendar appointments.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "CALENDAR_SYNC_FAILED", "Could not sync calendar from the active POS provider.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Appointments(c *fiber.Ctx) error {
	res, err := h.service.Appointments(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "APPOINTMENTS_FAILED", "Could not load appointments.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
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
	res, err := h.service.Attempts(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("status"), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Booking attempt status filter is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOKING_ATTEMPTS_FAILED", "Could not load booking attempts.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func streamCalendarEvents(w *bufio.Writer, service *Service, salonID string, ownerUserID string, cursor CalendarEventCursor) {
	pollTicker := time.NewTicker(2 * time.Second)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	if err := writeCalendarStreamComment(w, "connected"); err != nil {
		return
	}
	if err := w.Flush(); err != nil {
		return
	}

	for {
		events, err := service.CalendarEvents(context.Background(), salonID, ownerUserID, cursor, 20)
		if err != nil {
			_ = writeCalendarStreamError(w, "Calendar events are temporarily unavailable.")
			_ = w.Flush()
			return
		}
		for _, event := range events {
			if err := writeCalendarStreamEvent(w, event); err != nil {
				return
			}
			cursor = CalendarEventCursor{CreatedAt: event.CreatedAt, ID: event.ID}
		}
		if err := w.Flush(); err != nil {
			return
		}

		select {
		case <-pollTicker.C:
		case <-heartbeatTicker.C:
			if err := writeCalendarStreamComment(w, "heartbeat"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	}
}

func writeCalendarStreamEvent(w *bufio.Writer, event CalendarEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: calendar.booking\ndata: %s\n\n", event.Cursor, payload); err != nil {
		return err
	}
	return nil
}

func writeCalendarStreamComment(w *bufio.Writer, message string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", message)
	return err
}

func writeCalendarStreamError(w *bufio.Writer, message string) error {
	payload, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: calendar.error\ndata: %s\n\n", payload)
	return err
}

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}

func parseOffset(raw string) int {
	offset, _ := strconv.Atoi(raw)
	return offset
}

func parseCalendarRangeQuery(c *fiber.Ctx) (CalendarRangeRequest, error) {
	startTime, err := parseCalendarTime(c.Query("start"))
	if err != nil {
		return CalendarRangeRequest{}, err
	}
	endTime, err := parseCalendarTime(c.Query("end"))
	if err != nil {
		return CalendarRangeRequest{}, err
	}
	return CalendarRangeRequest{
		StartTime: startTime,
		EndTime:   endTime,
		View:      c.Query("view"),
	}, nil
}

func parseCalendarTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, ErrValidation
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed, nil
	}
	return time.Time{}, ErrValidation
}
