package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SalonBusinessHourPeriod struct {
	ent.Schema
}

func (SalonBusinessHourPeriod) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.Int("day_of_week"),
		field.String("start_local_time"),
		field.String("end_local_time"),
		field.String("source").Default("imported"),
		field.String("provider").Default(""),
		field.String("provider_location_id").Default(""),
		field.Int("provider_period_index").Default(0),
		field.Time("last_synced_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
