package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceTaxonomyServiceAlias struct {
	ent.Schema
}

func (ServiceTaxonomyServiceAlias) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("concept_id", uuid.UUID{}),
		field.String("alias").NotEmpty(),
		field.String("normalized_alias").NotEmpty(),
		field.Float("confidence"),
		field.String("status").Default("active"),
	}
}
