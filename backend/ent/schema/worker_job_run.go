package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type WorkerJobRun struct{ ent.Schema }

func (WorkerJobRun) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "worker_job_runs"}}
}

func (WorkerJobRun) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}),
		field.String("job_name").NotEmpty(),
		field.UUID("worker_instance_id", uuid.UUID{}),
		field.String("status").NotEmpty(),
		field.Time("started_at"),
		field.Time("heartbeat_at"),
		field.Time("completed_at").Optional().Nillable(),
		field.Int64("duration_ms").Optional().Nillable(),
		field.Int("processed_count").Optional().Nillable(),
		field.String("error_class").Optional(),
		field.String("error_code").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
