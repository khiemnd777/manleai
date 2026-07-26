package public_catalog

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler) {
	group := api.Group("/public/salons", middleware.DatabaseScope(databasecontext.ScopePublic))
	group.Get("/:slug", handler.GetBySlug)
}
