package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PlatformSupportAccessRequest struct{ ent.Schema }

func (PlatformSupportAccessRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("platform_user_id", uuid.UUID{}),
		field.UUID("requested_by_user_id", uuid.UUID{}),
		field.String("status").Default("pending_owner_review"),
		field.String("reason").NotEmpty(),
		field.Time("requested_expires_at"),
		field.Time("approved_expires_at").Optional().Nillable(),
		field.UUID("decision_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("decision_at").Optional().Nillable(),
		field.UUID("revoked_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.Int64("version").Default(1),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
