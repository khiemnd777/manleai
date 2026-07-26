package pos_square

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/pos"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type PlatformHandler struct {
	service        *Service
	access         platformAuthorizer
	runtimeLimiter platformRuntimeLimiter
}

type platformRuntimeLimiter interface {
	AllowPlatform(context.Context, middleware.ActorContext, string, access.Capability, string, int) (tenantruntime.Decision, error)
}

func NewPlatformHandler(service *Service, authorizer platformAuthorizer, limiters ...platformRuntimeLimiter) *PlatformHandler {
	handler := &PlatformHandler{service: service, access: authorizer}
	if len(limiters) > 0 {
		handler.runtimeLimiter = limiters[0]
	}
	return handler
}

func (h *PlatformHandler) authorize(c *fiber.Ctx, capability access.Capability) error {
	if h == nil || h.access == nil {
		return access.ErrForbidden
	}
	return h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
		Surface: access.SurfacePlatform, SalonID: c.Params("tenant_id"), Capability: capability,
	})
}

func (h *PlatformHandler) ConnectURL(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "SQUARE_CONNECT_URL_FAILED")
	}
	result, err := h.service.ConnectURLForPlatform(c.UserContext(), c.Params("tenant_id"))
	return h.respond(c, result, err, "SQUARE_CONNECT_URL_FAILED")
}

func (h *PlatformHandler) Status(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalRead); err != nil {
		return h.respond(c, nil, err, "SQUARE_STATUS_FAILED")
	}
	result, err := h.service.StatusForPlatform(c.UserContext(), c.Params("tenant_id"))
	return h.respond(c, result, err, "SQUARE_STATUS_FAILED")
}

func (h *PlatformHandler) Locations(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalRead); err != nil {
		return h.respond(c, nil, err, "SQUARE_LOCATIONS_FAILED")
	}
	result, err := h.service.LocationsForPlatform(c.UserContext(), c.Params("tenant_id"))
	if err != nil {
		return h.respond(c, nil, err, "SQUARE_LOCATIONS_FAILED")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"locations": result})
}

type platformSelectLocationRequest struct {
	LocationID string `json:"location_id"`
}

type platformAIBookingRequest struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (h *PlatformHandler) SelectLocation(c *fiber.Ctx) error {
	var req platformSelectLocationRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.LocationID) == "" {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "A Square location is required.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "SQUARE_LOCATION_FAILED")
	}
	if allowed, err := h.allowProviderWrite(c); !allowed || err != nil {
		return err
	}
	result, err := h.service.SelectLocationForPlatform(c.UserContext(), c.Params("tenant_id"), req.LocationID)
	return h.respond(c, result, err, "SQUARE_LOCATION_FAILED")
}

func (h *PlatformHandler) Sync(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "SQUARE_SYNC_FAILED")
	}
	if allowed, err := h.allowProviderWrite(c); !allowed || err != nil {
		return err
	}
	result, err := h.service.SyncForPlatform(c.UserContext(), c.Params("tenant_id"))
	if err != nil {
		return h.respond(c, nil, err, "SQUARE_SYNC_FAILED")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"ok": true, "summary": result})
}

func (h *PlatformHandler) EnableAIBooking(c *fiber.Ctx) error {
	return h.setAIBooking(c, true)
}

func (h *PlatformHandler) DisableAIBooking(c *fiber.Ctx) error {
	return h.setAIBooking(c, false)
}

func (h *PlatformHandler) setAIBooking(c *fiber.Ctx, enabled bool) error {
	var req platformAIBookingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "AI runtime control request is invalid.")
	}
	if err := h.authorize(c, access.CapabilityTechnicalWrite); err != nil {
		return h.respond(c, nil, err, "AI_RUNTIME_UPDATE_FAILED")
	}
	result, replayed, err := h.service.SetAIBookingForPlatform(
		c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), enabled, req.ActionKey, req.ExpectedVersion,
	)
	if err == nil {
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	}
	return h.respond(c, result, err, "AI_RUNTIME_UPDATE_FAILED")
}

func (h *PlatformHandler) allowProviderWrite(c *fiber.Ctx) (bool, error) {
	if h == nil || h.runtimeLimiter == nil {
		return true, nil
	}
	decision, err := h.runtimeLimiter.AllowPlatform(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), access.CapabilityTechnicalWrite, tenantruntime.MetricProviderWrite, 1)
	if errors.Is(err, tenantruntime.ErrQuotaExceeded) {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(decision.RetryAfterSec))
		return false, respond.Error(c, fiber.StatusTooManyRequests, "TENANT_QUOTA_EXCEEDED", "This salon has reached its current provider-operation limit. Retry later.")
	}
	if errors.Is(err, tenantruntime.ErrForbidden) {
		return false, respond.Error(c, fiber.StatusForbidden, "TECHNICAL_FORBIDDEN", "This Square technical action is not permitted.")
	}
	if err != nil {
		return false, respond.Error(c, fiber.StatusServiceUnavailable, "TENANT_QUOTA_UNAVAILABLE", "Tenant request protection is temporarily unavailable.")
	}
	return true, nil
}

