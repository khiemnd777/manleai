package public_catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type Repository struct {
	db *sql.DB
}

type catalogSource struct {
	Salon                      PublicSalon
	SalonID                    string
	Provider                   string
	SchedulingAuthority        string
	SchedulingAuthorityVersion int64
	InternalActivationCurrent  bool
	ExternalConnectionReady    bool
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetBySlug(ctx context.Context, slug string) (*Catalog, error) {
	source, err := r.getSalon(ctx, slug)
	if err != nil {
		return nil, err
	}
	return r.getCatalog(ctx, source)
}

func (r *Repository) GetFirstPublished(ctx context.Context) (*Catalog, error) {
	sources, err := r.listPublishedSalons(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sources {
		catalog, err := r.getCatalog(ctx, &sources[i])
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return catalog, nil
	}
	return nil, ErrNotFound
}

func (r *Repository) getCatalog(ctx context.Context, source *catalogSource) (*Catalog, error) {
	services, err := r.listServices(ctx, source)
	if err != nil {
		return nil, err
	}
	staff, err := r.listStaff(ctx, source)
	if err != nil {
		return nil, err
	}
	hours, err := r.listHours(ctx, source)
	if err != nil {
		return nil, err
	}

	// A stale published flag is never enough to keep a page public. The current
	// selected authority is re-evaluated on every read, so a switch or stale
	// provider/calendar snapshot fails closed without leaking unsafe catalog
	// projections.
	switch source.SchedulingAuthority {
	case booking.SchedulingAuthorityOwnerManual:
		if len(services) == 0 {
			return nil, ErrNotFound
		}
	case booking.SchedulingAuthorityManleAICalendar:
		if len(services) == 0 || len(hours) == 0 || !source.InternalActivationCurrent {
			return nil, ErrNotFound
		}
	case booking.SchedulingAuthorityExternalProvider:
		if len(services) == 0 || len(staff) == 0 || !source.ExternalConnectionReady {
			return nil, ErrNotFound
		}
	default:
		return nil, ErrNotFound
	}

	return &Catalog{
		Salon:                      source.Salon,
		SchedulingAuthority:        source.SchedulingAuthority,
		SchedulingAuthorityVersion: source.SchedulingAuthorityVersion,
		Services:                   services,
		Staff:                      staff,
		Hours:                      hours,
		BookingNote:                "Call the salon to request an appointment. Availability and confirmation are provided by the salon.",
	}, nil
}

func (r *Repository) listPublishedSalons(ctx context.Context) ([]catalogSource, error) {
	rows, err := r.db.QueryContext(ctx, publishedSalonSelect+`
		WHERE salon.public_catalog_enabled = true
		  AND salon.public_slug IS NOT NULL
		  AND salon.public_slug <> ''
		ORDER BY salon.created_at ASC, salon.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]catalogSource, 0)
	for rows.Next() {
		item, err := scanCatalogSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) getSalon(ctx context.Context, slug string) (*catalogSource, error) {
	row := r.db.QueryRowContext(ctx, publishedSalonSelect+`
		WHERE salon.public_catalog_enabled = true
		  AND lower(salon.public_slug) = lower($1)
	`, slug)
	return scanCatalogSource(row)
}

const publishedSalonSelect = `
	SELECT salon.id::text, salon.public_slug, salon.name, salon.phone,
	       COALESCE(salon.address, ''), COALESCE(salon.city, ''),
	       COALESCE(salon.state, ''), COALESCE(salon.zip_code, ''),
	       salon.timezone, salon.primary_language,
	       COALESCE(salon.secondary_language, ''), salon.active_pos_provider,
	       settings.scheduling_authority, settings.scheduling_authority_version,
	       EXISTS (
	           SELECT 1 FROM manleai_calendar_configs config
	           WHERE config.salon_id = salon.id
	             AND config.activated_at IS NOT NULL
	             AND config.activated_version = config.version
	       ),
	       EXISTS (
	           SELECT 1 FROM pos_connections connection
	           WHERE connection.salon_id = salon.id
	             AND connection.provider = salon.active_pos_provider
	             AND connection.status = 'active'
	             AND NULLIF(connection.location_id, '') IS NOT NULL
	             AND connection.last_sync_at IS NOT NULL
	             AND connection.snapshot_generation > 0
	       )
	FROM salons salon
	JOIN salon_settings settings ON settings.salon_id = salon.id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCatalogSource(row rowScanner) (*catalogSource, error) {
	var source catalogSource
	err := row.Scan(
		&source.SalonID,
		&source.Salon.Slug,
		&source.Salon.Name,
		&source.Salon.Phone,
		&source.Salon.Address,
		&source.Salon.City,
		&source.Salon.State,
		&source.Salon.ZipCode,
		&source.Salon.Timezone,
		&source.Salon.PrimaryLanguage,
		&source.Salon.SecondaryLanguage,
		&source.Provider,
		&source.SchedulingAuthority,
		&source.SchedulingAuthorityVersion,
		&source.InternalActivationCurrent,
		&source.ExternalConnectionReady,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *Repository) listServices(ctx context.Context, source *catalogSource) ([]PublicService, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT service.name, COALESCE(service.description, ''),
		       COALESCE(service.ai_description, ''), service.duration_minutes,
		       service.price_from, COALESCE(service.price_display, '')
		FROM services service
		WHERE service.salon_id = $1
		  AND service.active = true
		  AND service.ai_bookable = true
		  AND service.archived_at IS NULL
		  AND service.duration_minutes > 0
		  AND (
		      $2 = 'owner_manual'
		      OR (
		          $2 = 'manleai_calendar'
		          AND EXISTS (
		              SELECT 1 FROM manleai_calendar_service_policies policy
		              WHERE policy.salon_id = service.salon_id
		                AND policy.service_id = service.id
		                AND policy.enabled = true
		          )
		      )
		      OR (
		          $2 = 'external_provider'
		          AND service.pos_provider = $3
		          AND service.sync_status = 'synced'
		          AND COALESCE(service.pos_service_version, 0) > 0
		          AND EXISTS (
		              SELECT 1 FROM pos_entity_links link
		              WHERE link.salon_id = service.salon_id
		                AND link.entity_type = 'service'
		                AND link.entity_id = service.id
		                AND link.provider = $3
		                AND link.sync_status = 'synced'
		                AND NULLIF(link.provider_entity_id, '') IS NOT NULL
		          )
		      )
		  )
		ORDER BY service.name ASC, service.id ASC
	`, source.SalonID, source.SchedulingAuthority, source.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := make([]PublicService, 0)
	for rows.Next() {
		var item PublicService
		var price sql.NullFloat64
		if err := rows.Scan(&item.Name, &item.Description, &item.AIDescription, &item.DurationMinutes, &price, &item.PriceDisplay); err != nil {
			return nil, err
		}
		if price.Valid {
			item.PriceFrom = &price.Float64
		}
		services = append(services, item)
	}
	return services, rows.Err()
}

func (r *Repository) listStaff(ctx context.Context, source *catalogSource) ([]PublicStaffMember, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT staff.name
		FROM staff staff
		WHERE staff.salon_id = $1
		  AND staff.active = true
		  AND staff.ai_bookable = true
		  AND staff.archived_at IS NULL
		  AND (
		      $2 = 'owner_manual'
		      OR (
		          $2 = 'manleai_calendar'
		          AND EXISTS (
		              SELECT 1
		              FROM manleai_calendar_service_staff eligible
		              JOIN manleai_calendar_service_policies policy
		                ON policy.salon_id = eligible.salon_id
		               AND policy.service_id = eligible.service_id
		               AND policy.enabled = true
		              WHERE eligible.salon_id = staff.salon_id
		                AND eligible.staff_id = staff.id
		          )
		          AND EXISTS (
		              SELECT 1 FROM manleai_calendar_staff_weekly_periods weekly
		              WHERE weekly.salon_id = staff.salon_id AND weekly.staff_id = staff.id
		          )
		      )
		      OR (
		          $2 = 'external_provider'
		          AND staff.pos_provider = $3
		          AND staff.sync_status = 'synced'
		          AND EXISTS (
		              SELECT 1 FROM pos_entity_links link
		              WHERE link.salon_id = staff.salon_id
		                AND link.entity_type = 'staff'
		                AND link.entity_id = staff.id
		                AND link.provider = $3
		                AND link.sync_status = 'synced'
		                AND NULLIF(link.provider_entity_id, '') IS NOT NULL
		          )
		      )
		  )
		ORDER BY staff.name ASC, staff.id ASC
	`, source.SalonID, source.SchedulingAuthority, source.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	staff := make([]PublicStaffMember, 0)
	for rows.Next() {
		var item PublicStaffMember
		if err := rows.Scan(&item.Name); err != nil {
			return nil, err
		}
		staff = append(staff, item)
	}
	return staff, rows.Err()
}

func (r *Repository) listHours(ctx context.Context, source *catalogSource) ([]PublicBusinessHourPeriod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT day_of_week, start_local_time::text, end_local_time::text, source
		FROM salon_business_hour_periods
		WHERE salon_id = $1
		  AND (
		      ($2 = 'owner_manual' AND source IN ('local_override', 'local_migrated'))
		      OR ($2 = 'manleai_calendar' AND source = 'local_override')
		      OR ($2 = 'external_provider' AND source = 'imported' AND provider = $3)
		  )
		ORDER BY day_of_week ASC, start_local_time ASC, provider_period_index ASC
	`, source.SalonID, source.SchedulingAuthority, source.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hours := make([]PublicBusinessHourPeriod, 0)
	for rows.Next() {
		var item PublicBusinessHourPeriod
		if err := rows.Scan(&item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime, &item.Source); err != nil {
			return nil, err
		}
		hours = append(hours, item)
	}
	return hours, rows.Err()
}
