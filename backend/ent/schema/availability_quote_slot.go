package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AvailabilityQuoteSlot struct {
	ent.Schema
}

func (AvailabilityQuoteSlot) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("quote_id", uuid.UUID{}),
		field.String("slot_fingerprint").NotEmpty(),
		field.Time("start_time"),
		field.Time("end_time"),
		field.JSON("segments", json.RawMessage{}).Default(json.RawMessage("[]")),
		field.Time("created_at").Default(time.Now),
	}
}
