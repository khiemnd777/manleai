package scheduling_manleai_calendar

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons/:id/manleai-calendar", middleware.RequireAuth(jwtSecret))
	group.Get("/", handler.GetAggregate)
	group.Put("/config", handler.PutConfig)
	group.Put("/hours", handler.PutHours)
	group.Get("/staff/:staff_id", handler.GetStaff)
	group.Put("/staff/:staff_id", handler.PutStaff)
	group.Get("/services/:service_id", handler.GetService)
	group.Put("/services/:service_id", handler.PutService)
	group.Get("/resources", handler.ListResources)
	group.Post("/resources", handler.CreateResource)
	group.Put("/resources/:resource_id", handler.UpdateResource)
	group.Post("/resources/:resource_id/archive", handler.ArchiveResource)
	group.Post("/exceptions", handler.CreateException)
	group.Post("/exceptions/:exception_id/cancel", handler.CancelException)
	group.Post("/activate", handler.Activate)
}
