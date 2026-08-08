package access

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

func (h *Handler) ListPlatformUsers(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "25"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListPlatformUsers(c.UserContext(), middleware.Actor(c), c.Query("query"), limit)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) CreatePlatformUser(c *fiber.Ctx) error {
	var req PlatformUserCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.CreatePlatformUser(c.UserContext(), middleware.Actor(c), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusCreated, result)
}

func (h *Handler) UpdatePlatformUser(c *fiber.Ctx) error {
	var req PlatformUserUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.UpdatePlatformUser(c.UserContext(), middleware.Actor(c), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListTenantUsers(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "25"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListTenantUsers(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Query("query"), limit)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListMemberships(c *fiber.Ctx) error {
	result, err := h.service.ListMemberships(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutateMembership(c *fiber.Ctx) error {
	var req MembershipMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateMembership(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListPlatformRoles(c *fiber.Ctx) error {
	result, err := h.service.ListPlatformRoles(c.UserContext(), middleware.Actor(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListCapabilities(c *fiber.Ctx) error {
	result, err := h.service.ListCapabilities(c.UserContext(), middleware.Actor(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutatePlatformRole(c *fiber.Ctx) error {
	var req PlatformRoleMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutatePlatformRole(c.UserContext(), middleware.Actor(c), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListSalonAssignments(c *fiber.Ctx) error {
	result, err := h.service.ListSalonAssignments(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) MutateSalonAssignment(c *fiber.Ctx) error {
	var req SalonAssignmentMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateSalonAssignment(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("user_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListPIIGrants(c *fiber.Ctx) error {
	result, err := h.service.ListPIIGrants(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) GrantPIIAccess(c *fiber.Ctx) error {
	var req PIIGrantRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.GrantPIIAccess(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusCreated, result)
}

func (h *Handler) RevokePIIAccess(c *fiber.Ctx) error {
	var req PIIGrantRevokeRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.RevokePIIAccess(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("grant_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListAuditEvents(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "50"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	offset, err := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListAuditEvents(c.UserContext(), middleware.Actor(c), c.Query("salon_id"), limit, offset)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) ListAuditEventsV2(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "50"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	offset, err := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ListAuditEvents(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), limit, offset)
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"data": result.Events, "meta": fiber.Map{"replayed": false, "resource_version": 0, "page": fiber.Map{"limit": result.Limit, "offset": result.Offset, "has_more": result.HasMore}, "permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}}}})
}

func (h *Handler) ListPlatformSupportAccessRequests(c *fiber.Ctx) error {
	result, err := h.service.ListPlatformSupportAccessRequests(c.UserContext(), middleware.Actor(c), c.Params("salon_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) GetEffectiveSupportAccess(c *fiber.Ctx) error {
	result, err := h.service.GetEffectiveSupportAccess(c.UserContext(), middleware.Actor(c), c.Params("id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) PlatformTenantAccess(c *fiber.Ctx) error {
	actor := middleware.Actor(c)
	salonID := c.Params("tenant_id")
	memberships, err := h.service.ListMemberships(c.UserContext(), actor, salonID)
	if err != nil {
		return h.handleError(c, err)
	}
	roles, err := h.service.ListPlatformRoles(c.UserContext(), actor)
	if err != nil {
		return h.handleError(c, err)
	}
	assignments, err := h.service.ListSalonAssignments(c.UserContext(), actor, salonID)
	if err != nil {
		return h.handleError(c, err)
	}
	grants, err := h.service.ListPIIGrants(c.UserContext(), actor, salonID)
	if err != nil {
		return h.handleError(c, err)
	}
	capabilities, err := h.service.ListCapabilities(c.UserContext(), actor)
	if err != nil {
		return h.handleError(c, err)
	}
	requests, err := h.service.ListPlatformSupportAccessRequests(c.UserContext(), actor, salonID)
	if err != nil {
		return h.handleError(c, err)
	}
	return h.v2(c, fiber.StatusOK, fiber.Map{
		"memberships": memberships.Memberships, "platform_roles": roles.Assignments,
		"operator_assignments": assignments.Assignments, "pii_grants": grants.Grants,
		"capabilities": capabilities.Capabilities, "temporary_authorizations": requests.Requests,
	}, false, 0)
}

func (h *Handler) GetEffectiveSupportAccessV2(c *fiber.Ctx) error {
	result, err := h.service.GetEffectiveSupportAccess(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.v2(c, fiber.StatusOK, result, false, 0)
}

func (h *Handler) MutateMembershipV2(c *fiber.Ctx) error {
	var req MembershipMutationRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateMembership(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), c.Params("user_id"), req)
	return h.v2Mutation(c, fiber.StatusOK, result, replayed, err)
}
func (h *Handler) MutateSalonAssignmentV2(c *fiber.Ctx) error {
	var req SalonAssignmentMutationRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.MutateSalonAssignment(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), c.Params("user_id"), req)
	return h.v2Mutation(c, fiber.StatusOK, result, replayed, err)
}
func (h *Handler) GrantPIIAccessV2(c *fiber.Ctx) error {
	var req PIIGrantRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.GrantPIIAccess(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), req)
	return h.v2Mutation(c, fiber.StatusCreated, result, replayed, err)
}
func (h *Handler) RevokePIIAccessV2(c *fiber.Ctx) error {
	var req PIIGrantRevokeRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.RevokePIIAccess(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), c.Params("grant_id"), req)
	return h.v2Mutation(c, fiber.StatusOK, result, replayed, err)
}
func (h *Handler) CreateSupportAccessRequestV2(c *fiber.Ctx) error {
	var req SupportAccessRequestCreate
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.CreateSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), req)
	return h.v2Mutation(c, fiber.StatusCreated, result, replayed, err)
}
func (h *Handler) CancelSupportAccessRequestV2(c *fiber.Ctx) error {
	var req SupportAccessDecisionRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.CancelSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), c.Params("request_id"), req)
	return h.v2Mutation(c, fiber.StatusOK, result, replayed, err)
}
func (h *Handler) RevokeSupportAccessRequestV2(c *fiber.Ctx) error {
	var req SupportAccessDecisionRequest
	if c.BodyParser(&req) != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.RevokeSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("tenant_id"), c.Params("request_id"), req)
	return h.v2Mutation(c, fiber.StatusOK, result, replayed, err)
}

func (h *Handler) v2Mutation(c *fiber.Ctx, status int, result any, replayed bool, err error) error {
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	version := int64(0)
	switch value := result.(type) {
	case *Membership:
		version = value.Version
	case *SalonAssignment:
		version = value.Version
	case *PIIGrant:
		version = value.Version
	case *SupportAccessRequest:
		version = value.Version
	}
	return h.v2(c, status, result, replayed, version)
}

func (h *Handler) v2(c *fiber.Ctx, status int, data any, replayed bool, version int64) error {
	return respond.JSON(c, status, fiber.Map{"data": data, "meta": fiber.Map{"replayed": replayed, "resource_version": version, "permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}}}})
}

func (h *Handler) CreateSupportAccessRequest(c *fiber.Ctx) error {
	var req SupportAccessRequestCreate
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.CreateSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusCreated, result)
}

func (h *Handler) CancelSupportAccessRequest(c *fiber.Ctx) error {
	var req SupportAccessDecisionRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.CancelSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("request_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) RevokeSupportAccessRequest(c *fiber.Ctx) error {
	var req SupportAccessDecisionRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, replayed, err := h.service.RevokeSupportAccessRequest(c.UserContext(), middleware.Actor(c), c.Params("salon_id"), c.Params("request_id"), req)
	if err != nil {
		return h.handleError(c, err)
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	return respond.JSON(c, fiber.StatusOK, result)
}

func (h *Handler) handleError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "ACCESS_INVALID", "Access-management request is invalid.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "ACCESS_FORBIDDEN", "This action is not permitted.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "ACCESS_RECORD_NOT_FOUND", "The requested access record was not found.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "ACCESS_VERSION_CONFLICT", "Access state changed. Reload and retry with the current version.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "ACCESS_ACTION_CONFLICT", "The action key was already used with different input.")
	case errors.Is(err, ErrLastAdmin):
		return respond.Error(c, fiber.StatusConflict, "LAST_PLATFORM_ADMIN", "The last active platform administrator cannot be removed.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "ACCESS_OPERATION_FAILED", "Could not complete the access-management operation.")
	}
}

func defaultQuery(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
