package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSEntityLink struct {
	ent.Schema
}

func (POSEntityLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("entity_type").NotEmpty(),
		field.UUID("entity_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("provider_entity_id").Optional(),
		field.Int64("provider_version").Optional(),
		field.String("sync_status").Default("local_only"),
		field.Time("last_synced_at").Optional().Nillable(),
		field.String("last_error").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
