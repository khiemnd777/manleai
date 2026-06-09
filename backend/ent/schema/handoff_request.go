package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type HandoffRequest struct {
	ent.Schema
}

func (HandoffRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("call_session_id", uuid.UUID{}),
		field.String("status").Default("pending"),
		field.String("reason").NotEmpty(),
		field.String("customer_name").Optional(),
		field.String("customer_phone").Optional(),
		field.String("summary").NotEmpty(),
		field.Time("created_at").Default(time.Now),
		field.Time("resolved_at").Optional().Nillable(),
	}
}
