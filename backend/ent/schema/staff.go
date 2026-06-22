package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Staff struct {
	ent.Schema
}

func (Staff) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("pos_provider").Default("square"),
		field.String("pos_staff_id").Optional(),
		field.String("name").NotEmpty(),
		field.String("phone").Optional(),
		field.String("email").Optional(),
		field.Bool("ai_bookable").Default(true),
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
