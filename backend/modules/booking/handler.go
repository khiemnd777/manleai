package booking

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Handler struct {
	service HandlerService
}

type HandlerService interface {
	AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req AvailabilityRequest) (*AvailabilityResult, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req CreateBookingRequest) (*BookingAttempt, error)
	Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req RescheduleRequest) (*Appointment, *BookingAttempt, error)
	Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req CancelRequest) (*Appointment, *BookingAttempt, error)
	Calendar(ctx context.Context, salonID string, ownerUserID string, req CalendarRangeRequest) (*CalendarRangeResponse, error)
	EnsureCalendarEventAccess(ctx context.Context, salonID string, ownerUserID string) error
	CalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor CalendarEventCursor, limit int) ([]CalendarEvent, error)
	SyncCalendar(ctx context.Context, salonID string, ownerUserID string, req CalendarSyncRequest) (*CalendarSyncResponse, error)
	Appointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) (*ListAppointmentsResponse, error)
	Attempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*ListBookingAttemptsResponse, error)
	ReconciliationTasks(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*ListReconciliationTasksResponse, error)
	ReconciliationCandidates(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*ListReconciliationCandidatesResponse, error)
	ResolveReconciliation(ctx context.Context, salonID string, ownerUserID string, attemptID string, req ResolveReconciliationRequest) (*ReconciliationTask, error)
}

func NewHandler(service HandlerService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateBookingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "operation_key is required for a retry-safe booking request.")
	}
	req.Source = SourceOwnerDashboard
	attempt, err := h.service.Create(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrSchedulingAuthorityNotReady) {
		return schedulingAuthorityNotReadyResponse(c)
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Booking request is missing required customer, service, staff, or start time data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "BOOKING_RESOURCE_NOT_FOUND", "Salon, service, or staff was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if errors.Is(err, ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "BOOKING_OPERATION_CONFLICT", "This operation key is already assigned to a different booking request.")
	}
	if errors.Is(err, ErrAvailabilityQuoteRequired) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_REQUIRED", "Check current provider availability and choose a returned slot before booking.")
	}
	if errors.Is(err, ErrAvailabilityQuoteStale) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_STALE", "Availability changed or expired. Check availability again before booking.")
	}
	if errors.Is(err, ErrSlotCommitConflict) {
		return respond.Error(c, fiber.StatusConflict, "SLOT_COMMIT_CONFLICT", "The selected time is no longer available. Check availability again before booking.")
	}
	if errors.Is(err, ErrSlotClaimInProgress) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_CLAIM_IN_PROGRESS", "This exact booking operation is still in progress.")
	}
	if errors.Is(err, ErrSlotOutcomeUnknown) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_OUTCOME_UNKNOWN", "The provider outcome cannot be confirmed safely yet.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOKING_CREATE_FAILED", "Could not create booking request.")
	}
	status := fiber.StatusCreated
	if attempt.Status == StatusFallbackPending || attempt.Status == StatusPOSPending || attempt.Status == StatusProviderPending {
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
	if errors.Is(err, ErrSchedulingAuthorityNotReady) {
		return schedulingAuthorityNotReadyResponse(c)
	}
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
	if strings.TrimSpace(req.OperationKey) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "operation_key is required for a retry-safe reschedule request.")
	}
	req.Source = SourceOwnerDashboard
	appointment, fallback, err := h.service.Reschedule(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("appointment_id"), req)
	if errors.Is(err, ErrSchedulingAuthorityNotReady) {
		return schedulingAuthorityNotReadyResponse(c)
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Reschedule request is missing required appointment, POS version, staff, service, or start time data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "APPOINTMENT_NOT_FOUND", "Appointment was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if errors.Is(err, ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "BOOKING_OPERATION_CONFLICT", "This operation key is already assigned to a different reschedule request.")
	}
	if errors.Is(err, ErrAvailabilityQuoteRequired) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_REQUIRED", "Check current provider availability and choose a returned slot before rescheduling.")
	}
	if errors.Is(err, ErrAvailabilityQuoteStale) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_STALE", "Availability changed or expired. Check availability again before rescheduling.")
	}
	if errors.Is(err, ErrSlotCommitConflict) {
		return respond.Error(c, fiber.StatusConflict, "SLOT_COMMIT_CONFLICT", "The selected time is no longer available. Check availability again before rescheduling.")
	}
	if errors.Is(err, ErrSlotClaimInProgress) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_CLAIM_IN_PROGRESS", "This exact reschedule operation is still in progress.")
	}
	if errors.Is(err, ErrSlotOutcomeUnknown) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_OUTCOME_UNKNOWN", "The provider reschedule outcome cannot be confirmed safely yet.")
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
	if strings.TrimSpace(req.OperationKey) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "operation_key is required for a retry-safe cancellation request.")
	}
	req.Source = SourceOwnerDashboard
	appointment, fallback, err := h.service.Cancel(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("appointment_id"), req)
	if errors.Is(err, ErrSchedulingAuthorityNotReady) {
		return schedulingAuthorityNotReadyResponse(c)
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Cancel request is missing required appointment or POS version data.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "APPOINTMENT_NOT_FOUND", "Appointment was not found.")
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	if errors.Is(err, ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "BOOKING_OPERATION_CONFLICT", "This operation key is already assigned to a different cancellation request.")
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

func (h *Handler) ReconciliationTasks(c *fiber.Ctx) error {
	res, err := h.service.ReconciliationTasks(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("status"), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Reconciliation status filter is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "RECONCILIATION_TASKS_FAILED", "Could not load booking reconciliation tasks.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) ReconciliationCandidates(c *fiber.Ctx) error {
	res, err := h.service.ReconciliationCandidates(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("attempt_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Booking reconciliation attempt is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "RECONCILIATION_TASK_NOT_FOUND", "Reconciliation task was not found.")
	}
	if errors.Is(err, ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "RECONCILIATION_CONFLICT", "This reconciliation task is already resolved or no longer requires provider matching.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "RECONCILIATION_CANDIDATES_FAILED", "Could not load verified provider booking candidates.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) ResolveReconciliation(c *fiber.Ctx) error {
	var req ResolveReconciliationRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	task, err := h.service.ResolveReconciliation(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("attempt_id"), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Reconciliation action or provider result is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "RECONCILIATION_TASK_NOT_FOUND", "Reconciliation task was not found.")
	}
	if errors.Is(err, ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "RECONCILIATION_CONFLICT", "This reconciliation task was already resolved or no longer matches the booking attempt.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "RECONCILIATION_RESOLVE_FAILED", "Could not resolve booking reconciliation task.")
	}
	return respond.JSON(c, fiber.StatusOK, task)
}

func schedulingAuthorityNotReadyResponse(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_NOT_READY", "The salon's scheduling authority is not ready for booking actions.")
}

func streamCalendarEvents(w *bufio.Writer, service HandlerService, salonID string, ownerUserID string, cursor CalendarEventCursor) {
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
