package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AvailabilityQuoteSlotResourceAllocation struct {
	ent.Schema
}

func (AvailabilityQuoteSlotResourceAllocation) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "availability_quote_slot_resource_allocations"}}
}

func (AvailabilityQuoteSlotResourceAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("quote_slot_segment_id", uuid.UUID{}),
		field.UUID("resource_pool_id", uuid.UUID{}),
		field.Int("units_allocated").Positive(),
		field.Time("created_at").Default(time.Now),
	}
}
