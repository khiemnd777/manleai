package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SchedulingRequestSegment struct {
	ent.Schema
}

func (SchedulingRequestSegment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("scheduling_request_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.String("service_name").NotEmpty(),
		field.String("guest_reference").Optional(),
		field.Int("quantity").Default(1).Positive(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_name").Optional(),
		field.String("staff_selection_mode").Default("specific"),
		field.Int("duration_minutes").Positive(),
		field.Time("requested_start_time").Optional().Nillable(),
		field.Time("requested_end_time").Optional().Nillable(),
		field.Int("sort_order").Positive(),
		field.Time("redacted_at").Optional().Nillable(),
		field.Int("redaction_version").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
