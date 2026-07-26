package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OwnerNotificationDeliveryAction struct{ ent.Schema }

func (OwnerNotificationDeliveryAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("owner_notification_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_fingerprint").NotEmpty(),
		field.String("action_type").NotEmpty(),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("result_delivery_status").NotEmpty(),
		field.Time("created_at").Default(time.Now),
	}
}
