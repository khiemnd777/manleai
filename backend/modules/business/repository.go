package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/salon"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListTenantSalons(ctx context.Context, actorUserID string) ([]SalonSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT salon.id::text, salon.name, COALESCE(salon.city, ''), COALESCE(salon.state, ''),
		       salon.timezone, COALESCE(salon.public_slug, ''), salon.public_catalog_enabled, role.name,
		       settings.scheduling_authority, settings.scheduling_authority_version,
		       COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square')
		FROM salon_memberships membership
		JOIN salons salon ON salon.id = membership.salon_id
		JOIN roles role ON role.id = membership.role_id
		JOIN salon_settings settings ON settings.salon_id=salon.id
		WHERE membership.user_id = $1 AND membership.status = 'active'
		  AND role.scope = 'tenant'
		ORDER BY salon.name, salon.id
	`, actorUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSalonSummaries(rows)
}

func (r *Repository) ListPlatformSalons(ctx context.Context, actorUserID string) ([]SalonSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT salon.id::text, salon.name, COALESCE(salon.city, ''), COALESCE(salon.state, ''),
		       salon.timezone, COALESCE(salon.public_slug, ''), salon.public_catalog_enabled,
		       CASE role.name WHEN 'platform_admin' THEN 'global' ELSE 'assigned' END,
		       settings.scheduling_authority, settings.scheduling_authority_version,
		       COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square')
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id=salon.id
		JOIN platform_role_assignments platform_role ON platform_role.user_id=$1 AND platform_role.status='active'
		JOIN roles role ON role.id=platform_role.role_id
		WHERE role.name='platform_admin'
		   OR (role.name='platform_ops' AND EXISTS(
		       SELECT 1 FROM platform_salon_assignments assignment
		       WHERE assignment.user_id=$1 AND assignment.salon_id=salon.id AND assignment.status='active'
		   ))
		ORDER BY salon.name, salon.id
	`, actorUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSalonSummaries(rows)
}

