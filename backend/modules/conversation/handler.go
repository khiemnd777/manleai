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
	items, err := h.service.List(c.UserContext(), c.Params("id"), middleware.UserID(c), parseLimit(c.Query("limit")))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONVERSATIONS_FAILED", "Could not load conversation sessions.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"sessions": items})
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

func parseLimit(raw string) int {
	limit, _ := strconv.Atoi(raw)
	return limit
}
