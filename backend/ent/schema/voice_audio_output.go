package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type VoiceAudioOutput struct {
	ent.Schema
}

func (VoiceAudioOutput) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("call_session_id", uuid.UUID{}).Optional().Nillable(),
		field.String("provider").NotEmpty(),
		field.String("provider_call_id").Optional(),
		field.String("content_type").NotEmpty(),
		field.Bytes("audio_data"),
		field.Time("expires_at"),
		field.Time("created_at").Default(time.Now),
	}
}
