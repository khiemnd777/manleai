package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantOwnerInvitation struct{ ent.Schema }

func (TenantOwnerInvitation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("token_hash").NotEmpty().Unique().Sensitive(),
		field.String("status").Default("active"),
		field.Time("expires_at"),
		field.Time("used_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.UUID("created_by_user_id", uuid.UUID{}),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
