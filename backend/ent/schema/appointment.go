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
		field.String("pos_provider").Optional().Nillable(),
		field.String("pos_appointment_id").Optional().Nillable(),
		field.Int("pos_appointment_version").Optional().Nillable(),
		field.String("pos_customer_id").Optional(),
		field.String("scheduling_authority").Default("external_provider"),
		field.String("authority_provider").Optional(),
		field.String("authority_appointment_id").Optional(),
		field.Int("authority_appointment_version").Optional().Nillable().NonNegative(),
		field.String("authority_customer_id").Optional(),
		field.Int64("scheduling_authority_version").Optional().Nillable().Positive(),
		field.Int64("authority_config_version").Optional().Nillable().Positive(),
		field.Int("party_size").Default(1).Positive(),
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
		field.String("pos_sync_status").Optional().Nillable(),
		field.Time("last_pos_synced_at").Optional().Nillable(),
		field.String("pos_sync_error").Optional(),
		field.Time("confirmed_at").Optional().Nillable(),
		field.UUID("confirmed_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("confirmation_source").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
