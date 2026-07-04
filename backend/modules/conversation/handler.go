package conversation

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

func (h *Handler) Start(c *fiber.Ctx) error {
	var req StartSessionRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
		}
	}
	session, err := h.service.Start(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_START_FAILED", "Could not start conversation session.")
	}
	return respond.JSON(c, fiber.StatusCreated, session)
}

func (h *Handler) Message(c *fiber.Ctx) error {
	var req MessageRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	session, err := h.service.Message(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Message text is required.")
	}
	if errors.Is(err, ErrSessionClosed) {
		return respond.Error(c, fiber.StatusConflict, "CONVERSATION_SESSION_CLOSED", "Start a new simulator session before sending another message.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_MESSAGE_FAILED", "Could not process simulator message.")
	}
	return respond.JSON(c, fiber.StatusOK, session)
}

func (h *Handler) List(c *fiber.Ctx) error {
	res, err := h.service.List(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")), c.Query("lifecycle_status"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation lifecycle filter is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATIONS_FAILED", "Could not load conversation sessions.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	session, err := h.service.Get(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_FAILED", "Could not load conversation session.")
	}
	return respond.JSON(c, fiber.StatusOK, session)
}

func (h *Handler) RealtimeEvents(c *fiber.Ctx) error {
	events, err := h.service.ListWebhookEvents(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"), parseLimit(c.Query("limit")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_REALTIME_EVENTS_FAILED", "Could not load realtime events.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"events": events})
}

func (h *Handler) ListPartyBookingRequests(c *fiber.Ctx) error {
	items, err := h.service.ListPartyBookingRequests(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("status"), parseLimit(c.Query("limit")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Party request filter is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PARTY_REQUESTS_FAILED", "Could not load party booking requests.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"party_booking_requests": items})
}

func (h *Handler) UpdatePartyBookingRequestStatus(c *fiber.Ctx) error {
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.UpdatePartyBookingRequestStatus(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("request_id"), req.Status)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Party request status is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PARTY_REQUEST_NOT_FOUND", "Party booking request was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PARTY_REQUEST_UPDATE_FAILED", "Could not update party booking request.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"party_booking_request": item})
}

func (h *Handler) Archive(c *fiber.Ctx) error {
	session, err := h.service.Archive(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrLifecycle) {
		return respond.Error(c, fiber.StatusConflict, "CONVERSATION_LIFECYCLE_CONFLICT", "Conversation session cannot be archived.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_ARCHIVE_FAILED", "Could not archive conversation session.")
	}
	return respond.JSON(c, fiber.StatusOK, session)
}

func (h *Handler) Redact(c *fiber.Ctx) error {
	session, err := h.service.Redact(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrLifecycle) {
		return respond.Error(c, fiber.StatusConflict, "CONVERSATION_LIFECYCLE_CONFLICT", "Active or already incompatible conversation sessions cannot be redacted.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_REDACT_FAILED", "Could not redact conversation session.")
	}
	return respond.JSON(c, fiber.StatusOK, session)
}

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}

func parseOffset(raw string) int {
	offset, _ := strconv.Atoi(raw)
	return offset
}
