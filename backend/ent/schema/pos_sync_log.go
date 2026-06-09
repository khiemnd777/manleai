package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSSyncLog struct {
	ent.Schema
}

func (POSSyncLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("sync_type").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("message").Optional(),
		field.Time("started_at").Default(time.Now),
		field.Time("completed_at").Optional().Nillable(),
	}
}
