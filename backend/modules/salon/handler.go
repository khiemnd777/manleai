package salon

import (
	"errors"

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

func (h *Handler) List(c *fiber.Ctx) error {
	items, err := h.service.List(c.UserContext(), middleware.UserID(c))
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SALONS_FAILED", "Could not load salons.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"salons": items})
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateSalonRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.Create(c.UserContext(), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Salon name and phone are required.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SALON_CREATE_FAILED", "Could not create salon.")
	}
	return respond.JSON(c, fiber.StatusCreated, item)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	item, err := h.service.Get(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SALON_FAILED", "Could not load salon.")
	}
	return respond.JSON(c, fiber.StatusOK, item)
}

func (h *Handler) Update(c *fiber.Ctx) error {
	var req UpdateSalonRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	item, err := h.service.Update(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Salon name and phone are required.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SALON_UPDATE_FAILED", "Could not update salon.")
	}
	return respond.JSON(c, fiber.StatusOK, item)
}

func (h *Handler) GetSettings(c *fiber.Ctx) error {
	settings, err := h.service.GetSettings(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon settings not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SETTINGS_FAILED", "Could not load settings.")
	}
	return respond.JSON(c, fiber.StatusOK, settings)
}

func (h *Handler) UpdateSettings(c *fiber.Ctx) error {
	var req UpdateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	settings, err := h.service.UpdateSettings(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Settings contain invalid values.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon settings not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", "Could not update settings.")
	}
	return respond.JSON(c, fiber.StatusOK, settings)
}

func (h *Handler) GetPublicCatalogSettings(c *fiber.Ctx) error {
	settings, err := h.service.GetPublicCatalogSettings(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PUBLIC_CATALOG_FAILED", "Could not load public catalog settings.")
	}
	return respond.JSON(c, fiber.StatusOK, settings)
}

func (h *Handler) UpdatePublicCatalogSettings(c *fiber.Ctx) error {
	var req UpdatePublicCatalogRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	settings, err := h.service.UpdatePublicCatalogSettings(c.UserContext(), c.Params("id"), middleware.UserID(c), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Public catalog requires a slug, one synced AI-bookable service, and one synced AI-bookable staff member.")
	}
	if errors.Is(err, ErrSlugUnavailable) {
		return respond.Error(c, fiber.StatusConflict, "PUBLIC_SLUG_UNAVAILABLE", "Public page slug is already in use.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PUBLIC_CATALOG_UPDATE_FAILED", "Could not update public catalog settings.")
	}
	return respond.JSON(c, fiber.StatusOK, settings)
}

func (h *Handler) GetBusinessHours(c *fiber.Ctx) error {
	hours, err := h.service.GetBusinessHours(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BUSINESS_HOURS_FAILED", "Could not load business hours.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"periods": hours})
}

func (h *Handler) UpdateBusinessHours(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusConflict, "BUSINESS_HOURS_POS_MANAGED", "Business hours are managed in Square and imported through Square sync.")
}
