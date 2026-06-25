package public_catalog

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(api fiber.Router, handler *Handler) {
	api.Get("/public/salon", handler.GetFirstPublished)

	group := api.Group("/public/salons")
	group.Get("/:slug", handler.GetBySlug)
}
