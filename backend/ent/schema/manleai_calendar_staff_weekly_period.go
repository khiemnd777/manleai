package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarStaffWeeklyPeriod struct {
	ent.Schema
}

func (ManleAICalendarStaffWeeklyPeriod) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_staff_weekly_periods"}}
}

func (ManleAICalendarStaffWeeklyPeriod) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("staff_id", uuid.UUID{}),
		field.Int("day_of_week"),
		field.Int("start_minute").NonNegative(),
		field.Int("end_minute").Positive(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
