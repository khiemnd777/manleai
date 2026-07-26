package customernotification

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/customer-sms-policy", handler.GetPolicy)
	group.Put("/:id/customer-sms-policy", handler.UpdatePolicy)
	group.Post("/:id/customer-sms-consents/attest", handler.AttestConsent)
	group.Get("/:id/appointments/:appointment_id/customer-notifications", handler.AppointmentDetail)
	group.Post("/:id/appointments/:appointment_id/customer-notifications/:delivery_id/requeue", handler.Requeue)
	group.Get("/:id/scheduling-requests/:request_id/customer-notifications", handler.RequestDetail)
	group.Post("/:id/scheduling-requests/:request_id/customer-notifications/:delivery_id/requeue", handler.RequeueRequest)
}
