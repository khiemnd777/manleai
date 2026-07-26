package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarExecutionEvent struct {
	ent.Schema
}

func (ManleAICalendarExecutionEvent) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_execution_events"}}
}

func (ManleAICalendarExecutionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_id", uuid.UUID{}),
		field.UUID("appointment_id", uuid.UUID{}),
		field.String("event_type").NotEmpty(),
		field.Int64("scheduling_authority_version").Positive(),
		field.Int64("authority_config_version").Positive(),
		field.Int("authority_appointment_version").Positive(),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
