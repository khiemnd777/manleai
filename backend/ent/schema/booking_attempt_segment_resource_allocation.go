package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BookingAttemptSegmentResourceAllocation struct {
	ent.Schema
}

func (BookingAttemptSegmentResourceAllocation) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "booking_attempt_segment_resource_allocations"}}
}

func (BookingAttemptSegmentResourceAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_segment_id", uuid.UUID{}),
		field.UUID("resource_pool_id", uuid.UUID{}),
		field.Int("units_allocated").Positive(),
		field.Time("created_at").Default(time.Now),
	}
}
