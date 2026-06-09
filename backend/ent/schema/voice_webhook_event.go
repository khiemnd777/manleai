package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type VoiceWebhookEvent struct {
	ent.Schema
}

func (VoiceWebhookEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("call_session_id", uuid.UUID{}).Optional().Nillable(),
		field.String("provider").NotEmpty(),
		field.String("provider_call_id").Optional(),
		field.String("event_type").NotEmpty(),
		field.JSON("payload", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
