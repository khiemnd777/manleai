package training

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
)

type Handler struct {
	service    *Service
	limiter    tenantRuntimeLimiter
	normalized bool
}

type tenantRuntimeLimiter interface {
	AllowTenant(context.Context, middleware.ActorContext, string, string, int) (tenantruntime.Decision, error)
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func NewNormalizedHandler(service *Service) *Handler {
	return &Handler{service: service, normalized: true}
}

func trainingSalonID(c *fiber.Ctx) string {
	if value := c.Params("tenant_id"); value != "" {
		return value
	}
	return c.Params("id")
}

func (h *Handler) SetTenantRuntimeLimiter(limiter tenantRuntimeLimiter) *Handler {
	h.limiter = limiter
	return h
}

func (h *Handler) ListKnowledge(c *fiber.Ctx) error {
	items, err := h.service.ListKnowledge(c.UserContext(), trainingSalonID(c), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "KNOWLEDGE_FAILED", "Could not load knowledge items.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusOK, items)
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"knowledge_items": items})
}

func (h *Handler) CreateKnowledge(c *fiber.Ctx) error {
	var req KnowledgeItemInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.CreateKnowledge(c.UserContext(), trainingSalonID(c), middleware.UserID(c), req)
	return h.respondKnowledge(c, item, err, fiber.StatusCreated)
}

func (h *Handler) UpdateKnowledge(c *fiber.Ctx) error {
	var req KnowledgeItemInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.UpdateKnowledge(c.UserContext(), trainingSalonID(c), middleware.UserID(c), c.Params("item_id"), req)
	return h.respondKnowledge(c, item, err, fiber.StatusOK)
}

func (h *Handler) DeleteKnowledge(c *fiber.Ctx) error {
	err := h.service.DeleteKnowledge(c.UserContext(), trainingSalonID(c), middleware.UserID(c), c.Params("item_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Knowledge item request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "KNOWLEDGE_NOT_FOUND", "Knowledge item was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "KNOWLEDGE_DELETE_FAILED", "Could not delete knowledge item.")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListCorrections(c *fiber.Ctx) error {
	items, err := h.service.ListCorrections(c.UserContext(), trainingSalonID(c), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CORRECTIONS_FAILED", "Could not load owner corrections.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusOK, items)
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"owner_corrections": items})
}

func (h *Handler) ListServiceAliases(c *fiber.Ctx) error {
	items, err := h.service.ListServiceAliases(c.UserContext(), trainingSalonID(c), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_ALIASES_FAILED", "Could not load service aliases.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusOK, items)
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_aliases": items})
}

func (h *Handler) UpsertServiceAlias(c *fiber.Ctx) error {
	var req ServiceAliasInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.UpsertServiceAlias(c.UserContext(), trainingSalonID(c), middleware.UserID(c), req)
	return h.respondServiceAlias(c, item, err, fiber.StatusCreated)
}

func (h *Handler) CreateCorrection(c *fiber.Ctx) error {
	var req OwnerCorrectionInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.CreateCorrection(c.UserContext(), trainingSalonID(c), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Correction text and source are invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CORRECTION_SOURCE_NOT_FOUND", "Correction source was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CORRECTION_CREATE_FAILED", "Could not create owner correction.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusCreated, item)
	}
	return respond.JSON(c, fiber.StatusCreated, item)
}

func (h *Handler) Evaluate(c *fiber.Ctx) error {
	var req EvaluateRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if h.limiter != nil {
		decision, limitErr := h.limiter.AllowTenant(c.UserContext(), middleware.Actor(c), trainingSalonID(c), tenantruntime.MetricExpensiveRequest, 1)
		if errors.Is(limitErr, tenantruntime.ErrQuotaExceeded) {
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(decision.RetryAfterSec))
			return respond.Error(c, fiber.StatusTooManyRequests, "TENANT_QUOTA_EXCEEDED", "This salon has reached its current training evaluation limit. Retry later.")
		}
		if errors.Is(limitErr, tenantruntime.ErrForbidden) {
			return respond.Error(c, fiber.StatusForbidden, "TENANT_ACCESS_FORBIDDEN", "This salon is not available to the current tenant account.")
		}
		if limitErr != nil {
			return respond.Error(c, fiber.StatusServiceUnavailable, "TENANT_QUOTA_UNAVAILABLE", "Tenant request protection is temporarily unavailable.")
		}
	}
	result, err := h.service.Evaluate(c.UserContext(), trainingSalonID(c), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Training evaluation message is required.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TRAINING_EVALUATION_FAILED", "Could not evaluate training question.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusOK, result)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ApplyCorrection(c *fiber.Ctx) error {
	var req KnowledgeItemInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.ApplyCorrection(c.UserContext(), trainingSalonID(c), middleware.UserID(c), c.Params("correction_id"), req)
	return h.respondKnowledge(c, item, err, fiber.StatusCreated)
}

func (h *Handler) ApplyServiceAliasCorrection(c *fiber.Ctx) error {
	var req ServiceAliasInput
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.ApplyServiceAliasCorrection(c.UserContext(), trainingSalonID(c), middleware.UserID(c), c.Params("correction_id"), req)
	return h.respondServiceAlias(c, item, err, fiber.StatusCreated)
}

func (h *Handler) DismissCorrection(c *fiber.Ctx) error {
	item, err := h.service.DismissCorrection(c.UserContext(), trainingSalonID(c), middleware.UserID(c), c.Params("correction_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Correction request is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "CORRECTION_NOT_FOUND", "Correction was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CORRECTION_UPDATE_FAILED", "Could not update owner correction.")
	}
	if h.normalized {
		return h.respondNormalized(c, fiber.StatusOK, item)
	}
	return respond.JSON(c, fiber.StatusOK, item)
}

func (h *Handler) respondKnowledge(c *fiber.Ctx, item *KnowledgeItem, err error, status int) error {
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Knowledge item contains invalid values.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "KNOWLEDGE_NOT_FOUND", "Knowledge item was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "KNOWLEDGE_SAVE_FAILED", "Could not save knowledge item.")
	}
	if h.normalized {
		return h.respondNormalized(c, status, item)
	}
	return respond.JSON(c, status, item)
}

func (h *Handler) respondServiceAlias(c *fiber.Ctx, item *ServiceAlias, err error, status int) error {
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service alias request contains invalid values.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_ALIAS_SOURCE_NOT_FOUND", "Salon, correction, or service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_ALIAS_SAVE_FAILED", "Could not save service alias.")
	}
	if h.normalized {
		return h.respondNormalized(c, status, item)
	}
	return respond.JSON(c, status, item)
}

func (h *Handler) respondNormalized(c *fiber.Ctx, status int, data any) error {
	return respond.JSON(c, status, fiber.Map{"data": data, "meta": fiber.Map{"replayed": false, "resource_version": 0, "permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}}}})
}
