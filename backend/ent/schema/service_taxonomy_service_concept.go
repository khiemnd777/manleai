package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceTaxonomyServiceConcept struct {
	ent.Schema
}

func (ServiceTaxonomyServiceConcept) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("release_id", uuid.UUID{}),
		field.UUID("category_id", uuid.UUID{}),
		field.String("concept_key").NotEmpty(),
		field.String("canonical_name").NotEmpty(),
		field.String("normalized_name").NotEmpty(),
		field.Float("confidence"),
		field.String("status").Default("active"),
	}
}
