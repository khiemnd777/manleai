package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type TenantUsageMinuteBucket struct{ ent.Schema }

func (TenantUsageMinuteBucket) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("salon_id", uuid.UUID{}),
		field.Time("bucket_start"),
		field.String("metric").NotEmpty(),
		field.Int64("used_count").Default(0),
		field.Int64("rejected_count").Default(0),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
