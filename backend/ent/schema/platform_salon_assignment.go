package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PlatformSalonAssignment struct{ ent.Schema }

func (PlatformSalonAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("status").Default("active"),
		field.Int64("version").Default(1),
		field.UUID("created_by_user_id", uuid.UUID{}),
		field.UUID("updated_by_user_id", uuid.UUID{}),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
