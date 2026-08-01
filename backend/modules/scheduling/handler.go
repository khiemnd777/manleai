package scheduling

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
)

type SchedulingActionService interface {
	CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*AvailabilityResult, error)
	ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req ActionRequest) (*ActionResult, error)
}

type SchedulingRequestService interface {
	List(ctx context.Context, salonID string, ownerUserID string, status SchedulingRequestStatus, limit int, offset int) (*ListSchedulingRequestsResponse, error)
	Get(ctx context.Context, salonID string, ownerUserID string, requestID string) (*SchedulingRequest, error)
	Transition(ctx context.Context, salonID string, ownerUserID string, requestID string, req TransitionSchedulingRequest) (*SchedulingRequest, bool, error)
}

type Handler struct {
	actions  SchedulingActionService
	requests SchedulingRequestService
	limiter  tenantRuntimeLimiter
}

type tenantRuntimeLimiter interface {
	AllowTenant(context.Context, middleware.ActorContext, string, string, int) (tenantruntime.Decision, error)
}

func NewHandler(actions SchedulingActionService, requests SchedulingRequestService) *Handler {
	return &Handler{actions: actions, requests: requests}
}

func (h *Handler) SetTenantRuntimeLimiter(limiter tenantRuntimeLimiter) *Handler {
	h.limiter = limiter
	return h
}

func (h *Handler) Availability(c *fiber.Ctx) error {
	var req booking.AvailabilityRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if allowed, err := h.allowTenantRequest(c, tenantruntime.MetricExpensiveRequest); !allowed || err != nil {
		return err
	}
	result, err := h.actions.CheckAvailability(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_NOT_READY", "The salon's scheduling authority is not ready for scheduling actions.")
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_STALE", "Availability changed or cannot be verified safely. Check availability again.")
	}
	if errors.Is(err, booking.ErrValidation) || errors.Is(err, ErrInvalidSchedulingAction) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Scheduling availability input is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SCHEDULING_RESOURCE_NOT_FOUND", "Salon, service, staff, or target was not found.")
	}
	if errors.Is(err, booking.ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_PROVIDER_UNAVAILABLE", "The selected scheduling provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "SCHEDULING_AVAILABILITY_FAILED", "Could not load scheduling availability.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ExecuteAction(c *fiber.Ctx) error {
	var req ActionRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if allowed, err := h.allowTenantRequest(c, tenantruntime.MetricSchedulingWrite); !allowed || err != nil {
		return err
	}
	req.Source = booking.SourceOwnerDashboard
	result, err := h.actions.ExecuteAction(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrInvalidSchedulingAction) || errors.Is(err, booking.ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Scheduling action input is invalid.")
	}
	if errors.Is(err, booking.ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_OPERATION_CONFLICT", "This operation key is already assigned to different scheduling data.")
	}
	if errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_AUTHORITY_NOT_READY", "The salon's scheduling authority is not ready for scheduling actions.")
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteRequired) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_REQUIRED", "Verified availability is required for this scheduling authority.")
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		return respond.Error(c, fiber.StatusConflict, "AVAILABILITY_QUOTE_STALE", "Availability changed or expired. Check availability again.")
	}
	if errors.Is(err, booking.ErrSlotCommitConflict) {
		return respond.Error(c, fiber.StatusConflict, "SLOT_COMMIT_CONFLICT", "The selected time is no longer available. Check availability again.")
	}
	if errors.Is(err, booking.ErrSlotClaimInProgress) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_CLAIM_IN_PROGRESS", "This exact scheduling operation is still in progress.")
	}
	if errors.Is(err, booking.ErrSlotOutcomeUnknown) {
		return respond.Error(c, fiber.StatusAccepted, "SLOT_OUTCOME_UNKNOWN", "The provider outcome cannot be confirmed safely yet.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SCHEDULING_RESOURCE_NOT_FOUND", "Salon, service, staff, or target was not found.")
	}
	if errors.Is(err, booking.ErrProviderUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_PROVIDER_UNAVAILABLE", "The selected scheduling provider is unavailable.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_ACTION_FAILED", "Could not complete scheduling action.")
	}
	if result == nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_ACTION_FAILED", "Could not complete scheduling action.")
	}
	status := fiber.StatusOK
	if result.Kind == ActionKindConfirmedAppointment && result.OperationType == OperationKindBook {
		status = fiber.StatusCreated
	}
	if result.Kind == ActionKindPendingOwnerReview || result.Kind == ActionKindExternalFallbackPending {
		status = fiber.StatusAccepted
	}
	return respond.JSON(c, status, result)
}

