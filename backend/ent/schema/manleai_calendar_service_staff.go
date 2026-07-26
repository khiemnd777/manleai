package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarServiceStaff struct {
	ent.Schema
}

func (ManleAICalendarServiceStaff) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_service_staff"}}
}

func (ManleAICalendarServiceStaff) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.UUID("staff_id", uuid.UUID{}),
		field.Time("created_at").Default(time.Now),
	}
}
