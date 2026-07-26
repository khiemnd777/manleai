package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CustomerNotificationDelivery struct{ ent.Schema }

func (CustomerNotificationDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("customer_sms_consent_id", uuid.UUID{}),
		field.UUID("scheduling_request_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("booking_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.String("notification_type").NotEmpty(),
		field.Int("source_version").NonNegative(),
		field.String("dedupe_key").NotEmpty(),
		field.String("message_body").NotEmpty(),
		field.String("destination_e164").Optional(),
		field.String("destination_masked").Optional(),
		field.String("destination_hash").Optional(),
		field.Int("consent_version").Positive(),
		field.Int64("policy_version").Positive(),
		field.String("delivery_status").Default("queued"),
		field.String("delivery_provider").Optional(),
		field.Int("delivery_attempts").Default(0),
		field.Time("next_delivery_at").Default(time.Now),
		field.UUID("delivery_claim_token", uuid.UUID{}).Optional().Nillable(),
		field.Time("delivery_claimed_at").Optional().Nillable(),
		field.Time("delivery_lease_expires_at").Optional().Nillable(),
		field.Time("delivery_dispatch_started_at").Optional().Nillable(),
		field.String("provider_message_id").Optional(),
		field.String("provider_status").Optional(),
		field.Int("provider_status_rank").Default(0),
		field.Time("last_provider_event_at").Optional().Nillable(),
		field.Time("delivered_at").Optional().Nillable(),
		field.Time("dead_lettered_at").Optional().Nillable(),
		field.Time("suppressed_at").Optional().Nillable(),
		field.Int("requeue_count").Default(0),
		field.String("last_delivery_error_code").Optional(),
		field.Time("retention_expires_at").Optional().Nillable(),
		field.Time("redacted_at").Optional().Nillable(),
		field.Int("redaction_version").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
