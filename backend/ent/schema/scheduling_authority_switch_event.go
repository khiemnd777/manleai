package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// SchedulingAuthoritySwitchEvent is immutable evidence for preview, commit, or failure actions.
type SchedulingAuthoritySwitchEvent struct {
	ent.Schema
}

func (SchedulingAuthoritySwitchEvent) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "scheduling_authority_switch_events"}}
}

func (SchedulingAuthoritySwitchEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("switch_run_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("action_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
