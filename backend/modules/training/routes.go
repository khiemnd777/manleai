package training

import (
	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func RegisterRoutes(api fiber.Router, handler *Handler, jwtSecret string) {
	group := api.Group("/salons", middleware.RequireAuth(jwtSecret))
	group.Get("/:id/knowledge-items", handler.ListKnowledge)
	group.Post("/:id/knowledge-items", handler.CreateKnowledge)
	group.Put("/:id/knowledge-items/:item_id", handler.UpdateKnowledge)
	group.Delete("/:id/knowledge-items/:item_id", handler.DeleteKnowledge)
	group.Get("/:id/service-aliases", handler.ListServiceAliases)
	group.Post("/:id/service-aliases", handler.UpsertServiceAlias)
	group.Get("/:id/owner-corrections", handler.ListCorrections)
	group.Post("/:id/owner-corrections", handler.CreateCorrection)
	group.Post("/:id/owner-corrections/:correction_id/apply", handler.ApplyCorrection)
	group.Post("/:id/owner-corrections/:correction_id/apply-service-alias", handler.ApplyServiceAliasCorrection)
	group.Post("/:id/owner-corrections/:correction_id/dismiss", handler.DismissCorrection)
	group.Post("/:id/training/evaluate", handler.Evaluate)
}
