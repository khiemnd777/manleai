package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BookingReconciliationEvent struct {
	ent.Schema
}

func (BookingReconciliationEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_id", uuid.UUID{}),
		field.UUID("reconciliation_task_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("action_key").NotEmpty(),
		field.String("payload_fingerprint").NotEmpty(),
		field.String("action").NotEmpty(),
		field.String("provider_appointment_id").Optional(),
		field.Int("provider_appointment_version").Optional(),
		field.String("provider_status").Optional(),
		field.String("note").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
