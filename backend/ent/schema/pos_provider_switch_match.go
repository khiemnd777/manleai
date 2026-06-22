package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSProviderSwitchMatch struct {
	ent.Schema
}

func (POSProviderSwitchMatch) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("run_id", uuid.UUID{}),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("entity_type").NotEmpty(),
		field.UUID("canonical_entity_id", uuid.UUID{}).Optional().Nillable(),
		field.String("canonical_name").Optional(),
		field.String("provider_entity_id").NotEmpty(),
		field.String("provider_name").NotEmpty(),
		field.String("provider_phone").Optional(),
		field.String("provider_email").Optional(),
		field.Int("provider_duration_minutes").Optional(),
		field.String("match_status").Default("unmatched"),
		field.Int("match_confidence").Default(0),
		field.String("match_reason").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
