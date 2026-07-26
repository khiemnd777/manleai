package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BusinessResourceVersion struct{ ent.Schema }

func (BusinessResourceVersion) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.String("resource_type").NotEmpty(),
		field.String("resource_id").NotEmpty(),
		field.Int64("version").Default(1),
		field.UUID("updated_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
