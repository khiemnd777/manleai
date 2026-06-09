package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSOAuthState struct {
	ent.Schema
}

func (POSOAuthState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("state_hash").NotEmpty(),
		field.String("nonce_hash").NotEmpty(),
		field.Time("expires_at"),
		field.Time("used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
