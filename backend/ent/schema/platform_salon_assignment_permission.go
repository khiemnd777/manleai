package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PlatformSalonAssignmentPermission struct{ ent.Schema }

func (PlatformSalonAssignmentPermission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("assignment_id", uuid.UUID{}),
		field.UUID("permission_id", uuid.UUID{}),
		field.Time("created_at").Default(time.Now),
	}
}
