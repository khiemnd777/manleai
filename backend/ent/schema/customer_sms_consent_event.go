package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CustomerSMSConsentEvent struct{ ent.Schema }

func (CustomerSMSConsentEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("customer_sms_consent_id", uuid.UUID{}),
		field.Int("event_sequence").Positive(),
		field.Int("consent_version").Positive(),
		field.String("event_key").NotEmpty(),
		field.String("event_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("source").NotEmpty(),
		field.String("evidence_type").NotEmpty(),
		field.String("evidence_reference").NotEmpty(),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("provider_message_id").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
