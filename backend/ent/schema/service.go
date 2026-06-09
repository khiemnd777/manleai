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
		field.String("pos_service_id").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("description").Optional(),
		field.String("ai_description").Optional(),
		field.Int("duration_minutes").Default(0),
		field.Float("price_from").Optional(),
		field.String("price_display").Optional(),
		field.Bool("ai_bookable").Default(true),
		field.Bool("active").Default(true),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
