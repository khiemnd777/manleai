package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type CallSession struct {
	ent.Schema
}

func (CallSession) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.String("channel").Default("simulator"),
		field.String("provider").Optional(),
		field.String("provider_call_id").Optional(),
		field.String("inbound_phone").Optional(),
		field.String("outbound_phone").Optional(),
		field.String("status").Default("active"),
		field.String("intent").Default("unknown"),
		field.String("outcome").Default("collecting"),
		field.Int64("state_revision").Default(0).NonNegative(),
		field.String("customer_name").Optional(),
		field.String("customer_phone").Optional(),
		field.String("customer_email").Optional(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("staff_id", uuid.UUID{}).Optional().Nillable(),
		field.String("staff_selection_mode").Default("specific"),
		field.Time("requested_date").Optional().Nillable(),
		field.Time("requested_start_time").Optional().Nillable(),
		field.UUID("availability_quote_id", uuid.UUID{}).Optional().Nillable(),
		field.String("availability_slot_fingerprint").Optional(),
		field.JSON("offered_slots", []map[string]any{}).Optional(),
		field.JSON("booking_segments", []map[string]any{}).Optional(),
		field.JSON("dialog_state", map[string]any{}).Default(map[string]any{
			"version": 3, "phase": "open", "review_required": true, "review_accepted": false,
			"no_progress_count": 0, "draft_revision": 1, "reviewed_revision": 0, "authorized_revision": 0,
		}),
		field.UUID("booking_attempt_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("appointment_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("scheduling_request_id", uuid.UUID{}).Optional().Nillable(),
		field.String("summary").Optional(),
		field.String("lifecycle_status").Default("active"),
		field.Time("archived_at").Optional().Nillable(),
		field.Time("redacted_at").Optional().Nillable(),
		field.Time("retention_expires_at").Default(func() time.Time {
			return time.Now().UTC().Add(90 * 24 * time.Hour)
		}),
		field.Time("started_at").Default(time.Now),
		field.Time("ended_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
