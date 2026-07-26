package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CustomerNotificationDeliveryAttempt struct{ ent.Schema }

func (CustomerNotificationDeliveryAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("customer_notification_delivery_id", uuid.UUID{}),
		field.Int("attempt_number").Positive(),
		field.UUID("claim_token", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("outcome").NotEmpty(),
		field.String("provider_status").Optional(),
		field.String("provider_message_id").Optional(),
		field.String("error_code").Optional(),
		field.Time("started_at").Default(time.Now),
		field.Time("dispatch_started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
