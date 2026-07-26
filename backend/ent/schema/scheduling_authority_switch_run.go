package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// SchedulingAuthoritySwitchRun mirrors the durable owner-reviewed authority-switch preview.
// Database constraints preserve its immutable preview evidence and state transitions.
type SchedulingAuthoritySwitchRun struct {
	ent.Schema
}

func (SchedulingAuthoritySwitchRun) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "scheduling_authority_switch_runs"}}
}

func (SchedulingAuthoritySwitchRun) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("source_scheduling_authority").NotEmpty(),
		field.String("target_scheduling_authority").NotEmpty(),
		field.Int64("expected_source_authority_version").Positive(),
		field.String("operation_key").NotEmpty(),
		field.String("payload_fingerprint").NotEmpty(),
		field.JSON("readiness_snapshot", map[string]any{}).Default(map[string]any{}),
		field.JSON("blockers", []map[string]any{}).Default([]map[string]any{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("status").NotEmpty(),
		field.Time("previewed_at").Default(time.Now),
		field.Time("blocked_at").Optional().Nillable(),
		field.Time("committed_at").Optional().Nillable(),
		field.Time("failed_at").Optional().Nillable(),
		field.UUID("rollback_of_switch_run_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
