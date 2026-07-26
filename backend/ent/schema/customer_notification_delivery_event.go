package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CustomerNotificationDeliveryEvent struct{ ent.Schema }

func (CustomerNotificationDeliveryEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("customer_notification_delivery_id", uuid.UUID{}),
		field.String("event_key").NotEmpty(),
		field.String("event_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("delivery_status").NotEmpty(),
		field.String("provider_status").Optional(),
		field.String("error_code").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
