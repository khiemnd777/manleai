package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRegistrationRequestEvent struct{ ent.Schema }

func (TenantRegistrationRequestEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("event_type").NotEmpty(),
		field.String("from_status").Optional().Nillable(),
		field.String("to_status").Optional().Nillable(),
		field.Int64("request_version").Positive(),
		field.JSON("details", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
