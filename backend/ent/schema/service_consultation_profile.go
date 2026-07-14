package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type ServiceConsultationProfile struct {
	ent.Schema
}

func (ServiceConsultationProfile) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("salon_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}),
		field.String("status").Default("draft"),
		field.JSON("recommended_outcomes", []string{}).Default([]string{}),
		field.JSON("compatible_current_systems", []string{}).Default([]string{}),
		field.JSON("length_capabilities", []string{}).Default([]string{}),
		field.JSON("priority_tags", []string{}).Default([]string{}),
		field.JSON("finish_options", []string{}).Default([]string{}),
		field.String("maintenance_note").Optional(),
		field.String("owner_approved_summary").Optional(),
		field.Int("revision").Default(1),
		field.UUID("updated_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
