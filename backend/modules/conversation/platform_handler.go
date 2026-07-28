package conversation

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type PlatformHandler struct {
	service  *Service
	delegate *Handler
	access   platformAuthorizer
}

func NewPlatformHandler(service *Service, authorizer platformAuthorizer) *PlatformHandler {
	return &PlatformHandler{service: service, delegate: NewHandler(service), access: authorizer}
}

func (h *PlatformHandler) Start(c *fiber.Ctx) error {
	var req StartSessionRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
		}
	}
	session, err := h.service.StartForPlatform(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
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

func (h *PlatformHandler) Message(c *fiber.Ctx) error {
	var req MessageRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	session, err := h.service.MessageForPlatform(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("session_id"), req)
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
	return respond.JSON(c, fiber.StatusOK, session)
}
