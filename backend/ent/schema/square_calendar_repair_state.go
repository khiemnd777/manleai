package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SquareCalendarRepairState struct {
	ent.Schema
}

func (SquareCalendarRepairState) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "square_calendar_repair_state"}}
}

func (SquareCalendarRepairState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}).Unique(),
		field.Time("next_repair_at").Default(time.Now),
		field.Time("lease_expires_at").Optional().Nillable(),
		field.String("lease_token").Optional(),
		field.Int("repair_attempts").Default(0).NonNegative(),
		field.Time("last_repaired_at").Optional().Nillable(),
		field.String("last_error").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
