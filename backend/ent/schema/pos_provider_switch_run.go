package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSProviderSwitchRun struct {
	ent.Schema
}

func (POSProviderSwitchRun) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("from_provider").NotEmpty(),
		field.String("to_provider").NotEmpty(),
		field.String("status").Default("draft"),
		field.String("blocked_reason").Optional(),
		field.Bool("dry_run_ready").Default(false),
		field.Time("activated_at").Optional().Nillable(),
		field.Time("cancelled_at").Optional().Nillable(),
		field.UUID("created_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
