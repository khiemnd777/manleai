package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarAppointmentResourceAllocation struct {
	ent.Schema
}

func (ManleAICalendarAppointmentResourceAllocation) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_appointment_resource_allocations"}}
}

func (ManleAICalendarAppointmentResourceAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("appointment_service_id", uuid.UUID{}),
		field.UUID("resource_pool_id", uuid.UUID{}),
		field.Int("units_allocated").Positive(),
		field.Int("plan_version").Positive(),
		field.Time("occupied_start_time"),
		field.Time("occupied_end_time"),
		field.Time("released_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
