package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PlatformSupportAccessRequestPermission struct{ ent.Schema }

func (PlatformSupportAccessRequestPermission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("permission_id", uuid.UUID{}).Optional().Nillable(),
		field.String("pii_scope").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
