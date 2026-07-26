package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRuntimeLimitEvent struct{ ent.Schema }

func (TenantRuntimeLimitEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("action_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.Int64("previous_version"),
		field.Int64("result_version"),
		field.Strings("changed_fields"),
		field.Time("created_at").Default(time.Now),
	}
}
