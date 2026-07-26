package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SalonMembership struct{ ent.Schema }

func (SalonMembership) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.UUID("role_id", uuid.UUID{}),
		field.String("status").Default("active"),
		field.Bool("is_owner").Default(false),
		field.Int64("version").Default(1),
		field.UUID("created_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("updated_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
