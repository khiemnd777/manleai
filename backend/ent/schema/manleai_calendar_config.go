package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarConfig struct {
	ent.Schema
}

func (ManleAICalendarConfig) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_configs"}}
}

func (ManleAICalendarConfig) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}).Unique(),
		field.Int64("version").Default(1).Positive(),
		field.Int("slot_step_minutes").Positive(),
		field.Int("minimum_booking_notice_minutes").NonNegative(),
		field.Int("booking_horizon_days").Positive(),
		field.Int("reschedule_cutoff_minutes").Optional().Nillable().NonNegative(),
		field.Int("cancellation_cutoff_minutes").Optional().Nillable().NonNegative(),
		field.Int("max_party_size").Positive(),
		field.Int("default_buffer_before_minutes").Default(0).NonNegative(),
		field.Int("default_buffer_after_minutes").Default(0).NonNegative(),
		field.Time("activated_at").Optional().Nillable(),
		field.UUID("activated_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Int64("activated_version").Optional().Nillable().Positive(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
