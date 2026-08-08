package conversation

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service    *Service
	normalized bool
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func NewNormalizedHandler(service *Service) *Handler {
	return &Handler{service: service, normalized: true}
}

func conversationSalonID(c *fiber.Ctx) string {
	if value := c.Params("tenant_id"); value != "" {
		return value
	}
	return c.Params("id")
}

func (h *Handler) Start(c *fiber.Ctx) error {
	var req StartSessionRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
		}
	}
	session, err := h.service.Start(c.UserContext(), conversationSalonID(c), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_START_FAILED", "Could not start conversation session.")
	}
	return h.resource(c, fiber.StatusCreated, session, session.StateRevision)
}

func (h *Handler) Message(c *fiber.Ctx) error {
	var req MessageRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	session, err := h.service.Message(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("session_id"), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Message text is required.")
	}
	if errors.Is(err, ErrSessionClosed) {
		return respond.Error(c, fiber.StatusConflict, "CONVERSATION_SESSION_CLOSED", "Start a new simulator session before sending another message.")
	}
	if errors.Is(err, ErrSessionStateConflict) {
		return respond.Error(c, fiber.StatusConflict, "CONVERSATION_STATE_CONFLICT", "The conversation changed while this message was being processed. Retry the same message.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_MESSAGE_FAILED", "Could not process simulator message.")
	}
	return h.resource(c, fiber.StatusOK, session, session.StateRevision)
}

func (h *Handler) List(c *fiber.Ctx) error {
	res, err := h.service.List(c.UserContext(), conversationSalonID(c), middleware.UserID(c), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")), c.Query("lifecycle_status"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation lifecycle filter is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATIONS_FAILED", "Could not load conversation sessions.")
	}
	if h.normalized {
		return h.resource(c, fiber.StatusOK, res.Sessions, 0, fiber.Map{"limit": res.Limit, "offset": res.Offset, "has_more": res.HasMore})
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	session, err := h.service.Get(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("session_id"))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		if stage, ok := conversationDetailReadStage(err); ok {
			requestID := ""
			if value := c.Locals("requestid"); value != nil {
				requestID = fmt.Sprint(value)
			}
			log.Printf("conversation detail read failed request_id=%q tenant_id=%q session_id=%q stage=%q", requestID, conversationSalonID(c), c.Params("session_id"), stage)
			return respondConversationDetailReadError(c, stage)
		}
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_FAILED", "Could not load conversation session.")
	}
	return h.resource(c, fiber.StatusOK, session, session.StateRevision)
}

func respondConversationDetailReadError(c *fiber.Ctx, stage conversationDetailStage) error {
	switch stage {
	case conversationDetailStageTranscript:
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_TRANSCRIPT_FAILED", "Could not load the conversation transcript.")
	case conversationDetailStageHandoff:
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_HANDOFF_FAILED", "Could not load the conversation handoff details.")
	case conversationDetailStagePartyRequest:
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_PARTY_REQUEST_FAILED", "Could not load the conversation party request.")
	case conversationDetailStageSchedulingEvidence:
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_SCHEDULING_EVIDENCE_FAILED", "Could not load the conversation scheduling evidence.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_FAILED", "Could not load conversation session.")
	}
}

func (h *Handler) RealtimeEvents(c *fiber.Ctx) error {
	response, err := h.service.ListWebhookEvents(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("session_id"), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Conversation session request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CONVERSATION_NOT_FOUND", "Conversation session was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATION_REALTIME_EVENTS_FAILED", "Could not load realtime events.")
	}
	if h.normalized {
		return h.resource(c, fiber.StatusOK, response.Events, 0, fiber.Map{"limit": response.Limit, "offset": response.Offset, "has_more": response.HasMore})
	}
	return respond.JSON(c, fiber.StatusOK, response)
}

func (h *Handler) ListPartyBookingRequests(c *fiber.Ctx) error {
	res, err := h.service.ListPartyBookingRequests(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Query("status"), parseLimit(c.Query("limit")), parseOffset(c.Query("offset")))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Party request filter is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PARTY_REQUESTS_FAILED", "Could not load party booking requests.")
	}
	if h.normalized {
		return h.resource(c, fiber.StatusOK, res.PartyBookingRequests, 0, fiber.Map{"limit": res.Limit, "offset": res.Offset, "has_more": res.HasMore})
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) UpdatePartyBookingRequestStatus(c *fiber.Ctx) error {
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.UpdatePartyBookingRequestStatus(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("request_id"), req.Status)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Party request status is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PARTY_REQUEST_NOT_FOUND", "Party booking request was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PARTY_REQUEST_UPDATE_FAILED", "Could not update party booking request.")
	}
	if h.normalized {
		return h.resource(c, fiber.StatusOK, item, 0)
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"party_booking_request": item})
}

func (h *Handler) Archive(c *fiber.Ctx) error {
	session, err := h.service.Archive(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("session_id"))
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
	return h.resource(c, fiber.StatusOK, session, session.StateRevision)
}

func (h *Handler) Redact(c *fiber.Ctx) error {
	session, err := h.service.Redact(c.UserContext(), conversationSalonID(c), middleware.UserID(c), c.Params("session_id"))
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
	return h.resource(c, fiber.StatusOK, session, session.StateRevision)
}

func (h *Handler) resource(c *fiber.Ctx, status int, data any, version int64, pages ...fiber.Map) error {
	if !h.normalized {
		return respond.JSON(c, status, data)
	}
	requestID := ""
	if value := c.Locals("requestid"); value != nil {
		requestID = fmt.Sprint(value)
	}
	meta := fiber.Map{"request_id": requestID, "replayed": false, "resource_version": version, "permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}}}
	if len(pages) > 0 && pages[0] != nil {
		meta["page"] = pages[0]
	}
	return respond.JSON(c, status, fiber.Map{"data": data, "meta": meta})
}

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}

func parseOffset(raw string) int {
	offset, _ := strconv.Atoi(raw)
	return offset
}
