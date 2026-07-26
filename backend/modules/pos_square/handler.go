package pos_square

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Handler struct {
	service *Service
	cfg     config.Config
}

func (h *Handler) ListWebhookEvents(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(squareWebhookQueryDefault(c.Query("limit"), "25"))
	if err != nil {
		return h.webhookOperationsError(c, ErrWebhookOperationsValidation)
	}
	offset, err := strconv.Atoi(squareWebhookQueryDefault(c.Query("offset"), "0"))
	if err != nil {
		return h.webhookOperationsError(c, ErrWebhookOperationsValidation)
	}
	result, err := h.service.ListWebhookEvents(
		c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("status"), limit, offset,
	)
	if err != nil {
		return h.webhookOperationsError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) GetWebhookEvent(c *fiber.Ctx) error {
	result, err := h.service.GetWebhookEvent(
		c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("webhook_event_id"),
	)
	if err != nil {
		return h.webhookOperationsError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) RequeueWebhookEvent(c *fiber.Ctx) error {
	var req WebhookRequeueRequest
	if err := c.BodyParser(&req); err != nil {
		return h.webhookOperationsError(c, ErrWebhookOperationsValidation)
	}
	result, replayed, err := h.service.RequeueWebhookEvent(
		c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("webhook_event_id"), req,
	)
	if err != nil {
		return h.webhookOperationsError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) webhookOperationsError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrWebhookOperationsValidation):
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_WEBHOOK_OPERATIONS_INVALID", "Square webhook operations request is invalid.")
	case errors.Is(err, pos.ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	case errors.Is(err, ErrWebhookEventNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SQUARE_WEBHOOK_EVENT_NOT_FOUND", "Square webhook event was not found.")
	case errors.Is(err, ErrWebhookActionConflict):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_WEBHOOK_ACTION_CONFLICT", "The webhook action conflicts with an existing action.")
	case errors.Is(err, ErrWebhookRequeueBlocked):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_WEBHOOK_REQUEUE_BLOCKED", "This webhook event cannot be safely requeued.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "SQUARE_WEBHOOK_OPERATIONS_FAILED", "Could not process Square webhook operations.")
	}
}

func squareWebhookQueryDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func NewHandler(service *Service, cfg config.Config) *Handler {
	return &Handler{service: service, cfg: cfg}
}

func (h *Handler) ConnectURL(c *fiber.Ctx) error {
	salonID := strings.TrimSpace(c.Query("salon_id"))
	if salonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.ConnectURL(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CONNECT_URL_FAILED", "Square connection could not be prepared.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Callback(c *fiber.Ctx) error {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_SQUARE_CALLBACK", "Square callback is missing code or state.")
	}
	connection, err := h.service.HandleCallback(c.UserContext(), code, state, h.cfg.Square.RedirectURL)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CALLBACK_FAILED", "Square connection could not be completed.")
	}
	if c.Query("format") == "json" {
		return respond.JSON(c, fiber.StatusOK, connection)
	}
	redirect := h.cfg.FrontendURL + "/platform/tenants/" + connection.SalonID + "/technical?square=connected"
	return c.Redirect(redirect, fiber.StatusFound)
}

func (h *Handler) Webhook(c *fiber.Ctx) error {
	receipt, err := h.service.ReceiveBookingWebhook(
		c.UserContext(),
		c.Body(),
		c.Get("X-Square-HmacSha256-Signature"),
	)
	if errors.Is(err, ErrWebhookPayloadInvalid) {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_WEBHOOK_INVALID", "Square webhook payload is invalid.")
	}
	if errors.Is(err, ErrWebhookSignatureInvalid) {
		return respond.Error(c, fiber.StatusForbidden, "SQUARE_WEBHOOK_SIGNATURE_INVALID", "Square webhook signature is invalid.")
	}
	if errors.Is(err, ErrWebhookConfigMissing) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "SQUARE_WEBHOOK_NOT_CONFIGURED", "Square webhook verification is not configured.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "SQUARE_WEBHOOK_UNAVAILABLE", "Square webhook could not be persisted.")
	}
	return respond.JSON(c, fiber.StatusOK, receipt)
}

func (h *Handler) Status(c *fiber.Ctx) error {
	salonID := strings.TrimSpace(c.Query("salon_id"))
	if salonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.Status(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SQUARE_STATUS_FAILED", "Could not load Square status.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) BusinessReadiness(c *fiber.Ctx) error {
	readiness, err := h.service.Readiness(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "SCHEDULING_READINESS_UNAVAILABLE", "Scheduling readiness is temporarily unavailable.")
	}
	return respond.JSON(c, fiber.StatusOK, businessReadinessResponse(readiness))
}

func (h *Handler) Locations(c *fiber.Ctx) error {
	salonID := strings.TrimSpace(c.Query("salon_id"))
	if salonID == "" {
		return explicitSquareSalonIDError(c)
	}
	locations, err := h.service.Locations(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrNotConnected) {
		return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_LOCATIONS_FAILED", "Square locations could not be loaded.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"locations": locations})
}

type selectLocationRequest struct {
	SalonID    string `json:"salon_id"`
	LocationID string `json:"location_id"`
}

