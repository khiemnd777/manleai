package customer

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/customers", handler.List)
	group.Post("/:id/customers", handler.Create)
	group.Put("/:id/customers/:customer_id", handler.Update)
	group.Post("/:id/customers/:customer_id/archive", handler.Archive)
	group.Get("/:id/customers/search", handler.SearchPOS)
}
