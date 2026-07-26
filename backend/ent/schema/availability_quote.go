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
		field.String("provider").Optional().Nillable(),
		field.String("provider_location_id").Optional().Nillable(),
		field.Int64("provider_snapshot_generation").Optional().Nillable().Positive(),
		field.String("scheduling_authority").Default("external_provider"),
		field.String("authority_provider").Optional(),
		field.String("authority_location_id").Optional(),
		field.Int64("authority_snapshot_generation").Optional().Nillable().NonNegative(),
		field.Int64("scheduling_authority_version").Optional().Nillable().Positive(),
		field.String("authority_fence_provenance").Default("known"),
		field.UUID("retry_of_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.Int64("authority_config_version").Optional().Nillable().Positive(),
		field.String("operation_type").Optional().Nillable(),
		field.UUID("target_appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.Int("target_authority_appointment_version").Optional().Nillable().NonNegative(),
		field.Int("party_size").Default(1).Positive(),
		field.String("request_fingerprint").NotEmpty(),
		field.Time("expires_at"),
		field.Time("consumed_at").Optional().Nillable(),
		field.UUID("consumed_by_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
