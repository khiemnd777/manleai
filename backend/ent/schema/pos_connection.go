package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type POSConnection struct {
	ent.Schema
}

func (POSConnection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("provider").NotEmpty(),
		field.String("status").Default("not_connected"),
		field.String("access_token_encrypted").Optional().Sensitive(),
		field.String("refresh_token_encrypted").Optional().Sensitive(),
		field.String("merchant_id").Optional(),
		field.String("location_id").Optional(),
		field.Strings("scopes").Default([]string{}),
		field.Time("last_sync_at").Optional().Nillable(),
		field.String("error_message").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