func (h *Handler) allowTenantRequest(c *fiber.Ctx, metric string) (bool, error) {
	if h == nil || h.limiter == nil {
		return true, nil
	}
	decision, err := h.limiter.AllowTenant(c.UserContext(), middleware.Actor(c), c.Params("id"), metric, 1)
	if errors.Is(err, tenantruntime.ErrQuotaExceeded) {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(decision.RetryAfterSec))
		c.Set("TenantLimit-Limit", strconv.Itoa(decision.Limit))
		c.Set("TenantLimit-Remaining", strconv.FormatInt(decision.Remaining, 10))
		return false, respond.Error(c, fiber.StatusTooManyRequests, "TENANT_QUOTA_EXCEEDED", "This salon has reached its current scheduling request limit. Retry later.")
	}
	if errors.Is(err, tenantruntime.ErrForbidden) {
		return false, respond.Error(c, fiber.StatusForbidden, "TENANT_ACCESS_FORBIDDEN", "This salon is not available to the current tenant account.")
	}
	if err != nil {
		return false, respond.Error(c, fiber.StatusServiceUnavailable, "TENANT_QUOTA_UNAVAILABLE", "Tenant request protection is temporarily unavailable.")
	}
	return true, nil
}

func (h *Handler) ListRequests(c *fiber.Ctx) error {
	status := SchedulingRequestStatus(strings.TrimSpace(c.Query("status")))
	result, err := h.requests.List(c.UserContext(), c.Params("id"), middleware.UserID(c), status, parseBoundedInt(c.Query("limit"), 50), parseNonnegativeInt(c.Query("offset")))
	if errors.Is(err, ErrInvalidSchedulingAction) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Scheduling request filters are invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_REQUESTS_FAILED", "Could not load scheduling requests.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) GetRequest(c *fiber.Ctx) error {
	request, err := h.requests.Get(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("request_id"))
	if errors.Is(err, ErrInvalidSchedulingAction) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Scheduling request ID is invalid.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SCHEDULING_REQUEST_NOT_FOUND", "Scheduling request not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_REQUEST_FAILED", "Could not load scheduling request.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"scheduling_request": request})
}

func (h *Handler) TransitionRequest(c *fiber.Ctx) error {
	var req TransitionSchedulingRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	request, _, err := h.requests.Transition(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("request_id"), req)
	if errors.Is(err, ErrInvalidSchedulingAction) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Scheduling request transition is invalid.")
	}
	if errors.Is(err, ErrSchedulingRequestVersion) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_REQUEST_VERSION_CONFLICT", "The scheduling request changed. Reload it before trying again.")
	}
	if errors.Is(err, booking.ErrOperationConflict) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_REQUEST_ACTION_CONFLICT", "This action key is already assigned to different transition data.")
	}
	if errors.Is(err, ErrSchedulingRequestTerminal) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_REQUEST_TERMINAL", "This scheduling request is already closed.")
	}
	if errors.Is(err, ErrSchedulingRequestTransition) {
		return respond.Error(c, fiber.StatusConflict, "SCHEDULING_REQUEST_TRANSITION_CONFLICT", "This status transition is not allowed.")
	}
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SCHEDULING_REQUEST_NOT_FOUND", "Scheduling request not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SCHEDULING_REQUEST_UPDATE_FAILED", "Could not update scheduling request.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"scheduling_request": request})
}

func parseBoundedInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > 100 {
		return 100
	}
	return value
}

func parseNonnegativeInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	return value
}
