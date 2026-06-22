package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSSyncJob struct {
	ent.Schema
}

func (POSSyncJob) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("entity_type").NotEmpty(),
		field.UUID("entity_id", uuid.UUID{}),
		field.String("operation").NotEmpty(),
		field.String("status").Default("queued"),
		field.Int("attempt_count").Default(0),
		field.Int("max_attempts").Default(5),
		field.Time("next_attempt_at").Default(time.Now),
		field.Time("locked_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.String("last_error").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
