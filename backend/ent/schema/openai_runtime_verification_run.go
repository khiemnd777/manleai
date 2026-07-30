package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OpenAIRuntimeVerificationRun struct{ ent.Schema }

func (OpenAIRuntimeVerificationRun) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Table: "openai_runtime_verification_runs"}}
}

func (OpenAIRuntimeVerificationRun) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("integration_config_id", uuid.UUID{}),
		field.UUID("actor_user_id", uuid.UUID{}),
		field.String("action_key").NotEmpty(),
		field.String("request_fingerprint").NotEmpty(),
		field.Int64("config_version").Positive(),
		field.Int64("credential_revision").Positive(),
		field.String("destination_policy_version").NotEmpty(),
		field.String("verification_contract_version").NotEmpty(),
		field.String("status").Default("queued"),
		field.Int("attempt_count").Default(0).NonNegative(),
		field.UUID("claim_token", uuid.UUID{}).Optional().Nillable(),
		field.Time("lease_expires_at").Optional().Nillable(),
		field.String("error_code").Optional(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
