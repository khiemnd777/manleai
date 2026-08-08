package voice

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

type PlatformHandler struct {
	service    *Service
	access     platformAuthorizer
	normalized bool
}

func NewPlatformHandler(service *Service, authorizer platformAuthorizer) *PlatformHandler {
	return &PlatformHandler{service: service, access: authorizer}
}

func (h *PlatformHandler) Status(c *fiber.Ctx) error {
	salonID := c.Params("id")
	if salonID == "" {
		salonID = c.Params("tenant_id")
	}
	if h == nil || h.access == nil || h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: salonID, Capability: access.CapabilityCallsRead, PIIScope: access.PIIScopeCalls,
	}) != nil {
		return respond.Error(c, fiber.StatusForbidden, "CALLS_ACCESS_FORBIDDEN", "This Platform account is not authorized for the salon Calls view.")
	}
	if h.access.RecordPlatformSupportAction(c.UserContext(), middleware.Actor(c), salonID, access.CapabilityCallsRead, access.PIIScopeCalls, c.Method(), c.Path()) != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
	}
	status, err := h.service.StatusForPlatform(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_STATUS_INVALID", "Voice status request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_STATUS_FAILED", "Could not load voice status.")
	}
	if h.normalized {
		return respond.JSON(c, fiber.StatusOK, fiber.Map{
			"data": status,
			"meta": fiber.Map{
				"replayed": false, "resource_version": 0,
				"permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}},
			},
		})
	}
	return respond.JSON(c, fiber.StatusOK, status)
}

func (h *Handler) Status(c *fiber.Ctx) error {
	status, err := h.service.Status(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_STATUS_INVALID", "Voice status request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_STATUS_FAILED", "Could not load voice status.")
	}
	return respond.JSON(c, fiber.StatusOK, status)
}

func (h *Handler) SemanticCheck(c *fiber.Ctx) error {
	status, err := h.service.SemanticCheck(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_CHECK_INVALID", "Semantic check request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_SEMANTIC_CHECK_UNAVAILABLE", "Semantic contract verification is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "VOICE_SEMANTIC_CHECK_FAILED", "Could not verify the semantic contract.")
	}
	return respond.JSON(c, fiber.StatusOK, status)
}

func (h *Handler) SemanticEvaluate(c *fiber.Ctx) error {
	var req SemanticEvaluationRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_EVALUATION_INVALID", "Semantic evaluation request is invalid.")
	}
	result, err := h.service.SemanticEvaluate(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VOICE_SEMANTIC_EVALUATION_INVALID", "Semantic evaluation request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_SEMANTIC_EVALUATION_UNAVAILABLE", "Semantic evaluation is unavailable.")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return respond.Error(c, fiber.StatusGatewayTimeout, "VOICE_SEMANTIC_EVALUATION_TIMEOUT", "Semantic evaluation timed out.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "VOICE_SEMANTIC_EVALUATION_FAILED", "Could not evaluate the semantic turn.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) Audio(c *fiber.Ctx) error {
	output, err := h.service.Audio(c.UserContext(), c.Params("id"), c.Query("expires"), c.Query("signature"))
	if err != nil {
		return respond.Error(c, fiber.StatusNotFound, "VOICE_AUDIO_UNAVAILABLE", "Voice audio is unavailable.")
	}
	c.Set(fiber.HeaderContentType, output.ContentType)
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	return c.Status(fiber.StatusOK).Send(output.Audio)
}
