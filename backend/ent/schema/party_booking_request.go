package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type PartyBookingRequest struct {
	ent.Schema
}

func (PartyBookingRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("call_session_id", uuid.UUID{}),
		field.String("event_key").Default(""),
		field.String("status").Default("pending"),
		field.Int("party_size").Optional(),
		field.String("representative_name").Optional(),
		field.String("representative_phone").Optional(),
		field.Time("requested_date").Optional().Nillable(),
		field.String("requested_time_window").Optional(),
		field.JSON("guest_service_requests", []map[string]any{}).Optional(),
		field.String("flexibility_notes").Optional(),
		field.String("summary").NotEmpty(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("resolved_at").Optional().Nillable(),
		field.UUID("resolved_by", uuid.UUID{}).Optional().Nillable(),
	}
}
