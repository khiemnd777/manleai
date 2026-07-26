package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BusinessEvent struct{ ent.Schema }

func (BusinessEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("action_id", uuid.UUID{}),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("surface").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("resource_type").NotEmpty(),
		field.String("resource_id").NotEmpty(),
		field.Int64("previous_version"),
		field.Int64("result_version"),
		field.JSON("details", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
