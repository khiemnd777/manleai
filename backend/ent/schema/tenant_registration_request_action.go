package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRegistrationRequestAction struct{ ent.Schema }

func (TenantRegistrationRequestAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_type").NotEmpty(),
		field.String("request_fingerprint").NotEmpty(),
		field.JSON("result_snapshot", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
