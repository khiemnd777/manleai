package tenant_registration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Submit(c *fiber.Ctx) error {
	var req PublicSubmissionRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "REGISTRATION_INVALID", "Review the highlighted information and try again.")
	}
	result, err := h.service.Submit(c.UserContext(), req)
	if err != nil {
		return registrationError(c, err)
	}
	if result.Replayed {
		c.Set("X-Idempotent-Replay", "true")
	}
	return respond.JSON(c, fiber.StatusAccepted, result)
}

func (h *Handler) List(c *fiber.Ctx) error {
	limit, err := queryInt(c, "limit", 25)
	if err != nil {
		return registrationError(c, ErrValidation)
	}
	offset, err := queryInt(c, "offset", 0)
	if err != nil {
		return registrationError(c, ErrValidation)
	}
	from, err := ParseListTime(c.Query("created_from"), false)
	if err != nil {
		return registrationError(c, err)
	}
	to, err := ParseListTime(c.Query("created_to"), true)
	if err != nil {
		return registrationError(c, err)
	}
	result, err := h.service.List(c.UserContext(), middleware.Actor(c), ListFilter{Status: Status(strings.TrimSpace(c.Query("status"))), Query: c.Query("query"), AssignedTo: c.Query("assigned_to"), CreatedFrom: from, CreatedTo: to, Limit: limit, Offset: offset})
	if err != nil {
		return registrationError(c, err)
	}
	return c.JSON(result)
}
func (h *Handler) Get(c *fiber.Ctx) error {
	result, err := h.service.Get(c.UserContext(), middleware.Actor(c), c.Params("request_id"))
	if err != nil {
		return registrationError(c, err)
	}
	return c.JSON(result)
}
func (h *Handler) Mutate(c *fiber.Ctx) error {
	var req MutationRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return registrationError(c, ErrValidation)
	}
	result, err := h.service.Mutate(c.UserContext(), middleware.Actor(c), c.Params("request_id"), req)
	if err != nil {
		return registrationError(c, err)
	}
	if result.Replayed {
		c.Set("X-Idempotent-Replay", "true")
	}
	return c.JSON(result)
}
func (h *Handler) AddNote(c *fiber.Ctx) error {
	var req AddNoteRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return registrationError(c, ErrValidation)
	}
	result, err := h.service.AddNote(c.UserContext(), middleware.Actor(c), c.Params("request_id"), req)
	if err != nil {
		return registrationError(c, err)
	}
	if result.Replayed {
		c.Set("X-Idempotent-Replay", "true")
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

func decodeStrict(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func queryInt(c *fiber.Ctx, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
func registrationError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "REGISTRATION_INVALID", "Review the provided information and try again.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "REGISTRATION_FORBIDDEN", "This registration action is not permitted.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "REGISTRATION_NOT_FOUND", "Registration request not found.")
	case errors.Is(err, ErrSubmissionConflict):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_SUBMISSION_CONFLICT", "This submission attempt changed. Restart the form and try again.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_ACTION_CONFLICT", "This action key was already used for a different change.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_VERSION_CONFLICT", "This request changed. Reload it before continuing.")
	case errors.Is(err, ErrTransition):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_TRANSITION_CONFLICT", "That status transition is not permitted.")
	case errors.Is(err, ErrTerminal):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_TERMINAL", "This registration request is terminal.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "REGISTRATION_UNAVAILABLE", "Registration processing is temporarily unavailable.")
	}
}
