package pos_square

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Handler struct {
	service *Service
	cfg     config.Config
}

func NewHandler(service *Service, cfg config.Config) *Handler {
	return &Handler{service: service, cfg: cfg}
}

func (h *Handler) ConnectURL(c *fiber.Ctx) error {
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
	}
	res, err := h.service.ConnectURL(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CONNECT_URL_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Callback(c *fiber.Ctx) error {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_SQUARE_CALLBACK", "Square callback is missing code or state.")
	}
	connection, err := h.service.HandleCallback(c.UserContext(), code, state, h.cfg.Square.RedirectURL)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_CALLBACK_FAILED", err.Error())
	}
	if c.Query("format") == "json" {
		return respond.JSON(c, fiber.StatusOK, connection)
	}
	redirect := h.cfg.FrontendURL + "/dashboard/integrations?square=connected&salon_id=" + connection.SalonID
	return c.Redirect(redirect, fiber.StatusFound)
}

func (h *Handler) Status(c *fiber.Ctx) error {
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
	}
	res, err := h.service.Status(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SQUARE_STATUS_FAILED", "Could not load Square status.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Locations(c *fiber.Ctx) error {
	salonID := c.Query("salon_id")
	if salonID == "" {
		salonID = middleware.SalonID(c)
	}
	locations, err := h.service.Locations(c.UserContext(), salonID, middleware.UserID(c))
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if errors.Is(err, ErrNotConnected) {
		return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_LOCATIONS_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"locations": locations})
}

type selectLocationRequest struct {
	SalonID    string `json:"salon_id"`
	LocationID string `json:"location_id"`
}

func (h *Handler) SelectLocation(c *fiber.Ctx) error {
	var req selectLocationRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
	}
	connection, err := h.service.SelectLocation(c.UserContext(), req.SalonID, middleware.UserID(c), req.LocationID)
	if errors.Is(err, pos.ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "SQUARE_LOCATION_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, connection)
}

type syncRequest struct {
	SalonID string `json:"salon_id"`
}

func (h *Handler) Sync(c *fiber.Ctx) error {
	var req syncRequest
	_ = c.BodyParser(&req)
	if req.SalonID == "" {
		req.SalonID = middleware.SalonID(c)
	}
	if err := h.service.Sync(c.UserContext(), req.SalonID, middleware.UserID(c)); err != nil {
		if errors.Is(err, pos.ErrNotFound) {
			return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
		}
		if errors.Is(err, ErrNotConnected) {
			return respond.Error(c, fiber.StatusConflict, "SQUARE_NOT_CONNECTED", "Square is not connected.")
		}
		return respond.Error(c, fiber.StatusBadGateway, "SQUARE_SYNC_FAILED", err.Error())
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *Handler) NotYet(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusNotImplemented, "MILESTONE_3_REQUIRED", "This Square booking operation is reserved for Milestone 3.")
}
