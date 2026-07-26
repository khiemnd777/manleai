package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SchedulingRequest struct {
	ent.Schema
}

func (SchedulingRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("scheduling_authority").Default("owner_manual"),
		field.String("operation_key").NotEmpty(),
		field.String("request_fingerprint").NotEmpty(),
		field.String("operation_type").NotEmpty(),
		field.String("source").NotEmpty(),
		field.String("status").Default("pending"),
		field.Int("version").Default(1).Positive(),
		field.UUID("call_session_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("target_appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.String("target_scheduling_authority").Optional(),
		field.String("target_description").Optional(),
		field.String("customer_name").NotEmpty(),
		field.String("customer_phone").NotEmpty(),
		field.String("customer_email").Optional(),
		field.String("requested_timezone").NotEmpty(),
		field.Int("party_size").Positive(),
		field.Time("requested_start_time").Optional().Nillable(),
		field.Time("requested_end_time").Optional().Nillable(),
		field.String("notes").Optional(),
		field.String("resolution_reason").Optional(),
		field.Time("contacted_at").Optional().Nillable(),
		field.Time("resolved_at").Optional().Nillable(),
		field.Time("dismissed_at").Optional().Nillable(),
		field.Time("retention_expires_at").Optional().Nillable(),
		field.Time("redacted_at").Optional().Nillable(),
		field.Int("redaction_version").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
