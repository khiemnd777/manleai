package auth

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

func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	if req.Email == "" || req.Password == "" {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required.")
	}
	res, err := h.service.Login(c.UserContext(), req)
	if errors.Is(err, ErrInvalidCredentials) {
		return respond.Error(c, fiber.StatusUnauthorized, "INVALID_CREDENTIALS", "Email or password is incorrect.")
	}
	if errors.Is(err, ErrDisabledUser) {
		return respond.Error(c, fiber.StatusForbidden, "USER_DISABLED", "This user is disabled.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "LOGIN_FAILED", "Could not complete login.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) BootstrapStatus(c *fiber.Ctx) error {
	res, err := h.service.BootstrapStatus(c.UserContext())
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOTSTRAP_STATUS_FAILED", "Could not load account setup status.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) BootstrapOwner(c *fiber.Ctx) error {
	var req BootstrapOwnerRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	res, err := h.service.BootstrapOwner(c.UserContext(), req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "A valid email, owner name, and password with at least 8 characters are required.")
	}
	if errors.Is(err, ErrBootstrapClosed) {
		return respond.Error(c, fiber.StatusConflict, "BOOTSTRAP_CLOSED", "Owner account setup is already complete.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "BOOTSTRAP_OWNER_FAILED", "Could not create owner account.")
	}
	return respond.JSON(c, fiber.StatusCreated, res)
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	var req RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body is invalid.")
	}
	res, err := h.service.Refresh(c.UserContext(), req.RefreshToken)
	if err != nil {
		return respond.Error(c, fiber.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Refresh token is invalid or expired.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	var req LogoutRequest
	_ = c.BodyParser(&req)
	if err := h.service.Logout(c.UserContext(), req.RefreshToken); err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "LOGOUT_FAILED", "Could not complete logout.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *Handler) Me(c *fiber.Ctx) error {
	res, err := h.service.Me(c.UserContext(), middleware.UserID(c))
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "ME_FAILED", "Could not load current user.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}
