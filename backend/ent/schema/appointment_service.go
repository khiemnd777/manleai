package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AppointmentService struct {
	ent.Schema
}

func (AppointmentService) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("appointment_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_selection_mode").Default("specific"),
		field.String("pos_service_id").Optional().Nillable(),
		field.Int64("pos_service_version").Optional(),
		field.String("pos_staff_id").Optional(),
		field.String("scheduling_authority").Default("external_provider"),
		field.String("authority_provider").Optional(),
		field.String("authority_service_id").Optional(),
		field.Int64("authority_service_version").Optional().Nillable().NonNegative(),
		field.String("authority_staff_id").Optional(),
		field.String("name").NotEmpty(),
		field.Int("duration_minutes").Default(0),
		field.Float("price_from").Optional(),
		field.Int("sort_order").Default(1),
		field.Int("plan_version").Default(1).Positive(),
		field.String("guest_reference").Optional().Nillable(),
		field.Time("scheduled_start_time").Optional().Nillable(),
		field.Time("scheduled_end_time").Optional().Nillable(),
		field.Int("buffer_before_minutes").Optional().Nillable().NonNegative(),
		field.Int("buffer_after_minutes").Optional().Nillable().NonNegative(),
		field.Time("occupied_start_time").Optional().Nillable(),
		field.Time("occupied_end_time").Optional().Nillable(),
		field.Time("released_at").Optional().Nillable(),
		field.UUID("released_by_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
