package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type SalonSettings struct {
	ent.Schema
}

func (SalonSettings) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}).Unique(),
		field.String("ai_greeting").Default("Thank you for calling. This call may be recorded to help us manage appointments and improve service."),
		field.String("ai_voice").Default("professional_female"),
		field.String("ai_tone").Default("professional_warm"),
		field.String("booking_mode").Default("pending_approval"),
		field.Bool("recording_enabled").Default(true),
		field.String("recording_consent_message").Default("Thank you for calling. This call may be recorded to help us manage appointments and improve service."),
		field.Bool("sms_confirmation_enabled").Default(true),
		field.Bool("sms_reminder_enabled").Default(true),
		field.Int("reminder_hours_before").Default(24),
		field.Bool("handoff_enabled").Default(true),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
