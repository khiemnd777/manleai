package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OwnerCorrection struct {
	ent.Schema
}

func (OwnerCorrection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("call_session_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("transcript_message_id", uuid.UUID{}).Optional().Nillable(),
		field.String("correction"),
		field.String("status").Default("pending"),
		field.UUID("applied_knowledge_item_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
