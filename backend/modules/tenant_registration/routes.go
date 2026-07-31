package tenant_registration

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	api.Post("/public/tenant-registration-requests", middleware.DatabaseScope(databasecontext.ScopePublic), handler.Submit)
	group := api.Group("/platform/registration-requests", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.List)
	group.Get("/:request_id", handler.Get)
	group.Patch("/:request_id", handler.Mutate)
	group.Post("/:request_id/notes", handler.AddNote)
}
