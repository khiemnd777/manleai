package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Customer struct {
	ent.Schema
}

func (Customer) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("phone").Optional(),
		field.String("normalized_phone").Optional(),
		field.String("email").Optional(),
		field.String("normalized_email").Optional(),
		field.String("notes").Optional(),
		field.Bool("active").Default(true),
		field.String("sync_status").Default("local_only"),
		field.Time("archived_at").Optional().Nillable(),
		field.Time("last_synced_at").Optional().Nillable(),
		field.String("sync_error").Optional(),
		field.String("source").Default("local"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
