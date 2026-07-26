package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PlatformPIIAccessGrant struct{ ent.Schema }

func (PlatformPIIAccessGrant) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("scope").NotEmpty(),
		field.String("reason").NotEmpty(),
		field.Time("expires_at"),
		field.Time("revoked_at").Optional().Nillable(),
		field.UUID("revoked_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Int64("version").Default(1),
		field.UUID("created_by_user_id", uuid.UUID{}),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
