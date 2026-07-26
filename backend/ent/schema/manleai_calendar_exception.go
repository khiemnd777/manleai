package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarException struct {
	ent.Schema
}

func (ManleAICalendarException) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_exceptions"}}
}

func (ManleAICalendarException) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("scope_type").NotEmpty(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("resource_pool_id", uuid.UUID{}).Optional().Nillable(),
		field.String("effect").NotEmpty(),
		field.Time("starts_at"),
		field.Time("ends_at"),
		field.Int("capacity_override").Optional().Nillable().NonNegative(),
		field.String("reason").Optional().Nillable(),
		field.UUID("created_by_user_id", uuid.UUID{}),
		field.Time("cancelled_at").Optional().Nillable(),
		field.UUID("cancelled_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
