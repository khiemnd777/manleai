package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceCategory struct {
	ent.Schema
}

func (ServiceCategory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty(),
		field.String("description").Optional(),
		field.String("status").Default("active"),
		field.Int("sort_order").Default(0),
		field.String("source").Default("manual"),
		field.UUID("reviewed_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("reviewed_at").Optional().Nillable(),
		field.Time("archived_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
