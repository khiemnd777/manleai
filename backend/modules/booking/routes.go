package booking

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Post("/:id/availability", handler.Availability)
	group.Get("/:id/calendar", handler.Calendar)
	group.Get("/:id/calendar/events/stream", handler.CalendarEventStream)
	group.Post("/:id/calendar/sync", handler.SyncCalendar)
	group.Get("/:id/appointments", handler.Appointments)
	group.Post("/:id/appointments/:appointment_id/reschedule", handler.Reschedule)
	group.Post("/:id/appointments/:appointment_id/cancel", handler.Cancel)
	group.Get("/:id/booking-attempts", handler.Attempts)
	group.Get("/:id/booking-reconciliations", handler.ReconciliationTasks)
	group.Get("/:id/booking-reconciliations/:attempt_id/candidates", handler.ReconciliationCandidates)
	group.Post("/:id/booking-reconciliations/:attempt_id/resolve", handler.ResolveReconciliation)
	group.Post("/:id/booking-attempts", handler.Create)
}
