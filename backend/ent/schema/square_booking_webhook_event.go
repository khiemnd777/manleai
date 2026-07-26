package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SquareBookingWebhookEvent struct {
	ent.Schema
}

func (SquareBookingWebhookEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("event_id").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("merchant_id").NotEmpty(),
		field.String("location_id").NotEmpty(),
		field.String("pos_booking_id").NotEmpty(),
		field.Int("pos_booking_version").Optional().Nillable(),
		field.String("booking_status").Optional(),
		field.Time("booking_start_at").Optional().Nillable(),
		field.Time("delivered_at").Optional().Nillable(),
		field.String("processing_status").Default("pending"),
		field.Int("processing_attempts").Default(0).NonNegative(),
		field.String("processing_token").Optional(),
		field.Time("next_attempt_at").Default(time.Now),
		field.Time("processing_lease_expires_at").Optional().Nillable(),
		field.Time("processed_at").Optional().Nillable(),
		field.String("last_error").Optional(),
		field.String("last_error_class").Optional(),
		field.String("last_error_code").Optional(),
		field.Time("dead_lettered_at").Optional().Nillable(),
		field.Int("requeue_count").Default(0).NonNegative(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
