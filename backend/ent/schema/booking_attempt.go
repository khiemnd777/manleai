package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BookingAttempt struct {
	ent.Schema
}

func (BookingAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("source").Default("owner_dashboard"),
		field.String("status").NotEmpty(),
		field.String("pos_provider").Default("square"),
		field.String("pos_booking_id").Optional(),
		field.String("pos_idempotency_key").Optional(),
		field.String("customer_name").NotEmpty(),
		field.String("customer_phone").NotEmpty(),
		field.String("customer_email").Optional(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_selection_mode").Default("specific"),
		field.Time("requested_start_time"),
		field.Time("requested_end_time"),
		field.String("notes").Optional(),
		field.String("error_code").Optional(),
		field.String("error_message").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
