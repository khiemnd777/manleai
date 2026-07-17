package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceTaxonomyRelease struct {
	ent.Schema
}

func (ServiceTaxonomyRelease) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("release_key").NotEmpty().Unique(),
		field.String("locale").NotEmpty(),
		field.Int("version").Positive(),
		field.String("status").Default("draft"),
		field.Time("created_at").Default(time.Now),
		field.Time("activated_at").Optional().Nillable(),
	}
}
