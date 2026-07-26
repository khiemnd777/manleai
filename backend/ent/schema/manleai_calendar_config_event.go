package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ManleAICalendarConfigEvent struct {
	ent.Schema
}

func (ManleAICalendarConfigEvent) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "manleai_calendar_config_events"}}
}

func (ManleAICalendarConfigEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.Int64("previous_version").NonNegative(),
		field.Int64("result_version").Positive(),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
