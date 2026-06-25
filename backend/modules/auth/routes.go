package auth

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/auth")
	group.Post("/login", handler.Login)
	group.Get("/bootstrap/status", handler.BootstrapStatus)
	group.Post("/bootstrap-owner", handler.BootstrapOwner)
	group.Post("/refresh-token", handler.Refresh)
	group.Post("/logout", handler.Logout)
	group.Get("/me", middleware.RequireAuth(jwtSecret), handler.Me)
}