func (h *Handler) SelectLocation(c *fiber.Ctx) error {
	var req selectLocationRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	connection, err := h.service.SelectLocation(c.UserContext(), req.SalonID, middleware.UserID(c), req.LocationID)
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_LOCATION_FAILED", "The Square location could not be selected.")
	}
	return respond.JSON(c, fiber.StatusOK, connection)
}

type syncRequest struct {
	SalonID string `json:"salon_id"`
}

func (h *Handler) Sync(c *fiber.Ctx) error {
	var req syncRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	summary, err := h.service.Sync(c.UserContext(), req.SalonID, middleware.UserID(c))
	if err != nil {
		if errors.Is(err, pos.ErrNotFound) {
			return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
		}
		if errors.Is(err, ErrNotConnected) {
			return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
		}
		if errors.Is(err, pos.ErrStaleProviderSnapshot) {
			return respond.Error(c, fiber.StatusConflict, "SQUARE_SYNC_STALE", "The Square location or sync generation changed while this sync was running. Run Sync again for the selected location.")
		}
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_SYNC_FAILED", "Square sync could not be completed.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"ok": true, "summary": summary})
}

func (h *Handler) TestBooking(c *fiber.Ctx) error {
	var req TestBookingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "operation_key is required for a retry-safe test booking request.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.CreateTestBooking(c.UserContext(), req.SalonID, middleware.UserID(c), req)
	if handled, handleErr := h.handleGateError(c, err, "SQUARE_TEST_BOOKING_FAILED"); handled {
		return handleErr
	}
	if res == nil {
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_TEST_BOOKING_FAILED", "Square test booking did not return a response.")
	}
	status := fiber.StatusCreated
	if res.BookingAttempt != nil && (res.BookingAttempt.Status == booking.StatusFallbackPending || res.BookingAttempt.Status == booking.StatusPOSPending || res.BookingAttempt.Status == booking.StatusProviderPending) {
		status = fiber.StatusAccepted
	}
	return respond.JSON(c, status, res)
}

func (h *Handler) CancelTestBooking(c *fiber.Ctx) error {
	var req CancelTestBookingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "operation_key is required for a retry-safe test cancellation request.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.CancelTestBooking(c.UserContext(), req.SalonID, middleware.UserID(c), req)
	if handled, handleErr := h.handleGateError(c, err, "SQUARE_CANCEL_TEST_BOOKING_FAILED"); handled {
		return handleErr
	}
	if res == nil {
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_CANCEL_TEST_BOOKING_FAILED", "Square test booking cancellation did not return a response.")
	}
	status := fiber.StatusOK
	if res.BookingAttempt != nil && (res.BookingAttempt.Status == booking.StatusFallbackPending || res.BookingAttempt.Status == booking.StatusPOSPending || res.BookingAttempt.Status == booking.StatusProviderPending) {
		status = fiber.StatusAccepted
	}
	return respond.JSON(c, status, res)
}

func (h *Handler) EnableAIBooking(c *fiber.Ctx) error {
	var req GateRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.EnableAIBooking(c.UserContext(), req.SalonID, middleware.UserID(c))
	if handled, handleErr := h.handleGateError(c, err, "ENABLE_AI_BOOKING_FAILED"); handled {
		return handleErr
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) DisableAIBooking(c *fiber.Ctx) error {
	var req GateRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	req.SalonID = strings.TrimSpace(req.SalonID)
	if req.SalonID == "" {
		return explicitSquareSalonIDError(c)
	}
	res, err := h.service.DisableAIBooking(c.UserContext(), req.SalonID, middleware.UserID(c))
	if handled, handleErr := h.handleGateError(c, err, "DISABLE_AI_BOOKING_FAILED"); handled {
		return handleErr
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func explicitSquareSalonIDError(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusBadRequest, "SALON_ID_REQUIRED", "An explicit salon_id is required for this Square operation.")
}

func (h *Handler) handleGateError(c *fiber.Ctx, err error, internalCode string) (bool, error) {
	if err == nil {
		return false, nil
	}
	if errors.Is(err, pos.ErrNotFound) {
		return true, respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, booking.ErrValidation) {
		return true, respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Request is missing required booking readiness data.")
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteRequired) {
		return true, respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_REQUIRED", "Check current provider availability and choose a returned slot before creating a test booking.")
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		return true, respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_STALE", "Availability changed or expired. Check availability again before creating a test booking.")
	}
	if errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
		return true, respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_NOT_READY", "Scheduling is not ready for this salon.")
	}
	if errors.Is(err, ErrReadinessGate) {
		message := "Square readiness checks have not passed."
		if internalCode == "ENABLE_AI_BOOKING_FAILED" {
			message = "AI booking cannot be enabled until Square readiness checks pass."
		}
		return true, respond.Error(c, fiber.StatusConflict, "AI_BOOKING_NOT_READY", message)
	}
	if errors.Is(err, booking.ErrProviderUnavailable) || errors.Is(err, ErrBookingServiceUnavailable) {
		return true, respond.Error(c, fiber.StatusConflict, "POS_PROVIDER_UNAVAILABLE", "The active POS provider is unavailable.")
	}
	return true, respond.Error(c, fiber.StatusBadGateway, internalCode, "The request could not be completed.")
}
