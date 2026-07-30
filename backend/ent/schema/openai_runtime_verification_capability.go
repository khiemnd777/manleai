package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OpenAIRuntimeVerificationCapability struct{ ent.Schema }

func (OpenAIRuntimeVerificationCapability) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "openai_runtime_verification_capabilities"}}
}

func (OpenAIRuntimeVerificationCapability) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("run_id", uuid.UUID{}),
		field.String("capability").NotEmpty(),
		field.Bool("required"),
		field.String("status").Default("pending"),
		field.Int64("latency_ms").Optional().Nillable().NonNegative(),
		field.String("provider_request_id").Optional(),
		field.String("error_code").Optional(),
		field.Time("verified_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
