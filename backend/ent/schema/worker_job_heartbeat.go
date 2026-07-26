package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type WorkerJobHeartbeat struct{ ent.Schema }

func (WorkerJobHeartbeat) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "worker_job_heartbeats"}}
}

func (WorkerJobHeartbeat) Fields() []ent.Field {
	return []ent.Field{
		field.String("job_name").NotEmpty().Unique(),
		field.UUID("current_worker_instance_id", uuid.UUID{}),
		field.UUID("active_run_id", uuid.UUID{}).Optional().Nillable(),
		field.String("last_status").NotEmpty(),
		field.Int("interval_seconds").Positive(),
		field.Int("stale_after_seconds").Positive(),
		field.Time("last_started_at"),
		field.Time("last_completed_at").Optional().Nillable(),
		field.Time("last_success_at").Optional().Nillable(),
		field.Int64("last_duration_ms").Optional().Nillable(),
		field.Int("last_processed_count").Optional().Nillable(),
		field.String("last_error_class").Optional(),
		field.String("last_error_code").Optional(),
		field.Time("heartbeat_at"),
		field.Time("lease_expires_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
