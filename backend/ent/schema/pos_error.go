package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSError struct {
	ent.Schema
}

func (POSError) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("operation").NotEmpty(),
		field.String("error_code").NotEmpty(),
		field.String("error_message").NotEmpty(),
		field.JSON("payload", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
