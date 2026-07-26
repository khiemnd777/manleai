package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SquareBookingWebhookAction struct{ ent.Schema }

func (SquareBookingWebhookAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("webhook_event_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_fingerprint").NotEmpty(),
		field.String("action_type").NotEmpty(),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("result_processing_status").NotEmpty(),
		field.Int("result_processing_attempts").NonNegative(),
		field.Int("result_requeue_count").NonNegative(),
		field.Time("created_at").Default(time.Now),
	}
}
