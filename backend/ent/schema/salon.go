package schema

import (
	"regexp"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type Salon struct {
	ent.Schema
}

func (Salon) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Checks: map[string]string{
		"salons_creation_proof_pair_check":          "(creation_operation_key IS NULL AND creation_payload_fingerprint IS NULL) OR (creation_operation_key IS NOT NULL AND creation_payload_fingerprint IS NOT NULL)",
		"salons_creation_operation_key_check":       "creation_operation_key IS NULL OR (creation_operation_key = btrim(creation_operation_key) AND length(creation_operation_key) BETWEEN 1 AND 256)",
		"salons_creation_payload_fingerprint_check": "creation_payload_fingerprint IS NULL OR creation_payload_fingerprint ~ '^[0-9a-f]{64}$'",
	}}}
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
		field.String("public_slug").Optional(),
		field.Bool("public_catalog_enabled").Default(false),
		field.String("creation_operation_key").Optional().Nillable().NotEmpty(),
		field.String("creation_payload_fingerprint").Optional().Nillable().Match(regexp.MustCompile(`^[0-9a-f]{64}$`)),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Salon) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_user_id", "creation_operation_key").
			Unique().
			StorageKey("salons_owner_creation_operation_key").
			Annotations(entsql.IndexWhere("creation_operation_key IS NOT NULL")),
	}
}
