package public_catalog

import (
	"context"
	"database/sql"
	"errors"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetBySlug(ctx context.Context, slug string) (*Catalog, error) {
	salon, salonID, provider, err := r.getSalon(ctx, slug)
	if err != nil {
		return nil, err
	}
	return r.getCatalog(ctx, salon, salonID, provider)
}

func (r *Repository) GetFirstPublished(ctx context.Context) (*Catalog, error) {
	salon, salonID, provider, err := r.getFirstPublishedSalon(ctx)
	if err != nil {
		return nil, err
	}
	return r.getCatalog(ctx, salon, salonID, provider)
}

func (r *Repository) getCatalog(ctx context.Context, salon *PublicSalon, salonID string, provider string) (*Catalog, error) {
	services, err := r.listServices(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	staff, err := r.listStaff(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	hours, err := r.listHours(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	return &Catalog{
		Salon:       *salon,
		Services:    services,
		Staff:       staff,
		Hours:       hours,
		BookingNote: "Appointments are confirmed only after Square Appointments completes the booking.",
	}, nil
}

func (r *Repository) getFirstPublishedSalon(ctx context.Context) (*PublicSalon, string, string, error) {
	var salon PublicSalon
	var salonID string
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, public_slug, name, phone, COALESCE(address, ''), COALESCE(city, ''),
		       COALESCE(state, ''), COALESCE(zip_code, ''), timezone, primary_language,
		       COALESCE(secondary_language, ''), active_pos_provider
		FROM salons
		WHERE public_catalog_enabled = true
		  AND public_slug IS NOT NULL
		  AND public_slug <> ''
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`).Scan(
		&salonID,
		&salon.Slug,
		&salon.Name,
		&salon.Phone,
		&salon.Address,
		&salon.City,
		&salon.State,
		&salon.ZipCode,
		&salon.Timezone,
		&salon.PrimaryLanguage,
		&salon.SecondaryLanguage,
		&salon.ActivePOSProvider,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", ErrNotFound
	}
	if err != nil {
		return nil, "", "", err
	}
	return &salon, salonID, salon.ActivePOSProvider, nil
}

func (r *Repository) getSalon(ctx context.Context, slug string) (*PublicSalon, string, string, error) {
	var salon PublicSalon
	var salonID string
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, public_slug, name, phone, COALESCE(address, ''), COALESCE(city, ''),
		       COALESCE(state, ''), COALESCE(zip_code, ''), timezone, primary_language,
		       COALESCE(secondary_language, ''), active_pos_provider
		FROM salons
		WHERE public_catalog_enabled = true
		  AND lower(public_slug) = lower($1)
	`, slug).Scan(
		&salonID,
		&salon.Slug,
		&salon.Name,
		&salon.Phone,
		&salon.Address,
		&salon.City,
		&salon.State,
		&salon.ZipCode,
		&salon.Timezone,
		&salon.PrimaryLanguage,
		&salon.SecondaryLanguage,
		&salon.ActivePOSProvider,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", ErrNotFound
	}
	if err != nil {
		return nil, "", "", err
	}
	return &salon, salonID, salon.ActivePOSProvider, nil
}

func (r *Repository) listServices(ctx context.Context, salonID string, provider string) ([]PublicService, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT svc.name, COALESCE(svc.description, ''), COALESCE(svc.ai_description, ''),
		       svc.duration_minutes, svc.price_from, COALESCE(svc.price_display, '')
		FROM services svc
		WHERE svc.salon_id = $1
		  AND svc.pos_provider = $2
		  AND svc.active = true
		  AND svc.ai_bookable = true
		  AND svc.archived_at IS NULL
		  AND svc.sync_status = 'synced'
		  AND svc.duration_minutes > 0
		  AND COALESCE(svc.pos_service_version, 0) > 0
		  AND EXISTS (
		      SELECT 1
		      FROM pos_entity_links link
		      WHERE link.salon_id = svc.salon_id
		        AND link.entity_type = 'service'
		        AND link.entity_id = svc.id
		        AND link.provider = $2
		        AND link.sync_status = 'synced'
		        AND link.provider_entity_id IS NOT NULL
		        AND link.provider_entity_id <> ''
		  )
		ORDER BY svc.name ASC
	`, salonID, provider)
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

func (r *Repository) listStaff(ctx context.Context, salonID string, provider string) ([]PublicStaffMember, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.name
		FROM staff st
		WHERE st.salon_id = $1
		  AND st.pos_provider = $2
		  AND st.active = true
		  AND st.ai_bookable = true
		  AND st.archived_at IS NULL
		  AND st.sync_status = 'synced'
		  AND EXISTS (
		      SELECT 1
		      FROM pos_entity_links link
		      WHERE link.salon_id = st.salon_id
		        AND link.entity_type = 'staff'
		        AND link.entity_id = st.id
		        AND link.provider = $2
		        AND link.sync_status = 'synced'
		        AND link.provider_entity_id IS NOT NULL
		        AND link.provider_entity_id <> ''
		  )
		ORDER BY st.name ASC
	`, salonID, provider)
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

func (r *Repository) listHours(ctx context.Context, salonID string, provider string) ([]PublicBusinessHourPeriod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT day_of_week, start_local_time::text, end_local_time::text, source, provider
		FROM salon_business_hour_periods
		WHERE salon_id = $1
		  AND source = 'imported'
		  AND provider = $2
		ORDER BY day_of_week ASC, start_local_time ASC, provider_period_index ASC
	`, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hours := make([]PublicBusinessHourPeriod, 0)
	for rows.Next() {
		var item PublicBusinessHourPeriod
		if err := rows.Scan(&item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime, &item.Source, &item.Provider); err != nil {
			return nil, err
		}
		hours = append(hours, item)
	}
	return hours, rows.Err()
}