func (h *PlatformHandler) ListWebhookEvents(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityOperationsRead); err != nil {
		return h.webhookRespond(c, nil, false, err)
	}
	limit, err := strconv.Atoi(squareWebhookQueryDefault(c.Query("limit"), "25"))
	if err != nil {
		return h.webhookRespond(c, nil, false, ErrWebhookOperationsValidation)
	}
	offset, err := strconv.Atoi(squareWebhookQueryDefault(c.Query("offset"), "0"))
	if err != nil {
		return h.webhookRespond(c, nil, false, ErrWebhookOperationsValidation)
	}
	result, err := h.service.ListWebhookEventsForPlatform(c.UserContext(), c.Params("tenant_id"), c.Query("status"), limit, offset)
	return h.webhookRespond(c, result, false, err)
}

func (h *PlatformHandler) GetWebhookEvent(c *fiber.Ctx) error {
	if err := h.authorize(c, access.CapabilityOperationsRead); err != nil {
		return h.webhookRespond(c, nil, false, err)
	}
	result, err := h.service.GetWebhookEventForPlatform(c.UserContext(), c.Params("tenant_id"), c.Params("webhook_event_id"))
	return h.webhookRespond(c, result, false, err)
}

func (h *PlatformHandler) RequeueWebhookEvent(c *fiber.Ctx) error {
	var req WebhookRequeueRequest
	if err := c.BodyParser(&req); err != nil {
		return h.webhookRespond(c, nil, false, ErrWebhookOperationsValidation)
	}
	if err := h.authorize(c, access.CapabilityOperationsWrite); err != nil {
		return h.webhookRespond(c, nil, false, err)
	}
	result, replayed, err := h.service.RequeueWebhookEventForPlatform(c.UserContext(), c.Params("tenant_id"), middleware.UserID(c), c.Params("webhook_event_id"), req)
	return h.webhookRespond(c, result, replayed, err)
}

func (h *PlatformHandler) webhookRespond(c *fiber.Ctx, value any, replayed bool, err error) error {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "OPERATIONS_FORBIDDEN", "This Square operations action is not permitted.")
	case errors.Is(err, ErrWebhookOperationsValidation):
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_WEBHOOK_OPERATIONS_INVALID", "Square webhook operations request is invalid.")
	case errors.Is(err, pos.ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	case errors.Is(err, ErrWebhookEventNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SQUARE_WEBHOOK_EVENT_NOT_FOUND", "Square webhook event was not found.")
	case errors.Is(err, ErrWebhookActionConflict):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_WEBHOOK_ACTION_CONFLICT", "The webhook action conflicts with an existing action.")
	case errors.Is(err, ErrWebhookRequeueBlocked):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_WEBHOOK_REQUEUE_BLOCKED", "This webhook event cannot be safely requeued.")
	case err != nil:
		return respond.Error(c, fiber.StatusInternalServerError, "SQUARE_WEBHOOK_OPERATIONS_FAILED", "Could not process Square webhook operations.")
	default:
		c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
		return respond.JSON(c, fiber.StatusOK, value)
	}
}

func (h *PlatformHandler) respond(c *fiber.Ctx, value any, err error, code string) error {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "TECHNICAL_FORBIDDEN", "This Square technical action is not permitted.")
	case errors.Is(err, pos.ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_INVALID", "Square technical request is invalid.")
	case errors.Is(err, ErrNotConnected):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
	case errors.Is(err, pos.ErrStaleProviderSnapshot):
		return respond.Error(c, fiber.StatusConflict, "SQUARE_SYNC_STALE", "Square location or sync generation changed. Run sync again.")
	case errors.Is(err, pos.ErrTechnicalVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "TECHNICAL_VERSION_CONFLICT", "AI runtime settings changed. Reload before saving again.")
	case errors.Is(err, pos.ErrTechnicalActionConflict):
		return respond.Error(c, fiber.StatusConflict, "TECHNICAL_ACTION_CONFLICT", "This technical action key was already used for a different request.")
	case err != nil:
		return respond.Error(c, fiber.StatusBadGateway, code, "Square technical operation could not be completed.")
	default:
		return respond.JSON(c, fiber.StatusOK, value)
	}
}
