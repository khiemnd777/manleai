package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AvailabilityQuoteSlotSegment struct {
	ent.Schema
}

func (AvailabilityQuoteSlotSegment) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "availability_quote_slot_segments"}}
}

func (AvailabilityQuoteSlotSegment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("quote_slot_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.UUID("staff_id", uuid.UUID{}),
		field.String("staff_selection_mode").Default("specific"),
		field.String("guest_reference").Optional().Nillable(),
		field.Int("duration_minutes").Positive(),
		field.Int("sort_order").Positive(),
		field.Time("scheduled_start_time"),
		field.Time("scheduled_end_time"),
		field.Int("buffer_before_minutes").NonNegative(),
		field.Int("buffer_after_minutes").NonNegative(),
		field.Time("occupied_start_time"),
		field.Time("occupied_end_time"),
		field.Time("created_at").Default(time.Now),
	}
}
