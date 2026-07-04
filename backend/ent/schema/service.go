package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Service struct {
	ent.Schema
}

func (Service) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("pos_provider").Default("square"),
		field.String("pos_service_id").Optional(),
		field.Int64("pos_service_version").Optional(),
		field.String("name").NotEmpty(),
		field.String("description").Optional(),
		field.String("ai_description").Optional(),
		field.Int("duration_minutes").Default(0),
		field.Float("price_from").Optional(),
		field.String("price_display").Optional(),
		field.Bool("ai_bookable").Default(true),
		field.Bool("active").Default(true),
		field.String("sync_status").Default("local_only"),
		field.Time("archived_at").Optional().Nillable(),
		field.Time("last_synced_at").Optional().Nillable(),
		field.String("sync_error").Optional(),
		field.String("source").Default("local"),
		field.UUID("service_category_id", uuid.UUID{}).Optional().Nillable(),
		field.String("service_category_source").Default("unassigned"),
		field.Float("service_category_confidence").Optional(),
		field.UUID("service_category_reviewed_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("service_category_reviewed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
