package customernotification

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type handlerService interface {
	GetPolicy(context.Context, string, string) (*Policy, error)
	UpdatePolicy(context.Context, string, string, UpdatePolicyRequest) (*Policy, error)
	AttestConsent(context.Context, string, string, AttestConsentRequest) (*Consent, bool, error)
	DetailForAppointment(context.Context, string, string, string) (*Detail, error)
	DetailForRequest(context.Context, string, string, string) (*Detail, error)
	Requeue(context.Context, string, string, string, string, RequeueRequest) (*Detail, bool, error)
	RequeueRequest(context.Context, string, string, string, string, RequeueRequest) (*Detail, bool, error)
}

func (h *Handler) RequestDetail(c *fiber.Ctx) error {
	result, err := h.service.DetailForRequest(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("request_id"))
	return respondCustomer(c, result, err)
}

type Handler struct{ service handlerService }

func NewHandler(service handlerService) *Handler { return &Handler{service: service} }

func (h *Handler) GetPolicy(c *fiber.Ctx) error {
	result, err := h.service.GetPolicy(c.UserContext(), c.Params("id"), middleware.UserID(c))
	return respondCustomer(c, result, err)
}

func (h *Handler) RequeueRequest(c *fiber.Ctx) error {
	var req RequeueRequest
	if err := c.BodyParser(&req); err != nil {
		return customerInvalid(c)
	}
	result, replayed, err := h.service.RequeueRequest(c.UserContext(), c.Params("id"), middleware.UserID(c),
		c.Params("request_id"), c.Params("delivery_id"), req)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return respondCustomer(c, result, err)
}

func (h *Handler) UpdatePolicy(c *fiber.Ctx) error {
	var req UpdatePolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return customerInvalid(c)
	}
	result, err := h.service.UpdatePolicy(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	return respondCustomer(c, result, err)
}

func (h *Handler) AttestConsent(c *fiber.Ctx) error {
	var req AttestConsentRequest
	if err := c.BodyParser(&req); err != nil {
		return customerInvalid(c)
	}
	result, replayed, err := h.service.AttestConsent(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return respondCustomer(c, result, err)
}

func (h *Handler) AppointmentDetail(c *fiber.Ctx) error {
	result, err := h.service.DetailForAppointment(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("appointment_id"))
	return respondCustomer(c, result, err)
}

func (h *Handler) Requeue(c *fiber.Ctx) error {
	var req RequeueRequest
	if err := c.BodyParser(&req); err != nil {
		return customerInvalid(c)
	}
	result, replayed, err := h.service.Requeue(c.UserContext(), c.Params("id"), middleware.UserID(c),
		c.Params("appointment_id"), c.Params("delivery_id"), req)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return respondCustomer(c, result, err)
}

func respondCustomer(c *fiber.Ctx, result any, err error) error {
	switch {
	case err == nil:
		return respond.JSON(c, fiber.StatusOK, result)
	case errors.Is(err, ErrValidation):
		return customerInvalid(c)
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "CUSTOMER_NOTIFICATION_NOT_FOUND", "Customer notification data was not found.")
	case errors.Is(err, ErrConflict):
		return respond.Error(c, fiber.StatusConflict, "CUSTOMER_NOTIFICATION_CONFLICT", "Customer notification state changed. Refresh and try again.")
	case errors.Is(err, ErrRequeueBlocked):
		return respond.Error(c, fiber.StatusConflict, "CUSTOMER_NOTIFICATION_REQUEUE_BLOCKED", "This notification cannot be safely requeued.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "CUSTOMER_NOTIFICATION_FAILED", "Could not process customer notification data.")
	}
}

func customerInvalid(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusBadRequest, "CUSTOMER_NOTIFICATION_INVALID", "Customer notification request is invalid.")
}
