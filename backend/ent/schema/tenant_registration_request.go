package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantRegistrationRequest struct{ ent.Schema }

func (TenantRegistrationRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("public_reference").NotEmpty().Unique(),
		field.UUID("submission_key", uuid.UUID{}).Unique(),
		field.String("submission_payload_fingerprint").NotEmpty(),
		field.String("status").Default("new"),
		field.Int64("version").Default(1).Positive(),
		field.UUID("assigned_to_user_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("converted_salon_id", uuid.UUID{}).Optional().Nillable().Unique(),
		field.Time("converted_at").Optional().Nillable(),
		field.Time("terminal_at").Optional().Nillable(),
		field.String("contact_full_name").Optional().Nillable(),
		field.String("contact_email").Optional().Nillable(),
		field.String("contact_email_normalized").Optional().Nillable(),
		field.String("contact_phone").Optional().Nillable(),
		field.String("contact_phone_normalized").Optional().Nillable(),
		field.String("salon_name").Optional().Nillable(),
		field.String("salon_phone").Optional().Nillable(),
		field.String("salon_phone_normalized").Optional().Nillable(),
		field.String("salon_website").Optional().Nillable(),
		field.String("city").Optional().Nillable(),
		field.String("state").Optional().Nillable(),
		field.String("zip_code").Optional().Nillable(),
		field.Int("location_count").Optional().Nillable(),
		field.String("preferred_contact_language").Optional().Nillable(),
		field.String("current_booking_system").Optional().Nillable(),
		field.String("estimated_weekly_call_volume").Optional().Nillable(),
		field.String("requested_help").Optional().Nillable(),
		field.String("notes").Optional().Nillable(),
		field.String("locale").NotEmpty(),
		field.String("source_page").NotEmpty(),
		field.String("marketing_plan_interest").Optional().Nillable(),
		field.String("consent_version").NotEmpty(),
		field.Time("consent_at"),
		field.Bool("possible_duplicate").Default(false),
		field.JSON("provisioning_draft", map[string]any{}).Default(map[string]any{}),
		field.UUID("provisioning_draft_updated_by_user_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("provisioning_draft_updated_at").Optional().Nillable(),
		field.Time("retention_expires_at").Optional().Nillable(),
		field.Time("redacted_at").Optional().Nillable(),
		field.String("redaction_version").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
