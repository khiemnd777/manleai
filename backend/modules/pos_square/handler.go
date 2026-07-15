package pos_square

import (
	"errors"
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

func NewHandler(service *Service, cfg config.Config) *Handler {
	return &Handler{service: service, cfg: cfg}
}

func (h *Handler) ConnectURL(c *fiber.Ctx) error {
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
	}
	res, err := h.service.ConnectURL(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CONNECT_URL_FAILED", err.Error())
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
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CALLBACK_FAILED", err.Error())
	}
	if c.Query("format") == "json" {
		return respond.JSON(c, fiber.StatusOK, connection)
	}
	redirect := h.cfg.FrontendURL + "/dashboard/integrations?square=connected&salon_id=" + connection.SalonID
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
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
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

func (h *Handler) Locations(c *fiber.Ctx) error {
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
	}
	locations, err := h.service.Locations(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrNotConnected) {
		return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_LOCATIONS_FAILED", err.Error())
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
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
	}
	connection, err := h.service.SelectLocation(c.UserContext(), req.SalonID, middleware.UserID(c), req.LocationID)
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_LOCATION_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, connection)
}

type syncRequest struct {
	SalonID string `json:"salon_id"`
}

func (h *Handler) Sync(c *fiber.Ctx) error {
	var req syncRequest
	_ = c.BodyParser(&req)
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
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
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_SYNC_FAILED", err.Error())
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
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
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
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
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
	_ = c.BodyParser(&req)
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
	}
	res, err := h.service.EnableAIBooking(c.UserContext(), req.SalonID, middleware.UserID(c))
	if handled, handleErr := h.handleGateError(c, err, "ENABLE_AI_BOOKING_FAILED"); handled {
		return handleErr
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) DisableAIBooking(c *fiber.Ctx) error {
	var req GateRequest
	_ = c.BodyParser(&req)
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
	}
	res, err := h.service.DisableAIBooking(c.UserContext(), req.SalonID, middleware.UserID(c))
	if handled, handleErr := h.handleGateError(c, err, "DISABLE_AI_BOOKING_FAILED"); handled {
		return handleErr
	}
	return respond.JSON(c, fiber.StatusOK, res)
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
	return true, respond.Error(c, fiber.StatusBadGateway, internalCode, err.Error())
}
