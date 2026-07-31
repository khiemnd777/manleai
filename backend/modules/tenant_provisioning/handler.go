package tenant_provisioning

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) Provision(c *fiber.Ctx) error {
	var req ProvisionRequest
	if err := decode(c.Body(), &req); err != nil {
		return provisioningError(c, ErrValidation)
	}
	result, err := h.service.Provision(c.UserContext(), middleware.Actor(c), c.Params("request_id"), req)
	if err != nil {
		return provisioningError(c, err)
	}
	if result.Replayed {
		c.Set("X-Idempotent-Replay", "true")
	}
	return c.JSON(result)
}
func (h *Handler) CreateInvitation(c *fiber.Ctx) error {
	var req InvitationRequest
	if err := decode(c.Body(), &req); err != nil {
		return provisioningError(c, ErrValidation)
	}
	result, err := h.service.CreateInvitation(c.UserContext(), middleware.Actor(c), c.Params("request_id"), req)
	if err != nil {
		return provisioningError(c, err)
	}
	if result.Replayed {
		c.Set("X-Idempotent-Replay", "true")
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}
func (h *Handler) AcceptInvitation(c *fiber.Ctx) error {
	var req AcceptInvitationRequest
	if err := decode(c.Body(), &req); err != nil {
		return provisioningError(c, ErrInvitationInvalid)
	}
	result, err := h.service.AcceptInvitation(c.UserContext(), req)
	if err != nil {
		return provisioningError(c, err)
	}
	return c.JSON(result)
}
func (h *Handler) SearchTenantIdentities(c *fiber.Ctx) error {
	result, err := h.service.SearchTenantIdentities(c.UserContext(), middleware.Actor(c), c.Query("query"))
	if err != nil {
		return provisioningError(c, err)
	}
	return c.JSON(result)
}
func decode(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid body")
	}
	return nil
}
func provisioningError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "TENANT_PROVISIONING_INVALID", "Review the verified owner and salon information.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "TENANT_PROVISIONING_FORBIDDEN", "Only a Platform Administrator can perform this action.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "REGISTRATION_NOT_FOUND", "Registration request not found.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_VERSION_CONFLICT", "This request changed. Reload it before continuing.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "REGISTRATION_ACTION_CONFLICT", "This action key was already used for a different operation.")
	case errors.Is(err, ErrStatusConflict):
		return respond.Error(c, fiber.StatusConflict, "TENANT_PROVISIONING_STATUS_CONFLICT", "This request is not ready for provisioning.")
	case errors.Is(err, ErrIdentityConflict):
		return respond.Error(c, fiber.StatusConflict, "TENANT_OWNER_IDENTITY_CONFLICT", "The verified owner identity does not match an eligible Tenant principal.")
	case errors.Is(err, ErrInvitationUnavailable):
		return respond.Error(c, fiber.StatusConflict, "OWNER_INVITATION_UNAVAILABLE", "An invitation cannot be created for this owner state. Rotate an active invitation explicitly.")
	case errors.Is(err, ErrInvitationInvalid):
		return respond.Error(c, fiber.StatusBadRequest, "OWNER_INVITATION_INVALID", "The invitation is invalid, expired, or already used.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "TENANT_PROVISIONING_UNAVAILABLE", "Tenant provisioning is temporarily unavailable.")
	}
}
