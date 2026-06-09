package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CallTranscriptMessage struct {
	ent.Schema
}

func (CallTranscriptMessage) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("speaker").NotEmpty(),
		field.String("body").NotEmpty(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Int("sequence"),
		field.Time("created_at").Default(time.Now),
	}
}
