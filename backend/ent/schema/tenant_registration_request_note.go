package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRegistrationRequestNote struct{ ent.Schema }

func (TenantRegistrationRequestNote) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("author_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Int64("request_version").Positive(),
		field.String("content").Optional().Nillable(),
		field.Time("redacted_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}
