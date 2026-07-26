package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRuntimeLimitAction struct{ ent.Schema }

func (TenantRuntimeLimitAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("request_fingerprint").NotEmpty(),
		field.JSON("result_snapshot", map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
