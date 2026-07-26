package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type BookingAttempt struct {
	ent.Schema
}

func (BookingAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("source").Default("owner_dashboard"),
		field.String("status").NotEmpty(),
		field.String("pos_provider").Optional().Nillable(),
		field.String("pos_booking_id").Optional(),
		field.String("pos_idempotency_key").Optional(),
		field.String("scheduling_authority").Default("external_provider"),
		field.String("authority_provider").Optional(),
		field.String("authority_appointment_id").Optional(),
		field.Int("authority_appointment_version").Optional().Nillable().NonNegative(),
		field.Int("target_authority_appointment_version").Optional().Nillable().NonNegative(),
		field.String("authority_idempotency_key").Optional(),
		field.String("authority_location_id").Optional(),
		field.Int64("authority_snapshot_generation").Optional().Nillable().NonNegative(),
		field.String("operation_key").Optional(),
		field.String("request_fingerprint").Optional(),
		field.UUID("retry_of_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("superseded_by_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.Int("retry_sequence").Default(0),
		field.Time("superseded_at").Optional().Nillable(),
		field.Int("pos_booking_version").Optional().Nillable(),
		field.Int("target_pos_booking_version").Optional().Nillable(),
		field.UUID("availability_quote_id", uuid.UUID{}).Optional().Nillable(),
		field.String("availability_slot_fingerprint").Optional(),
		field.String("provider_location_id").Optional(),
		field.Int64("provider_snapshot_generation").Optional().Nillable().Positive(),
		field.Int64("scheduling_authority_version").Optional().Nillable().Positive(),
		field.Int64("authority_config_version").Optional().Nillable().Positive(),
		field.Int("party_size").Default(1).Positive(),
		field.String("operation_type").Default("book"),
		field.UUID("target_appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.String("processing_token").Optional(),
		field.Time("processing_lease_expires_at").Optional().Nillable(),
		field.String("provider_outcome").Default("not_started"),
		field.String("retry_policy").Default("none"),
		field.String("reconciliation_status").Default("not_required"),
		field.String("reconciliation_resolution").Optional(),
		field.Time("reconciliation_resolved_at").Optional().Nillable(),
		field.String("customer_name").NotEmpty(),
		field.String("customer_phone").NotEmpty(),
		field.String("customer_email").Optional(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_selection_mode").Default("specific"),
		field.Time("requested_start_time"),
		field.Time("requested_end_time"),
		field.String("notes").Optional(),
		field.String("error_code").Optional(),
		field.String("error_message").Optional(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
