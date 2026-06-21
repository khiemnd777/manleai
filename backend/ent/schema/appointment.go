package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Appointment struct {
	ent.Schema
}

func (Appointment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_id", uuid.UUID{}),
		field.String("pos_provider").Default("square"),
		field.String("pos_appointment_id").NotEmpty(),
		field.Int("pos_appointment_version").Default(0),
		field.String("status").NotEmpty(),
		field.String("customer_name").NotEmpty(),
		field.String("customer_phone").NotEmpty(),
		field.String("customer_email").Optional(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_selection_mode").Default("specific"),
		field.Time("start_time"),
		field.Time("end_time"),
		field.String("notes").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
