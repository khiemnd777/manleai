package customernotification

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/customer-sms-policy", handler.GetPolicy)
	group.Put("/:id/customer-sms-policy", handler.UpdatePolicy)
	group.Post("/:id/customer-sms-consents/attest", handler.AttestConsent)
	group.Get("/:id/appointments/:appointment_id/customer-notifications", handler.AppointmentDetail)
	group.Get("/:id/scheduling-requests/:request_id/customer-notifications", handler.RequestDetail)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:tenant_id/operations/customer-notifications", middleware.RequireAuth(jwtSecret))
	group.Get("/appointments/:appointment_id", handler.guard(access.CapabilityOperationsRead, handler.delegate.AppointmentDetail))
	group.Post("/appointments/:appointment_id/deliveries/:delivery_id/requeue", handler.guard(access.CapabilityOperationsWrite, handler.delegate.Requeue))
	group.Get("/scheduling-requests/:request_id", handler.guard(access.CapabilityOperationsRead, handler.delegate.RequestDetail))
	group.Post("/scheduling-requests/:request_id/deliveries/:delivery_id/requeue", handler.guard(access.CapabilityOperationsWrite, handler.delegate.RequeueRequest))
}
