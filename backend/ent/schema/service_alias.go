package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceAlias struct {
	ent.Schema
}

func (ServiceAlias) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.String("alias").NotEmpty(),
		field.String("normalized_alias").NotEmpty(),
		field.String("source").Default("owner"),
		field.String("status").Default("active"),
		field.Float("confidence").Default(0.94),
		field.UUID("correction_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
