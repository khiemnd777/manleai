package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BookingReconciliationTask struct {
	ent.Schema
}

func (BookingReconciliationTask) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("booking_attempt_id", uuid.UUID{}),
		field.String("status").Default("open"),
		field.String("resolution").Optional(),
		field.UUID("assigned_user_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("resolved_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.String("resolution_note").Optional(),
		field.Time("resolved_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
