package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SchedulingRequestEvent struct {
	ent.Schema
}

func (SchedulingRequestEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("scheduling_request_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.Int("request_version").Positive(),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("redacted_at").Optional().Nillable(),
		field.Int("redaction_version").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
