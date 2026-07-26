package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AccessControlAction struct{ ent.Schema }

func (AccessControlAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.UUID("salon_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("target_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("action_key").NotEmpty(),
		field.String("action_type").NotEmpty(),
		field.String("request_fingerprint").NotEmpty(),
		field.JSON("response_payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
