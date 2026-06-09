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
		field.UUID("appointment_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.String("pos_service_id").NotEmpty(),
		field.Int64("pos_service_version").Optional(),
		field.String("name").NotEmpty(),
		field.Int("duration_minutes").Default(0),
		field.Float("price_from").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
