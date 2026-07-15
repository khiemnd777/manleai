package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type OwnerNotification struct {
	ent.Schema
}

func (OwnerNotification) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.String("type").NotEmpty(),
		field.String("status").Default("pending"),
		field.String("title").NotEmpty(),
		field.String("message").NotEmpty(),
		field.String("dedupe_key").Optional(),
		field.JSON("payload", json.RawMessage{}).Default(json.RawMessage("{}")),
		field.String("delivery_status").Default("queued"),
		field.Int("delivery_attempts").Default(0),
		field.Time("next_delivery_at").Default(time.Now),
		field.Time("delivered_at").Optional().Nillable(),
		field.String("last_delivery_error").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("read_at").Optional().Nillable(),
	}
}
