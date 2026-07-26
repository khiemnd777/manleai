package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarServicePolicy struct {
	ent.Schema
}

func (ManleAICalendarServicePolicy) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_service_policies"}}
}

func (ManleAICalendarServicePolicy) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.Bool("enabled").Default(false),
		field.String("capacity_mode").Optional().Nillable(),
		field.Int("buffer_before_minutes").Optional().Nillable().NonNegative(),
		field.Int("buffer_after_minutes").Optional().Nillable().NonNegative(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
