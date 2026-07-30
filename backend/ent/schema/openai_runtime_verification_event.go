package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OpenAIRuntimeVerificationEvent struct{ ent.Schema }

func (OpenAIRuntimeVerificationEvent) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "openai_runtime_verification_events"}}
}

func (OpenAIRuntimeVerificationEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("run_id", uuid.UUID{}),
		field.String("event_key").NotEmpty(),
		field.String("event_fingerprint").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("capability").Optional(),
		field.String("error_code").Optional(),
		field.Time("created_at").Default(time.Now),
	}
}
