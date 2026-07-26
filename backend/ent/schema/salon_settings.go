package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SalonSettings struct {
	ent.Schema
}

func (SalonSettings) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Checks: map[string]string{
		"salon_settings_booking_mode_check":              "booking_mode IN ('confirmed_booking', 'pending_approval', 'disabled')",
		"salon_settings_scheduling_authority_check":      "scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')",
		"salon_settings_owner_manual_booking_mode_guard": "scheduling_authority <> 'owner_manual' OR booking_mode <> 'confirmed_booking'",
	}}}
}

func (SalonSettings) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}).Unique(),
		field.String("ai_greeting").Default("Thank you for calling. How can I help today?"),
		field.String("ai_voice").Default("professional_female"),
		field.String("ai_tone").Default("professional_warm"),
		field.String("booking_mode").Default("pending_approval"),
		field.String("scheduling_authority").Default("external_provider"),
		field.Int64("scheduling_authority_version").Default(1).Positive(),
		field.Bool("recording_enabled").Default(true),
		field.String("recording_consent_message").Default("This call may be recorded to help us manage appointments and improve service."),
		field.Bool("sms_confirmation_enabled").Default(true),
		field.Bool("sms_reminder_enabled").Default(true),
		field.Int("reminder_hours_before").Default(24),
		field.Bool("customer_sms_enabled").Default(false),
		field.String("customer_sms_quiet_start").Optional(),
		field.String("customer_sms_quiet_end").Optional(),
		field.Int64("customer_sms_policy_version").Default(1).Positive(),
		field.Bool("handoff_enabled").Default(true),
		field.Bool("consultation_enabled").Default(false),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
