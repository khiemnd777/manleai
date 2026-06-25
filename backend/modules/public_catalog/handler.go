package public_catalog

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetFirstPublished(c *fiber.Ctx) error {
	catalog, err := h.service.GetFirstPublished(c.UserContext())
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PUBLIC_CATALOG_NOT_FOUND", "Public salon page not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PUBLIC_CATALOG_FAILED", "Could not load public salon page.")
	}
	return respond.JSON(c, fiber.StatusOK, catalog)
}

func (h *Handler) GetBySlug(c *fiber.Ctx) error {
	catalog, err := h.service.GetBySlug(c.UserContext(), c.Params("slug"))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PUBLIC_CATALOG_NOT_FOUND", "Public salon page not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PUBLIC_CATALOG_FAILED", "Could not load public salon page.")
	}
	return respond.JSON(c, fiber.StatusOK, catalog)
}