func scanSalonSummaries(rows *sql.Rows) ([]SalonSummary, error) {
	items := []SalonSummary{}
	for rows.Next() {
		var item SalonSummary
		if err := rows.Scan(
			&item.ID, &item.Name, &item.City, &item.State, &item.Timezone,
			&item.PublicSlug, &item.PublicCatalogEnabled, &item.BusinessAccess,
			&item.SchedulingAuthority, &item.SchedulingAuthorityVersion, &item.ActivePOSProvider,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetSalonProfile(ctx context.Context, salonID string) (*SalonProfile, error) {
	var item SalonProfile
	err := r.db.QueryRowContext(ctx, `
		SELECT salon.id::text, salon.name, salon.phone, COALESCE(salon.address, ''),
		       COALESCE(salon.city, ''), COALESCE(salon.state, ''), COALESCE(salon.zip_code, ''),
		       salon.timezone, salon.primary_language, salon.secondary_language,
		       COALESCE(salon.handoff_phone, ''), COALESCE(salon.public_slug, ''),
		       salon.public_catalog_enabled, version.version, salon.updated_at
		FROM salons salon
		JOIN business_resource_versions version
		  ON version.salon_id = salon.id AND version.resource_type = 'salon_profile'
		 AND version.resource_id = salon.id::text
		WHERE salon.id = $1
	`, salonID).Scan(&item.ID, &item.Name, &item.Phone, &item.Address, &item.City, &item.State, &item.ZipCode,
		&item.Timezone, &item.PrimaryLanguage, &item.SecondaryLanguage, &item.HandoffPhone, &item.PublicSlug,
		&item.PublicCatalogEnabled, &item.Version, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *Repository) ListServices(ctx context.Context, salonID string) ([]Service, error) {
	rows, err := r.db.QueryContext(ctx, serviceSelect+` WHERE service.salon_id = $1 ORDER BY service.archived_at NULLS FIRST, service.name, service.id`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Service{}
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetService(ctx context.Context, salonID, serviceID string) (*Service, error) {
	return scanService(r.db.QueryRowContext(ctx, serviceSelect+` WHERE service.salon_id = $1 AND service.id = $2`, salonID, serviceID))
}

const serviceSelect = `
	SELECT service.id::text, service.name, COALESCE(service.description, ''), COALESCE(service.ai_description, ''),
	       service.duration_minutes, service.price_from, COALESCE(service.price_display, ''), service.ai_bookable,
	       service.active, service.archived_at,
	       CASE WHEN EXISTS (
	         SELECT 1 FROM salons salon
	         JOIN pos_entity_links link ON link.salon_id = salon.id AND link.provider = salon.active_pos_provider
	          AND link.entity_type = 'service' AND link.entity_id = service.id
	         WHERE salon.id = service.salon_id AND link.sync_status = 'synced'
	           AND NULLIF(link.provider_entity_id, '') IS NOT NULL
	       ) THEN 'provider_read_only' ELSE 'local' END,
	       version.version,
	       category.id::text, category.name, category.slug, COALESCE(category.description, ''),
	       category.sort_order, category.status, category_version.version, category.archived_at,
	       profile.status, profile.recommended_outcomes, profile.compatible_current_systems,
	       profile.length_capabilities, profile.priority_tags, profile.finish_options,
	       COALESCE(profile.maintenance_note, ''), COALESCE(profile.owner_approved_summary, '')
	FROM services service
	JOIN business_resource_versions version ON version.salon_id = service.salon_id
	 AND version.resource_type = 'service' AND version.resource_id = service.id::text
	LEFT JOIN service_categories category ON category.salon_id = service.salon_id AND category.id = service.service_category_id
	LEFT JOIN business_resource_versions category_version ON category_version.salon_id = category.salon_id
	 AND category_version.resource_type = 'service_category' AND category_version.resource_id = category.id::text
	LEFT JOIN service_consultation_profiles profile ON profile.salon_id = service.salon_id AND profile.service_id = service.id
`

func scanService(row rowScanner) (*Service, error) {
	var item Service
	var price sql.NullFloat64
	var archived sql.NullTime
	var categoryID, categoryName, categorySlug, categoryDescription, categoryStatus sql.NullString
	var categorySort sql.NullInt64
	var categoryVersion sql.NullInt64
	var categoryArchived sql.NullTime
	var profileStatus sql.NullString
	var outcomes, systems, lengths, priorities, finishes []byte
	var maintenance, summary sql.NullString
	err := row.Scan(&item.ID, &item.Name, &item.Description, &item.AIDescription, &item.DurationMinutes, &price,
		&item.PriceDisplay, &item.AIBookable, &item.Active, &archived, &item.ManagementMode, &item.Version,
		&categoryID, &categoryName, &categorySlug, &categoryDescription, &categorySort, &categoryStatus, &categoryVersion, &categoryArchived,
		&profileStatus, &outcomes, &systems, &lengths, &priorities, &finishes, &maintenance, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if price.Valid {
		value := price.Float64
		item.PriceFrom = &value
	}
	if archived.Valid {
		value := archived.Time
		item.ArchivedAt = &value
	}
	if categoryID.Valid {
		item.Category = &ServiceCategory{ID: categoryID.String, Name: categoryName.String, Slug: categorySlug.String, Description: categoryDescription.String, SortOrder: int(categorySort.Int64), Status: categoryStatus.String, Version: categoryVersion.Int64}
		if categoryArchived.Valid {
			value := categoryArchived.Time
			item.Category.ArchivedAt = &value
		}
	}
	if profileStatus.Valid {
		profile := &ConsultationProfile{Status: profileStatus.String, MaintenanceNote: maintenance.String, OwnerApprovedSummary: summary.String}
		if err := decodeStringArray(outcomes, &profile.RecommendedOutcomes); err != nil {
			return nil, err
		}
		if err := decodeStringArray(systems, &profile.CompatibleCurrentSystems); err != nil {
			return nil, err
		}
		if err := decodeStringArray(lengths, &profile.LengthCapabilities); err != nil {
			return nil, err
		}
		if err := decodeStringArray(priorities, &profile.PriorityTags); err != nil {
			return nil, err
		}
		if err := decodeStringArray(finishes, &profile.FinishOptions); err != nil {
			return nil, err
		}
		item.ConsultationProfile = profile
	}
	return &item, nil
}

func decodeStringArray(raw []byte, target *[]string) error {
	if len(raw) == 0 {
		*target = []string{}
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	if *target == nil {
		*target = []string{}
	}
	return nil
}

func (r *Repository) ListServiceCategories(ctx context.Context, salonID string) ([]ServiceCategory, error) {
	rows, err := r.db.QueryContext(ctx, categorySelect+` WHERE category.salon_id = $1 ORDER BY category.status, category.sort_order, category.name`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ServiceCategory{}
	for rows.Next() {
		item, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetServiceCategory(ctx context.Context, salonID, categoryID string) (*ServiceCategory, error) {
	return scanCategory(r.db.QueryRowContext(ctx, categorySelect+` WHERE category.salon_id = $1 AND category.id = $2`, salonID, categoryID))
}

const categorySelect = `
	SELECT category.id::text, category.name, category.slug, COALESCE(category.description, ''),
	       category.sort_order, category.status, version.version, category.archived_at
	FROM service_categories category
	JOIN business_resource_versions version ON version.salon_id = category.salon_id
	 AND version.resource_type = 'service_category' AND version.resource_id = category.id::text
`

func scanCategory(row rowScanner) (*ServiceCategory, error) {
	var item ServiceCategory
	var archived sql.NullTime
	err := row.Scan(&item.ID, &item.Name, &item.Slug, &item.Description, &item.SortOrder, &item.Status, &item.Version, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archived.Valid {
		value := archived.Time
		item.ArchivedAt = &value
	}
	return &item, nil
}

func (r *Repository) ListStaff(ctx context.Context, salonID string) ([]StaffMember, error) {
	rows, err := r.db.QueryContext(ctx, staffSelect+` WHERE staff.salon_id = $1 GROUP BY staff.id, version.version, eligibility_version.version ORDER BY staff.archived_at NULLS FIRST, staff.name, staff.id`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []StaffMember{}
	for rows.Next() {
		item, err := scanStaff(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetStaff(ctx context.Context, salonID, staffID string) (*StaffMember, error) {
	return scanStaff(r.db.QueryRowContext(ctx, staffSelect+` WHERE staff.salon_id = $1 AND staff.id = $2 GROUP BY staff.id, version.version, eligibility_version.version`, salonID, staffID))
}

const staffSelect = `
	SELECT staff.id::text, staff.name, COALESCE(staff.phone,''), COALESCE(staff.email,''),
	       staff.ai_bookable, staff.active, staff.archived_at,
	       CASE WHEN EXISTS (
	         SELECT 1 FROM salons salon
	         JOIN pos_entity_links link ON link.salon_id=salon.id AND link.provider=salon.active_pos_provider
	          AND link.entity_type='staff' AND link.entity_id=staff.id
	         WHERE salon.id=staff.salon_id AND link.sync_status='synced' AND NULLIF(link.provider_entity_id,'') IS NOT NULL
	       ) THEN 'provider_read_only' ELSE 'local' END,
	       COALESCE(array_agg(eligible.service_id::text ORDER BY eligible.service_id::text) FILTER (WHERE eligible.service_id IS NOT NULL), ARRAY[]::text[]),
	       version.version, eligibility_version.version
	FROM staff staff
	JOIN business_resource_versions version ON version.salon_id=staff.salon_id
	 AND version.resource_type='staff' AND version.resource_id=staff.id::text
	JOIN business_resource_versions eligibility_version ON eligibility_version.salon_id=staff.salon_id
	 AND eligibility_version.resource_type='staff_service_eligibility' AND eligibility_version.resource_id=staff.salon_id::text
	LEFT JOIN manleai_calendar_service_staff eligible ON eligible.salon_id=staff.salon_id AND eligible.staff_id=staff.id
`

func scanStaff(row rowScanner) (*StaffMember, error) {
	var item StaffMember
	var archived sql.NullTime
	err := row.Scan(&item.ID, &item.Name, &item.Phone, &item.Email, &item.AIBookable, &item.Active, &archived, &item.ManagementMode, pq.Array(&item.ServiceIDs), &item.Version, &item.EligibilityVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archived.Valid {
		value := archived.Time
		item.ArchivedAt = &value
	}
	if item.ServiceIDs == nil {
		item.ServiceIDs = []string{}
	}
	return &item, nil
}

func (r *Repository) GetBusinessHours(ctx context.Context, salonID string) (*BusinessHours, error) {
	var authority, provider, location string
	var version int64
	err := r.db.QueryRowContext(ctx, `SELECT settings.scheduling_authority,salon.active_pos_provider,COALESCE(connection.location_id,''),version.version FROM salons salon JOIN salon_settings settings ON settings.salon_id=salon.id LEFT JOIN pos_connections connection ON connection.salon_id=salon.id AND connection.provider=salon.active_pos_provider JOIN business_resource_versions version ON version.salon_id=salon.id AND version.resource_type='business_hours' AND version.resource_id=salon.id::text WHERE salon.id=$1`, salonID).Scan(&authority, &provider, &location, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	mode := ManagementModeLocal
	query := `SELECT id::text,day_of_week,to_char(start_local_time,'HH24:MI'),to_char(end_local_time,'HH24:MI'),end_at_midnight FROM salon_business_hour_periods WHERE salon_id=$1 AND source='local_override' ORDER BY day_of_week,start_local_time,id`
	args := []any{salonID}
	if authority == "external_provider" {
		mode = ManagementModeProviderReadOnly
		query = `SELECT id::text,day_of_week,to_char(start_local_time,'HH24:MI'),to_char(end_local_time,'HH24:MI'),end_at_midnight FROM salon_business_hour_periods WHERE salon_id=$1 AND source='imported' AND provider=$2 AND provider_location_id=$3 ORDER BY day_of_week,start_local_time,id`
		args = []any{salonID, provider, location}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	periods := []BusinessHourPeriod{}
	for rows.Next() {
		var item BusinessHourPeriod
		if err := rows.Scan(&item.ID, &item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime, &item.EndAtMidnight); err != nil {
			return nil, err
		}
		periods = append(periods, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &BusinessHours{Periods: periods, ManagementMode: mode, Version: version}, nil
}

func (r *Repository) GetPublicCatalogSettings(ctx context.Context, salonID string) (*PublicCatalogSettings, error) {
	canonical, err := salon.NewRepository(r.db).GetPublicCatalogSettingsForSalon(ctx, salonID)
	if errors.Is(err, salon.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var version int64
	if err := r.db.QueryRowContext(ctx, `SELECT version FROM business_resource_versions WHERE salon_id=$1 AND resource_type='public_catalog' AND resource_id=$1::text`, salonID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &PublicCatalogSettings{
		PublicSlug:           canonical.PublicSlug,
		PublicCatalogEnabled: canonical.PublicCatalogEnabled,
		PublicPath:           canonical.PublicPath,
		CanPublish:           canonical.CanPublish,
		BlockedReason:        canonical.BlockedReason,
		Version:              version,
	}, nil
}

func (r *Repository) ListCustomers(ctx context.Context, salonID string, limit, offset int) ([]Customer, error) {
	rows, err := r.db.QueryContext(ctx, customerSelect+` WHERE customer.salon_id=$1 ORDER BY customer.archived_at NULLS FIRST,customer.updated_at DESC,customer.id LIMIT $2 OFFSET $3`, salonID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Customer{}
	for rows.Next() {
		item, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetCustomer(ctx context.Context, salonID, customerID string) (*Customer, error) {
	return scanCustomer(r.db.QueryRowContext(ctx, customerSelect+` WHERE customer.salon_id=$1 AND customer.id=$2`, salonID, customerID))
}

const customerSelect = `SELECT customer.id::text,customer.name,COALESCE(customer.phone,''),COALESCE(customer.email,''),COALESCE(customer.notes,''),customer.active,CASE WHEN EXISTS(SELECT 1 FROM salons salon JOIN pos_entity_links link ON link.salon_id=salon.id AND link.provider=salon.active_pos_provider AND link.entity_type='customer' AND link.entity_id=customer.id WHERE salon.id=customer.salon_id AND link.sync_status='synced' AND NULLIF(link.provider_entity_id,'') IS NOT NULL) THEN 'provider_read_only' ELSE 'local' END,version.version,customer.archived_at FROM customers customer JOIN business_resource_versions version ON version.salon_id=customer.salon_id AND version.resource_type='customer' AND version.resource_id=customer.id::text`

func scanCustomer(row rowScanner) (*Customer, error) {
	var item Customer
	var archived sql.NullTime
	err := row.Scan(&item.ID, &item.Name, &item.Phone, &item.Email, &item.Notes, &item.Active, &item.ManagementMode, &item.Version, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archived.Valid {
		v := archived.Time
		item.ArchivedAt = &v
	}
	return &item, nil
}

type rowScanner interface{ Scan(...any) error }

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505":
			return ErrDuplicate
		case "23P01":
			return ErrValidation
		case "23503":
			return ErrNotFound
		}
	}
	return fmt.Errorf("business persistence: %w", err)
}

func utcNow() time.Time { return time.Now().UTC() }

func emptyToNull(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
