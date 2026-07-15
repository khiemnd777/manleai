package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AvailabilityQuote struct {
	ent.Schema
}

func (AvailabilityQuote) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("provider_location_id").NotEmpty(),
		field.Int64("provider_snapshot_generation").Positive(),
		field.String("request_fingerprint").NotEmpty(),
		field.Time("expires_at"),
		field.Time("consumed_at").Optional().Nillable(),
		field.UUID("consumed_by_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
