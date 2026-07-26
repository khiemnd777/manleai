package scheduling_authority_switch

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons/:id/scheduling-authority-switches", middleware.RequireAuth(jwtSecret))
	group.Post("/preview", handler.Preview)
	group.Get("/latest", handler.Latest)
	group.Get("/:run_id", handler.Get)
	group.Post("/:run_id/commit", handler.Commit)
}
