package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Salon struct {
	ent.Schema
}

func (Salon) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty(),
		field.String("phone").NotEmpty(),
		field.String("address").Optional(),
		field.String("city").Optional(),
		field.String("state").Optional(),
		field.String("zip_code").Optional(),
		field.String("timezone").Default("America/Chicago"),
		field.UUID("owner_user_id", uuid.UUID{}),
		field.String("primary_language").Default("en"),
		field.String("secondary_language").Default("vi"),
		field.String("handoff_phone").Optional(),
		field.Bool("ai_enabled").Default(false),
		field.String("active_pos_provider").Default("square").NotEmpty(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
