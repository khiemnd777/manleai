package conversation

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type platformAuthorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
	RecordPlatformSupportAction(context.Context, middleware.ActorContext, string, access.Capability, access.PIIScope, string, string) error
}

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret), middleware.RequireTenantSalonAccess())
	group.Get("/:id/conversation-sessions", handler.List)
	group.Post("/:id/conversation-sessions", handler.Start)
	group.Get("/:id/conversation-sessions/:session_id", handler.Get)
	group.Get("/:id/conversation-sessions/:session_id/realtime-events", handler.RealtimeEvents)
	group.Post("/:id/conversation-sessions/:session_id/archive", handler.Archive)
	group.Post("/:id/conversation-sessions/:session_id/redact", handler.Redact)
	group.Post("/:id/conversation-sessions/:session_id/messages", handler.Message)
	group.Get("/:id/party-booking-requests", handler.ListPartyBookingRequests)
	group.Patch("/:id/party-booking-requests/:request_id/status", handler.UpdatePartyBookingRequestStatus)
}

func RegisterPlatformRoutes(api fiber.Router, handler *PlatformHandler, jwtSecret string) {
	group := api.Group("/platform/tenants/:id", middleware.RequireAuth(jwtSecret))
	read := handler.guard(access.CapabilityCallsRead, access.PIIScopeCalls)
	manage := handler.guard(access.CapabilityCallsManage, access.PIIScopeCalls)
	simulate := handler.guard(access.CapabilityCallsSimulate, access.PIIScopeCalls)
	redact := handler.guard(access.CapabilityCallsRedact, access.PIIScopeCalls)
	group.Get("/conversation-sessions", read, handler.delegate.List)
	group.Post("/conversation-sessions", simulate, handler.Start)
	group.Get("/conversation-sessions/:session_id", read, handler.delegate.Get)
	group.Get("/conversation-sessions/:session_id/realtime-events", read, handler.delegate.RealtimeEvents)
	group.Post("/conversation-sessions/:session_id/archive", manage, handler.delegate.Archive)
	group.Post("/conversation-sessions/:session_id/redact", redact, handler.delegate.Redact)
	group.Post("/conversation-sessions/:session_id/messages", simulate, handler.Message)
	group.Get("/party-booking-requests", read, handler.delegate.ListPartyBookingRequests)
	group.Patch("/party-booking-requests/:request_id/status", manage, handler.delegate.UpdatePartyBookingRequestStatus)
}

func (h *PlatformHandler) guard(capability access.Capability, piiScope access.PIIScope) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h == nil || h.access == nil || h.access.Authorize(c.UserContext(), middleware.Actor(c), access.AccessCheck{
			Surface: access.SurfacePlatform, SalonID: c.Params("id"), Capability: capability, PIIScope: piiScope,
		}) != nil {
			return respond.Error(c, fiber.StatusForbidden, "CALLS_ACCESS_FORBIDDEN", "This Platform account is not authorized for this salon Calls action.")
		}
		if h.access.RecordPlatformSupportAction(c.UserContext(), middleware.Actor(c), c.Params("id"), capability, piiScope, c.Method(), c.Path()) != nil {
			return respond.Error(c, fiber.StatusInternalServerError, "SUPPORT_AUDIT_FAILED", "Could not record this authorized support action.")
		}
		return c.Next()
	}
}
