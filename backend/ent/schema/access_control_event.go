package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AccessControlEvent struct{ ent.Schema }

func (AccessControlEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("action_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("salon_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("target_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("event_type").NotEmpty(),
		field.String("object_type").NotEmpty(),
		field.String("object_id").NotEmpty(),
		field.JSON("details", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
