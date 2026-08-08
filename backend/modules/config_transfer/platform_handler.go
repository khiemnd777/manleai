package configtransfer

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformTransferAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

type PlatformHandler struct {
	service    *PlatformService
	access     platformTransferAuthorizer
	normalized bool
}

func NewPlatformHandler(service *PlatformService, authorizer platformTransferAuthorizer) *PlatformHandler {
	return &PlatformHandler{service: service, access: authorizer}
}

func (h *PlatformHandler) Export(c *fiber.Ctx) error {
	sections := splitSections(c.Query("sections"))
	if !validUUID(c.Params("tenant_id")) {
		return h.respond(c, nil, ErrValidation)
	}
	if err := h.authorizeSections(c, c.Params("tenant_id"), sections, false); err != nil {
		return h.respond(c, nil, err)
	}
	bundle, err := h.service.Export(c.UserContext(), c.Params("tenant_id"), sections)
	if err == nil {
		c.Set(fiber.HeaderContentDisposition, `attachment; filename="manleai-platform-configuration.json"`)
	}
	return h.respond(c, bundle, err)
}

func (h *PlatformHandler) Preview(c *fiber.Ctx) error {
	var req PlatformTransferRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_TRANSFER_INVALID", "Transfer request is invalid.")
	}
	targetID := c.Params("tenant_id")
	if err := validatePlatformTransferShape(targetID, req); err != nil {
		return h.respond(c, nil, err)
	}
	if err := h.authorizeSections(c, targetID, req.IncludedSections, false); err != nil {
		return h.respond(c, nil, err)
	}
	if req.SourceType == PlatformSourceTenant {
		if err := h.authorizeSections(c, req.SourceSalonID, req.IncludedSections, false); err != nil {
			return h.respond(c, nil, err)
		}
	}
	result, err := h.service.Preview(c.UserContext(), targetID, middleware.UserID(c), req)
	return h.respond(c, result, err)
}

func (h *PlatformHandler) Apply(c *fiber.Ctx) error {
	var req PlatformTransferApplyRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_TRANSFER_INVALID", "Transfer request is invalid.")
	}
	targetID := c.Params("tenant_id")
	if err := validatePlatformTransferShape(targetID, req.PlatformTransferRequest); err != nil || !validUUID(req.PreviewID) || !validPlatformActionKey(req.ActionKey) {
		return h.respond(c, nil, ErrValidation)
	}
	if req.SourceType == PlatformSourceTenant {
		if err := h.authorizeSections(c, req.SourceSalonID, req.IncludedSections, false); err != nil {
			return h.respond(c, nil, err)
		}
	}
	if err := h.authorizeSections(c, targetID, req.IncludedSections, true); err != nil {
		return h.respond(c, nil, err)
	}
	result, err := h.service.Apply(c.UserContext(), targetID, middleware.UserID(c), req)
	return h.respond(c, result, err)
}

func (h *PlatformHandler) Runs(c *fiber.Ctx) error {
	if !validUUID(c.Params("tenant_id")) {
		return h.respond(c, nil, ErrValidation)
	}
	if err := h.authorizeCapability(c, c.Params("tenant_id"), access.CapabilityAuditRead); err != nil {
		return h.respond(c, nil, err)
	}
	limit, _ := strconv.Atoi(c.Query("limit", "25"))
	result, err := h.service.Runs(c.UserContext(), c.Params("tenant_id"), limit)
	return h.respond(c, result, err)
}

func validatePlatformTransferShape(targetID string, req PlatformTransferRequest) error {
	if !validUUID(targetID) {
		return ErrValidation
	}
	if _, err := normalizeSectionSelection(req.IncludedSections, platformConfigurationSections); err != nil || len(req.IncludedSections) == 0 {
		return ErrValidation
	}
	switch strings.TrimSpace(req.SourceType) {
	case PlatformSourceTenant:
		if !validUUID(req.SourceSalonID) || strings.TrimSpace(req.SourceSalonID) == strings.TrimSpace(targetID) || req.Configuration != nil {
			return ErrValidation
		}
	case PlatformSourceJSON:
		if strings.TrimSpace(req.SourceSalonID) != "" || req.Configuration == nil {
			return ErrValidation
		}
	default:
		return ErrValidation
	}
	return nil
}

