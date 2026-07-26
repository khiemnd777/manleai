package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CustomerSMSConsent struct{ ent.Schema }

func (CustomerSMSConsent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("normalized_destination").NotEmpty(),
		field.String("destination_masked").NotEmpty(),
		field.String("status").NotEmpty(),
		field.Int("version").Default(1).Positive(),
		field.String("source").NotEmpty(),
		field.String("evidence_type").NotEmpty(),
		field.String("evidence_reference").NotEmpty(),
		field.UUID("call_session_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("requested_at").Optional().Nillable(),
		field.Time("consented_at").Optional().Nillable(),
		field.Time("declined_at").Optional().Nillable(),
		field.Time("opted_out_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
