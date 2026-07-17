package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceTaxonomyCategory struct {
	ent.Schema
}

func (ServiceTaxonomyCategory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("release_id", uuid.UUID{}),
		field.String("category_key").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty(),
		field.String("description").Optional(),
		field.Int("sort_order").Default(0),
		field.Float("confidence"),
		field.String("status").Default("active"),
	}
}