func (h *PlatformHandler) authorizeSections(c *fiber.Ctx, salonID string, sections []string, write bool) error {
	if h == nil || h.access == nil {
		return access.ErrForbidden
	}
	if len(sections) == 0 {
		return ErrValidation
	}
	capabilities := map[access.Capability]bool{}
	for _, section := range sections {
		capability, ok := transferSectionCapability(strings.TrimSpace(section), write)
		if !ok {
			return ErrValidation
		}
		capabilities[capability] = true
	}
	for capability := range capabilities {
		if err := h.authorizeCapability(c, strings.TrimSpace(salonID), capability); err != nil {
			return err
		}
	}
	return nil
}

func (h *PlatformHandler) authorizeCapability(c *fiber.Ctx, salonID string, capability access.Capability) error {
	if h == nil || h.access == nil || strings.TrimSpace(salonID) == "" {
		return access.ErrForbidden
	}
	actor := middleware.Actor(c)
	if err := h.access.Authorize(c.UserContext(), actor, access.AccessCheck{Surface: access.SurfacePlatform, SalonID: salonID, Capability: capability}); err != nil {
		return err
	}
	if err := h.access.RecordPlatformSupportAction(c.UserContext(), actor, salonID, capability, "", c.Method(), c.Path()); err != nil {
		return err
	}
	return nil
}

func transferSectionCapability(section string, write bool) (access.Capability, bool) {
	switch section {
	case SectionSalon, SectionPublic, SectionLocalHours:
		if write {
			return access.CapabilityBusinessWrite, true
		}
		return access.CapabilityBusinessRead, true
	case SectionCategories, SectionServiceAliases, SectionConsultation:
		if write {
			return access.CapabilityServicesWrite, true
		}
		return access.CapabilityServicesRead, true
	case SectionKnowledge:
		if write {
			return access.CapabilityTrainingWrite, true
		}
		return access.CapabilityTrainingRead, true
	case SectionAI, SectionIntegrations:
		if write {
			return access.CapabilityTechnicalWrite, true
		}
		return access.CapabilityTechnicalRead, true
	default:
		return "", false
	}
}

func splitSections(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func (h *PlatformHandler) respond(c *fiber.Ctx, value any, err error) error {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "CONFIGURATION_TRANSFER_FORBIDDEN", "This Platform account is not authorized for every selected source and destination section.")
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_TRANSFER_INVALID", "Transfer source, scope, preview, or action key is invalid.")
	case errors.Is(err, ErrUnsupportedSchema):
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_TRANSFER_SCHEMA_UNSUPPORTED", "Only Platform v9, compatibility v8, and scoped content-only v7 configuration bundles are supported.")
	case errors.Is(err, ErrTransferPreview):
		return respond.Error(c, fiber.StatusNotFound, "CONFIGURATION_TRANSFER_PREVIEW_NOT_FOUND", "Transfer preview or salon was not found.")
	case errors.Is(err, ErrTransferStale):
		return respond.Error(c, fiber.StatusConflict, "CONFIGURATION_TRANSFER_STALE", "Source, destination, or scheduling authority changed after preview. Preview again before applying.")
	case errors.Is(err, ErrTransferActionConflict):
		return respond.Error(c, fiber.StatusConflict, "CONFIGURATION_TRANSFER_ACTION_CONFLICT", "This action key was already used for a different transfer.")
	case errors.Is(err, ErrTransferTooLarge):
		return respond.Error(c, fiber.StatusRequestEntityTooLarge, "CONFIGURATION_TRANSFER_TOO_LARGE", "Platform configuration JSON must be 3 MB or smaller. Use direct tenant transfer for a larger source.")
	case errors.Is(err, ErrImportConflict):
		return respond.JSON(c, fiber.StatusConflict, value)
	case err != nil:
		return respond.Error(c, fiber.StatusInternalServerError, "CONFIGURATION_TRANSFER_FAILED", "Could not complete Platform configuration transfer.")
	default:
		if h.normalized {
			return respond.JSON(c, fiber.StatusOK, fiber.Map{"data": value, "meta": fiber.Map{"replayed": false, "resource_version": 0, "permissions": fiber.Map{"can_read": true, "allowed_actions": []string{}}}})
		}
		return respond.JSON(c, fiber.StatusOK, value)
	}
}
