package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRuntimeLimit struct{ ent.Schema }

func (TenantRuntimeLimit) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.Int("expensive_requests_per_minute").Default(60),
		field.Int("scheduling_writes_per_minute").Default(120),
		field.Int("provider_writes_per_minute").Default(30),
		field.Int("voice_starts_per_minute").Default(30),
		field.Int("worker_claims_per_batch").Default(2),
		field.Int64("version").Default(1),
		field.UUID("updated_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
