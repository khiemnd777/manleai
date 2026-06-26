package configtransfer

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/training"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(c *fiber.Ctx) error {
	export, err := h.service.Get(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_EXPORT_INVALID", "Configuration export request is invalid.")
	}
	if errors.Is(err, salon.ErrNotFound) || errors.Is(err, integrationconfig.ErrNotFound) || errors.Is(err, training.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONFIGURATION_EXPORT_FAILED", "Could not export configuration.")
	}
	return respond.JSON(c, fiber.StatusOK, export)
}

func (h *Handler) PreviewImport(c *fiber.Ctx) error {
	req, err := parseImportRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_CONFIGURATION_IMPORT", "Request body must include a valid configuration bundle.")
	}
	result, err := h.service.PreviewImport(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	return h.importResponse(c, result, err)
}

func (h *Handler) ApplyImport(c *fiber.Ctx) error {
	req, err := parseImportRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_CONFIGURATION_IMPORT", "Request body must include a valid configuration bundle.")
	}
	result, err := h.service.ApplyImport(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	return h.importResponse(c, result, err)
}

func (h *Handler) importResponse(c *fiber.Ctx, result *ImportResponse, err error) error {
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_IMPORT_INVALID", "Configuration import request is invalid.")
	}
	if errors.Is(err, ErrUnsupportedSchema) {
		return respond.Error(c, fiber.StatusBadRequest, "CONFIGURATION_IMPORT_SCHEMA_UNSUPPORTED", "Configuration import schema is not supported.")
	}
	if errors.Is(err, ErrImportConflict) {
		return respond.JSON(c, fiber.StatusConflict, result)
	}
	if errors.Is(err, salon.ErrNotFound) || errors.Is(err, integrationconfig.ErrNotFound) || errors.Is(err, training.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "CONFIGURATION_IMPORT_FAILED", "Could not import configuration.")
	}
	return respond.JSON(c, fiber.StatusOK, result)
}

func parseImportRequest(c *fiber.Ctx) (*ImportRequest, error) {
	var req ImportRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}
