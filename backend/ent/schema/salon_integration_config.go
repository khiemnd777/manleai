package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SalonIntegrationConfig struct {
	ent.Schema
}

func (SalonIntegrationConfig) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.Bool("enabled").Default(true),
		field.JSON("settings", map[string]string{}).Optional(),
		field.String("secrets_encrypted").Optional().Sensitive(),
		field.String("credential_fingerprint_hmac").Optional().Sensitive(),
		field.Int64("credential_revision").Default(0).NonNegative(),
		field.String("destination_profile").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
