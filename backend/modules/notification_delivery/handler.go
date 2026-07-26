package notificationdelivery

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type handlerService interface {
	List(context.Context, string, string, string, int, int) (*ListResponse, error)
	Get(context.Context, string, string, string) (*DetailResponse, error)
	Requeue(context.Context, string, string, string, RequeueRequest) (*DetailResponse, bool, error)
}

type Handler struct{ service handlerService }

func NewHandler(service handlerService) *Handler { return &Handler{service: service} }

func (h *Handler) List(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "25"))
	if err != nil {
		return invalid(c)
	}
	offset, err := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
	if err != nil {
		return invalid(c)
	}
	res, serviceErr := h.service.List(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Query("status"), limit, offset)
	return respondResult(c, res, serviceErr)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	res, err := h.service.Get(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("notification_id"))
	return respondResult(c, res, err)
}

func (h *Handler) Requeue(c *fiber.Ctx) error {
	var req RequeueRequest
	if err := c.BodyParser(&req); err != nil {
		return invalid(c)
	}
	res, replayed, err := h.service.Requeue(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("notification_id"), req)
	if err != nil {
		return respondResult(c, nil, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, res)
}

func respondResult(c *fiber.Ctx, body any, err error) error {
	switch {
	case err == nil:
		return respond.JSON(c, fiber.StatusOK, body)
	case errors.Is(err, ErrValidation):
		return invalid(c)
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "OWNER_NOTIFICATION_DELIVERY_NOT_FOUND", "Owner notification delivery was not found.")
	case errors.Is(err, ErrConflict):
		return respond.Error(c, fiber.StatusConflict, "OWNER_NOTIFICATION_DELIVERY_CONFLICT", "The delivery action conflicts with an existing action.")
	case errors.Is(err, ErrRequeueBlocked):
		return respond.Error(c, fiber.StatusConflict, "OWNER_NOTIFICATION_REQUEUE_BLOCKED", "This delivery cannot be safely requeued.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "OWNER_NOTIFICATION_DELIVERY_FAILED", "Could not process owner notification delivery.")
	}
}

func invalid(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusBadRequest, "OWNER_NOTIFICATION_DELIVERY_INVALID", "Owner notification delivery request is invalid.")
}
func defaultQuery(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
