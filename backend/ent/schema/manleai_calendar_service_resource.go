package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarServiceResource struct {
	ent.Schema
}

func (ManleAICalendarServiceResource) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_service_resources"}}
}

func (ManleAICalendarServiceResource) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.UUID("resource_pool_id", uuid.UUID{}),
		field.Int("units_required").Positive(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
