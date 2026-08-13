package auth

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service *Service
}

const refreshCookieName = "manleai_refresh"

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
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "LOGIN_FAILED", "Could not complete login.")
	}
	h.setRefreshCookie(c, res.RefreshToken)
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
	h.setRefreshCookie(c, res.RefreshToken)
	return respond.JSON(c, fiber.StatusCreated, res)
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	res, err := h.service.Refresh(c.UserContext(), c.Cookies(refreshCookieName))
	if err != nil {
		return respond.Error(c, fiber.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Refresh token is invalid or expired.")
	}
	h.setRefreshCookie(c, res.RefreshToken)
	return respond.JSON(c, fiber.StatusOK, res)
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies(refreshCookieName)
	h.clearRefreshCookie(c)
	if err := h.service.Logout(c.UserContext(), refreshToken); err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "LOGOUT_FAILED", "Could not complete logout.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *Handler) setRefreshCookie(c *fiber.Ctx, refreshToken string) {
	if h == nil || h.service == nil || refreshToken == "" {
		return
	}
	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    refreshToken,
		Path:     "/api/auth",
		MaxAge:   int(h.service.cfg.RefreshTokenTTL / time.Second),
		Expires:  time.Now().UTC().Add(h.service.cfg.RefreshTokenTTL),
		HTTPOnly: true,
		Secure:   h.service.cfg.IsProductionDeployment(),
		SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func (h *Handler) clearRefreshCookie(c *fiber.Ctx) {
	secure := h != nil && h.service != nil && h.service.cfg.IsProductionDeployment()
	c.Cookie(&fiber.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func (h *Handler) Me(c *fiber.Ctx) error {
	res, err := h.service.Me(c.UserContext(), middleware.UserID(c))
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "ME_FAILED", "Could not load current user.")
	}
	return respond.JSON(c, fiber.StatusOK, res)
}
