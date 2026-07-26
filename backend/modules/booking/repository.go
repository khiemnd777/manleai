package booking

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

type Repository struct {
	db *sql.DB
}

func requireExactlyOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return ErrOperationConflict
	}
	return nil
}

func lockBookingCalendarReconciliationTx(ctx context.Context, tx *sql.Tx, salonID string) error {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return ErrValidation
	}
	_, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		fence.AdvisoryKey(salonID),
	)
	return err
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)`, salonID, ownerUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return pos.ErrNotFound
	}
	return nil
}

func (r *Repository) GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	var provider string
	err := r.db.QueryRowContext(ctx, `
		SELECT active_pos_provider
		FROM salons
		WHERE id = $1
		  AND owner_user_id = $2
	`, salonID, ownerUserID).Scan(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return "", pos.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return pos.ProviderSquare, nil
	}
	return provider, nil
}

func (r *Repository) GetActiveProviderFence(ctx context.Context, salonID string, ownerUserID string) (string, pos.ProviderFence, error) {
	var provider string
	var status string
	var fence pos.ProviderFence
	var lastSyncAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square'),
		       COALESCE(connection.status, ''),
		       COALESCE(connection.location_id, ''),
		       COALESCE(connection.snapshot_generation, 0),
		       connection.last_sync_at
		FROM salons salon
		LEFT JOIN pos_connections connection
		  ON connection.salon_id = salon.id
		 AND connection.provider = COALESCE(NULLIF(BTRIM(salon.active_pos_provider), ''), 'square')
		WHERE salon.id = $1
		  AND salon.owner_user_id = $2
	`, salonID, ownerUserID).Scan(
		&provider,
		&status,
		&fence.LocationID,
		&fence.SnapshotGeneration,
		&lastSyncAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", pos.ProviderFence{}, pos.ErrNotFound
	}
	if err != nil {
		return "", pos.ProviderFence{}, err
	}
	provider = strings.TrimSpace(provider)
	fence.LocationID = strings.TrimSpace(fence.LocationID)
	if provider == "" || status != pos.StatusActive || !lastSyncAt.Valid || !validProviderFence(fence) {
		return "", pos.ProviderFence{}, pos.ErrStaleProviderFence
	}
	return provider, fence, nil
}

func (r *Repository) GetBookableService(ctx context.Context, salonID string, provider string, serviceID string) (*ServiceRef, error) {
	var item ServiceRef
	err := r.db.QueryRowContext(ctx, `
		SELECT svc.id::text, link.provider, link.provider_entity_id,
		       COALESCE(link.provider_version, svc.pos_service_version, 0),
		       connection.location_id, connection.snapshot_generation,
		       svc.name, svc.duration_minutes, COALESCE(svc.price_from, 0)
		FROM services svc
		JOIN pos_entity_links link
		  ON link.salon_id = svc.salon_id
		 AND link.entity_type = 'service'
		 AND link.entity_id = svc.id
		 AND link.provider = $3
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		JOIN pos_connections connection
		  ON connection.salon_id = svc.salon_id
		 AND connection.provider = link.provider
		 AND connection.status = 'active'
		 AND NULLIF(BTRIM(connection.location_id), '') IS NOT NULL
		 AND connection.snapshot_generation > 0
		 AND connection.last_sync_at IS NOT NULL
		WHERE svc.id = $1
		  AND svc.salon_id = $2
		  AND svc.active = true
		  AND svc.ai_bookable = true
		  AND svc.archived_at IS NULL
		  AND svc.sync_status = 'synced'
		  AND svc.pos_provider = $3
	`, serviceID, salonID, provider).Scan(
		&item.ID,
		&item.POSProvider,
		&item.POSServiceID,
		&item.POSServiceVersion,
		&item.ProviderFence.LocationID,
		&item.ProviderFence.SnapshotGeneration,
		&item.Name,
		&item.DurationMinutes,
		&item.PriceFrom,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetBookableStaff(ctx context.Context, salonID string, provider string, staffID string) (*StaffRef, error) {
	var item StaffRef
	err := r.db.QueryRowContext(ctx, `
		SELECT st.id::text, link.provider, link.provider_entity_id,
		       connection.location_id, connection.snapshot_generation, st.name
		FROM staff st
		JOIN pos_entity_links link
		  ON link.salon_id = st.salon_id
		 AND link.entity_type = 'staff'
		 AND link.entity_id = st.id
		 AND link.provider = $3
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		JOIN pos_connections connection
		  ON connection.salon_id = st.salon_id
		 AND connection.provider = link.provider
		 AND connection.status = 'active'
		 AND NULLIF(BTRIM(connection.location_id), '') IS NOT NULL
		 AND connection.snapshot_generation > 0
		 AND connection.last_sync_at IS NOT NULL
		WHERE st.id = $1
		  AND st.salon_id = $2
		  AND st.active = true
		  AND st.ai_bookable = true
		  AND st.archived_at IS NULL
		  AND st.sync_status = 'synced'
		  AND st.pos_provider = $3
	`, staffID, salonID, provider).Scan(
		&item.ID,
		&item.POSProvider,
		&item.POSStaffID,
		&item.ProviderFence.LocationID,
		&item.ProviderFence.SnapshotGeneration,
		&item.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ListBookableStaffRefs(ctx context.Context, salonID string, provider string) ([]StaffRef, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT st.id::text, link.provider, link.provider_entity_id,
		       connection.location_id, connection.snapshot_generation, st.name
		FROM staff st
		JOIN pos_entity_links link
		  ON link.salon_id = st.salon_id
		 AND link.entity_type = 'staff'
		 AND link.entity_id = st.id
		 AND link.provider = $2
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		JOIN pos_connections connection
		  ON connection.salon_id = st.salon_id
		 AND connection.provider = link.provider
		 AND connection.status = 'active'
		 AND NULLIF(BTRIM(connection.location_id), '') IS NOT NULL
		 AND connection.snapshot_generation > 0
		 AND connection.last_sync_at IS NOT NULL
		WHERE st.salon_id = $1
		  AND st.active = true
		  AND st.ai_bookable = true
		  AND st.archived_at IS NULL
		  AND st.sync_status = 'synced'
		  AND st.pos_provider = $2
		ORDER BY st.name ASC
	`, salonID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffRef, 0)
	for rows.Next() {
		var item StaffRef
		if err := rows.Scan(
			&item.ID,
			&item.POSProvider,
			&item.POSStaffID,
			&item.ProviderFence.LocationID,
			&item.ProviderFence.SnapshotGeneration,
			&item.Name,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ResolveBookingCustomer(ctx context.Context, salonID string, provider string, name string, phone string, email string) (*CustomerRef, error) {
	name = strings.TrimSpace(name)
	phone = validation.NormalizePhone(phone)
	email = strings.ToLower(strings.TrimSpace(email))
	if name == "" || phone == "" {
		return nil, ErrValidation
	}
	if customer, err := r.findBookingCustomer(ctx, salonID, provider, phone, email); err != nil {
		return nil, err
	} else if customer != nil {
		return customer, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var customerID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO customers (
			salon_id, name, phone, normalized_phone, email, normalized_email, active, sync_status, source
		)
		VALUES ($1, $2, $3, $3, NULLIF($4, ''), NULLIF($4, ''), true, 'local_only', 'local')
		RETURNING id::text
	`, salonID, name, phone, email).Scan(&customerID)
	if err != nil {
		if isUniqueViolation(err) {
			if customer, findErr := r.findBookingCustomer(ctx, salonID, provider, phone, email); findErr != nil {
				return nil, findErr
			} else if customer != nil {
				return customer, nil
			}
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
		)
		VALUES ($1, 'customer', $2, $3, NULL, 'local_only', NULL, NULL)
		ON CONFLICT (salon_id, entity_type, entity_id, provider)
		DO UPDATE SET sync_status = 'local_only',
		              provider_entity_id = NULL,
		              provider_version = NULL,
		              last_synced_at = NULL,
		              last_error = NULL,
		              updated_at = now()
	`, salonID, customerID, provider); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.findBookingCustomerByID(ctx, salonID, provider, customerID)
}

func (r *Repository) LinkBookingCustomer(ctx context.Context, salonID string, provider string, customerID string, customer pos.Customer) (*CustomerRef, error) {
	providerCustomerID := strings.TrimSpace(customer.POSCustomerID)
	if providerCustomerID == "" {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET phone = COALESCE(NULLIF(phone, ''), NULLIF($3, '')),
		    normalized_phone = COALESCE(NULLIF(normalized_phone, ''), NULLIF($4, '')),
		    email = COALESCE(NULLIF(email, ''), NULLIF($5, '')),
		    normalized_email = COALESCE(NULLIF(normalized_email, ''), NULLIF($6, '')),
		    sync_status = 'synced',
		    last_synced_at = now(),
		    sync_error = NULL,
		    updated_at = now()
		WHERE salon_id = $1
		  AND id = $2
		  AND archived_at IS NULL
	`, salonID, customerID, validation.NormalizePhone(customer.Phone), validation.NormalizePhone(customer.Phone), strings.ToLower(strings.TrimSpace(customer.Email)), strings.ToLower(strings.TrimSpace(customer.Email))); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at, last_error
		)
		VALUES ($1, 'customer', $2, $3, $4, 'synced', now(), NULL)
		ON CONFLICT (salon_id, entity_type, entity_id, provider)
		DO UPDATE SET provider_entity_id = EXCLUDED.provider_entity_id,
		              sync_status = 'synced',
		              last_synced_at = now(),
		              last_error = NULL,
		              updated_at = now()
	`, salonID, customerID, provider, providerCustomerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.findBookingCustomerByID(ctx, salonID, provider, customerID)
}

func (r *Repository) findBookingCustomer(ctx context.Context, salonID string, provider string, phone string, email string) (*CustomerRef, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT c.id::text, c.name, COALESCE(c.phone, ''), COALESCE(c.email, ''),
		       COALESCE(link.provider, $4), COALESCE(link.provider_entity_id, '')
		FROM customers c
		LEFT JOIN pos_entity_links link
		  ON link.salon_id = c.salon_id
		 AND link.entity_type = 'customer'
		 AND link.entity_id = c.id
		 AND link.provider = $4
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		WHERE c.salon_id = $1
		  AND c.archived_at IS NULL
		  AND (
		    c.normalized_phone = $2
		    OR (NULLIF($3, '') IS NOT NULL AND c.normalized_email = $3)
		  )
		ORDER BY CASE WHEN c.normalized_phone = $2 THEN 0 ELSE 1 END, c.updated_at DESC
		LIMIT 1
	`, salonID, phone, email, provider)
	customer, err := scanCustomerRef(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return customer, err
}

func (r *Repository) findBookingCustomerByID(ctx context.Context, salonID string, provider string, customerID string) (*CustomerRef, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT c.id::text, c.name, COALESCE(c.phone, ''), COALESCE(c.email, ''),
		       COALESCE(link.provider, $3), COALESCE(link.provider_entity_id, '')
		FROM customers c
		LEFT JOIN pos_entity_links link
		  ON link.salon_id = c.salon_id
		 AND link.entity_type = 'customer'
		 AND link.entity_id = c.id
		 AND link.provider = $3
		 AND link.sync_status = 'synced'
		 AND link.provider_entity_id IS NOT NULL
		 AND link.provider_entity_id <> ''
		WHERE c.salon_id = $1
		  AND c.id = $2
	`, salonID, customerID, provider)
	customer, err := scanCustomerRef(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	return customer, err
}

func scanCustomerRef(row interface {
	Scan(dest ...any) error
}) (*CustomerRef, error) {
	var item CustomerRef
	if err := row.Scan(&item.ID, &item.Name, &item.Phone, &item.Email, &item.POSProvider, &item.POSCustomerID); err != nil {
		return nil, err
	}
	return &item, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (r *Repository) GetSchedule(ctx context.Context, salonID string, provider string, fence pos.ProviderFence) (*Schedule, error) {
	if strings.TrimSpace(provider) == "" || !validProviderFence(fence) {
		return nil, ErrValidation
	}
	var schedule Schedule
	if err := r.db.QueryRowContext(ctx, `
		SELECT salon.timezone
		FROM salons salon
		JOIN pos_connections connection
		  ON connection.salon_id = salon.id
		 AND connection.provider = $2
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = $3
		 AND connection.snapshot_generation = $4
		WHERE salon.id = $1
		  AND salon.active_pos_provider = $2
	`, salonID, provider, fence.LocationID, fence.SnapshotGeneration).Scan(&schedule.Timezone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT bhp.day_of_week, bhp.start_local_time::text, bhp.end_local_time::text
		FROM salon_business_hour_periods bhp
		JOIN pos_connections connection
		  ON connection.salon_id = bhp.salon_id
		 AND connection.provider = $2
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = $3
		 AND connection.snapshot_generation = $4
		WHERE bhp.salon_id = $1
		  AND bhp.source = 'imported'
		  AND bhp.provider = $2
		  AND bhp.provider_location_id = $3
		ORDER BY bhp.day_of_week ASC, bhp.start_local_time ASC, bhp.provider_period_index ASC
	`, salonID, provider, fence.LocationID, fence.SnapshotGeneration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedule.BusinessHourPeriods = make([]BusinessHourPeriod, 0)
	for rows.Next() {
		var period BusinessHourPeriod
		if err := rows.Scan(&period.DayOfWeek, &period.StartLocalTime, &period.EndLocalTime); err != nil {
			return nil, err
		}
		schedule.BusinessHourPeriods = append(schedule.BusinessHourPeriods, period)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *Repository) CreateAvailabilityQuote(ctx context.Context, record AvailabilityQuoteRecord) (*AvailabilityQuote, error) {
	targetAware := record.OperationType == BookingActionReschedule
	retryAware := strings.TrimSpace(record.RetryOfAttemptID) != ""
	if strings.TrimSpace(record.SalonID) == "" || strings.TrimSpace(record.Provider) == "" || strings.TrimSpace(record.RequestFingerprint) == "" || !validProviderFence(record.ProviderFence) || !record.ExpiresAt.After(time.Now().UTC()) || len(record.Slots) == 0 ||
		(record.OperationType != "" && !targetAware) ||
		(targetAware && (strings.TrimSpace(record.TargetAppointmentID) == "" || record.TargetAuthorityAppointmentVersion < 0)) ||
		(!targetAware && strings.TrimSpace(record.TargetAppointmentID) != "") ||
		(targetAware && retryAware) || !validOptionalUUID(record.RetryOfAttemptID) {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.SalonID); err != nil {
		return nil, err
	}
	var authority string
	var authorityVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority, scheduling_authority_version FROM salon_settings WHERE salon_id = $1 FOR SHARE`, record.SalonID).Scan(&authority, &authorityVersion); err != nil {
		return nil, err
	}
	if authority != SchedulingAuthorityExternalProvider {
		if !targetAware && !retryAware {
			return nil, ErrAvailabilityQuoteStale
		}
	}
	if retryAware {
		if err := validateRetryAvailabilityQuoteOriginTx(ctx, tx, record); err != nil {
			return nil, err
		}
	}
	if targetAware {
		var targetVersion int
		if err := tx.QueryRowContext(ctx, `
			SELECT authority_appointment_version
			FROM appointments
			WHERE id::text=$1 AND salon_id::text=$2
			  AND scheduling_authority='external_provider'
			  AND pos_provider=$3
			  AND authority_appointment_version=$4
			  AND status NOT IN ('cancelled','declined','no_show')
			FOR SHARE
		`, record.TargetAppointmentID, record.SalonID, record.Provider, record.TargetAuthorityAppointmentVersion).Scan(&targetVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrAvailabilityQuoteStale
			}
			return nil, err
		}
	}
	quote := AvailabilityQuote{
		SalonID:                           record.SalonID,
		SchedulingAuthority:               SchedulingAuthorityExternalProvider,
		SchedulingAuthorityVersion:        authorityVersion,
		AuthorityProvider:                 record.Provider,
		AuthorityLocationID:               record.ProviderFence.LocationID,
		AuthoritySnapshotGeneration:       record.ProviderFence.SnapshotGeneration,
		Provider:                          record.Provider,
		ProviderFence:                     record.ProviderFence,
		RequestFingerprint:                record.RequestFingerprint,
		OperationType:                     record.OperationType,
		TargetAppointmentID:               record.TargetAppointmentID,
		TargetAuthorityAppointmentVersion: record.TargetAuthorityAppointmentVersion,
		ExpiresAt:                         record.ExpiresAt.UTC(),
		Slots:                             append([]AvailabilitySlot(nil), record.Slots...),
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO availability_quotes (
			scheduling_authority, scheduling_authority_version, authority_fence_provenance,
			authority_provider, authority_location_id, authority_snapshot_generation,
			salon_id, provider, provider_location_id, provider_snapshot_generation,
			request_fingerprint, operation_type, retry_of_attempt_id, target_appointment_id, target_authority_appointment_version, expires_at
		)
		SELECT $7, CASE WHEN $9 = 'reschedule' OR $12 <> '' THEN NULL ELSE settings.scheduling_authority_version END,
		       CASE WHEN $9 = 'reschedule' THEN 'target_origin' WHEN $12 <> '' THEN 'retry_origin' ELSE 'known' END,
		       connection.provider, connection.location_id, connection.snapshot_generation,
		       salon.id, connection.provider, connection.location_id, connection.snapshot_generation,
		       $5, NULLIF($9,''), NULLIF($12,'')::uuid, NULLIF($10,'')::uuid,
		       CASE WHEN $9 = 'reschedule' THEN $11::integer ELSE NULL END, $6
		FROM salons salon
		JOIN salon_settings settings
		  ON settings.salon_id = salon.id
		 AND settings.scheduling_authority_version = $8
		 AND ($9 = 'reschedule' OR $12 <> '' OR settings.scheduling_authority = $7)
		JOIN pos_connections connection
		  ON connection.salon_id = salon.id
		 AND connection.provider = $2
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = $3
		 AND connection.snapshot_generation = $4
		WHERE salon.id = $1
		  AND salon.active_pos_provider = $2
		RETURNING id::text, scheduling_authority, COALESCE(scheduling_authority_version,0), authority_provider,
		          COALESCE(authority_location_id, ''), COALESCE(authority_snapshot_generation, 0),
		          COALESCE(operation_type,''), COALESCE(target_appointment_id::text,''),
		          COALESCE(target_authority_appointment_version,0), created_at
	`, quote.SalonID, quote.Provider, quote.ProviderFence.LocationID, quote.ProviderFence.SnapshotGeneration,
		quote.RequestFingerprint, quote.ExpiresAt, SchedulingAuthorityExternalProvider, authorityVersion,
		quote.OperationType, quote.TargetAppointmentID, quote.TargetAuthorityAppointmentVersion, record.RetryOfAttemptID).Scan(
		&quote.ID,
		&quote.SchedulingAuthority,
		&quote.SchedulingAuthorityVersion,
		&quote.AuthorityProvider,
		&quote.AuthorityLocationID,
		&quote.AuthoritySnapshotGeneration,
		&quote.OperationType,
		&quote.TargetAppointmentID,
		&quote.TargetAuthorityAppointmentVersion,
		&quote.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAvailabilityQuoteStale
		}
		return nil, err
	}
	for _, slot := range quote.Slots {
		if strings.TrimSpace(slot.Fingerprint) == "" || slot.StartTime.IsZero() || !slot.EndTime.After(slot.StartTime) {
			return nil, ErrValidation
		}
		segments, err := json.Marshal(slot.Segments)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slots (salon_id, quote_id, slot_fingerprint, start_time, end_time, segments)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		`, quote.SalonID, quote.ID, slot.Fingerprint, slot.StartTime.UTC(), slot.EndTime.UTC(), string(segments)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &quote, nil
}

func validateRetryAvailabilityQuoteOriginTx(ctx context.Context, tx *sql.Tx, record AvailabilityQuoteRecord) error {
	var provider string
	var location sql.NullString
	var generation sql.NullInt64
	var requestedStart, requestedEnd time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT attempt.pos_provider, attempt.provider_location_id, attempt.provider_snapshot_generation,
		       attempt.requested_start_time, attempt.requested_end_time
		FROM booking_attempts attempt
		WHERE attempt.id::text=$1 AND attempt.salon_id::text=$2
		  AND attempt.scheduling_authority='external_provider'
		  AND COALESCE(attempt.operation_type,'book')='book'
		  AND attempt.status='fallback_pending' AND attempt.retry_policy='safe'
		  AND attempt.superseded_at IS NULL
		FOR SHARE
	`, record.RetryOfAttemptID, record.SalonID).Scan(&provider, &location, &generation, &requestedStart, &requestedEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOperationConflict
	}
	if err != nil {
		return err
	}
	if provider != record.Provider || !location.Valid || strings.TrimSpace(location.String) != strings.TrimSpace(record.ProviderFence.LocationID) ||
		!generation.Valid || record.ProviderFence.SnapshotGeneration < generation.Int64 || len(record.Slots) != 1 ||
		!record.Slots[0].StartTime.UTC().Equal(requestedStart.UTC()) || !record.Slots[0].EndTime.UTC().Equal(requestedEnd.UTC()) {
		return ErrOperationConflict
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(service_id::text,''), COALESCE(staff_id::text,''), staff_selection_mode, duration_minutes, sort_order
		FROM booking_attempt_segments
		WHERE salon_id::text=$1 AND booking_attempt_id::text=$2
		ORDER BY sort_order, id
		FOR SHARE
	`, record.SalonID, record.RetryOfAttemptID)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(record.Slots[0].Segments) {
			return ErrOperationConflict
		}
		var serviceID, staffID, selectionMode string
		var duration, sortOrder int
		if err := rows.Scan(&serviceID, &staffID, &selectionMode, &duration, &sortOrder); err != nil {
			return err
		}
		segment := record.Slots[0].Segments[index]
		if serviceID != segment.ServiceID || staffID != segment.StaffID || selectionMode != segment.StaffSelectionMode ||
			duration != segment.DurationMinutes || sortOrder != index+1 {
			return ErrOperationConflict
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index == 0 || index != len(record.Slots[0].Segments) {
		return ErrOperationConflict
	}
	return nil
}

func (r *Repository) GetAvailabilityQuoteProviderFence(ctx context.Context, salonID string, provider string, quoteID string, slotFingerprint string) (pos.ProviderFence, error) {
	var fence pos.ProviderFence
	err := r.db.QueryRowContext(ctx, `
		SELECT quote.provider_location_id, quote.provider_snapshot_generation
		FROM availability_quotes quote
		JOIN availability_quote_slots slot
		  ON slot.quote_id = quote.id
		 AND slot.slot_fingerprint = $4
		JOIN pos_connections connection
		  ON connection.salon_id = quote.salon_id
		 AND connection.provider = quote.provider
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = quote.provider_location_id
		 AND connection.snapshot_generation = quote.provider_snapshot_generation
		WHERE quote.id = $1
		  AND quote.salon_id = $2
		  AND quote.provider = $3
		  AND quote.expires_at > now()
	`, quoteID, salonID, provider, slotFingerprint).Scan(&fence.LocationID, &fence.SnapshotGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return pos.ProviderFence{}, ErrAvailabilityQuoteStale
	}
	if err != nil {
		return pos.ProviderFence{}, err
	}
	if !validProviderFence(fence) {
		return pos.ProviderFence{}, ErrAvailabilityQuoteStale
	}
	return fence, nil
}

// CleanupAvailabilityQuotes removes only quote rows that are past their
// retention cutoff and no longer participate in a booking-attempt audit trail.
// The single statement is bounded and uses SKIP LOCKED so multiple workers can
// run safely without blocking one another. Deleting a quote cascades only to
// its availability_quote_slots children.
func (r *Repository) CleanupAvailabilityQuotes(ctx context.Context, unconsumedExpiredBefore time.Time, consumedBefore time.Time, limit int) (int, error) {
	if unconsumedExpiredBefore.IsZero() || consumedBefore.IsZero() {
		return 0, ErrValidation
	}
	var deleted int
	err := r.db.QueryRowContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT quote.id
			FROM availability_quotes quote
			WHERE quote.consumed_by_attempt_id IS NULL
			  AND NOT EXISTS (
			      SELECT 1
			      FROM booking_attempts attempt
			      WHERE attempt.availability_quote_id = quote.id
			  )
			  AND (
			      (quote.consumed_at IS NULL AND quote.expires_at <= $1)
			      OR
			      (quote.consumed_at IS NOT NULL AND quote.consumed_at <= $2)
			  )
			ORDER BY COALESCE(quote.consumed_at, quote.expires_at) ASC, quote.id ASC
			LIMIT $3
			FOR UPDATE OF quote SKIP LOCKED
		), deleted AS (
			DELETE FROM availability_quotes quote
			USING candidates
			WHERE quote.id = candidates.id
			RETURNING quote.id
		)
		SELECT count(*)
		FROM deleted
	`, unconsumedExpiredBefore.UTC(), consumedBefore.UTC(), clampAvailabilityQuoteCleanupLimit(limit)).Scan(&deleted)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *Repository) ClaimPendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingOperationClaim, error) {
	attempt := BookingAttempt{
		SalonID:             record.SalonID,
		Source:              record.Source,
		Status:              StatusPOSPending,
		POSProvider:         record.Provider,
		POSIdempotencyKey:   record.POSIdempotencyKey,
		OperationKey:        record.OperationKey,
		RequestFingerprint:  record.RequestFingerprint,
		RetryOfAttemptID:    record.RetryOfAttemptID,
		AvailabilityQuoteID: record.AvailabilityQuoteID,
		SlotFingerprint:     record.SlotFingerprint,
		ProviderFence:       record.ProviderFence,
		OperationType:       BookingActionBook,
		ProcessingToken:     record.ProcessingToken,
		ProviderOutcome:     ProviderOutcomeNotStarted,
		RetryPolicy:         RetryPolicyNone,
		Reconciliation:      ReconciliationNotRequired,
		CustomerName:        record.CustomerName,
		CustomerPhone:       record.CustomerPhone,
		CustomerEmail:       record.CustomerEmail,
		ServiceID:           record.Service.ID,
		StaffID:             record.Staff.ID,
		StaffSelectionMode:  record.StaffSelectionMode,
		RequestedStartTime:  record.StartTime,
		RequestedEndTime:    record.EndTime,
		Notes:               record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	return r.claimPendingOperation(ctx, attempt, record.LeaseExpiresAt, record.Segments)
}

func (r *Repository) MarkBookingOperationStarted(ctx context.Context, salonID string, attemptID string, processingToken string, leaseExpiresAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET provider_outcome = $1,
		    processing_lease_expires_at = $2,
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
		  AND status = $5
		  AND provider_outcome = $6
		  AND processing_token = $7
		  AND superseded_at IS NULL
	`, ProviderOutcomeInFlight, leaseExpiresAt, attemptID, salonID, StatusPOSPending, ProviderOutcomeNotStarted, processingToken)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrOperationInProgress
	}
	return nil
}

type bookingLeaseExpiryPolicy struct {
	ProviderOutcome          string
	RetryPolicy              string
	Reconciliation           string
	ErrorCode                string
	ErrorMessage             string
	OperationSuffix          string
	NotificationDedupePrefix string
	NotificationMessage      string
	FailurePhase             string
}

func bookingLeaseExpiryPolicyFor(providerOutcome string) bookingLeaseExpiryPolicy {
	if providerOutcome == ProviderOutcomeNotStarted {
		return bookingLeaseExpiryPolicy{
			ProviderOutcome:          ProviderOutcomeFailed,
			RetryPolicy:              RetryPolicySafe,
			Reconciliation:           ReconciliationNotRequired,
			ErrorCode:                pos.ErrorTimeout,
			ErrorMessage:             "Booking operation lease expired before provider dispatch began.",
			OperationSuffix:          "_lease_expired_before_dispatch",
			NotificationDedupePrefix: "booking-result:",
			NotificationMessage:      "The provider call did not start before the operation lease expired. This exact request is safe to retry.",
			FailurePhase:             "pre_dispatch",
		}
	}
	return bookingLeaseExpiryPolicy{
		ProviderOutcome:          ProviderOutcomeUnknown,
		RetryPolicy:              RetryPolicyBlocked,
		Reconciliation:           ReconciliationRequired,
		ErrorCode:                pos.ErrorTimeout,
		ErrorMessage:             "Booking operation lease expired while the provider result was in flight.",
		OperationSuffix:          "_lease_expired",
		NotificationDedupePrefix: "booking-reconciliation:",
		NotificationMessage:      "The provider result is unknown. Verify the action in the active POS before retrying.",
		FailurePhase:             "in_flight",
	}
}

func (r *Repository) ExpireBookingOperationLeases(ctx context.Context, salonID string) error {
	_, err := r.expireBookingOperationLeases(ctx, salonID, 50)
	return err
}

func clampBookingLeaseSweepLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

type bookingLeaseCandidate struct {
	AttemptID           string
	SalonID             string
	Provider            string
	OperationType       string
	TargetAppointmentID string
	POSBookingID        string
}

type expiredBookingOperation struct {
	bookingLeaseCandidate
	POSBookingVersion       int
	TargetPOSBookingVersion int
	ProviderFence           pos.ProviderFence
	ProviderOutcome         string
	ServiceID               string
	StaffID                 string
	StaffSelectionMode      string
	RequestedStartTime      time.Time
	RequestedEndTime        time.Time
	RetryPolicy             string
	Reconciliation          string
	ErrorMessage            string
}

func bookingAttemptSegmentsMatchAppointmentTx(ctx context.Context, tx *sql.Tx, attemptID string, appointmentID string) (bool, error) {
	var matches bool
	err := tx.QueryRowContext(ctx, `
		SELECT
			COALESCE((
				SELECT jsonb_agg(jsonb_build_array(
					COALESCE(segment.service_id::text, ''),
					segment.pos_service_id,
					COALESCE(segment.pos_service_version, 0),
					COALESCE(segment.staff_id::text, ''),
					COALESCE(segment.pos_staff_id, ''),
					COALESCE(segment.staff_selection_mode, 'specific'),
					segment.duration_minutes,
					segment.sort_order
				) ORDER BY segment.sort_order, segment.id)
				FROM booking_attempt_segments segment
				WHERE segment.booking_attempt_id = $1
			), '[]'::jsonb) =
			COALESCE((
				SELECT jsonb_agg(jsonb_build_array(
					COALESCE(segment.service_id::text, ''),
					segment.pos_service_id,
					COALESCE(segment.pos_service_version, 0),
					COALESCE(segment.staff_id::text, ''),
					COALESCE(segment.pos_staff_id, ''),
					COALESCE(segment.staff_selection_mode, 'specific'),
					segment.duration_minutes,
					segment.sort_order
				) ORDER BY segment.sort_order, segment.id)
				FROM appointment_services segment
				WHERE segment.appointment_id = $2
			), '[]'::jsonb)
			AND EXISTS (
				SELECT 1 FROM booking_attempt_segments WHERE booking_attempt_id = $1
			)
	`, attemptID, appointmentID).Scan(&matches)
	return matches, err
}

func (r *Repository) expireBookingOperationLeases(ctx context.Context, salonID string, limit int) (int, error) {
	preDispatchPolicy := bookingLeaseExpiryPolicyFor(ProviderOutcomeNotStarted)
	inFlightPolicy := bookingLeaseExpiryPolicyFor(ProviderOutcomeInFlight)
	rows, err := r.db.QueryContext(ctx, `
		SELECT attempt.id::text, attempt.salon_id::text, attempt.pos_provider,
		       attempt.operation_type, COALESCE(attempt.target_appointment_id::text, ''),
		       COALESCE(attempt.pos_booking_id, '')
		FROM booking_attempts attempt
		WHERE (NULLIF($1, '')::uuid IS NULL OR attempt.salon_id = NULLIF($1, '')::uuid)
		  AND attempt.scheduling_authority = 'external_provider'
		  AND attempt.status = $2
		  AND attempt.provider_outcome IN ($3, $4)
		  AND attempt.superseded_at IS NULL
		  AND attempt.processing_lease_expires_at IS NOT NULL
		  AND attempt.processing_lease_expires_at <= now()
		ORDER BY attempt.processing_lease_expires_at ASC, attempt.id ASC
		LIMIT $5
	`, salonID, StatusPOSPending, ProviderOutcomeNotStarted, ProviderOutcomeInFlight, clampBookingLeaseSweepLimit(limit))
	if err != nil {
		return 0, err
	}
	candidates := make([]bookingLeaseCandidate, 0)
	for rows.Next() {
		var item bookingLeaseCandidate
		if err := rows.Scan(
			&item.AttemptID,
			&item.SalonID,
			&item.Provider,
			&item.OperationType,
			&item.TargetAppointmentID,
			&item.POSBookingID,
		); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	expiredCount := 0
	for _, candidate := range candidates {
		changed, err := r.expireBookingOperationLeaseCandidate(ctx, candidate, preDispatchPolicy, inFlightPolicy)
		if err != nil {
			return expiredCount, err
		}
		if changed {
			expiredCount++
		}
	}
	return expiredCount, nil
}

func (r *Repository) expireBookingOperationLeaseCandidate(ctx context.Context, candidate bookingLeaseCandidate, preDispatchPolicy bookingLeaseExpiryPolicy, inFlightPolicy bookingLeaseExpiryPolicy) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, candidate.SalonID); err != nil {
		return false, err
	}

	var currentAppointment *Appointment
	var mirror *calendarAppointmentSnapshot
	if candidate.OperationType == BookingActionBook && strings.TrimSpace(candidate.POSBookingID) != "" {
		var appointmentID string
		err := tx.QueryRowContext(ctx, `
			SELECT id::text
			FROM appointments
			WHERE salon_id = $1
			  AND scheduling_authority = 'external_provider'
			  AND pos_provider = $2
			  AND pos_appointment_id = $3
			FOR UPDATE
		`, candidate.SalonID, candidate.Provider, candidate.POSBookingID).Scan(&appointmentID)
		if err == nil {
			snapshot, loadErr := loadCalendarAppointmentSnapshotTx(ctx, tx, candidate.SalonID, appointmentID, candidate.Provider, candidate.POSBookingID)
			if loadErr != nil {
				return false, loadErr
			}
			mirror = &snapshot
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	} else if candidate.TargetAppointmentID != "" {
		appointment, loadErr := scanAppointment(tx.QueryRowContext(ctx, `
			SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
			       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
			       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
			       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
			       COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
			       COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''),
			       COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''),
			       created_at, updated_at
			FROM appointments
			WHERE id = $1
			  AND salon_id = $2
			  AND scheduling_authority = 'external_provider'
			  AND pos_provider = $3
			  AND pos_appointment_id = $4
			FOR UPDATE
		`, candidate.TargetAppointmentID, candidate.SalonID, candidate.Provider, candidate.POSBookingID))
		if loadErr == nil {
			currentAppointment = appointment
		} else if !errors.Is(loadErr, pos.ErrNotFound) {
			return false, loadErr
		}
	}

	item := expiredBookingOperation{bookingLeaseCandidate: candidate}
	err = tx.QueryRowContext(ctx, `
		SELECT attempt.id::text, attempt.salon_id::text, attempt.pos_provider,
		       attempt.operation_type, COALESCE(attempt.target_appointment_id::text, ''),
		       COALESCE(attempt.pos_booking_id, ''), COALESCE(attempt.pos_booking_version, 0),
		       COALESCE(attempt.target_pos_booking_version, 0),
		       COALESCE(attempt.provider_location_id, ''), COALESCE(attempt.provider_snapshot_generation, 0),
		       attempt.provider_outcome, COALESCE(attempt.service_id::text, ''),
		       COALESCE(attempt.staff_id::text, ''), COALESCE(attempt.staff_selection_mode, 'specific'),
		       attempt.requested_start_time, attempt.requested_end_time
		FROM booking_attempts attempt
		WHERE attempt.id = $1
		  AND attempt.salon_id = $2
		  AND attempt.scheduling_authority = 'external_provider'
		  AND attempt.status = $3
		  AND attempt.provider_outcome IN ($4, $5)
		  AND attempt.superseded_at IS NULL
		  AND attempt.processing_lease_expires_at IS NOT NULL
		  AND attempt.processing_lease_expires_at <= now()
		FOR UPDATE
	`, candidate.AttemptID, candidate.SalonID, StatusPOSPending, ProviderOutcomeNotStarted, ProviderOutcomeInFlight).Scan(
		&item.AttemptID,
		&item.SalonID,
		&item.Provider,
		&item.OperationType,
		&item.TargetAppointmentID,
		&item.POSBookingID,
		&item.POSBookingVersion,
		&item.TargetPOSBookingVersion,
		&item.ProviderFence.LocationID,
		&item.ProviderFence.SnapshotGeneration,
		&item.ProviderOutcome,
		&item.ServiceID,
		&item.StaffID,
		&item.StaffSelectionMode,
		&item.RequestedStartTime,
		&item.RequestedEndTime,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if item.Provider != candidate.Provider ||
		item.OperationType != candidate.OperationType ||
		item.TargetAppointmentID != candidate.TargetAppointmentID ||
		item.POSBookingID != candidate.POSBookingID {
		return false, nil
	}

	converged := false
	terminalStatus := ""
	var terminalAppointment *Appointment
	if item.ProviderOutcome == ProviderOutcomeInFlight {
		switch item.OperationType {
		case BookingActionBook:
			if mirror != nil &&
				mirror.OriginSource == SourcePOSCalendarSync &&
				!mirror.OriginSuperseded &&
				validProviderFence(item.ProviderFence) &&
				strings.TrimSpace(mirror.ProviderLocationID) == strings.TrimSpace(item.ProviderFence.LocationID) &&
				mirror.ProviderGeneration > 0 &&
				mirror.POSAppointmentVersion >= item.POSBookingVersion &&
				(mirror.Status == StatusConfirmed || mirror.Status == StatusRescheduled) &&
				mirror.StartTime.Equal(item.RequestedStartTime) &&
				mirror.EndTime.Equal(item.RequestedEndTime) &&
				mirror.ServiceID == item.ServiceID &&
				mirror.StaffID == item.StaffID &&
				mirror.StaffSelectionMode == item.StaffSelectionMode {
				segmentsMatch, matchErr := bookingAttemptSegmentsMatchAppointmentTx(ctx, tx, item.AttemptID, mirror.AppointmentID)
				if matchErr != nil {
					return false, matchErr
				}
				converged = segmentsMatch
				terminalStatus = StatusConfirmed
				appointment := appointmentFromCalendarSnapshot(*mirror, item.AttemptID)
				appointment.SalonID = item.SalonID
				terminalAppointment = &appointment
			}
		case BookingActionReschedule:
			if currentAppointment != nil &&
				currentAppointment.POSAppointmentVersion > item.TargetPOSBookingVersion &&
				(currentAppointment.Status == StatusConfirmed || currentAppointment.Status == StatusRescheduled) &&
				currentAppointment.StartTime.Equal(item.RequestedStartTime) &&
				currentAppointment.EndTime.Equal(item.RequestedEndTime) &&
				currentAppointment.ServiceID == item.ServiceID &&
				currentAppointment.StaffID == item.StaffID &&
				currentAppointment.StaffSelectionMode == item.StaffSelectionMode {
				segmentsMatch, matchErr := bookingAttemptSegmentsMatchAppointmentTx(ctx, tx, item.AttemptID, currentAppointment.ID)
				if matchErr != nil {
					return false, matchErr
				}
				converged = segmentsMatch
				terminalStatus = StatusRescheduled
				terminalAppointment = currentAppointment
			}
		case BookingActionCancel:
			if currentAppointment != nil &&
				currentAppointment.POSAppointmentVersion > item.TargetPOSBookingVersion &&
				currentAppointment.Status == StatusCancelled {
				converged = true
				terminalStatus = StatusCancelled
				terminalAppointment = currentAppointment
			}
		}
	}
	if converged && terminalAppointment != nil {
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_attempts
			SET status = $1,
			    pos_booking_id = $2,
			    pos_booking_version = $3,
			    authority_appointment_id = $2,
			    authority_appointment_version = $3,
			    provider_outcome = $4,
			    retry_policy = $5,
			    reconciliation_status = $6,
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    error_code = NULL,
			    error_message = NULL,
			    updated_at = now()
			WHERE id = $7
			  AND salon_id = $8
			  AND scheduling_authority = 'external_provider'
			  AND status = $9
			  AND provider_outcome = $10
		`, terminalStatus, terminalAppointment.POSAppointmentID, terminalAppointment.POSAppointmentVersion,
			ProviderOutcomeSucceeded, RetryPolicyNone, ReconciliationNotRequired,
			item.AttemptID, item.SalonID, StatusPOSPending, ProviderOutcomeInFlight)
		if err := requireExactlyOneRow(result, err); err != nil {
			return false, err
		}
		if item.OperationType == BookingActionBook && mirror != nil {
			canonical, err := canonicalizeCalendarMirrorTx(ctx, tx, item.SalonID, item.AttemptID, *mirror)
			if err != nil {
				return false, err
			}
			terminalAppointment = &canonical
			payload := ownerNotificationPayload(NotificationTypeBookingConfirmed, item.SalonID, item.AttemptID, canonical.ID, map[string]any{
				"booking_status": StatusConfirmed,
				"pos_provider":   item.Provider,
				"pos_booking_id": canonical.POSAppointmentID,
			})
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
				VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8::jsonb)
				ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
			`, item.SalonID, item.AttemptID, canonical.ID, NotificationTypeBookingConfirmed,
				"New POS-confirmed appointment", "Provider calendar synchronization confirmed the appointment.",
				"booking-confirmed:"+item.AttemptID, payload); err != nil {
				return false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}

	policy := inFlightPolicy
	if item.ProviderOutcome == ProviderOutcomeNotStarted {
		policy = preDispatchPolicy
	}
	item.ProviderOutcome = policy.ProviderOutcome
	item.RetryPolicy = policy.RetryPolicy
	item.Reconciliation = policy.Reconciliation
	item.ErrorMessage = policy.ErrorMessage
	result, err := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    provider_outcome = $2,
		    retry_policy = $3,
		    reconciliation_status = $4,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    error_code = $5,
		    error_message = $6,
		    updated_at = now()
		WHERE id = $7
		  AND salon_id = $8
		  AND scheduling_authority = 'external_provider'
		  AND status = $9
	`, StatusFallbackPending, item.ProviderOutcome, item.RetryPolicy, item.Reconciliation,
		policy.ErrorCode, item.ErrorMessage, item.AttemptID, item.SalonID, StatusPOSPending)
	if err := requireExactlyOneRow(result, err); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, item.SalonID, item.Provider, item.OperationType+policy.OperationSuffix, policy.ErrorCode, item.ErrorMessage); err != nil {
		return false, err
	}
	reconciliationRequired := item.Reconciliation == ReconciliationRequired
	notificationType, title := expiredOperationNotification(item.OperationType, reconciliationRequired)
	payload := ownerNotificationPayload(notificationType, item.SalonID, item.AttemptID, item.TargetAppointmentID, map[string]any{
		"booking_status":          StatusFallbackPending,
		"pos_provider":            item.Provider,
		"provider_outcome":        item.ProviderOutcome,
		"retry_policy":            item.RetryPolicy,
		"reconciliation_status":   item.Reconciliation,
		"reconciliation_required": reconciliationRequired,
		"error_code":              policy.ErrorCode,
		"failure_phase":           policy.FailurePhase,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'pending', $5, $6, $7, $8::jsonb)
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, item.SalonID, item.AttemptID, item.TargetAppointmentID, notificationType, title,
		policy.NotificationMessage, policy.NotificationDedupePrefix+item.AttemptID, payload); err != nil {
		return false, err
	}
	if reconciliationRequired {
		if err := ensureReconciliationTaskTx(ctx, tx, item.SalonID, item.AttemptID, item.ErrorMessage); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SweepExpiredBookingOperationLeases makes lease recovery independent from a
// dashboard read. Candidate selection is bounded but lock-free; each candidate
// is re-read only after taking the salon calendar advisory lock.
func (r *Repository) SweepExpiredBookingOperationLeases(ctx context.Context, limit int) (int, error) {
	return r.expireBookingOperationLeases(ctx, "", clampBookingLeaseSweepLimit(limit))
}

func expiredOperationNotification(operationType string, reconciliationRequired bool) (string, string) {
	if !reconciliationRequired {
		switch operationType {
		case BookingActionReschedule:
			return NotificationTypeRescheduleFallback, "Reschedule request is safe to retry"
		case BookingActionCancel:
			return NotificationTypeCancellationFallback, "Cancellation request is safe to retry"
		default:
			return NotificationTypeBookingFallback, "Booking request is safe to retry"
		}
	}
	switch operationType {
	case BookingActionReschedule:
		return NotificationTypeRescheduleFallback, "Reschedule result needs POS reconciliation"
	case BookingActionCancel:
		return NotificationTypeCancellationFallback, "Cancellation result needs POS reconciliation"
	default:
		return NotificationTypeBookingFallback, "Booking result needs POS reconciliation"
	}
}

func ownerNotificationPayload(eventType string, salonID string, attemptID string, appointmentID string, fields map[string]any) string {
	payload := map[string]any{
		"schema_version":     1,
		"event_type":         eventType,
		"salon_id":           salonID,
		"booking_attempt_id": attemptID,
	}
	if strings.TrimSpace(appointmentID) != "" {
		payload["appointment_id"] = appointmentID
	}
	for key, value := range fields {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func ensureReconciliationTaskTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, note string) error {
	var attemptSuperseded bool
	if err := tx.QueryRowContext(ctx, `
		SELECT superseded_at IS NOT NULL
		FROM booking_attempts
		WHERE salon_id = $1 AND id = $2
		FOR UPDATE
	`, salonID, attemptID).Scan(&attemptSuperseded); err != nil {
		return err
	}
	if attemptSuperseded {
		return nil
	}
	var taskID string
	var taskStatus string
	var taskResolution string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, status, COALESCE(resolution, '')
		FROM booking_reconciliation_tasks
		WHERE salon_id = $1 AND booking_attempt_id = $2
		FOR UPDATE
	`, salonID, attemptID).Scan(&taskID, &taskStatus, &taskResolution)
	createOpenedEvent := false
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
			VALUES ($1, $2, 'open')
			RETURNING id::text
		`, salonID, attemptID).Scan(&taskID); err != nil {
			return err
		}
		createOpenedEvent = true
	} else if err != nil {
		return err
	} else if taskStatus == "resolved" && taskResolution == ReconciliationResolutionSuperseded {
		return nil
	} else if taskStatus != "open" {
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_reconciliation_tasks
			SET status = 'open', resolution = NULL, resolved_by_user_id = NULL,
			    resolution_note = NULL, resolved_at = NULL, updated_at = now()
			WHERE id = $1 AND salon_id = $2 AND status <> 'open'
		`, taskID, salonID)
		if err := requireExactlyOneRow(result, err); err != nil {
			return err
		}
		createOpenedEvent = true
	}
	if !createOpenedEvent {
		return nil
	}
	note = strings.TrimSpace(note)
	actionKey := fmt.Sprintf("opened:%s:%d", attemptID, time.Now().UTC().UnixNano())
	payloadFingerprint := fingerprintJSON(struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}{Action: "opened", Note: note})
	_, err = tx.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_events (
			salon_id, booking_attempt_id, reconciliation_task_id, action_key,
			payload_fingerprint, action, note
		)
		VALUES ($1, $2, $3, $4, $5, 'opened', NULLIF($6, ''))
	`, salonID, attemptID, taskID, actionKey, payloadFingerprint, note)
	return err
}

func closeSupersededReconciliationTaskTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, supersededByAttemptID string) error {
	salonID = strings.TrimSpace(salonID)
	attemptID = strings.TrimSpace(attemptID)
	supersededByAttemptID = strings.TrimSpace(supersededByAttemptID)
	if salonID == "" || attemptID == "" || supersededByAttemptID == "" || attemptID == supersededByAttemptID {
		return ErrValidation
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET reconciliation_status = $1,
		    reconciliation_resolution = $2,
		    reconciliation_resolved_at = COALESCE(reconciliation_resolved_at, now()),
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
		  AND superseded_at IS NOT NULL
		  AND superseded_by_attempt_id = $5
	`, ReconciliationResolved, ReconciliationResolutionSuperseded, attemptID, salonID, supersededByAttemptID)
	if err := requireExactlyOneRow(result, err); err != nil {
		return err
	}
	var taskID string
	var taskStatus string
	var taskResolution string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, status, COALESCE(resolution, '')
		FROM booking_reconciliation_tasks
		WHERE salon_id = $1 AND booking_attempt_id = $2
		FOR UPDATE
	`, salonID, attemptID).Scan(&taskID, &taskStatus, &taskResolution)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	const note = "Booking attempt was superseded by a successor attempt."
	if taskStatus != "resolved" || taskResolution != ReconciliationResolutionSuperseded {
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_reconciliation_tasks
			SET status = 'resolved',
			    resolution = $1,
			    resolved_by_user_id = NULL,
			    resolution_note = $2,
			    resolved_at = now(),
			    updated_at = now()
			WHERE id = $3 AND salon_id = $4
		`, ReconciliationResolutionSuperseded, note, taskID, salonID)
		if err := requireExactlyOneRow(result, err); err != nil {
			return err
		}
	}
	actionKey := ReconciliationResolutionSuperseded + ":" + supersededByAttemptID
	payloadFingerprint := fingerprintJSON(struct {
		Action                string `json:"action"`
		SupersededByAttemptID string `json:"superseded_by_attempt_id"`
	}{
		Action:                ReconciliationResolutionSuperseded,
		SupersededByAttemptID: supersededByAttemptID,
	})
	_, err = tx.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_events (
			salon_id, booking_attempt_id, reconciliation_task_id, action_key,
			payload_fingerprint, action, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (reconciliation_task_id, action_key) DO NOTHING
	`, salonID, attemptID, taskID, actionKey, payloadFingerprint, ReconciliationResolutionSuperseded, note)
	return err
}

func loadAvailabilityQuoteProviderFenceTx(ctx context.Context, tx *sql.Tx, attempt BookingAttempt) (pos.ProviderFence, error) {
	var fence pos.ProviderFence
	err := tx.QueryRowContext(ctx, `
		SELECT quote.provider_location_id, quote.provider_snapshot_generation
		FROM availability_quotes quote
		JOIN availability_quote_slots slot
		  ON slot.quote_id = quote.id
		 AND slot.slot_fingerprint = $4
		JOIN pos_connections connection
		  ON connection.salon_id = quote.salon_id
		 AND connection.provider = quote.provider
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = quote.provider_location_id
		 AND connection.snapshot_generation = quote.provider_snapshot_generation
		WHERE quote.id = $1
		  AND quote.salon_id = $2
		  AND quote.provider = $3
		  AND quote.expires_at > now()
		  AND length($4) = 64
		  AND (
		      ($5 = 'book' AND $8 = '' AND quote.operation_type IS NULL
		       AND quote.authority_fence_provenance = 'known'
		       AND quote.scheduling_authority_version IS NOT NULL
		       AND quote.retry_of_attempt_id IS NULL
		       AND quote.target_appointment_id IS NULL AND quote.target_authority_appointment_version IS NULL)
		      OR
		      ($5 = 'book' AND $8 <> '' AND quote.operation_type IS NULL
		       AND quote.authority_fence_provenance = 'retry_origin'
		       AND quote.scheduling_authority_version IS NULL
		       AND quote.retry_of_attempt_id::text = $8
		       AND quote.target_appointment_id IS NULL AND quote.target_authority_appointment_version IS NULL)
		      OR
		      ($5 = 'reschedule' AND quote.operation_type = 'reschedule'
		       AND quote.authority_fence_provenance = 'target_origin'
		       AND quote.retry_of_attempt_id IS NULL
		       AND quote.target_appointment_id::text = $6
		       AND quote.target_authority_appointment_version = $7)
		  )
		FOR UPDATE OF quote
	`, attempt.AvailabilityQuoteID, attempt.SalonID, attempt.POSProvider, attempt.SlotFingerprint,
		attempt.OperationType, attempt.TargetAppointmentID, attempt.TargetAuthorityAppointmentVersion,
		attempt.RetryOfAttemptID).Scan(
		&fence.LocationID,
		&fence.SnapshotGeneration,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return pos.ProviderFence{}, ErrAvailabilityQuoteStale
	}
	if err != nil {
		return pos.ProviderFence{}, err
	}
	return fence, nil
}

func sameRetryProviderLocation(location sql.NullString, generation sql.NullInt64, fence pos.ProviderFence) bool {
	if !location.Valid || strings.TrimSpace(location.String) == "" || !generation.Valid || generation.Int64 <= 0 || !validProviderFence(fence) {
		return false
	}
	return strings.TrimSpace(location.String) == strings.TrimSpace(fence.LocationID) &&
		fence.SnapshotGeneration >= generation.Int64
}

func (r *Repository) claimPendingOperation(ctx context.Context, attempt BookingAttempt, leaseExpiresAt time.Time, segments []BookingSegmentRecord) (*BookingOperationClaim, error) {
	requestedProcessingToken := attempt.ProcessingToken
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, attempt.SalonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, attempt.SalonID+":"+attempt.OperationType+":"+attempt.RequestFingerprint); err != nil {
		return nil, err
	}
	existingByOperationKey, err := scanBookingOperationAttempt(tx.QueryRowContext(ctx, bookingOperationSelectSQL+`
		WHERE ba.salon_id = $1
		  AND ba.operation_key = $2
		LIMIT 1
		FOR UPDATE
	`, attempt.SalonID, attempt.OperationKey))
	if err == nil {
		if existingByOperationKey.RequestFingerprint != attempt.RequestFingerprint {
			return nil, ErrOperationConflict
		}
		acquired := false
		if existingByOperationKey.SupersededAt == nil && existingByOperationKey.Status == StatusPOSPending && existingByOperationKey.ProviderOutcome == ProviderOutcomeNotStarted && (existingByOperationKey.ProcessingLeaseEnds == nil || existingByOperationKey.ProcessingLeaseEnds.Before(time.Now().UTC())) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE booking_attempts
				SET processing_token = $1, processing_lease_expires_at = $2, updated_at = now()
				WHERE id = $3 AND provider_outcome = $4 AND status = $5 AND superseded_at IS NULL
			`, requestedProcessingToken, leaseExpiresAt, existingByOperationKey.ID, ProviderOutcomeNotStarted, StatusPOSPending); err != nil {
				return nil, err
			}
			existingByOperationKey.ProcessingToken = requestedProcessingToken
			value := leaseExpiresAt
			existingByOperationKey.ProcessingLeaseEnds = &value
			acquired = true
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		if err := r.hydrateClaimAttempt(ctx, existingByOperationKey); err != nil {
			return nil, err
		}
		return &BookingOperationClaim{Attempt: existingByOperationKey, Acquired: acquired}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if attempt.OperationType == BookingActionBook && attempt.RetryOfAttemptID == "" {
		var currentAuthority string
		var currentVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority, scheduling_authority_version FROM salon_settings WHERE salon_id = $1 FOR SHARE`, attempt.SalonID).Scan(&currentAuthority, &currentVersion); err != nil {
			return nil, err
		}
		if currentAuthority != SchedulingAuthorityExternalProvider {
			return nil, ErrAvailabilityQuoteStale
		}
		if attempt.AvailabilityQuoteID != "" {
			var quoteVersion sql.NullInt64
			var provenance string
			if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority_version, authority_fence_provenance FROM availability_quotes WHERE id::text=$1 AND salon_id::text=$2`, attempt.AvailabilityQuoteID, attempt.SalonID).Scan(&quoteVersion, &provenance); err != nil || provenance != "known" || !quoteVersion.Valid || quoteVersion.Int64 != currentVersion {
				return nil, ErrAvailabilityQuoteStale
			}
		}
	}
	if attempt.AvailabilityQuoteID != "" {
		quoteFence, err := loadAvailabilityQuoteProviderFenceTx(ctx, tx, attempt)
		if err != nil {
			return nil, err
		}
		if validProviderFence(attempt.ProviderFence) && !sameProviderFence(attempt.ProviderFence, quoteFence) {
			return nil, ErrAvailabilityQuoteStale
		}
		attempt.ProviderFence = quoteFence
	}
	if attempt.OperationType == BookingActionReschedule || attempt.OperationType == BookingActionCancel {
		if err := validateAppointmentActionProviderFenceTx(ctx, tx, attempt); err != nil {
			return nil, err
		}
	}

	if attempt.RetryOfAttemptID != "" {
		var retryLocation sql.NullString
		var retryGeneration sql.NullInt64
		if err := tx.QueryRowContext(ctx, `
			UPDATE booking_attempts
			SET superseded_at = now(), updated_at = now()
			WHERE id = $1
			  AND salon_id = $2
			  AND request_fingerprint = $3
			  AND status = $4
			  AND retry_policy = $5
			  AND COALESCE(operation_key, '') <> $6
			  AND superseded_at IS NULL
			RETURNING retry_sequence + 1, provider_location_id, provider_snapshot_generation
		`, attempt.RetryOfAttemptID, attempt.SalonID, attempt.RequestFingerprint, StatusFallbackPending, RetryPolicySafe, attempt.OperationKey).Scan(
			&attempt.RetrySequence,
			&retryLocation,
			&retryGeneration,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrOperationConflict
			}
			return nil, err
		}
		if !sameRetryProviderLocation(retryLocation, retryGeneration, attempt.ProviderFence) {
			return nil, ErrOperationConflict
		}
	}

	preexisting, err := scanBookingOperationAttempt(tx.QueryRowContext(ctx, bookingOperationSelectSQL+`
		WHERE ba.salon_id = $1
		  AND (
		      ba.operation_key = $2
		      OR (
		          ba.operation_type = $3
		          AND ba.request_fingerprint = $4
		          AND ba.superseded_at IS NULL
		      )
		  )
		ORDER BY CASE WHEN ba.operation_key = $2 THEN 0 ELSE 1 END, ba.created_at ASC
		LIMIT 1
		FOR UPDATE
	`, attempt.SalonID, attempt.OperationKey, attempt.OperationType, attempt.RequestFingerprint))
	if err == nil {
		if preexisting.RequestFingerprint != attempt.RequestFingerprint {
			return nil, ErrOperationConflict
		}
		if attempt.RetryOfAttemptID != "" {
			return nil, ErrOperationConflict
		}
		attempt = *preexisting
		acquired := false
		if attempt.SupersededAt == nil && attempt.Status == StatusPOSPending && attempt.ProviderOutcome == ProviderOutcomeNotStarted && (attempt.ProcessingLeaseEnds == nil || attempt.ProcessingLeaseEnds.Before(time.Now().UTC())) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE booking_attempts
				SET processing_token = $1, processing_lease_expires_at = $2, updated_at = now()
				WHERE id = $3 AND provider_outcome = $4 AND status = $5 AND superseded_at IS NULL
			`, requestedProcessingToken, leaseExpiresAt, attempt.ID, ProviderOutcomeNotStarted, StatusPOSPending); err != nil {
				return nil, err
			}
			attempt.ProcessingToken = requestedProcessingToken
			value := leaseExpiresAt
			attempt.ProcessingLeaseEnds = &value
			acquired = true
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		if err := r.hydrateClaimAttempt(ctx, &attempt); err != nil {
			return nil, err
		}
		return &BookingOperationClaim{Attempt: &attempt, Acquired: acquired}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	inserted := false
	var lease sql.NullTime
	err = tx.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_idempotency_key,
			operation_key, request_fingerprint, retry_of_attempt_id, retry_sequence,
			availability_quote_id, availability_slot_fingerprint, operation_type, target_appointment_id,
			target_pos_booking_version, processing_token, processing_lease_expires_at, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time, notes,
			provider_location_id, provider_snapshot_generation,
			scheduling_authority, authority_provider, authority_appointment_id,
			authority_appointment_version, target_authority_appointment_version,
			authority_idempotency_key, authority_location_id, authority_snapshot_generation
		)
		VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''),
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, '')::uuid, $10,
			NULLIF($11, '')::uuid, NULLIF($12, ''), $13, NULLIF($14, '')::uuid,
			$15, NULLIF($16, ''), $17, $18, $19, $20,
			$21, $22, NULLIF($23, ''), NULLIF($24, '')::uuid, NULLIF($25, '')::uuid, $26,
			$27, $28, NULLIF($29, ''),
			NULLIF($30, ''), NULLIF($31, 0),
			$32, $4, NULLIF($5, ''), NULL, $15, NULLIF($6, ''), NULLIF($30, ''), NULLIF($31, 0)
		)
		ON CONFLICT DO NOTHING
		RETURNING id::text, created_at, updated_at, processing_lease_expires_at,
		          scheduling_authority, authority_provider, COALESCE(authority_appointment_id, ''),
		          COALESCE(authority_appointment_version, 0), COALESCE(target_authority_appointment_version, 0),
		          COALESCE(authority_idempotency_key, ''), COALESCE(authority_location_id, ''),
		          COALESCE(authority_snapshot_generation, 0)
	`, attempt.SalonID, attempt.Source, attempt.Status, attempt.POSProvider, attempt.POSBookingID, attempt.POSIdempotencyKey,
		attempt.OperationKey, attempt.RequestFingerprint, attempt.RetryOfAttemptID, attempt.RetrySequence,
		attempt.AvailabilityQuoteID, attempt.SlotFingerprint, attempt.OperationType, attempt.TargetAppointmentID,
		attempt.TargetPOSBookingVersion, attempt.ProcessingToken, leaseExpiresAt, attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation,
		attempt.CustomerName, attempt.CustomerPhone, attempt.CustomerEmail, attempt.ServiceID, attempt.StaffID, attempt.StaffSelectionMode,
		attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes,
		attempt.ProviderFence.LocationID, attempt.ProviderFence.SnapshotGeneration,
		SchedulingAuthorityExternalProvider,
	).Scan(
		&attempt.ID, &attempt.CreatedAt, &attempt.UpdatedAt, &lease,
		&attempt.SchedulingAuthority, &attempt.AuthorityProvider, &attempt.AuthorityAppointmentID,
		&attempt.AuthorityAppointmentVersion, &attempt.TargetAuthorityAppointmentVersion,
		&attempt.AuthorityIdempotencyKey, &attempt.AuthorityLocationID, &attempt.AuthoritySnapshotGeneration,
	)
	if err == nil {
		inserted = true
		if lease.Valid {
			value := lease.Time
			attempt.ProcessingLeaseEnds = &value
		}
		if err := insertBookingAttemptSegments(ctx, tx, attempt.SalonID, attempt.ID, segments); err != nil {
			return nil, err
		}
		if attempt.RetryOfAttemptID != "" {
			result, err := tx.ExecContext(ctx, `
				UPDATE booking_attempts
				SET superseded_by_attempt_id = $1, updated_at = now()
				WHERE id = $2
				  AND salon_id = $3
				  AND superseded_at IS NOT NULL
				  AND superseded_by_attempt_id IS NULL
			`, attempt.ID, attempt.RetryOfAttemptID, attempt.SalonID)
			if err != nil {
				return nil, err
			}
			rows, err := result.RowsAffected()
			if err != nil || rows != 1 {
				return nil, ErrOperationConflict
			}
			if err := closeSupersededReconciliationTaskTx(ctx, tx, attempt.SalonID, attempt.RetryOfAttemptID, attempt.ID); err != nil {
				return nil, err
			}
		}
		if attempt.AvailabilityQuoteID != "" {
			var consumedQuoteID string
			err := tx.QueryRowContext(ctx, `
				UPDATE availability_quotes quote
				SET consumed_at = COALESCE(consumed_at, now()), consumed_by_attempt_id = $1
				WHERE quote.id = $2
				  AND quote.salon_id = $3
				  AND quote.provider = $4
				  AND quote.provider_location_id = $9
				  AND quote.provider_snapshot_generation = $10
				  AND (
				      quote.consumed_at IS NULL
				      OR quote.consumed_by_attempt_id = NULLIF($8, '')::uuid
				  )
				  AND quote.expires_at > now()
				  AND (
				      ($11 = 'book' AND $12 = '' AND quote.authority_fence_provenance = 'known'
				       AND quote.scheduling_authority_version IS NOT NULL AND quote.retry_of_attempt_id IS NULL)
				      OR ($11 = 'book' AND $12 <> '' AND quote.authority_fence_provenance = 'retry_origin'
				          AND quote.scheduling_authority_version IS NULL AND quote.retry_of_attempt_id::text = $12)
				      OR ($11 = 'reschedule' AND quote.authority_fence_provenance = 'target_origin'
				          AND quote.retry_of_attempt_id IS NULL)
				  )
				  AND EXISTS (
				      SELECT 1
				      FROM pos_connections connection
				      WHERE connection.salon_id = quote.salon_id
				        AND connection.provider = quote.provider
				        AND connection.status = 'active'
				        AND connection.last_sync_at IS NOT NULL
				        AND connection.location_id = quote.provider_location_id
				        AND connection.snapshot_generation = quote.provider_snapshot_generation
				  )
				  AND EXISTS (
				      SELECT 1
				      FROM availability_quote_slots slot
				      WHERE slot.quote_id = quote.id
				        AND slot.slot_fingerprint = $5
				        AND slot.start_time = $6
				        AND slot.end_time = $7
				        AND (
				            SELECT COALESCE(jsonb_agg(
				                jsonb_build_array(
				                    COALESCE(segment.service_id::text, ''),
				                    COALESCE(segment.staff_id::text, ''),
				                    segment.staff_selection_mode,
				                    segment.duration_minutes
				                ) ORDER BY segment.sort_order, segment.id
				            ), '[]'::jsonb)
				            FROM booking_attempt_segments segment
				            WHERE segment.booking_attempt_id = $1
				        ) = (
				            SELECT COALESCE(jsonb_agg(
				                jsonb_build_array(
				                    COALESCE(quoted_segment.value->>'service_id', ''),
				                    COALESCE(quoted_segment.value->>'staff_id', ''),
				                    COALESCE(quoted_segment.value->>'staff_selection_mode', 'specific'),
				                    COALESCE((quoted_segment.value->>'duration_minutes')::integer, 0)
				                ) ORDER BY quoted_segment.ordinality
				            ), '[]'::jsonb)
				            FROM jsonb_array_elements(slot.segments) WITH ORDINALITY AS quoted_segment(value, ordinality)
				        )
				  )
				RETURNING quote.id::text
			`, attempt.ID, attempt.AvailabilityQuoteID, attempt.SalonID, attempt.POSProvider, attempt.SlotFingerprint, attempt.RequestedStartTime.UTC(), attempt.RequestedEndTime.UTC(), attempt.RetryOfAttemptID, attempt.ProviderFence.LocationID, attempt.ProviderFence.SnapshotGeneration, attempt.OperationType, attempt.RetryOfAttemptID).Scan(&consumedQuoteID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrAvailabilityQuoteStale
			}
			if err != nil {
				return nil, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if !inserted {
		isRetry := attempt.RetryOfAttemptID != ""
		row := tx.QueryRowContext(ctx, bookingOperationSelectSQL+`
			WHERE ba.salon_id = $1
			  AND (
			      ba.operation_key = $2
			      OR (
			          ba.request_fingerprint = $3
			          AND ba.superseded_at IS NULL
			          AND ba.status IN ('pos_pending', 'provider_pending', 'confirmed', 'rescheduled', 'cancelled', 'fallback_pending')
			      )
			  )
			ORDER BY CASE WHEN ba.operation_key = $2 THEN 0 ELSE 1 END, ba.created_at ASC
			LIMIT 1
			FOR UPDATE
		`, attempt.SalonID, attempt.OperationKey, attempt.RequestFingerprint)
		existing, err := scanBookingOperationAttempt(row)
		if err != nil {
			return nil, err
		}
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return nil, ErrOperationConflict
		}
		if isRetry {
			return nil, ErrOperationConflict
		}
		attempt = *existing
		if attempt.SupersededAt == nil && attempt.Status == StatusPOSPending && attempt.ProviderOutcome == ProviderOutcomeNotStarted && (attempt.ProcessingLeaseEnds == nil || attempt.ProcessingLeaseEnds.Before(time.Now().UTC())) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE booking_attempts
				SET processing_token = $1, processing_lease_expires_at = $2, updated_at = now()
				WHERE id = $3 AND provider_outcome = $4 AND status = $5 AND superseded_at IS NULL
			`, requestedProcessingToken, leaseExpiresAt, attempt.ID, ProviderOutcomeNotStarted, StatusPOSPending); err != nil {
				return nil, err
			}
			attempt.ProcessingToken = requestedProcessingToken
			value := leaseExpiresAt
			attempt.ProcessingLeaseEnds = &value
			inserted = true
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := r.hydrateClaimAttempt(ctx, &attempt); err != nil {
		return nil, err
	}
	return &BookingOperationClaim{Attempt: &attempt, Acquired: inserted}, nil
}

func validateAppointmentActionProviderFenceTx(ctx context.Context, tx *sql.Tx, attempt BookingAttempt) error {
	if strings.TrimSpace(attempt.TargetAppointmentID) == "" || strings.TrimSpace(attempt.POSProvider) == "" || !validProviderFence(attempt.ProviderFence) {
		return pos.ErrStaleProviderFence
	}
	var appointmentID string
	err := tx.QueryRowContext(ctx, `
		SELECT appointment.id::text
		FROM appointments appointment
		JOIN booking_attempts origin
		  ON origin.id = appointment.booking_attempt_id
		 AND origin.salon_id = appointment.salon_id
		 AND origin.pos_provider = appointment.pos_provider
		 AND origin.operation_type = 'book'
		JOIN salons salon ON salon.id = appointment.salon_id
		JOIN pos_connections connection
		  ON connection.salon_id = appointment.salon_id
		 AND connection.provider = appointment.pos_provider
		WHERE appointment.id = $1
		  AND appointment.salon_id = $2
		  AND appointment.pos_provider = $3
		  AND COALESCE(appointment.pos_appointment_version, 0) = $4
		  AND salon.active_pos_provider = $3
		  AND origin.provider_location_id = $5
		  AND origin.provider_snapshot_generation IS NOT NULL
		  AND origin.provider_snapshot_generation > 0
		  AND connection.status = 'active'
		  AND connection.last_sync_at IS NOT NULL
		  AND connection.location_id = $5
		  AND connection.snapshot_generation = $6
		  AND connection.snapshot_generation >= origin.provider_snapshot_generation
		  AND EXISTS (
		      SELECT 1
		      FROM appointment_services segment
		      WHERE segment.appointment_id = appointment.id
		  )
		  AND NOT EXISTS (
		      SELECT 1
		      FROM appointment_services segment
		      LEFT JOIN services service
		        ON service.id = segment.service_id
		       AND service.salon_id = appointment.salon_id
		      LEFT JOIN pos_entity_links service_link
		        ON service_link.salon_id = appointment.salon_id
		       AND service_link.entity_type = 'service'
		       AND service_link.entity_id = segment.service_id
		       AND service_link.provider = appointment.pos_provider
		       AND service_link.sync_status = 'synced'
		      LEFT JOIN staff staff_member
		        ON staff_member.id = segment.staff_id
		       AND staff_member.salon_id = appointment.salon_id
		      LEFT JOIN pos_entity_links staff_link
		        ON staff_link.salon_id = appointment.salon_id
		       AND staff_link.entity_type = 'staff'
		       AND staff_link.entity_id = segment.staff_id
		       AND staff_link.provider = appointment.pos_provider
		       AND staff_link.sync_status = 'synced'
		      WHERE segment.appointment_id = appointment.id
		        AND (
		            segment.service_id IS NULL
		            OR service.id IS NULL
		            OR NULLIF(BTRIM(COALESCE(segment.pos_service_id, '')), '') IS NULL
		            OR COALESCE(segment.pos_service_version, 0) <= 0
		            OR service_link.provider_entity_id IS DISTINCT FROM segment.pos_service_id
		            OR COALESCE(service_link.provider_version, service.pos_service_version, 0) IS DISTINCT FROM segment.pos_service_version
		            OR segment.staff_id IS NULL
		            OR staff_member.id IS NULL
		            OR NULLIF(BTRIM(COALESCE(segment.pos_staff_id, '')), '') IS NULL
		            OR staff_link.provider_entity_id IS DISTINCT FROM segment.pos_staff_id
		            OR COALESCE(segment.duration_minutes, 0) <= 0
		        )
		  )
		FOR SHARE OF appointment, origin, salon, connection
	`, attempt.TargetAppointmentID, attempt.SalonID, attempt.POSProvider, attempt.TargetPOSBookingVersion, attempt.ProviderFence.LocationID, attempt.ProviderFence.SnapshotGeneration).Scan(&appointmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return pos.ErrStaleProviderFence
	}
	return err
}

func (r *Repository) GetBookingOperation(ctx context.Context, salonID string, ownerUserID string, operationKey string) (*BookingAttempt, error) {
	attempt, err := scanBookingOperationAttempt(r.db.QueryRowContext(ctx, bookingOperationSelectSQL+`
		WHERE ba.salon_id = $1
		  AND ba.operation_key = $2
		  AND EXISTS (
		      SELECT 1
		      FROM salons salon
		      WHERE salon.id = ba.salon_id
		        AND salon.owner_user_id = $3
		  )
		LIMIT 1
	`, strings.TrimSpace(salonID), strings.TrimSpace(operationKey), strings.TrimSpace(ownerUserID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.hydrateClaimAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (r *Repository) GetSafeRetryAvailabilityOrigin(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*BookingAttempt, error) {
	attempt, err := scanBookingOperationAttempt(r.db.QueryRowContext(ctx, bookingOperationSelectSQL+`
		WHERE ba.salon_id = $1
		  AND ba.id::text = $2
		  AND ba.scheduling_authority = 'external_provider'
		  AND COALESCE(ba.operation_type, 'book') = 'book'
		  AND ba.status = 'fallback_pending'
		  AND ba.retry_policy = 'safe'
		  AND ba.superseded_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM salons salon
		      WHERE salon.id = ba.salon_id AND salon.owner_user_id = $3
		  )
		LIMIT 1
	`, strings.TrimSpace(salonID), strings.TrimSpace(attemptID), strings.TrimSpace(ownerUserID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOperationConflict
	}
	if err != nil {
		return nil, err
	}
	if err := r.hydrateClaimAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}

const bookingOperationSelectSQL = `
	SELECT ba.id::text, ba.salon_id::text, ba.source, ba.status, COALESCE(ba.pos_provider, ''),
	       COALESCE(ba.pos_booking_id, ''), COALESCE(ba.pos_booking_version, 0), COALESCE(ba.pos_idempotency_key, ''),
	       ba.scheduling_authority, COALESCE(ba.authority_provider, ''), COALESCE(ba.authority_appointment_id, ''),
	       COALESCE(ba.authority_appointment_version, 0), COALESCE(ba.target_authority_appointment_version, 0),
	       COALESCE(ba.authority_idempotency_key, ''), COALESCE(ba.authority_location_id, ''),
	       COALESCE(ba.authority_snapshot_generation, 0),
	       COALESCE(ba.operation_key, ''), COALESCE(ba.request_fingerprint, ''),
	       COALESCE(ba.retry_of_attempt_id::text, ''), COALESCE(ba.superseded_by_attempt_id::text, ''), COALESCE(ba.retry_sequence, 0), ba.superseded_at,
	       COALESCE(ba.availability_quote_id::text, ''), COALESCE(ba.availability_slot_fingerprint, ''),
	       COALESCE(ba.provider_location_id, ''), COALESCE(ba.provider_snapshot_generation, 0),
	       COALESCE(ba.operation_type, 'book'),
	       COALESCE(ba.target_appointment_id::text, ''), COALESCE(ba.target_pos_booking_version, 0),
	       COALESCE(ba.processing_token, ''), ba.processing_lease_expires_at,
	       COALESCE(ba.provider_outcome, 'not_started'), COALESCE(ba.retry_policy, 'none'), COALESCE(ba.reconciliation_status, 'not_required'),
	       COALESCE(ba.reconciliation_resolution, ''), ba.reconciliation_resolved_at,
	       ba.customer_name, ba.customer_phone, COALESCE(ba.customer_email, ''),
	       COALESCE(ba.service_id::text, ''), COALESCE(ba.staff_id::text, ''), COALESCE(ba.staff_selection_mode, 'specific'),
	       ba.requested_start_time, ba.requested_end_time, COALESCE(ba.notes, ''),
	       COALESCE(ba.error_code, ''), COALESCE(ba.error_message, ''), ba.created_at, ba.updated_at
	FROM booking_attempts ba
`

type bookingAttemptScanner interface {
	Scan(dest ...any) error
}

func scanBookingOperationAttempt(row bookingAttemptScanner) (*BookingAttempt, error) {
	var attempt BookingAttempt
	var lease sql.NullTime
	var supersededAt sql.NullTime
	var reconciliationResolvedAt sql.NullTime
	if err := row.Scan(
		&attempt.ID, &attempt.SalonID, &attempt.Source, &attempt.Status, &attempt.POSProvider,
		&attempt.POSBookingID, &attempt.POSBookingVersion, &attempt.POSIdempotencyKey,
		&attempt.SchedulingAuthority, &attempt.AuthorityProvider, &attempt.AuthorityAppointmentID,
		&attempt.AuthorityAppointmentVersion, &attempt.TargetAuthorityAppointmentVersion,
		&attempt.AuthorityIdempotencyKey, &attempt.AuthorityLocationID, &attempt.AuthoritySnapshotGeneration,
		&attempt.OperationKey, &attempt.RequestFingerprint,
		&attempt.RetryOfAttemptID, &attempt.SupersededByAttemptID, &attempt.RetrySequence, &supersededAt, &attempt.AvailabilityQuoteID, &attempt.SlotFingerprint,
		&attempt.ProviderFence.LocationID, &attempt.ProviderFence.SnapshotGeneration, &attempt.OperationType,
		&attempt.TargetAppointmentID, &attempt.TargetPOSBookingVersion, &attempt.ProcessingToken, &lease, &attempt.ProviderOutcome, &attempt.RetryPolicy, &attempt.Reconciliation,
		&attempt.ReconciliationResolution, &reconciliationResolvedAt,
		&attempt.CustomerName, &attempt.CustomerPhone, &attempt.CustomerEmail, &attempt.ServiceID, &attempt.StaffID, &attempt.StaffSelectionMode,
		&attempt.RequestedStartTime, &attempt.RequestedEndTime, &attempt.Notes, &attempt.ErrorCode, &attempt.ErrorMessage,
		&attempt.CreatedAt, &attempt.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if lease.Valid {
		value := lease.Time
		attempt.ProcessingLeaseEnds = &value
	}
	if supersededAt.Valid {
		value := supersededAt.Time
		attempt.SupersededAt = &value
	}
	if reconciliationResolvedAt.Valid {
		value := reconciliationResolvedAt.Time
		attempt.ReconciliationResolvedAt = &value
	}
	annotateBookingAttemptPolicy(&attempt)
	return &attempt, nil
}

func (r *Repository) hydrateClaimAttempt(ctx context.Context, attempt *BookingAttempt) error {
	if attempt == nil {
		return nil
	}
	items := []BookingAttempt{*attempt}
	if err := r.attachBookingAttemptSegments(ctx, items); err != nil {
		return err
	}
	*attempt = items[0]

	appointment, err := scanAppointment(r.db.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
		       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
		       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
		       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
		       COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
		       COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'),
		       start_time, end_time, COALESCE(notes, ''), created_at, updated_at
		FROM appointments
		WHERE salon_id = $1
		  AND (id = NULLIF($2, '')::uuid OR booking_attempt_id = $3)
		ORDER BY CASE WHEN id = NULLIF($2, '')::uuid THEN 0 ELSE 1 END
		LIMIT 1
	`, attempt.SalonID, attempt.TargetAppointmentID, attempt.ID))
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pos.ErrNotFound) {
		return r.annotateBookingAttemptPoliciesCurrent(ctx, attempt)
	}
	if err != nil {
		return err
	}
	appointmentItems := []Appointment{*appointment}
	if err := r.attachAppointmentSegments(ctx, appointmentItems); err != nil {
		return err
	}
	attempt.Appointment = &appointmentItems[0]
	return r.annotateBookingAttemptPoliciesCurrent(ctx, attempt)
}

func annotateBookingAttemptPolicy(attempt *BookingAttempt) {
	if attempt == nil {
		return
	}
	attempt.CanRetry = false
	attempt.RetryBlockedReason = ""
	if attempt.RetryPolicy == RetryPolicyBlocked {
		attempt.RetryBlockedReason = "The POS result may be incomplete or unknown. Reconcile with the provider before retrying."
		return
	}
	if attempt.SupersededAt != nil {
		attempt.RetryBlockedReason = "This request was superseded by a newer retry."
		return
	}
	if attempt.Status != StatusFallbackPending || attempt.RetryPolicy != RetryPolicySafe {
		return
	}
	// Unit-test stores and other in-memory implementations do not own current POS
	// state. Repository responses always set this flag after an authoritative DB
	// check; an unchecked value preserves the Store interface's pure annotation.
	if !attempt.retryPrerequisitesChecked {
		attempt.CanRetry = true
		return
	}
	if !attempt.retryPrerequisitesCurrent {
		attempt.RetryBlockedReason = attempt.retryPrerequisitesReason
		if attempt.RetryBlockedReason == "" {
			attempt.RetryBlockedReason = "The request is no longer safe to retry. Refresh the booking data first."
		}
		return
	}
	attempt.CanRetry = true
}

func (r *Repository) annotateBookingAttemptPoliciesCurrent(ctx context.Context, attempts ...*BookingAttempt) error {
	ids := make([]string, 0, len(attempts))
	byID := make(map[string]*BookingAttempt, len(attempts))
	for _, attempt := range attempts {
		if attempt == nil || strings.TrimSpace(attempt.ID) == "" {
			continue
		}
		ids = append(ids, attempt.ID)
		byID[attempt.ID] = attempt
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT ba.id::text, ba.status, COALESCE(ba.retry_policy, 'none'),
		       COALESCE(ba.operation_type, 'book'), ba.superseded_at,
		       COALESCE(ba.provider_location_id, ''), COALESCE(ba.provider_snapshot_generation, 0),
		       COALESCE(ba.target_appointment_id::text, ''), COALESCE(ba.target_pos_booking_version, 0),
		       ba.provider_location_id IS NOT NULL
		           AND NULLIF(BTRIM(ba.provider_location_id), '') IS NOT NULL
		           AND ba.provider_snapshot_generation IS NOT NULL
		           AND ba.provider_snapshot_generation > 0 AS has_provider_fence,
		       EXISTS (
		           SELECT 1
		           FROM salons salon
		           JOIN pos_connections connection
		             ON connection.salon_id = salon.id
		            AND connection.provider = ba.pos_provider
		            AND connection.status = 'active'
		            AND connection.last_sync_at IS NOT NULL
		            AND connection.snapshot_generation > 0
		            AND connection.snapshot_generation >= ba.provider_snapshot_generation
		            AND connection.location_id = ba.provider_location_id
		           WHERE salon.id = ba.salon_id
		             AND salon.active_pos_provider = ba.pos_provider
		       ) AS provider_location_current,
		       EXISTS (
		           SELECT 1
		           FROM booking_attempt_segments segment
		           WHERE segment.booking_attempt_id = ba.id
		       ) AND NOT EXISTS (
		           SELECT 1
		           FROM booking_attempt_segments segment
		           WHERE segment.booking_attempt_id = ba.id
		             AND (
		                 NULLIF(BTRIM(segment.pos_service_id), '') IS NULL
		                 OR NULLIF(BTRIM(COALESCE(segment.pos_staff_id, '')), '') IS NULL
		                 OR segment.service_id IS NULL
		                 OR segment.staff_id IS NULL
		                 OR NOT EXISTS (
		                     SELECT 1
		                     FROM services service
		                     JOIN pos_entity_links service_link
		                       ON service_link.salon_id = service.salon_id
		                      AND service_link.entity_type = 'service'
		                      AND service_link.entity_id = service.id
		                      AND service_link.provider = ba.pos_provider
		                      AND service_link.sync_status = 'synced'
		                      AND service_link.provider_entity_id = segment.pos_service_id
		                     WHERE service.id = segment.service_id
		                       AND service.salon_id = ba.salon_id
		                       AND service.pos_provider = ba.pos_provider
		                       AND service.active = true
		                       AND service.ai_bookable = true
		                       AND service.archived_at IS NULL
		                       AND service.sync_status = 'synced'
		                       AND COALESCE(service_link.provider_version, service.pos_service_version, 0) = COALESCE(segment.pos_service_version, 0)
		                 )
		                 OR NOT EXISTS (
		                     SELECT 1
		                     FROM staff
		                     JOIN pos_entity_links staff_link
		                       ON staff_link.salon_id = staff.salon_id
		                      AND staff_link.entity_type = 'staff'
		                      AND staff_link.entity_id = staff.id
		                      AND staff_link.provider = ba.pos_provider
		                      AND staff_link.sync_status = 'synced'
		                      AND staff_link.provider_entity_id = segment.pos_staff_id
		                     WHERE staff.id = segment.staff_id
		                       AND staff.salon_id = ba.salon_id
		                       AND staff.pos_provider = ba.pos_provider
		                       AND staff.active = true
		                       AND staff.ai_bookable = true
		                       AND staff.archived_at IS NULL
		                       AND staff.sync_status = 'synced'
		                 )
		             )
		       ) AS catalog_mapping_current,
		       CASE
		           WHEN COALESCE(ba.operation_type, 'book') = 'book' THEN true
		           ELSE EXISTS (
		               SELECT 1
		               FROM appointments target
		               WHERE target.id = ba.target_appointment_id
		                 AND target.salon_id = ba.salon_id
		                 AND target.pos_provider = ba.pos_provider
		                 AND target.pos_appointment_id = ba.pos_booking_id
		                 AND COALESCE(target.pos_appointment_version, 0) = COALESCE(ba.target_pos_booking_version, 0)
		                 AND target.status IN ('confirmed', 'rescheduled')
		           )
		       END AS target_current
		FROM booking_attempts ba
		WHERE ba.id = ANY($1::uuid[])
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	seen := make(map[string]struct{}, len(ids))
	for rows.Next() {
		var id string
		var status string
		var retryPolicy string
		var operationType string
		var supersededAt sql.NullTime
		var locationID string
		var generation int64
		var targetAppointmentID string
		var targetVersion int
		var hasFence bool
		var providerCurrent bool
		var catalogCurrent bool
		var targetCurrent bool
		if err := rows.Scan(
			&id, &status, &retryPolicy, &operationType, &supersededAt,
			&locationID, &generation, &targetAppointmentID, &targetVersion,
			&hasFence, &providerCurrent, &catalogCurrent, &targetCurrent,
		); err != nil {
			return err
		}
		attempt := byID[id]
		if attempt == nil {
			continue
		}
		seen[id] = struct{}{}
		attempt.Status = status
		attempt.RetryPolicy = retryPolicy
		attempt.OperationType = operationType
		attempt.ProviderFence = pos.ProviderFence{LocationID: locationID, SnapshotGeneration: generation}
		attempt.TargetAppointmentID = targetAppointmentID
		attempt.TargetPOSBookingVersion = targetVersion
		attempt.SupersededAt = nil
		if supersededAt.Valid {
			value := supersededAt.Time
			attempt.SupersededAt = &value
		}
		attempt.retryPrerequisitesChecked = true
		attempt.retryPrerequisitesCurrent = false
		attempt.retryPrerequisitesReason = ""
		switch operationType {
		case BookingActionBook, BookingActionReschedule:
			switch {
			case !hasFence:
				attempt.retryPrerequisitesReason = "This legacy request has no trusted provider catalog snapshot. Refresh availability before retrying."
			case !providerCurrent:
				attempt.retryPrerequisitesReason = "The active POS catalog location changed or is not synchronized. Refresh availability before retrying."
			case !catalogCurrent:
				attempt.retryPrerequisitesReason = "The requested service or staff mapping changed. Refresh availability before retrying."
			case operationType == BookingActionReschedule && !targetCurrent:
				attempt.retryPrerequisitesReason = "The target appointment changed since this request was created. Refresh it before retrying."
			default:
				attempt.retryPrerequisitesCurrent = true
			}
		case BookingActionCancel:
			if targetCurrent {
				attempt.retryPrerequisitesCurrent = true
			} else {
				attempt.retryPrerequisitesReason = "The target appointment changed since this request was created. Refresh it before retrying."
			}
		default:
			attempt.retryPrerequisitesReason = "This request type cannot be retried safely."
		}
		annotateBookingAttemptPolicy(attempt)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, attempt := range attempts {
		if attempt == nil {
			continue
		}
		if _, ok := seen[attempt.ID]; ok {
			continue
		}
		attempt.retryPrerequisitesChecked = true
		attempt.retryPrerequisitesCurrent = false
		attempt.retryPrerequisitesReason = "The booking request could not be verified for retry."
		annotateBookingAttemptPolicy(attempt)
	}
	return nil
}

func setBookingAttemptPOSVersionTx(ctx context.Context, tx *sql.Tx, attemptID string, posBookingID string, version int) error {
	if strings.TrimSpace(posBookingID) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET pos_booking_version = $1,
		    authority_appointment_version = $1,
		    updated_at = now()
		WHERE id = $2
	`, version, attemptID)
	return err
}

func normalizedConfirmedBookingSegments(record ConfirmedBookingRecord) []BookingSegmentRecord {
	segments := append([]BookingSegmentRecord(nil), record.Segments...)
	if len(segments) == 0 {
		segments = []BookingSegmentRecord{{
			Service:            record.Service,
			Staff:              record.Staff,
			StaffSelectionMode: record.StaffSelectionMode,
			SortOrder:          1,
		}}
	}
	for index := range segments {
		if segments[index].SortOrder <= 0 {
			segments[index].SortOrder = index + 1
		}
		if strings.TrimSpace(segments[index].StaffSelectionMode) == "" {
			segments[index].StaffSelectionMode = StaffSelectionSpecific
		}
	}
	return segments
}

func validConfirmedBookingRecord(record ConfirmedBookingRecord, segments []BookingSegmentRecord) bool {
	if strings.TrimSpace(record.AttemptID) == "" ||
		strings.TrimSpace(record.SalonID) == "" ||
		strings.TrimSpace(record.Provider) == "" ||
		strings.TrimSpace(record.POSBookingID) == "" ||
		record.POSBookingVersion < 0 ||
		strings.TrimSpace(record.ProcessingToken) == "" ||
		!validProviderFence(record.ProviderFence) ||
		record.StartTime.IsZero() ||
		!record.EndTime.After(record.StartTime) ||
		len(segments) == 0 {
		return false
	}
	primary := segments[0]
	if record.Service.ID != primary.Service.ID ||
		record.Service.POSServiceID != primary.Service.POSServiceID ||
		record.Service.POSServiceVersion != primary.Service.POSServiceVersion ||
		record.Staff.ID != primary.Staff.ID ||
		record.Staff.POSStaffID != primary.Staff.POSStaffID ||
		record.StaffSelectionMode != primary.StaffSelectionMode {
		return false
	}
	for index, segment := range segments {
		if segment.SortOrder != index+1 ||
			strings.TrimSpace(segment.Service.ID) == "" ||
			strings.TrimSpace(segment.Service.POSServiceID) == "" ||
			segment.Service.POSServiceVersion <= 0 ||
			strings.TrimSpace(segment.Staff.ID) == "" ||
			strings.TrimSpace(segment.Staff.POSStaffID) == "" ||
			segment.Service.DurationMinutes <= 0 {
			return false
		}
	}
	return true
}

func confirmedBookingMirrorMatches(snapshot calendarAppointmentSnapshot, record ConfirmedBookingRecord, segments []BookingSegmentRecord) bool {
	if snapshot.OriginSource != SourcePOSCalendarSync ||
		snapshot.OriginSuperseded ||
		snapshot.Provider != record.Provider ||
		strings.TrimSpace(snapshot.ProviderLocationID) == "" ||
		strings.TrimSpace(snapshot.ProviderLocationID) != strings.TrimSpace(record.ProviderFence.LocationID) ||
		snapshot.ProviderGeneration <= 0 ||
		snapshot.POSAppointmentID != strings.TrimSpace(record.POSBookingID) ||
		snapshot.POSAppointmentVersion != record.POSBookingVersion ||
		snapshot.POSCustomerID != strings.TrimSpace(record.POSCustomerID) ||
		snapshot.Status != StatusConfirmed ||
		!snapshot.StartTime.Equal(record.StartTime) ||
		!snapshot.EndTime.Equal(record.EndTime) ||
		len(snapshot.Segments) != len(segments) {
		return false
	}
	primary := segments[0]
	if snapshot.ServiceID != primary.Service.ID ||
		snapshot.StaffID != primary.Staff.ID ||
		snapshot.StaffSelectionMode != primary.StaffSelectionMode {
		return false
	}
	for index, persisted := range snapshot.Segments {
		incoming := segments[index]
		if persisted.ServiceID != incoming.Service.ID ||
			persisted.POSServiceID != strings.TrimSpace(incoming.Service.POSServiceID) ||
			persisted.POSServiceVersion != incoming.Service.POSServiceVersion ||
			persisted.StaffID != incoming.Staff.ID ||
			persisted.POSStaffID != strings.TrimSpace(incoming.Staff.POSStaffID) ||
			persisted.StaffSelectionMode != incoming.StaffSelectionMode ||
			persisted.DurationMinutes != incoming.Service.DurationMinutes ||
			persisted.SortOrder != incoming.SortOrder {
			return false
		}
	}
	return true
}

func appointmentFromCalendarSnapshot(snapshot calendarAppointmentSnapshot, canonicalAttemptID string) Appointment {
	return Appointment{
		ID:                          snapshot.AppointmentID,
		BookingAttemptID:            canonicalAttemptID,
		SchedulingAuthority:         snapshot.SchedulingAuthority,
		AuthorityProvider:           snapshot.AuthorityProvider,
		AuthorityAppointmentID:      snapshot.AuthorityAppointmentID,
		AuthorityAppointmentVersion: snapshot.AuthorityAppointmentVersion,
		AuthorityCustomerID:         snapshot.AuthorityCustomerID,
		POSProvider:                 snapshot.Provider,
		POSAppointmentID:            snapshot.POSAppointmentID,
		POSAppointmentVersion:       snapshot.POSAppointmentVersion,
		POSCustomerID:               snapshot.POSCustomerID,
		Status:                      snapshot.Status,
		CustomerName:                snapshot.CustomerName,
		CustomerPhone:               snapshot.CustomerPhone,
		CustomerEmail:               snapshot.CustomerEmail,
		ServiceID:                   snapshot.ServiceID,
		StaffID:                     snapshot.StaffID,
		StaffSelectionMode:          snapshot.StaffSelectionMode,
		StartTime:                   snapshot.StartTime,
		EndTime:                     snapshot.EndTime,
		Notes:                       snapshot.Notes,
		ConfirmedAt:                 snapshot.ConfirmedAt,
		ConfirmedByUserID:           snapshot.ConfirmedByUserID,
		ConfirmationSource:          snapshot.ConfirmationSource,
		CreatedAt:                   snapshot.CreatedAt,
		UpdatedAt:                   snapshot.UpdatedAt,
	}
}

func fallbackCreateMirrorMatches(snapshot calendarAppointmentSnapshot, record FallbackBookingRecord, fence pos.ProviderFence, segments []BookingSegmentRecord) bool {
	if snapshot.OriginSource != SourcePOSCalendarSync ||
		snapshot.OriginSuperseded ||
		snapshot.Provider != record.Provider ||
		strings.TrimSpace(snapshot.ProviderLocationID) != strings.TrimSpace(fence.LocationID) ||
		snapshot.ProviderGeneration <= 0 ||
		snapshot.POSAppointmentID != strings.TrimSpace(record.POSBookingID) ||
		(record.POSBookingVersion > 0 && snapshot.POSAppointmentVersion < record.POSBookingVersion) ||
		(snapshot.Status != StatusConfirmed && snapshot.Status != StatusRescheduled) ||
		!snapshot.StartTime.Equal(record.StartTime) ||
		!snapshot.EndTime.Equal(record.EndTime) ||
		len(snapshot.Segments) != len(segments) ||
		len(segments) == 0 {
		return false
	}
	primary := segments[0]
	if snapshot.ServiceID != primary.Service.ID ||
		snapshot.StaffID != primary.Staff.ID ||
		snapshot.StaffSelectionMode != primary.StaffSelectionMode {
		return false
	}
	for index, persisted := range snapshot.Segments {
		incoming := segments[index]
		if persisted.ServiceID != incoming.Service.ID ||
			persisted.POSServiceID != strings.TrimSpace(incoming.Service.POSServiceID) ||
			persisted.POSServiceVersion != incoming.Service.POSServiceVersion ||
			persisted.StaffID != incoming.Staff.ID ||
			persisted.POSStaffID != strings.TrimSpace(incoming.Staff.POSStaffID) ||
			persisted.StaffSelectionMode != incoming.StaffSelectionMode ||
			persisted.DurationMinutes != incoming.Service.DurationMinutes ||
			persisted.SortOrder != incoming.SortOrder {
			return false
		}
	}
	return true
}

func canonicalizeCalendarMirrorTx(ctx context.Context, tx *sql.Tx, salonID string, canonicalAttemptID string, mirror calendarAppointmentSnapshot) (Appointment, error) {
	appointment := appointmentFromCalendarSnapshot(mirror, canonicalAttemptID)
	appointment.SalonID = salonID
	if mirror.SchedulingAuthority != SchedulingAuthorityExternalProvider {
		return Appointment{}, ErrOperationConflict
	}
	if mirror.BookingAttemptID != canonicalAttemptID {
		if mirror.OriginSource != SourcePOSCalendarSync || mirror.OriginSuperseded {
			return Appointment{}, ErrOperationConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_attempts
			SET superseded_by_attempt_id = $1,
			    superseded_at = COALESCE(superseded_at, now()),
			    updated_at = now()
			WHERE id = $2
			  AND salon_id = $3
			  AND scheduling_authority = 'external_provider'
			  AND source = $4
			  AND superseded_at IS NULL
		`, canonicalAttemptID, mirror.BookingAttemptID, salonID, SourcePOSCalendarSync)
		if err := requireExactlyOneRow(result, err); err != nil {
			return Appointment{}, err
		}
		if err := closeSupersededReconciliationTaskTx(ctx, tx, salonID, mirror.BookingAttemptID, canonicalAttemptID); err != nil {
			return Appointment{}, err
		}
	}
	var confirmedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		UPDATE appointments
		SET booking_attempt_id = $1,
		    confirmed_at = COALESCE(confirmed_at, now()),
		    confirmation_source = COALESCE(confirmation_source, $5),
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
		  AND booking_attempt_id = $4
		  AND scheduling_authority = 'external_provider'
		  AND pos_sync_status = 'synced'
		  AND EXISTS (
		      SELECT 1
		      FROM booking_attempts canonical
		      WHERE canonical.id = $1
		        AND canonical.salon_id = $3
		        AND canonical.scheduling_authority = 'external_provider'
		  )
		RETURNING confirmed_at, confirmation_source, updated_at
	`, canonicalAttemptID, mirror.AppointmentID, salonID, mirror.BookingAttemptID, SchedulingAuthorityExternalProvider).Scan(
		&confirmedAt,
		&appointment.ConfirmationSource,
		&appointment.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Appointment{}, ErrOperationConflict
		}
		return Appointment{}, err
	}
	appointment.ConfirmedAt = &confirmedAt
	return appointment, nil
}

func (r *Repository) SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error) {
	segments := normalizedConfirmedBookingSegments(record)
	if strings.TrimSpace(record.StaffSelectionMode) == "" {
		record.StaffSelectionMode = StaffSelectionSpecific
	}
	if !validConfirmedBookingRecord(record, segments) {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.SalonID); err != nil {
		return nil, err
	}

	attempt := BookingAttempt{
		ID:                          record.AttemptID,
		SalonID:                     record.SalonID,
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityProvider:           record.Provider,
		AuthorityAppointmentID:      record.POSBookingID,
		AuthorityAppointmentVersion: record.POSBookingVersion,
		AuthorityLocationID:         record.ProviderFence.LocationID,
		AuthoritySnapshotGeneration: record.ProviderFence.SnapshotGeneration,
		Source:                      record.Source,
		Status:                      StatusConfirmed,
		POSProvider:                 record.Provider,
		POSBookingID:                record.POSBookingID,
		POSBookingVersion:           record.POSBookingVersion,
		CustomerName:                record.CustomerName,
		CustomerPhone:               record.CustomerPhone,
		CustomerEmail:               record.CustomerEmail,
		ServiceID:                   record.Service.ID,
		StaffID:                     record.Staff.ID,
		StaffSelectionMode:          record.StaffSelectionMode,
		RequestedStartTime:          record.StartTime,
		RequestedEndTime:            record.EndTime,
		Notes:                       record.Notes,
		OperationType:               BookingActionBook,
		ProviderOutcome:             ProviderOutcomeSucceeded,
		RetryPolicy:                 RetryPolicyNone,
		Reconciliation:              ReconciliationNotRequired,
		ProviderFence:               record.ProviderFence,
	}

	var mirror *calendarAppointmentSnapshot
	var mirrorAppointmentID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM appointments
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND pos_appointment_id = $3
		FOR UPDATE
	`, record.SalonID, record.Provider, record.POSBookingID).Scan(&mirrorAppointmentID)
	if err == nil {
		snapshot, loadErr := loadCalendarAppointmentSnapshotTx(ctx, tx, record.SalonID, mirrorAppointmentID, record.Provider, record.POSBookingID)
		if loadErr != nil {
			return nil, loadErr
		}
		mirror = &snapshot
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT created_at
		FROM booking_attempts
		WHERE id = $1
		  AND salon_id = $2
		  AND pos_provider = $3
		  AND operation_type = $4
		  AND status = $5
		  AND processing_token = $6
		  AND provider_location_id = $7
		  AND provider_snapshot_generation = $8
		  AND superseded_at IS NULL
		FOR UPDATE
	`, attempt.ID, attempt.SalonID, attempt.POSProvider, BookingActionBook, StatusPOSPending,
		record.ProcessingToken, record.ProviderFence.LocationID, record.ProviderFence.SnapshotGeneration).Scan(&attempt.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	if mirror != nil && !confirmedBookingMirrorMatches(*mirror, record, segments) {
		attempt.Status = StatusFallbackPending
		attempt.ProviderOutcome = ProviderOutcomeSucceeded
		attempt.RetryPolicy = RetryPolicyBlocked
		attempt.Reconciliation = ReconciliationRequired
		attempt.ErrorCode = pos.ErrorBookingConflict
		attempt.ErrorMessage = "POS returned a booking ID, but the synchronized provider appointment did not exactly match the accepted booking response."
		if err := tx.QueryRowContext(ctx, `
			UPDATE booking_attempts
			SET status = $1,
			    pos_booking_id = $2,
			    pos_booking_version = $3,
			    authority_appointment_id = $2,
			    authority_appointment_version = $3,
			    staff_selection_mode = $4,
			    requested_start_time = $5,
			    requested_end_time = $6,
			    notes = NULLIF($7, ''),
			    error_code = $8,
			    error_message = $9,
			    provider_outcome = $10,
			    retry_policy = $11,
			    reconciliation_status = $12,
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    updated_at = now()
			WHERE id = $13
			  AND salon_id = $14
			  AND status = $15
			  AND processing_token = $16
			  AND superseded_at IS NULL
			RETURNING updated_at
		`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
			attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ErrorCode, attempt.ErrorMessage,
			attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation, attempt.ID, attempt.SalonID,
			StatusPOSPending, record.ProcessingToken).Scan(&attempt.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, pos.ErrNotFound
			}
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
			VALUES ($1, $2, $3, $4, $5)
		`, record.SalonID, record.Provider, "create_booking", attempt.ErrorCode, attempt.ErrorMessage); err != nil {
			return nil, err
		}
		payload := ownerNotificationPayload(NotificationTypeBookingFallback, record.SalonID, attempt.ID, mirror.AppointmentID, map[string]any{
			"booking_status":        attempt.Status,
			"pos_provider":          record.Provider,
			"pos_booking_id":        record.POSBookingID,
			"provider_outcome":      attempt.ProviderOutcome,
			"retry_policy":          attempt.RetryPolicy,
			"reconciliation_status": attempt.Reconciliation,
			"error_code":            attempt.ErrorCode,
		})
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
			VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8::jsonb)
			ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		`, record.SalonID, attempt.ID, mirror.AppointmentID, NotificationTypeBookingFallback,
			"Booking result needs POS reconciliation", attempt.ErrorMessage,
			"booking-result:"+attempt.ID, payload); err != nil {
			return nil, err
		}
		if err := ensureReconciliationTaskTx(ctx, tx, record.SalonID, attempt.ID, attempt.ErrorMessage); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		if err := r.annotateBookingAttemptPoliciesCurrent(ctx, &attempt); err != nil {
			return nil, err
		}
		return &attempt, nil
	}

	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = $2,
		    pos_booking_version = $3,
		    authority_appointment_id = $2,
		    authority_appointment_version = $3,
		    staff_selection_mode = $4,
		    requested_start_time = $5,
		    requested_end_time = $6,
		    notes = NULLIF($7, ''),
		    error_code = NULL,
		    error_message = NULL,
		    provider_outcome = $8,
		    retry_policy = $9,
		    reconciliation_status = $10,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    updated_at = now()
		WHERE id = $11
		  AND salon_id = $12
		  AND status = $13
		  AND processing_token = $14
		  AND provider_location_id = $15
		  AND provider_snapshot_generation = $16
		  AND superseded_at IS NULL
		RETURNING updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
		attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ProviderOutcome,
		attempt.RetryPolicy, attempt.Reconciliation, attempt.ID, attempt.SalonID, StatusPOSPending,
		record.ProcessingToken, record.ProviderFence.LocationID, record.ProviderFence.SnapshotGeneration,
	).Scan(&attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	var appointment Appointment
	if mirror == nil {
		appointment = Appointment{
			SalonID:                     record.SalonID,
			BookingAttemptID:            attempt.ID,
			SchedulingAuthority:         SchedulingAuthorityExternalProvider,
			AuthorityProvider:           record.Provider,
			AuthorityAppointmentID:      record.POSBookingID,
			AuthorityAppointmentVersion: record.POSBookingVersion,
			AuthorityCustomerID:         record.POSCustomerID,
			POSProvider:                 record.Provider,
			POSAppointmentID:            record.POSBookingID,
			POSAppointmentVersion:       record.POSBookingVersion,
			POSCustomerID:               record.POSCustomerID,
			Status:                      StatusConfirmed,
			CustomerName:                record.CustomerName,
			CustomerPhone:               record.CustomerPhone,
			CustomerEmail:               record.CustomerEmail,
			ServiceID:                   record.Service.ID,
			StaffID:                     record.Staff.ID,
			StaffSelectionMode:          attempt.StaffSelectionMode,
			StartTime:                   record.StartTime,
			EndTime:                     record.EndTime,
			Notes:                       record.Notes,
			ConfirmationSource:          SchedulingAuthorityExternalProvider,
		}
		var confirmedAt time.Time
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO appointments (
				salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version, pos_customer_id,
				status, customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode, start_time, end_time, notes,
				scheduling_authority, authority_provider, authority_appointment_id,
				authority_appointment_version, authority_customer_id, confirmed_at, confirmation_source
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, $15, NULLIF($16, ''),
			        $17, $3, $4, $5, NULLIF($6, ''), now(), $17)
			RETURNING id::text, confirmed_at, confirmation_source, created_at, updated_at
		`, appointment.SalonID, appointment.BookingAttemptID, appointment.POSProvider, appointment.POSAppointmentID,
			appointment.POSAppointmentVersion, appointment.POSCustomerID, appointment.Status, appointment.CustomerName,
			appointment.CustomerPhone, appointment.CustomerEmail, appointment.ServiceID, appointment.StaffID,
			appointment.StaffSelectionMode, appointment.StartTime, appointment.EndTime, appointment.Notes,
			SchedulingAuthorityExternalProvider,
		).Scan(&appointment.ID, &confirmedAt, &appointment.ConfirmationSource, &appointment.CreatedAt, &appointment.UpdatedAt); err != nil {
			return nil, err
		}
		appointment.ConfirmedAt = &confirmedAt
		if err := insertAppointmentServices(ctx, tx, appointment.SalonID, appointment.ID, segments); err != nil {
			return nil, err
		}
	} else {
		appointment, err = canonicalizeCalendarMirrorTx(ctx, tx, record.SalonID, attempt.ID, *mirror)
		if err != nil {
			return nil, err
		}
	}

	payload := ownerNotificationPayload(NotificationTypeBookingConfirmed, record.SalonID, attempt.ID, appointment.ID, map[string]any{
		"booking_status": StatusConfirmed,
		"pos_provider":   record.Provider,
		"pos_booking_id": record.POSBookingID,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8::jsonb)
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, record.SalonID, attempt.ID, appointment.ID, NotificationTypeBookingConfirmed, "New POS-confirmed appointment", confirmedNotificationMessage(record), "booking-confirmed:"+attempt.ID, payload); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Appointment = &appointment
	return &attempt, nil
}

func (r *Repository) SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error) {
	segments := append([]BookingSegmentRecord(nil), record.Segments...)
	if len(segments) == 0 {
		segments = []BookingSegmentRecord{{
			Service:            record.Service,
			Staff:              record.Staff,
			StaffSelectionMode: record.StaffSelectionMode,
			SortOrder:          1,
		}}
	}
	for index := range segments {
		if segments[index].SortOrder <= 0 {
			segments[index].SortOrder = index + 1
		}
		if strings.TrimSpace(segments[index].StaffSelectionMode) == "" {
			segments[index].StaffSelectionMode = StaffSelectionSpecific
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.SalonID); err != nil {
		return nil, err
	}

	var mirror *calendarAppointmentSnapshot
	if strings.TrimSpace(record.POSBookingID) != "" {
		var mirrorAppointmentID string
		err := tx.QueryRowContext(ctx, `
			SELECT id::text
			FROM appointments
			WHERE salon_id = $1
			  AND scheduling_authority = 'external_provider'
			  AND pos_provider = $2
			  AND pos_appointment_id = $3
			FOR UPDATE
		`, record.SalonID, record.Provider, record.POSBookingID).Scan(&mirrorAppointmentID)
		if err == nil {
			snapshot, loadErr := loadCalendarAppointmentSnapshotTx(ctx, tx, record.SalonID, mirrorAppointmentID, record.Provider, record.POSBookingID)
			if loadErr != nil {
				return nil, loadErr
			}
			mirror = &snapshot
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	status := strings.TrimSpace(record.Status)
	if status == "" {
		status = StatusFallbackPending
	}
	attempt := BookingAttempt{
		ID:                          record.AttemptID,
		SalonID:                     record.SalonID,
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityProvider:           record.Provider,
		AuthorityAppointmentID:      record.POSBookingID,
		AuthorityAppointmentVersion: record.POSBookingVersion,
		Source:                      record.Source,
		Status:                      status,
		POSProvider:                 record.Provider,
		POSBookingID:                record.POSBookingID,
		POSBookingVersion:           record.POSBookingVersion,
		CustomerName:                record.CustomerName,
		CustomerPhone:               record.CustomerPhone,
		CustomerEmail:               record.CustomerEmail,
		ServiceID:                   record.Service.ID,
		StaffID:                     record.Staff.ID,
		StaffSelectionMode:          record.StaffSelectionMode,
		RequestedStartTime:          record.StartTime,
		RequestedEndTime:            record.EndTime,
		Notes:                       record.Notes,
		ErrorCode:                   record.ErrorCode,
		ErrorMessage:                record.ErrorMessage,
		OperationType:               BookingActionBook,
		ProviderOutcome:             record.ProviderOutcome,
		RetryPolicy:                 record.RetryPolicy,
		Reconciliation:              record.Reconciliation,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT created_at, COALESCE(provider_location_id, ''), COALESCE(provider_snapshot_generation, 0)
		FROM booking_attempts
		WHERE id = $1
		  AND salon_id = $2
		  AND pos_provider = $3
		  AND operation_type = $4
		  AND status = $5
		  AND processing_token = $6
		  AND superseded_at IS NULL
		FOR UPDATE
	`, attempt.ID, attempt.SalonID, attempt.POSProvider, BookingActionBook, StatusPOSPending, record.ProcessingToken).Scan(
		&attempt.CreatedAt,
		&attempt.ProviderFence.LocationID,
		&attempt.ProviderFence.SnapshotGeneration,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	attempt.AuthorityLocationID = attempt.ProviderFence.LocationID
	attempt.AuthoritySnapshotGeneration = attempt.ProviderFence.SnapshotGeneration

	if mirror != nil && validProviderFence(attempt.ProviderFence) && fallbackCreateMirrorMatches(*mirror, record, attempt.ProviderFence, segments) {
		attempt.Status = StatusConfirmed
		attempt.POSBookingVersion = mirror.POSAppointmentVersion
		attempt.AuthorityAppointmentVersion = mirror.POSAppointmentVersion
		attempt.ProviderOutcome = ProviderOutcomeSucceeded
		attempt.RetryPolicy = RetryPolicyNone
		attempt.Reconciliation = ReconciliationNotRequired
		attempt.ErrorCode = ""
		attempt.ErrorMessage = ""
		if err := tx.QueryRowContext(ctx, `
			UPDATE booking_attempts
			SET status = $1,
			    pos_booking_id = $2,
			    pos_booking_version = $3,
			    authority_appointment_id = $2,
			    authority_appointment_version = $3,
			    staff_selection_mode = $4,
			    requested_start_time = $5,
			    requested_end_time = $6,
			    notes = NULLIF($7, ''),
			    error_code = NULL,
			    error_message = NULL,
			    provider_outcome = $8,
			    retry_policy = $9,
			    reconciliation_status = $10,
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    updated_at = now()
			WHERE id = $11
			  AND salon_id = $12
			  AND status = $13
			  AND processing_token = $14
			  AND superseded_at IS NULL
			RETURNING updated_at
		`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
			attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ProviderOutcome,
			attempt.RetryPolicy, attempt.Reconciliation, attempt.ID, attempt.SalonID, StatusPOSPending,
			record.ProcessingToken).Scan(&attempt.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, pos.ErrNotFound
			}
			return nil, err
		}
		appointment, err := canonicalizeCalendarMirrorTx(ctx, tx, record.SalonID, attempt.ID, *mirror)
		if err != nil {
			return nil, err
		}
		payload := ownerNotificationPayload(NotificationTypeBookingConfirmed, record.SalonID, attempt.ID, appointment.ID, map[string]any{
			"booking_status": StatusConfirmed,
			"pos_provider":   record.Provider,
			"pos_booking_id": record.POSBookingID,
		})
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
			VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8::jsonb)
			ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		`, record.SalonID, attempt.ID, appointment.ID, NotificationTypeBookingConfirmed,
			"New POS-confirmed appointment", "Provider calendar synchronization confirmed the appointment.",
			"booking-confirmed:"+attempt.ID, payload); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		attempt.Appointment = &appointment
		return &attempt, nil
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    pos_booking_version = CASE WHEN NULLIF($2, '') IS NULL THEN pos_booking_version ELSE $3 END,
		    authority_appointment_id = NULLIF($2, ''),
		    authority_appointment_version = CASE WHEN NULLIF($2, '') IS NULL THEN authority_appointment_version ELSE $3 END,
		    staff_selection_mode = $4,
		    requested_start_time = $5,
		    requested_end_time = $6,
		    notes = NULLIF($7, ''),
		    error_code = $8,
		    error_message = $9,
		    provider_outcome = $10,
		    retry_policy = $11,
		    reconciliation_status = $12,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    updated_at = now()
		WHERE id = $13
		  AND salon_id = $14
		  AND status = $15
		  AND processing_token = $16
		  AND operation_type = $17
		  AND superseded_at IS NULL
		RETURNING updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
		attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ErrorCode,
		attempt.ErrorMessage, attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation,
		attempt.ID, attempt.SalonID, StatusPOSPending, record.ProcessingToken, BookingActionBook,
	).Scan(&attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, record.SalonID, record.Provider, record.Operation, record.ErrorCode, record.ErrorMessage); err != nil {
		return nil, err
	}

	payload := ownerNotificationPayload(NotificationTypeBookingFallback, record.SalonID, attempt.ID, "", map[string]any{
		"booking_status":        attempt.Status,
		"pos_provider":          record.Provider,
		"pos_booking_id":        attempt.POSBookingID,
		"provider_outcome":      attempt.ProviderOutcome,
		"retry_policy":          attempt.RetryPolicy,
		"reconciliation_status": attempt.Reconciliation,
		"error_code":            attempt.ErrorCode,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, type, status, title, message, dedupe_key, payload)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7::jsonb)
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, record.SalonID, attempt.ID, NotificationTypeBookingFallback, fallbackNotificationTitle(record), fallbackNotificationMessage(record), "booking-result:"+attempt.ID, payload); err != nil {
		return nil, err
	}
	if attempt.Reconciliation == ReconciliationRequired {
		if err := ensureReconciliationTaskTx(ctx, tx, record.SalonID, attempt.ID, attempt.ErrorMessage); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, &attempt); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *Repository) GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT appointment.scheduling_authority
		FROM appointments appointment
		JOIN salons salon
		  ON salon.id = appointment.salon_id
		 AND salon.owner_user_id = $3
		WHERE appointment.id = $1
		  AND appointment.salon_id = $2
	`, appointmentID, salonID, ownerUserID).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if authority == SchedulingAuthorityManleAICalendar {
		return r.getInternalAppointmentForOwner(ctx, salonID, ownerUserID, appointmentID)
	}
	return r.getExternalAppointmentForOwner(ctx, salonID, ownerUserID, appointmentID)
}

func (r *Repository) getExternalAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT a.id::text, a.salon_id::text, a.booking_attempt_id::text,
		       a.scheduling_authority, COALESCE(a.authority_appointment_version, 0), a.party_size,
		       a.pos_provider,
		       origin.provider_location_id, connection.location_id, connection.snapshot_generation,
		       a.pos_appointment_id, COALESCE(a.pos_appointment_version, 0), a.status,
		       a.customer_name, a.customer_phone, COALESCE(a.customer_email, ''),
	       COALESCE(a.start_time, now()), COALESCE(a.end_time, now()), COALESCE(a.notes, ''),
	       a.created_at, a.updated_at, COALESCE(a.staff_selection_mode, 'specific'),
	       COALESCE(s.id::text, ''), COALESCE(service_link.provider, s.pos_provider, a.pos_provider),
	       COALESCE(service_link.provider_entity_id, ''), COALESCE(service_link.provider_version, s.pos_service_version, 0),
	       COALESCE(s.name, ''), COALESCE(s.duration_minutes, 0),
	       COALESCE(s.price_from, 0),
	       COALESCE(st.id::text, ''), COALESCE(staff_link.provider, st.pos_provider, a.pos_provider), COALESCE(staff_link.provider_entity_id, ''),
	       COALESCE(st.name, '')
		FROM appointments a
		JOIN salons salon ON salon.id = a.salon_id
		JOIN booking_attempts origin
		  ON origin.id = a.booking_attempt_id
		 AND origin.salon_id = a.salon_id
		 AND origin.pos_provider = a.pos_provider
		 AND origin.operation_type = 'book'
		 AND NULLIF(BTRIM(origin.provider_location_id), '') IS NOT NULL
		 AND origin.provider_snapshot_generation IS NOT NULL
		 AND origin.provider_snapshot_generation > 0
		JOIN pos_connections connection
		  ON connection.salon_id = a.salon_id
		 AND connection.provider = a.pos_provider
		 AND connection.status = 'active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id = origin.provider_location_id
		 AND connection.snapshot_generation >= origin.provider_snapshot_generation
		LEFT JOIN services s ON s.id = a.service_id
	LEFT JOIN staff st ON st.id = a.staff_id
	LEFT JOIN pos_entity_links service_link
	  ON service_link.salon_id = a.salon_id
	 AND service_link.entity_type = 'service'
	 AND service_link.entity_id = s.id
	 AND service_link.provider = a.pos_provider
	 AND service_link.sync_status = 'synced'
	 AND service_link.provider_entity_id IS NOT NULL
	 AND service_link.provider_entity_id <> ''
	LEFT JOIN pos_entity_links staff_link
	  ON staff_link.salon_id = a.salon_id
	 AND staff_link.entity_type = 'staff'
	 AND staff_link.entity_id = st.id
	 AND staff_link.provider = a.pos_provider
	 AND staff_link.sync_status = 'synced'
	 AND staff_link.provider_entity_id IS NOT NULL
	 AND staff_link.provider_entity_id <> ''
		WHERE a.id = $1
		  AND a.salon_id = $2
		  AND salon.owner_user_id = $3
		  AND salon.active_pos_provider = a.pos_provider
		`, appointmentID, salonID, ownerUserID)

	var item AppointmentActionRef
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.BookingAttemptID,
		&item.SchedulingAuthority,
		&item.AuthorityAppointmentVersion,
		&item.PartySize,
		&item.POSProvider,
		&item.ProviderLocationID,
		&item.ProviderFence.LocationID,
		&item.ProviderFence.SnapshotGeneration,
		&item.POSAppointmentID,
		&item.POSAppointmentVersion,
		&item.Status,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.StartTime,
		&item.EndTime,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.StaffSelectionMode,
		&item.Service.ID,
		&item.Service.POSProvider,
		&item.Service.POSServiceID,
		&item.Service.POSServiceVersion,
		&item.Service.Name,
		&item.Service.DurationMinutes,
		&item.Service.PriceFrom,
		&item.Staff.ID,
		&item.Staff.POSProvider,
		&item.Staff.POSStaffID,
		&item.Staff.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	segments, err := r.loadAppointmentActionSegments(ctx, item.ID, item.POSProvider, item.ProviderFence)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, pos.ErrNotFound
	}
	item.Segments = segments
	item.Service = segments[0].Service
	item.Staff = segments[0].Staff
	return &item, nil
}

func (r *Repository) getInternalAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	var item AppointmentActionRef
	err := r.db.QueryRowContext(ctx, `
		SELECT appointment.id::text, appointment.salon_id::text, appointment.booking_attempt_id::text,
		       appointment.scheduling_authority, appointment.authority_appointment_version,
		       appointment.party_size, appointment.status,
		       appointment.customer_name, appointment.customer_phone, COALESCE(appointment.customer_email, ''),
		       appointment.start_time, appointment.end_time, COALESCE(appointment.notes, ''),
		       appointment.created_at, appointment.updated_at,
		       COALESCE(appointment.staff_selection_mode, 'specific')
		FROM appointments appointment
		JOIN salons salon
		  ON salon.id = appointment.salon_id
		 AND salon.owner_user_id = $3
		WHERE appointment.id = $1
		  AND appointment.salon_id = $2
		  AND appointment.scheduling_authority = 'manleai_calendar'
	`, appointmentID, salonID, ownerUserID).Scan(
		&item.ID,
		&item.SalonID,
		&item.BookingAttemptID,
		&item.SchedulingAuthority,
		&item.AuthorityAppointmentVersion,
		&item.PartySize,
		&item.Status,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.StartTime,
		&item.EndTime,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.StaffSelectionMode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if item.Status != StatusConfirmed && item.Status != StatusRescheduled {
		return nil, pos.ErrNotFound
	}
	segments, err := r.loadInternalAppointmentActionSegments(ctx, item.SalonID, item.ID, item.AuthorityAppointmentVersion)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, pos.ErrNotFound
	}
	item.Segments = segments
	item.Service = segments[0].Service
	item.Staff = segments[0].Staff
	item.StaffSelectionMode = segments[0].StaffSelectionMode
	return &item, nil
}

func (r *Repository) ListRescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req RescheduleLookupRequest) ([]AppointmentActionRef, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 5 {
		limit = 5
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT candidate.id
		FROM (
		    SELECT appointment.id::text AS id, appointment.customer_name, appointment.start_time
		    FROM appointments appointment
		    JOIN salons salon
		      ON salon.id = appointment.salon_id
		     AND salon.owner_user_id = $2
		    WHERE appointment.salon_id = $1
		      AND appointment.scheduling_authority = 'manleai_calendar'
		      AND right(regexp_replace(appointment.customer_phone, '[^0-9]', '', 'g'), 10) =
		          right(regexp_replace($3, '[^0-9]', '', 'g'), 10)
		      AND appointment.status IN ($4, $5)
		      AND appointment.start_time >= now()
		      AND EXISTS (
		          SELECT 1
		          FROM appointment_services segment
		          WHERE segment.salon_id = appointment.salon_id
		            AND segment.appointment_id = appointment.id
		            AND segment.scheduling_authority = 'manleai_calendar'
		            AND segment.plan_version = appointment.authority_appointment_version
		            AND segment.released_at IS NULL
		      )
		    UNION ALL
		    SELECT appointment.id::text AS id, appointment.customer_name, appointment.start_time
		    FROM appointments appointment
		    JOIN salons salon
		      ON salon.id = appointment.salon_id
		     AND salon.owner_user_id = $2
		    JOIN booking_attempts origin
		      ON origin.id = appointment.booking_attempt_id
		     AND origin.salon_id = appointment.salon_id
		     AND origin.pos_provider = appointment.pos_provider
		     AND origin.operation_type = 'book'
		     AND NULLIF(BTRIM(origin.provider_location_id), '') IS NOT NULL
		     AND origin.provider_snapshot_generation IS NOT NULL
		     AND origin.provider_snapshot_generation > 0
		    JOIN pos_connections connection
		      ON connection.salon_id = appointment.salon_id
		     AND connection.provider = appointment.pos_provider
		     AND connection.status = 'active'
		     AND connection.last_sync_at IS NOT NULL
		     AND connection.location_id = origin.provider_location_id
		     AND connection.snapshot_generation >= origin.provider_snapshot_generation
		    WHERE appointment.salon_id = $1
		      AND appointment.scheduling_authority = 'external_provider'
		      AND salon.active_pos_provider = appointment.pos_provider
		      AND right(regexp_replace(appointment.customer_phone, '[^0-9]', '', 'g'), 10) =
		          right(regexp_replace($3, '[^0-9]', '', 'g'), 10)
		      AND appointment.status IN ($4, $5)
		      AND appointment.start_time >= now()
		      AND appointment.pos_appointment_id <> ''
		      AND appointment.pos_appointment_version >= 0
		) candidate
		ORDER BY
		  CASE WHEN NULLIF($6, '') IS NOT NULL AND lower(candidate.customer_name) = lower($6) THEN 0 ELSE 1 END,
		  candidate.start_time ASC,
		  candidate.id ASC
		LIMIT $7
	`, salonID, ownerUserID, req.CustomerPhone, StatusConfirmed, StatusRescheduled, req.CustomerName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]AppointmentActionRef, 0, len(ids))
	for _, id := range ids {
		item, err := r.GetAppointmentForOwner(ctx, salonID, ownerUserID, id)
		if errors.Is(err, pos.ErrNotFound) {
			// The provider fence or catalog mapping can change after the candidate
			// query. Skip that stale row instead of hiding later valid candidates.
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *Repository) loadInternalAppointmentActionSegments(ctx context.Context, salonID string, appointmentID string, planVersion int) ([]BookingSegmentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT segment.id::text,
		       service.id::text, segment.name, segment.duration_minutes, COALESCE(service.price_from, 0),
		       staff_member.id::text, staff_member.name,
		       segment.staff_selection_mode, COALESCE(segment.guest_reference, ''),
		       segment.plan_version, segment.scheduled_start_time, segment.scheduled_end_time,
		       segment.occupied_start_time, segment.occupied_end_time,
		       segment.buffer_before_minutes, segment.buffer_after_minutes, segment.sort_order
		FROM appointment_services segment
		JOIN services service
		  ON service.id = segment.service_id
		 AND service.salon_id = segment.salon_id
		JOIN staff staff_member
		  ON staff_member.id = segment.staff_id
		 AND staff_member.salon_id = segment.salon_id
		WHERE segment.salon_id = $1
		  AND segment.appointment_id = $2
		  AND segment.scheduling_authority = 'manleai_calendar'
		  AND segment.plan_version = $3
		  AND segment.released_at IS NULL
		ORDER BY segment.sort_order ASC, segment.id ASC
	`, salonID, appointmentID, planVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]BookingSegmentRecord, 0)
	for rows.Next() {
		var segment BookingSegmentRecord
		if err := rows.Scan(
			&segment.AppointmentServiceID,
			&segment.Service.ID,
			&segment.Service.Name,
			&segment.Service.DurationMinutes,
			&segment.Service.PriceFrom,
			&segment.Staff.ID,
			&segment.Staff.Name,
			&segment.StaffSelectionMode,
			&segment.GuestReference,
			&segment.PlanVersion,
			&segment.ScheduledStartTime,
			&segment.ScheduledEndTime,
			&segment.OccupiedStartTime,
			&segment.OccupiedEndTime,
			&segment.BufferBeforeMinutes,
			&segment.BufferAfterMinutes,
			&segment.SortOrder,
		); err != nil {
			return nil, err
		}
		segment.Quantity = 1
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachInternalActionSegmentResources(ctx, salonID, planVersion, segments); err != nil {
		return nil, err
	}
	return segments, nil
}

func (r *Repository) attachInternalActionSegmentResources(ctx context.Context, salonID string, planVersion int, segments []BookingSegmentRecord) error {
	if len(segments) == 0 {
		return nil
	}
	ids := make([]string, 0, len(segments))
	indexByID := make(map[string]int, len(segments))
	for index := range segments {
		ids = append(ids, segments[index].AppointmentServiceID)
		indexByID[segments[index].AppointmentServiceID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT allocation.appointment_service_id::text, pool.id::text, pool.name, allocation.units_allocated
		FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN manleai_calendar_resource_pools pool
		  ON pool.id = allocation.resource_pool_id
		 AND pool.salon_id = allocation.salon_id
		WHERE allocation.salon_id = $1
		  AND allocation.appointment_service_id = ANY($2::uuid[])
		  AND allocation.plan_version = $3
		  AND allocation.released_at IS NULL
		ORDER BY allocation.appointment_service_id, pool.id
	`, salonID, pq.Array(ids), planVersion)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var segmentID string
		var allocation AvailabilityResourceAllocation
		if err := rows.Scan(&segmentID, &allocation.ResourcePoolID, &allocation.ResourceName, &allocation.UnitsAllocated); err != nil {
			return err
		}
		if index, ok := indexByID[segmentID]; ok {
			segments[index].ResourceAllocations = append(segments[index].ResourceAllocations, allocation)
		}
	}
	return rows.Err()
}

func (r *Repository) loadAppointmentActionSegments(ctx context.Context, appointmentID string, provider string, fence pos.ProviderFence) ([]BookingSegmentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(aps.service_id::text, ''),
		       $2,
		       COALESCE(aps.pos_service_id, ''),
		       COALESCE(aps.pos_service_version, 0),
		       aps.name,
		       COALESCE(aps.duration_minutes, 0),
		       COALESCE(aps.price_from, s.price_from, 0),
		       COALESCE(aps.staff_id::text, ''),
		       $2,
		       COALESCE(aps.pos_staff_id, ''),
		       COALESCE(st.name, ''),
		       COALESCE(aps.staff_selection_mode, 'specific'),
		       COALESCE(aps.sort_order, 1),
		       aps.service_id IS NOT NULL
		       AND s.id IS NOT NULL
		       AND NULLIF(BTRIM(COALESCE(aps.pos_service_id, '')), '') IS NOT NULL
		       AND COALESCE(aps.pos_service_version, 0) > 0
		       AND service_link.provider_entity_id = aps.pos_service_id
		       AND COALESCE(service_link.provider_version, s.pos_service_version, 0) = aps.pos_service_version
		       AND aps.staff_id IS NOT NULL
		       AND st.id IS NOT NULL
		       AND NULLIF(BTRIM(COALESCE(aps.pos_staff_id, '')), '') IS NOT NULL
		       AND staff_link.provider_entity_id = aps.pos_staff_id
		       AND COALESCE(aps.duration_minutes, 0) > 0 AS mapping_current
		FROM appointment_services aps
		LEFT JOIN services s ON s.id = aps.service_id
		LEFT JOIN staff st ON st.id = aps.staff_id
		LEFT JOIN appointments a ON a.id = aps.appointment_id
		LEFT JOIN pos_entity_links service_link
		  ON service_link.salon_id = a.salon_id
		 AND service_link.entity_type = 'service'
		 AND service_link.entity_id = s.id
		 AND service_link.provider = $2
		 AND service_link.sync_status = 'synced'
		 AND service_link.provider_entity_id IS NOT NULL
		 AND service_link.provider_entity_id <> ''
		LEFT JOIN pos_entity_links staff_link
		  ON staff_link.salon_id = a.salon_id
		 AND staff_link.entity_type = 'staff'
		 AND staff_link.entity_id = st.id
		 AND staff_link.provider = $2
		 AND staff_link.sync_status = 'synced'
		 AND staff_link.provider_entity_id IS NOT NULL
		 AND staff_link.provider_entity_id <> ''
		WHERE aps.appointment_id = $1
		ORDER BY aps.sort_order ASC, aps.created_at ASC
	`, appointmentID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]BookingSegmentRecord, 0)
	for rows.Next() {
		var segment BookingSegmentRecord
		var mappingCurrent bool
		if err := rows.Scan(
			&segment.Service.ID,
			&segment.Service.POSProvider,
			&segment.Service.POSServiceID,
			&segment.Service.POSServiceVersion,
			&segment.Service.Name,
			&segment.Service.DurationMinutes,
			&segment.Service.PriceFrom,
			&segment.Staff.ID,
			&segment.Staff.POSProvider,
			&segment.Staff.POSStaffID,
			&segment.Staff.Name,
			&segment.StaffSelectionMode,
			&segment.SortOrder,
			&mappingCurrent,
		); err != nil {
			return nil, err
		}
		if !mappingCurrent {
			return nil, pos.ErrNotFound
		}
		segment.Service.ProviderFence = fence
		segment.Staff.ProviderFence = fence
		segment.Quantity = 1
		segment.PlanVersion = 1
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (r *Repository) ClaimPendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingOperationClaim, error) {
	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	attempt := BookingAttempt{
		SalonID:                           record.SalonID,
		Source:                            source,
		Status:                            StatusPOSPending,
		POSProvider:                       record.Provider,
		POSBookingID:                      record.Appointment.POSAppointmentID,
		POSIdempotencyKey:                 record.POSIdempotencyKey,
		OperationKey:                      record.OperationKey,
		RequestFingerprint:                record.RequestFingerprint,
		RetryOfAttemptID:                  record.RetryOfAttemptID,
		AvailabilityQuoteID:               record.AvailabilityQuoteID,
		SlotFingerprint:                   record.SlotFingerprint,
		ProviderFence:                     record.ProviderFence,
		OperationType:                     record.OperationType,
		TargetAppointmentID:               record.Appointment.ID,
		TargetPOSBookingVersion:           record.Appointment.POSAppointmentVersion,
		TargetAuthorityAppointmentVersion: record.Appointment.AuthorityAppointmentVersion,
		ProcessingToken:                   record.ProcessingToken,
		ProviderOutcome:                   ProviderOutcomeNotStarted,
		RetryPolicy:                       RetryPolicyNone,
		Reconciliation:                    ReconciliationNotRequired,
		CustomerName:                      record.Appointment.CustomerName,
		CustomerPhone:                     record.Appointment.CustomerPhone,
		CustomerEmail:                     record.Appointment.CustomerEmail,
		ServiceID:                         primary.Service.ID,
		StaffID:                           primary.Staff.ID,
		StaffSelectionMode:                primary.StaffSelectionMode,
		RequestedStartTime:                record.RequestedStartTime,
		RequestedEndTime:                  record.RequestedEndTime,
		Notes:                             record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	return r.claimPendingOperation(ctx, attempt, record.LeaseExpiresAt, segments)
}

func loadDirectMutationAppointmentTx(ctx context.Context, tx *sql.Tx, record AppointmentActionRef) (*Appointment, error) {
	appointment, err := scanAppointment(tx.QueryRowContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
		       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
		       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
		       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
		       COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
		       COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''),
		       COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''),
		       created_at, updated_at
		FROM appointments
		WHERE id = $1
		  AND salon_id = $2
		  AND pos_provider = $3
		  AND pos_appointment_id = $4
		FOR UPDATE
	`, record.ID, record.SalonID, record.POSProvider, record.POSAppointmentID))
	if errors.Is(err, pos.ErrNotFound) {
		return nil, ErrOperationConflict
	}
	return appointment, err
}

func directMutationSegmentsMatchTx(ctx context.Context, tx *sql.Tx, appointmentID string, expected []BookingSegmentRecord) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(service_id::text, ''), pos_service_id, COALESCE(pos_service_version, 0),
		       COALESCE(staff_id::text, ''), COALESCE(pos_staff_id, ''),
		       COALESCE(staff_selection_mode, 'specific'), duration_minutes, sort_order
		FROM appointment_services
		WHERE appointment_id = $1
		ORDER BY sort_order ASC, id ASC
	`, appointmentID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(expected) {
			return false, nil
		}
		var persisted calendarAppointmentSegmentSnapshot
		if err := rows.Scan(
			&persisted.ServiceID,
			&persisted.POSServiceID,
			&persisted.POSServiceVersion,
			&persisted.StaffID,
			&persisted.POSStaffID,
			&persisted.StaffSelectionMode,
			&persisted.DurationMinutes,
			&persisted.SortOrder,
		); err != nil {
			return false, err
		}
		segment := expected[index]
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		staffSelectionMode := segment.StaffSelectionMode
		if strings.TrimSpace(staffSelectionMode) == "" {
			staffSelectionMode = StaffSelectionSpecific
		}
		if persisted.ServiceID != segment.Service.ID ||
			persisted.POSServiceID != strings.TrimSpace(segment.Service.POSServiceID) ||
			persisted.POSServiceVersion != segment.Service.POSServiceVersion ||
			persisted.StaffID != segment.Staff.ID ||
			persisted.POSStaffID != strings.TrimSpace(segment.Staff.POSStaffID) ||
			persisted.StaffSelectionMode != staffSelectionMode ||
			persisted.DurationMinutes != segment.Service.DurationMinutes ||
			persisted.SortOrder != sortOrder {
			return false, nil
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return index == len(expected) && index > 0, nil
}

func (r *Repository) SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.Appointment.SalonID); err != nil {
		return nil, err
	}
	currentAppointment, err := loadDirectMutationAppointmentTx(ctx, tx, record.Appointment)
	if err != nil {
		return nil, err
	}
	if record.POSBookingVersion <= record.Appointment.POSAppointmentVersion {
		return nil, ErrOperationConflict
	}

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
		if record.Staff.ID != "" {
			segments = applyStaffToBookingSegments(segments, record.Staff)
		}
	}
	for index := range segments {
		if segments[index].SortOrder <= 0 {
			segments[index].SortOrder = index + 1
		}
		if strings.TrimSpace(segments[index].StaffSelectionMode) == "" {
			segments[index].StaffSelectionMode = StaffSelectionSpecific
		}
	}
	primary := segments[0]
	providerMirrorConverged := currentAppointment.POSAppointmentVersion >= record.POSBookingVersion &&
		currentAppointment.POSAppointmentVersion > record.Appointment.POSAppointmentVersion
	if providerMirrorConverged {
		segmentsMatch, err := directMutationSegmentsMatchTx(ctx, tx, record.Appointment.ID, segments)
		if err != nil {
			return nil, err
		}
		if (currentAppointment.Status != StatusConfirmed && currentAppointment.Status != StatusRescheduled) ||
			!currentAppointment.StartTime.Equal(record.StartTime) ||
			!currentAppointment.EndTime.Equal(record.EndTime) ||
			currentAppointment.ServiceID != primary.Service.ID ||
			currentAppointment.StaffID != primary.Staff.ID ||
			currentAppointment.StaffSelectionMode != primary.StaffSelectionMode ||
			!segmentsMatch {
			return nil, ErrOperationConflict
		}
	}
	finalPOSBookingVersion := record.POSBookingVersion
	if providerMirrorConverged {
		finalPOSBookingVersion = currentAppointment.POSAppointmentVersion
	}
	attempt := BookingAttempt{
		ID:                          record.AttemptID,
		SalonID:                     record.Appointment.SalonID,
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityProvider:           record.Appointment.POSProvider,
		AuthorityAppointmentID:      record.Appointment.POSAppointmentID,
		AuthorityAppointmentVersion: finalPOSBookingVersion,
		Source:                      source,
		Status:                      StatusRescheduled,
		POSProvider:                 record.Appointment.POSProvider,
		POSBookingID:                record.Appointment.POSAppointmentID,
		POSBookingVersion:           finalPOSBookingVersion,
		CustomerName:                record.Appointment.CustomerName,
		CustomerPhone:               record.Appointment.CustomerPhone,
		CustomerEmail:               record.Appointment.CustomerEmail,
		ServiceID:                   primary.Service.ID,
		StaffID:                     primary.Staff.ID,
		StaffSelectionMode:          primary.StaffSelectionMode,
		RequestedStartTime:          record.StartTime,
		RequestedEndTime:            record.EndTime,
		Notes:                       record.Notes,
		OperationType:               BookingActionReschedule,
		ProviderOutcome:             ProviderOutcomeSucceeded,
		RetryPolicy:                 RetryPolicyNone,
		Reconciliation:              ReconciliationNotRequired,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    pos_booking_version = $3,
		    authority_appointment_id = NULLIF($2, ''),
		    authority_appointment_version = $3,
		    staff_id = $4,
		    staff_selection_mode = $5,
		    requested_start_time = $6,
		    requested_end_time = $7,
		    notes = NULLIF($8, ''),
		    error_code = NULL,
		    error_message = NULL,
		    provider_outcome = $9,
		    retry_policy = $10,
		    reconciliation_status = $11,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    updated_at = now()
		WHERE id = $12
		  AND salon_id = $13
		  AND status = $14
		  AND processing_token = $15
		  AND operation_type = $16
		  AND target_appointment_id = $17
		  AND target_pos_booking_version = $18
		  AND superseded_at IS NULL
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffID, attempt.StaffSelectionMode,
		attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ProviderOutcome,
		attempt.RetryPolicy, attempt.Reconciliation, attempt.ID, attempt.SalonID, StatusPOSPending,
		record.ProcessingToken, BookingActionReschedule, record.Appointment.ID, record.Appointment.POSAppointmentVersion,
	).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	var appointment *Appointment
	if providerMirrorConverged {
		appointment = currentAppointment
	} else {
		appointment, err = scanAppointment(tx.QueryRowContext(ctx, `
			UPDATE appointments
			SET status = $1,
			    service_id = $2,
			    staff_id = $3,
			    staff_selection_mode = $4,
			    start_time = $5,
			    end_time = $6,
			    notes = NULLIF($7, ''),
			    pos_appointment_version = $8,
			    authority_appointment_version = $8,
			    updated_at = now()
			WHERE id = $9
			  AND salon_id = $10
			  AND pos_provider = $11
			  AND pos_appointment_id = $12
			  AND COALESCE(pos_appointment_version, 0) < $8
			RETURNING id::text, salon_id::text, booking_attempt_id::text, pos_provider, pos_appointment_id,
			          COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
			          COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
			          COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
			          COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
			          COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'),
			          start_time, end_time, COALESCE(notes, ''), created_at, updated_at
		`, StatusRescheduled, primary.Service.ID, primary.Staff.ID, attempt.StaffSelectionMode,
			record.StartTime, record.EndTime, record.Notes, record.POSBookingVersion, record.Appointment.ID,
			record.Appointment.SalonID, record.Appointment.POSProvider, record.Appointment.POSAppointmentID))
		if err == nil {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM appointment_services
				WHERE salon_id = $1 AND appointment_id = $2
			`, record.Appointment.SalonID, record.Appointment.ID); err != nil {
				return nil, err
			}
			if err := insertAppointmentServices(ctx, tx, record.Appointment.SalonID, record.Appointment.ID, segments); err != nil {
				return nil, err
			}
		}
	}
	if errors.Is(err, pos.ErrNotFound) {
		return nil, ErrOperationConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	appointment.Segments = authorityBookingSegmentSnapshots(segments)
	return appointment, nil
}

func (r *Repository) SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.Appointment.SalonID); err != nil {
		return nil, err
	}
	currentAppointment, err := loadDirectMutationAppointmentTx(ctx, tx, record.Appointment)
	if err != nil {
		return nil, err
	}
	if record.POSBookingVersion <= record.Appointment.POSAppointmentVersion {
		return nil, ErrOperationConflict
	}
	providerMirrorConverged := currentAppointment.POSAppointmentVersion >= record.POSBookingVersion &&
		currentAppointment.POSAppointmentVersion > record.Appointment.POSAppointmentVersion
	if providerMirrorConverged && currentAppointment.Status != StatusCancelled {
		return nil, ErrOperationConflict
	}
	finalPOSBookingVersion := record.POSBookingVersion
	if providerMirrorConverged {
		finalPOSBookingVersion = currentAppointment.POSAppointmentVersion
	}

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := appointmentActionSegments(record.Appointment)
	primary := segments[0]
	attempt := BookingAttempt{
		ID:                          record.AttemptID,
		SalonID:                     record.Appointment.SalonID,
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityProvider:           record.Appointment.POSProvider,
		AuthorityAppointmentID:      record.Appointment.POSAppointmentID,
		AuthorityAppointmentVersion: finalPOSBookingVersion,
		Source:                      source,
		Status:                      StatusCancelled,
		POSProvider:                 record.Appointment.POSProvider,
		POSBookingID:                record.Appointment.POSAppointmentID,
		POSBookingVersion:           finalPOSBookingVersion,
		CustomerName:                record.Appointment.CustomerName,
		CustomerPhone:               record.Appointment.CustomerPhone,
		CustomerEmail:               record.Appointment.CustomerEmail,
		ServiceID:                   primary.Service.ID,
		StaffID:                     primary.Staff.ID,
		StaffSelectionMode:          primary.StaffSelectionMode,
		RequestedStartTime:          record.Appointment.StartTime,
		RequestedEndTime:            record.Appointment.EndTime,
		Notes:                       record.Reason,
		OperationType:               BookingActionCancel,
		ProviderOutcome:             ProviderOutcomeSucceeded,
		RetryPolicy:                 RetryPolicyNone,
		Reconciliation:              ReconciliationNotRequired,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    pos_booking_version = $3,
		    authority_appointment_id = NULLIF($2, ''),
		    authority_appointment_version = $3,
		    staff_selection_mode = $4,
		    notes = NULLIF($5, ''),
		    error_code = NULL,
		    error_message = NULL,
		    provider_outcome = $6,
		    retry_policy = $7,
		    reconciliation_status = $8,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    updated_at = now()
		WHERE id = $9
		  AND salon_id = $10
		  AND status = $11
		  AND processing_token = $12
		  AND operation_type = $13
		  AND target_appointment_id = $14
		  AND target_pos_booking_version = $15
		  AND superseded_at IS NULL
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
		attempt.Notes, attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation,
		attempt.ID, attempt.SalonID, StatusPOSPending, record.ProcessingToken, BookingActionCancel,
		record.Appointment.ID, record.Appointment.POSAppointmentVersion,
	).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	var appointment *Appointment
	if providerMirrorConverged {
		appointment = currentAppointment
	} else {
		appointment, err = scanAppointment(tx.QueryRowContext(ctx, `
			UPDATE appointments
			SET status = $1,
			    pos_appointment_version = $2,
			    authority_appointment_version = $2,
			    updated_at = now()
			WHERE id = $3
			  AND salon_id = $4
			  AND pos_provider = $5
			  AND pos_appointment_id = $6
			  AND COALESCE(pos_appointment_version, 0) < $2
			RETURNING id::text, salon_id::text, booking_attempt_id::text, pos_provider, pos_appointment_id,
			          COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
			          COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
			          COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
			          COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
			          COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'),
			          start_time, end_time, COALESCE(notes, ''), created_at, updated_at
		`, StatusCancelled, record.POSBookingVersion, record.Appointment.ID, record.Appointment.SalonID, record.Appointment.POSProvider, record.Appointment.POSAppointmentID))
		if errors.Is(err, pos.ErrNotFound) {
			return nil, ErrOperationConflict
		}
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	appointment.Segments = authorityBookingSegmentSnapshots(segments)
	return appointment, nil
}

func (r *Repository) SaveAppointmentActionFallback(ctx context.Context, record AppointmentActionFallbackRecord) (*BookingAttempt, error) {
	operationType := BookingActionBook
	if record.NotificationType == NotificationTypeRescheduleFallback {
		operationType = BookingActionReschedule
	} else if record.NotificationType == NotificationTypeCancellationFallback {
		operationType = BookingActionCancel
	}
	segments := append([]BookingSegmentRecord(nil), record.Segments...)
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	for index := range segments {
		if segments[index].SortOrder <= 0 {
			segments[index].SortOrder = index + 1
		}
		if strings.TrimSpace(segments[index].StaffSelectionMode) == "" {
			segments[index].StaffSelectionMode = StaffSelectionSpecific
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, record.SalonID); err != nil {
		return nil, err
	}
	currentAppointment, err := loadDirectMutationAppointmentTx(ctx, tx, record.Appointment)
	if err != nil {
		return nil, err
	}

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	primary := segments[0]
	status := strings.TrimSpace(record.Status)
	if status == "" {
		status = StatusFallbackPending
	}
	posBookingVersion := record.POSBookingVersion
	if strings.TrimSpace(record.POSBookingID) == "" {
		posBookingVersion = record.Appointment.POSAppointmentVersion
	}
	attempt := BookingAttempt{
		ID:                          record.AttemptID,
		SalonID:                     record.SalonID,
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityProvider:           record.Provider,
		AuthorityAppointmentID:      defaultString(record.POSBookingID, record.Appointment.POSAppointmentID),
		AuthorityAppointmentVersion: posBookingVersion,
		Source:                      source,
		Status:                      status,
		POSProvider:                 record.Provider,
		POSBookingID:                defaultString(record.POSBookingID, record.Appointment.POSAppointmentID),
		POSBookingVersion:           posBookingVersion,
		CustomerName:                record.Appointment.CustomerName,
		CustomerPhone:               record.Appointment.CustomerPhone,
		CustomerEmail:               record.Appointment.CustomerEmail,
		ServiceID:                   primary.Service.ID,
		StaffID:                     primary.Staff.ID,
		StaffSelectionMode:          primary.StaffSelectionMode,
		RequestedStartTime:          record.RequestedStartTime,
		RequestedEndTime:            record.RequestedEndTime,
		Notes:                       record.Notes,
		ErrorCode:                   record.ErrorCode,
		ErrorMessage:                record.ErrorMessage,
		OperationType:               operationType,
		ProviderOutcome:             record.ProviderOutcome,
		RetryPolicy:                 record.RetryPolicy,
		Reconciliation:              record.Reconciliation,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT created_at, COALESCE(provider_location_id, ''), COALESCE(provider_snapshot_generation, 0)
		FROM booking_attempts
		WHERE id = $1
		  AND salon_id = $2
		  AND pos_provider = $3
		  AND operation_type = $4
		  AND target_appointment_id = $5
		  AND target_pos_booking_version = $6
		  AND status = $7
		  AND processing_token = $8
		  AND superseded_at IS NULL
		FOR UPDATE
	`, attempt.ID, attempt.SalonID, attempt.POSProvider, attempt.OperationType,
		record.Appointment.ID, record.Appointment.POSAppointmentVersion, StatusPOSPending, record.ProcessingToken).Scan(
		&attempt.CreatedAt,
		&attempt.ProviderFence.LocationID,
		&attempt.ProviderFence.SnapshotGeneration,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	attempt.AuthorityLocationID = attempt.ProviderFence.LocationID
	attempt.AuthoritySnapshotGeneration = attempt.ProviderFence.SnapshotGeneration

	providerMirrorConverged := record.ProviderOutcome != ProviderOutcomeFailed &&
		currentAppointment.POSAppointmentVersion > record.Appointment.POSAppointmentVersion &&
		currentAppointment.POSAppointmentVersion >= posBookingVersion
	if providerMirrorConverged && attempt.OperationType == BookingActionReschedule {
		segmentsMatch, matchErr := directMutationSegmentsMatchTx(ctx, tx, currentAppointment.ID, segments)
		if matchErr != nil {
			return nil, matchErr
		}
		providerMirrorConverged = (currentAppointment.Status == StatusConfirmed || currentAppointment.Status == StatusRescheduled) &&
			currentAppointment.StartTime.Equal(record.RequestedStartTime) &&
			currentAppointment.EndTime.Equal(record.RequestedEndTime) &&
			currentAppointment.ServiceID == primary.Service.ID &&
			currentAppointment.StaffID == primary.Staff.ID &&
			currentAppointment.StaffSelectionMode == primary.StaffSelectionMode &&
			segmentsMatch
	} else if providerMirrorConverged && attempt.OperationType == BookingActionCancel {
		providerMirrorConverged = currentAppointment.Status == StatusCancelled
	} else {
		providerMirrorConverged = false
	}
	if providerMirrorConverged {
		attempt.POSBookingVersion = currentAppointment.POSAppointmentVersion
		attempt.AuthorityAppointmentVersion = currentAppointment.POSAppointmentVersion
		attempt.ProviderOutcome = ProviderOutcomeSucceeded
		attempt.RetryPolicy = RetryPolicyNone
		attempt.Reconciliation = ReconciliationNotRequired
		attempt.ErrorCode = ""
		attempt.ErrorMessage = ""
		if attempt.OperationType == BookingActionReschedule {
			attempt.Status = StatusRescheduled
		} else {
			attempt.Status = StatusCancelled
		}
		if err := tx.QueryRowContext(ctx, `
			UPDATE booking_attempts
			SET status = $1,
			    pos_booking_id = $2,
			    pos_booking_version = $3,
			    authority_appointment_id = $2,
			    authority_appointment_version = $3,
			    staff_id = $4,
			    staff_selection_mode = $5,
			    requested_start_time = $6,
			    requested_end_time = $7,
			    notes = NULLIF($8, ''),
			    error_code = NULL,
			    error_message = NULL,
			    provider_outcome = $9,
			    retry_policy = $10,
			    reconciliation_status = $11,
			    processing_token = NULL,
			    processing_lease_expires_at = NULL,
			    updated_at = now()
			WHERE id = $12
			  AND salon_id = $13
			  AND status = $14
			  AND processing_token = $15
			  AND superseded_at IS NULL
			RETURNING updated_at
		`, attempt.Status, currentAppointment.POSAppointmentID, attempt.POSBookingVersion, attempt.StaffID,
			attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes,
			attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation, attempt.ID, attempt.SalonID,
			StatusPOSPending, record.ProcessingToken).Scan(&attempt.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, pos.ErrNotFound
			}
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		currentAppointment.Segments = authorityBookingSegmentSnapshots(segments)
		attempt.POSBookingID = currentAppointment.POSAppointmentID
		attempt.AuthorityAppointmentID = currentAppointment.POSAppointmentID
		attempt.Appointment = currentAppointment
		attempt.Segments = authorityBookingSegmentSnapshots(segments)
		return &attempt, nil
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    pos_booking_version = $3,
		    authority_appointment_id = NULLIF($2, ''),
		    authority_appointment_version = $3,
		    staff_selection_mode = $4,
		    requested_start_time = $5,
		    requested_end_time = $6,
		    notes = NULLIF($7, ''),
		    error_code = $8,
		    error_message = $9,
		    provider_outcome = $10,
		    retry_policy = $11,
		    reconciliation_status = $12,
		    processing_token = NULL,
		    processing_lease_expires_at = NULL,
		    updated_at = now()
		WHERE id = $13
		  AND salon_id = $14
		  AND status = $15
		  AND processing_token = $16
		  AND operation_type = $17
		  AND target_appointment_id = $18
		  AND target_pos_booking_version = $19
		  AND superseded_at IS NULL
		RETURNING updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.POSBookingVersion, attempt.StaffSelectionMode,
		attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ErrorCode,
		attempt.ErrorMessage, attempt.ProviderOutcome, attempt.RetryPolicy, attempt.Reconciliation,
		attempt.ID, attempt.SalonID, StatusPOSPending, record.ProcessingToken, attempt.OperationType,
		record.Appointment.ID, record.Appointment.POSAppointmentVersion,
	).Scan(&attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, record.SalonID, record.Provider, record.Operation, record.ErrorCode, record.ErrorMessage); err != nil {
		return nil, err
	}

	notificationType := record.NotificationType
	if notificationType == "" {
		notificationType = NotificationTypeBookingFallback
	}
	payload := ownerNotificationPayload(notificationType, record.SalonID, attempt.ID, record.Appointment.ID, map[string]any{
		"booking_status":        attempt.Status,
		"pos_provider":          record.Provider,
		"pos_booking_id":        attempt.POSBookingID,
		"provider_outcome":      attempt.ProviderOutcome,
		"retry_policy":          attempt.RetryPolicy,
		"reconciliation_status": attempt.Reconciliation,
		"error_code":            attempt.ErrorCode,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message, dedupe_key, payload)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8::jsonb)
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, record.SalonID, attempt.ID, record.Appointment.ID, notificationType, appointmentActionFallbackTitle(record), appointmentActionFallbackMessage(record), "booking-action-result:"+attempt.ID, payload); err != nil {
		return nil, err
	}
	if attempt.Reconciliation == ReconciliationRequired {
		if err := ensureReconciliationTaskTx(ctx, tx, record.SalonID, attempt.ID, attempt.ErrorMessage); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Segments = authorityBookingSegmentSnapshots(segments)
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, &attempt); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *Repository) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	var item TestBookingRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT ba.id::text, COALESCE(ba.operation_type, 'book'), ba.status, COALESCE(ba.pos_booking_id, ''), COALESCE(ba.pos_booking_version, 0),
		       ba.scheduling_authority, COALESCE(ba.authority_provider, ''),
		       COALESCE(ba.authority_appointment_id, ''), COALESCE(ba.authority_appointment_version, 0), ba.customer_name,
		       ba.customer_phone, COALESCE(ba.service_id::text, ''), COALESCE(ba.staff_id::text, ''),
		       ba.requested_start_time, ba.requested_end_time, COALESCE(ba.error_code, ''),
		       COALESCE(ba.error_message, ''), COALESCE(ba.provider_outcome, 'not_started'),
		       COALESCE(ba.retry_policy, 'none'), COALESCE(ba.reconciliation_status, 'not_required'),
		       ba.created_at, ba.updated_at
		FROM booking_attempts ba
		JOIN salons s ON s.id = ba.salon_id
		WHERE ba.salon_id = $1
		  AND s.owner_user_id = $2
		  AND ba.source = $3
		ORDER BY ba.created_at DESC
		LIMIT 1
	`, salonID, ownerUserID, SourceSquareTestBooking).Scan(
		&item.BookingAttemptID,
		&item.OperationType,
		&item.Status,
		&item.POSBookingID,
		&item.POSAppointmentVersion,
		&item.SchedulingAuthority,
		&item.AuthorityProvider,
		&item.AuthorityAppointmentID,
		&item.AuthorityAppointmentVersion,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.ServiceID,
		&item.StaffID,
		&item.StartTime,
		&item.EndTime,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.ProviderOutcome,
		&item.RetryPolicy,
		&item.Reconciliation,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	attempt := &BookingAttempt{
		ID:            item.BookingAttemptID,
		Status:        item.Status,
		OperationType: item.OperationType,
		RetryPolicy:   item.RetryPolicy,
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, attempt); err != nil {
		return nil, err
	}
	item.CanRetry = attempt.CanRetry
	item.RetryBlockedReason = attempt.RetryBlockedReason

	if item.POSBookingID == "" {
		return &item, nil
	}
	var appointmentUpdatedAt time.Time
	err = r.db.QueryRowContext(ctx, `
		SELECT id::text, status, COALESCE(pos_appointment_version, 0), start_time, end_time, updated_at
		FROM appointments
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND pos_appointment_id = $3
	`, salonID, pos.ProviderSquare, item.POSBookingID).Scan(
		&item.AppointmentID,
		&item.AppointmentStatus,
		&item.POSAppointmentVersion,
		&item.StartTime,
		&item.EndTime,
		&appointmentUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return &item, nil
	}
	if err != nil {
		return nil, err
	}
	if appointmentUpdatedAt.After(item.UpdatedAt) {
		item.UpdatedAt = appointmentUpdatedAt
	}
	return &item, nil
}

func (r *Repository) ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) ([]Appointment, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
		       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
		       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
		       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
		       COALESCE(confirmation_source, ''), status, party_size, customer_name, customer_phone, COALESCE(customer_email, ''), COALESCE(service_id::text, ''),
		       COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''),
		       COALESCE(pos_sync_status, ''), last_pos_synced_at, COALESCE(pos_sync_error, ''), created_at, updated_at
		FROM appointments
		WHERE salon_id = $1
		ORDER BY start_time DESC, id DESC
		LIMIT $2
		OFFSET $3
	`, salonID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Appointment, 0)
	for rows.Next() {
		var item Appointment
		var lastPOSSyncedAt sql.NullTime
		var confirmedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.BookingAttemptID,
			&item.POSProvider,
			&item.POSAppointmentID,
			&item.POSAppointmentVersion,
			&item.SchedulingAuthority,
			&item.AuthorityProvider,
			&item.AuthorityAppointmentID,
			&item.AuthorityAppointmentVersion,
			&item.AuthorityCustomerID,
			&confirmedAt,
			&item.ConfirmedByUserID,
			&item.ConfirmationSource,
			&item.Status,
			&item.PartySize,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.StartTime,
			&item.EndTime,
			&item.Notes,
			&item.POSSyncStatus,
			&lastPOSSyncedAt,
			&item.POSSyncError,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastPOSSyncedAt.Valid {
			item.LastPOSSyncedAt = &lastPOSSyncedAt.Time
		}
		if confirmedAt.Valid {
			item.ConfirmedAt = &confirmedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachAppointmentSegments(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]BookingAttempt, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ba.id::text, ba.salon_id::text, ba.source, ba.status, COALESCE(ba.pos_provider, ''),
		       COALESCE(ba.pos_booking_id, ''), COALESCE(ba.pos_booking_version, 0),
		       ba.scheduling_authority, COALESCE(ba.authority_provider, ''), COALESCE(ba.authority_appointment_id, ''),
		       COALESCE(ba.authority_appointment_version, 0), COALESCE(ba.target_authority_appointment_version, 0),
		       COALESCE(ba.authority_idempotency_key, ''), COALESCE(ba.authority_location_id, ''),
		       COALESCE(ba.authority_snapshot_generation, 0),
		       COALESCE(ba.operation_key, ''), COALESCE(ba.retry_of_attempt_id::text, ''),
		       COALESCE(ba.superseded_by_attempt_id::text, ''), COALESCE(ba.retry_sequence, 0),
		       ba.superseded_at, COALESCE(ba.availability_quote_id::text, ''), COALESCE(ba.availability_slot_fingerprint, ''),
		       COALESCE(ba.provider_location_id, ''), COALESCE(ba.provider_snapshot_generation, 0),
		       COALESCE(ba.operation_type, 'book'), COALESCE(ba.provider_outcome, 'not_started'),
		       COALESCE(ba.retry_policy, 'none'), COALESCE(ba.reconciliation_status, 'not_required'),
		       COALESCE(ba.reconciliation_resolution, ''), ba.reconciliation_resolved_at, ba.processing_lease_expires_at,
		       ba.customer_name, ba.customer_phone, COALESCE(ba.customer_email, ''), COALESCE(ba.service_id::text, ''),
		       COALESCE(ba.staff_id::text, ''), COALESCE(ba.staff_selection_mode, 'specific'), ba.requested_start_time, ba.requested_end_time, COALESCE(ba.notes, ''),
		       COALESCE(ba.error_code, ''), COALESCE(ba.error_message, ''), ba.created_at, ba.updated_at,
		       COALESCE(ba.target_appointment_id::text, note.appointment_id::text, ''), COALESCE(note.type, ''), COALESCE(note.status, '')
		FROM booking_attempts ba
		LEFT JOIN LATERAL (
			SELECT appointment_id, type, status
			FROM owner_notifications
			WHERE booking_attempt_id = ba.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) note ON TRUE
		WHERE ba.salon_id = $1
		  AND ($2 = '' OR ba.status = $2)
		ORDER BY ba.created_at DESC, ba.id DESC
		LIMIT $3
		OFFSET $4
	`, salonID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BookingAttempt, 0)
	for rows.Next() {
		var item BookingAttempt
		var processingLease sql.NullTime
		var supersededAt sql.NullTime
		var reconciliationResolvedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.Source,
			&item.Status,
			&item.POSProvider,
			&item.POSBookingID,
			&item.POSBookingVersion,
			&item.SchedulingAuthority,
			&item.AuthorityProvider,
			&item.AuthorityAppointmentID,
			&item.AuthorityAppointmentVersion,
			&item.TargetAuthorityAppointmentVersion,
			&item.AuthorityIdempotencyKey,
			&item.AuthorityLocationID,
			&item.AuthoritySnapshotGeneration,
			&item.OperationKey,
			&item.RetryOfAttemptID,
			&item.SupersededByAttemptID,
			&item.RetrySequence,
			&supersededAt,
			&item.AvailabilityQuoteID,
			&item.SlotFingerprint,
			&item.ProviderFence.LocationID,
			&item.ProviderFence.SnapshotGeneration,
			&item.OperationType,
			&item.ProviderOutcome,
			&item.RetryPolicy,
			&item.Reconciliation,
			&item.ReconciliationResolution,
			&reconciliationResolvedAt,
			&processingLease,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.RequestedStartTime,
			&item.RequestedEndTime,
			&item.Notes,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.TargetAppointmentID,
			&item.NotificationType,
			&item.NotificationStatus,
		); err != nil {
			return nil, err
		}
		if processingLease.Valid {
			value := processingLease.Time
			item.ProcessingLeaseEnds = &value
		}
		if supersededAt.Valid {
			value := supersededAt.Time
			item.SupersededAt = &value
		}
		if reconciliationResolvedAt.Valid {
			value := reconciliationResolvedAt.Time
			item.ReconciliationResolvedAt = &value
		}
		item.BookingAction = bookingActionForAttempt(item.Status, item.NotificationType)
		if item.OperationType != "" {
			item.BookingAction = item.OperationType
		}
		annotateBookingAttemptPolicy(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachBookingAttemptSegments(ctx, items); err != nil {
		return nil, err
	}
	if err := r.attachAttemptTargetAppointments(ctx, salonID, items); err != nil {
		return nil, err
	}
	attempts := make([]*BookingAttempt, 0, len(items))
	for index := range items {
		attempts = append(attempts, &items[index])
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, attempts...); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListReconciliationTasks(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]ReconciliationTask, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT task.id::text, task.salon_id::text, task.booking_attempt_id::text,
		       task.status, COALESCE(task.resolution, ''), COALESCE(task.resolution_note, ''),
		       task.resolved_at, task.created_at, task.updated_at,
		       attempt.status, attempt.pos_provider, COALESCE(attempt.pos_booking_id, ''), COALESCE(attempt.pos_booking_version, 0),
		       attempt.scheduling_authority, COALESCE(attempt.authority_provider, ''),
		       COALESCE(attempt.authority_appointment_id, ''), COALESCE(attempt.authority_appointment_version, 0),
		       COALESCE(attempt.target_authority_appointment_version, 0),
		       attempt.operation_type, attempt.provider_outcome, attempt.retry_policy,
		       attempt.reconciliation_status, COALESCE(attempt.target_appointment_id::text, ''),
		       attempt.customer_name, attempt.customer_phone, COALESCE(attempt.customer_email, ''),
		       COALESCE(attempt.service_id::text, ''), COALESCE(attempt.staff_id::text, ''), attempt.staff_selection_mode,
		       attempt.requested_start_time, attempt.requested_end_time,
		       COALESCE(attempt.error_code, ''), COALESCE(attempt.error_message, '')
			FROM booking_reconciliation_tasks task
			JOIN booking_attempts attempt ON attempt.id = task.booking_attempt_id
			JOIN salons salon ON salon.id = task.salon_id
			WHERE task.salon_id = $1
			  AND salon.owner_user_id = $2
			  AND task.status = $3
			  AND attempt.superseded_at IS NULL
			ORDER BY task.created_at ASC, task.id ASC
		LIMIT $4 OFFSET $5
	`, salonID, ownerUserID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ReconciliationTask, 0)
	for rows.Next() {
		item, err := scanReconciliationTask(rows, salonID)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	attempts := make([]*BookingAttempt, 0, len(items))
	for index := range items {
		attempts = append(attempts, items[index].Attempt)
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, attempts...); err != nil {
		return nil, err
	}
	return items, nil
}

type reconciliationTaskScanner interface {
	Scan(dest ...any) error
}

func scanReconciliationTask(scanner reconciliationTaskScanner, salonID string) (*ReconciliationTask, error) {
	var item ReconciliationTask
	var resolvedAt sql.NullTime
	attempt := &BookingAttempt{SalonID: salonID}
	if err := scanner.Scan(
		&item.ID, &item.SalonID, &item.BookingAttemptID, &item.Status, &item.Resolution,
		&item.ResolutionNote, &resolvedAt, &item.CreatedAt, &item.UpdatedAt,
		&attempt.Status, &attempt.POSProvider, &attempt.POSBookingID, &attempt.POSBookingVersion,
		&attempt.SchedulingAuthority, &attempt.AuthorityProvider, &attempt.AuthorityAppointmentID,
		&attempt.AuthorityAppointmentVersion, &attempt.TargetAuthorityAppointmentVersion, &attempt.OperationType,
		&attempt.ProviderOutcome, &attempt.RetryPolicy, &attempt.Reconciliation,
		&attempt.TargetAppointmentID, &attempt.CustomerName, &attempt.CustomerPhone, &attempt.CustomerEmail,
		&attempt.ServiceID, &attempt.StaffID, &attempt.StaffSelectionMode, &attempt.RequestedStartTime,
		&attempt.RequestedEndTime, &attempt.ErrorCode, &attempt.ErrorMessage,
	); err != nil {
		return nil, err
	}
	attempt.ID = item.BookingAttemptID
	annotateBookingAttemptPolicy(attempt)
	item.Attempt = attempt
	if resolvedAt.Valid {
		value := resolvedAt.Time
		item.ResolvedAt = &value
	}
	return &item, nil
}

func (r *Repository) getReconciliationTaskForOwner(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*ReconciliationTask, error) {
	item, err := scanReconciliationTask(r.db.QueryRowContext(ctx, `
		SELECT task.id::text, task.salon_id::text, task.booking_attempt_id::text,
		       task.status, COALESCE(task.resolution, ''), COALESCE(task.resolution_note, ''),
		       task.resolved_at, task.created_at, task.updated_at,
		       attempt.status, attempt.pos_provider, COALESCE(attempt.pos_booking_id, ''), COALESCE(attempt.pos_booking_version, 0),
		       attempt.scheduling_authority, COALESCE(attempt.authority_provider, ''),
		       COALESCE(attempt.authority_appointment_id, ''), COALESCE(attempt.authority_appointment_version, 0),
		       COALESCE(attempt.target_authority_appointment_version, 0),
		       attempt.operation_type, attempt.provider_outcome, attempt.retry_policy,
		       attempt.reconciliation_status, COALESCE(attempt.target_appointment_id::text, ''),
		       attempt.customer_name, attempt.customer_phone, COALESCE(attempt.customer_email, ''),
		       COALESCE(attempt.service_id::text, ''), COALESCE(attempt.staff_id::text, ''), attempt.staff_selection_mode,
		       attempt.requested_start_time, attempt.requested_end_time,
		       COALESCE(attempt.error_code, ''), COALESCE(attempt.error_message, '')
		FROM booking_reconciliation_tasks task
		JOIN booking_attempts attempt ON attempt.id = task.booking_attempt_id
		JOIN salons salon ON salon.id = task.salon_id
		WHERE task.salon_id = $1
		  AND task.booking_attempt_id = $2
		  AND salon.owner_user_id = $3
	`, salonID, attemptID, ownerUserID), salonID)
	if err != nil {
		return nil, err
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, item.Attempt); err != nil {
		return nil, err
	}
	return item, nil
}

type reconciliationAttemptMatchRecord struct {
	BookingAttemptID        string
	TaskStatus              string
	AttemptStatus           string
	Reconciliation          string
	OperationType           string
	Provider                string
	POSBookingID            string
	TargetAppointmentID     string
	TargetPOSBookingVersion int
	CustomerPhone           string
	ServiceID               string
	StartTime               time.Time
	EndTime                 time.Time
	Superseded              bool
}

type verifiedReconciliationCandidate struct {
	ReconciliationCandidate
	BookingAttemptID           string
	ProviderLocationID         string
	ProviderSnapshotGeneration int64
	AppointmentStatus          string
}

func (r *Repository) ListReconciliationCandidates(ctx context.Context, salonID string, ownerUserID string, attemptID string) ([]ReconciliationCandidate, error) {
	var attempt reconciliationAttemptMatchRecord
	if err := r.db.QueryRowContext(ctx, `
			SELECT attempt.id::text, task.status, attempt.status, attempt.reconciliation_status,
			       attempt.operation_type, attempt.pos_provider, COALESCE(attempt.pos_booking_id, ''),
			       COALESCE(attempt.target_appointment_id::text, ''),
			       COALESCE(attempt.target_pos_booking_version, 0),
			       attempt.customer_phone, COALESCE(attempt.service_id::text, ''),
			       attempt.requested_start_time, attempt.requested_end_time,
			       attempt.superseded_at IS NOT NULL
		FROM booking_reconciliation_tasks task
		JOIN booking_attempts attempt ON attempt.id = task.booking_attempt_id
		JOIN salons salon ON salon.id = task.salon_id
		WHERE task.salon_id = $1
		  AND task.booking_attempt_id = $2
		  AND salon.owner_user_id = $3
	`, salonID, attemptID, ownerUserID).Scan(
		&attempt.BookingAttemptID, &attempt.TaskStatus, &attempt.AttemptStatus, &attempt.Reconciliation,
		&attempt.OperationType, &attempt.Provider, &attempt.POSBookingID, &attempt.TargetAppointmentID,
		&attempt.TargetPOSBookingVersion, &attempt.CustomerPhone, &attempt.ServiceID, &attempt.StartTime, &attempt.EndTime,
		&attempt.Superseded,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	if attempt.Superseded || attempt.TaskStatus == "resolved" || attempt.Reconciliation != ReconciliationRequired {
		return nil, ErrOperationConflict
	}
	verified, err := queryVerifiedReconciliationCandidates(ctx, r.db, salonID, attempt, attempt.POSBookingID, false)
	if err != nil {
		return nil, err
	}
	items := make([]ReconciliationCandidate, 0, len(verified))
	for _, candidate := range verified {
		items = append(items, candidate.ReconciliationCandidate)
	}
	return items, nil
}

type reconciliationCandidateQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryVerifiedReconciliationCandidates(ctx context.Context, queryer reconciliationCandidateQueryer, salonID string, attempt reconciliationAttemptMatchRecord, providerAppointmentID string, forUpdate bool) ([]verifiedReconciliationCandidate, error) {
	query := `
		SELECT appointment.id::text, appointment.booking_attempt_id::text,
		       origin.provider_location_id, origin.provider_snapshot_generation,
		       appointment.pos_provider, appointment.pos_appointment_id,
		       COALESCE(appointment.pos_appointment_version, 0), appointment.status,
		       appointment.customer_name, appointment.customer_phone,
		       COALESCE(appointment.customer_email, ''),
		       COALESCE(appointment.service_id::text, ''),
		       COALESCE(appointment.staff_id::text, ''),
		       appointment.start_time, appointment.end_time
		FROM appointments appointment
		JOIN booking_attempts origin
		  ON origin.id = appointment.booking_attempt_id
		 AND origin.salon_id = appointment.salon_id
		 AND origin.pos_provider = appointment.pos_provider
		 AND NULLIF(BTRIM(origin.provider_location_id), '') IS NOT NULL
		 AND origin.provider_snapshot_generation IS NOT NULL
		 AND origin.provider_snapshot_generation > 0
		WHERE appointment.salon_id = $1
		  AND appointment.pos_provider = $2
		  AND appointment.pos_sync_status = 'synced'
		  AND ($8 = '' OR appointment.pos_appointment_id = $8)
		  AND EXISTS (
		      SELECT 1
		      FROM appointment_services segment
		      WHERE segment.appointment_id = appointment.id
		  )
		  AND EXISTS (
		      SELECT 1
		      FROM booking_attempt_segments segment
		      WHERE segment.booking_attempt_id = $9
		  )
		  AND COALESCE((
			      SELECT jsonb_agg(jsonb_build_array(
			          COALESCE(segment.service_id::text, ''),
			          COALESCE(segment.staff_id::text, '')
			      ) ORDER BY segment.sort_order, segment.id)
			      FROM appointment_services segment
			      WHERE segment.appointment_id = appointment.id
			  ), '[]'::jsonb) = COALESCE((
			      SELECT jsonb_agg(jsonb_build_array(
			          COALESCE(segment.service_id::text, ''),
			          COALESCE(segment.staff_id::text, '')
			      ) ORDER BY segment.sort_order, segment.id)
			      FROM booking_attempt_segments segment
			      WHERE segment.booking_attempt_id = $9
			  ), '[]'::jsonb)
		  AND (
		      ($3 = 'book'
		       AND appointment.status IN ('confirmed', 'rescheduled')
		       AND appointment.start_time = $5
		       AND appointment.end_time = $6
		       AND COALESCE(appointment.service_id::text, '') = $7)
			      OR ($3 = 'reschedule'
			          AND appointment.id::text = $4
			          AND appointment.status IN ('confirmed', 'rescheduled')
			          AND appointment.start_time = $5
			          AND appointment.end_time = $6)
		      OR ($3 = 'cancel'
		          AND appointment.id::text = $4
		          AND appointment.status = 'cancelled')
		  )
		ORDER BY appointment.start_time ASC, appointment.id ASC
	`
	if forUpdate {
		query += " FOR UPDATE OF appointment"
	}
	rows, err := queryer.QueryContext(ctx, query, salonID, attempt.Provider, attempt.OperationType,
		attempt.TargetAppointmentID, attempt.StartTime, attempt.EndTime, attempt.ServiceID,
		providerAppointmentID, attempt.BookingAttemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]verifiedReconciliationCandidate, 0)
	for rows.Next() {
		var candidate verifiedReconciliationCandidate
		if err := rows.Scan(
			&candidate.AppointmentID, &candidate.BookingAttemptID,
			&candidate.ProviderLocationID, &candidate.ProviderSnapshotGeneration, &candidate.Provider,
			&candidate.ProviderAppointmentID, &candidate.ProviderAppointmentVersion,
			&candidate.AppointmentStatus, &candidate.CustomerName, &candidate.CustomerPhone,
			&candidate.CustomerEmail, &candidate.ServiceID, &candidate.StaffID,
			&candidate.StartTime, &candidate.EndTime,
		); err != nil {
			return nil, err
		}
		if !reconciliationCandidateMatchesAttempt(attempt, candidate) {
			continue
		}
		candidate.ProviderStatus = reconciliationCandidateProviderStatus(candidate.AppointmentStatus)
		items = append(items, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func reconciliationCandidateMatchesAttempt(attempt reconciliationAttemptMatchRecord, candidate verifiedReconciliationCandidate) bool {
	switch attempt.OperationType {
	case BookingActionBook:
		if candidate.AppointmentStatus != StatusConfirmed && candidate.AppointmentStatus != StatusRescheduled {
			return false
		}
		if !candidate.StartTime.Equal(attempt.StartTime) || !candidate.EndTime.Equal(attempt.EndTime) || strings.TrimSpace(attempt.ServiceID) == "" || candidate.ServiceID != attempt.ServiceID {
			return false
		}
		if attempt.POSBookingID != "" {
			return candidate.ProviderAppointmentID == attempt.POSBookingID
		}
		attemptPhone := validation.NormalizePhone(attempt.CustomerPhone)
		candidatePhone := validation.NormalizePhone(candidate.CustomerPhone)
		return attemptPhone != "" && candidatePhone != "" && attemptPhone == candidatePhone
	case BookingActionReschedule:
		return candidate.AppointmentID == attempt.TargetAppointmentID &&
			(candidate.AppointmentStatus == StatusConfirmed || candidate.AppointmentStatus == StatusRescheduled) &&
			candidate.ProviderAppointmentVersion > attempt.TargetPOSBookingVersion &&
			candidate.StartTime.Equal(attempt.StartTime) &&
			candidate.EndTime.Equal(attempt.EndTime)
	case BookingActionCancel:
		return candidate.AppointmentID == attempt.TargetAppointmentID &&
			candidate.AppointmentStatus == StatusCancelled &&
			candidate.ProviderAppointmentVersion > attempt.TargetPOSBookingVersion
	default:
		return false
	}
}

func reconciliationCandidateProviderStatus(appointmentStatus string) string {
	switch appointmentStatus {
	case StatusConfirmed, StatusRescheduled:
		return string(pos.AppointmentStatusAccepted)
	case StatusCancelled:
		return string(pos.AppointmentStatusCancelled)
	default:
		return string(pos.AppointmentStatusUnknown)
	}
}

func reconciliationNotCreatedBlockedByAttempt(attemptStatus string, operationType string, posBookingID string) bool {
	if attemptStatus == StatusProviderPending {
		return true
	}
	return operationType == BookingActionBook && strings.TrimSpace(posBookingID) != ""
}

func (r *Repository) ResolveReconciliationTask(ctx context.Context, salonID string, ownerUserID string, attemptID string, req ResolveReconciliationRequest) (*ReconciliationTask, error) {
	if strings.TrimSpace(req.ActionKey) == "" || strings.TrimSpace(req.PayloadFingerprint) == "" {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	type reconciliationAttempt struct {
		TaskID                  string
		TaskStatus              string
		AttemptStatus           string
		OperationType           string
		Provider                string
		POSBookingID            string
		TargetAppointmentID     string
		TargetPOSBookingVersion int
		CustomerName            string
		CustomerPhone           string
		CustomerEmail           string
		ServiceID               string
		StaffID                 string
		StaffSelectionMode      string
		StartTime               time.Time
		EndTime                 time.Time
		Notes                   string
		Superseded              bool
	}
	var item reconciliationAttempt
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt.status, attempt.operation_type, attempt.pos_provider,
		       COALESCE(attempt.pos_booking_id, ''),
		       COALESCE(attempt.target_appointment_id::text, ''),
		       COALESCE(attempt.target_pos_booking_version, 0), attempt.customer_name,
		       attempt.customer_phone, COALESCE(attempt.customer_email, ''),
		       COALESCE(attempt.service_id::text, ''), COALESCE(attempt.staff_id::text, ''),
		       attempt.staff_selection_mode, attempt.requested_start_time,
		       attempt.requested_end_time, COALESCE(attempt.notes, ''),
		       attempt.superseded_at IS NOT NULL
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		WHERE attempt.salon_id = $1
		  AND attempt.id = $2
		  AND salon.owner_user_id = $3
		FOR UPDATE OF attempt
	`, salonID, attemptID, ownerUserID).Scan(
		&item.AttemptStatus, &item.OperationType, &item.Provider, &item.POSBookingID,
		&item.TargetAppointmentID, &item.TargetPOSBookingVersion, &item.CustomerName, &item.CustomerPhone,
		&item.CustomerEmail, &item.ServiceID, &item.StaffID, &item.StaffSelectionMode,
		&item.StartTime, &item.EndTime, &item.Notes, &item.Superseded,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM booking_reconciliation_tasks
		WHERE salon_id = $1
		  AND booking_attempt_id = $2
		FOR UPDATE
	`, salonID, attemptID).Scan(&item.TaskID, &item.TaskStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}
	if item.Superseded {
		return nil, ErrOperationConflict
	}
	var existingPayloadFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT payload_fingerprint
		FROM booking_reconciliation_events
		WHERE reconciliation_task_id = $1
		  AND action_key = $2
	`, item.TaskID, req.ActionKey).Scan(&existingPayloadFingerprint)
	if err == nil {
		if existingPayloadFingerprint != req.PayloadFingerprint {
			return nil, ErrOperationConflict
		}
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		return r.getReconciliationTaskForOwner(ctx, salonID, ownerUserID, attemptID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if item.TaskStatus == "resolved" {
		return nil, ErrOperationConflict
	}

	taskStatus := "resolved"
	resolution := req.Action
	providerStatus := pos.NormalizeAppointmentStatus(req.ProviderStatus)
	switch req.Action {
	case ReconciliationActionNotCreated:
		if reconciliationNotCreatedBlockedByAttempt(item.AttemptStatus, item.OperationType, item.POSBookingID) {
			return nil, ErrOperationConflict
		}
		matches, err := queryVerifiedReconciliationCandidates(ctx, tx, salonID, reconciliationAttemptMatchRecord{
			BookingAttemptID:        attemptID,
			TaskStatus:              item.TaskStatus,
			AttemptStatus:           item.AttemptStatus,
			Reconciliation:          ReconciliationRequired,
			OperationType:           item.OperationType,
			Provider:                item.Provider,
			POSBookingID:            item.POSBookingID,
			TargetAppointmentID:     item.TargetAppointmentID,
			TargetPOSBookingVersion: item.TargetPOSBookingVersion,
			CustomerPhone:           item.CustomerPhone,
			ServiceID:               item.ServiceID,
			StartTime:               item.StartTime,
			EndTime:                 item.EndTime,
		}, item.POSBookingID, true)
		if err != nil {
			return nil, err
		}
		if len(matches) != 0 {
			return nil, ErrOperationConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_attempts
			SET status = $1, provider_outcome = $2, retry_policy = $3,
			    reconciliation_status = $4, reconciliation_resolution = $5,
			    reconciliation_resolved_at = now(), updated_at = now()
			WHERE id = $6 AND salon_id = $7 AND reconciliation_status = $8
		`, StatusFallbackPending, ProviderOutcomeFailed, RetryPolicySafe, ReconciliationResolved,
			ReconciliationActionNotCreated, attemptID, salonID, ReconciliationRequired)
		if err := requireExactlyOneRow(result, err); err != nil {
			return nil, err
		}
	case ReconciliationActionEscalated:
		taskStatus = "escalated"
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_attempts
			SET reconciliation_resolution = $1, updated_at = now()
			WHERE id = $2 AND salon_id = $3 AND reconciliation_status = $4
		`, ReconciliationActionEscalated, attemptID, salonID, ReconciliationRequired)
		if err := requireExactlyOneRow(result, err); err != nil {
			return nil, err
		}
	case ReconciliationActionProviderAttached:
		if item.OperationType == BookingActionCancel && providerStatus != pos.AppointmentStatusCancelled {
			return nil, ErrValidation
		}
		if item.OperationType != BookingActionCancel && providerStatus != pos.AppointmentStatusAccepted {
			return nil, ErrValidation
		}
		matches, err := queryVerifiedReconciliationCandidates(ctx, tx, salonID, reconciliationAttemptMatchRecord{
			BookingAttemptID:        attemptID,
			TaskStatus:              item.TaskStatus,
			AttemptStatus:           item.AttemptStatus,
			Reconciliation:          ReconciliationRequired,
			OperationType:           item.OperationType,
			Provider:                item.Provider,
			POSBookingID:            item.POSBookingID,
			TargetAppointmentID:     item.TargetAppointmentID,
			TargetPOSBookingVersion: item.TargetPOSBookingVersion,
			CustomerPhone:           item.CustomerPhone,
			ServiceID:               item.ServiceID,
			StartTime:               item.StartTime,
			EndTime:                 item.EndTime,
		}, req.ProviderAppointmentID, true)
		if err != nil {
			return nil, err
		}
		if len(matches) != 1 {
			return nil, ErrOperationConflict
		}
		mirror := matches[0]
		if mirror.ProviderAppointmentVersion != req.ProviderAppointmentVersion || mirror.ProviderStatus != string(providerStatus) {
			return nil, ErrOperationConflict
		}
		finalStatus := StatusConfirmed
		if item.OperationType == BookingActionReschedule {
			finalStatus = StatusRescheduled
		} else if item.OperationType == BookingActionCancel {
			finalStatus = StatusCancelled
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE booking_attempts
			SET status = $1, pos_booking_id = $2, pos_booking_version = $3,
			    authority_appointment_id = $2, authority_appointment_version = $3,
			    provider_outcome = $4,
			    retry_policy = $5, reconciliation_status = $6,
			    reconciliation_resolution = $7, reconciliation_resolved_at = now(),
			    provider_location_id = COALESCE(provider_location_id, $8),
			    provider_snapshot_generation = COALESCE(provider_snapshot_generation, $9),
			    authority_location_id = COALESCE(authority_location_id, $8),
			    authority_snapshot_generation = COALESCE(authority_snapshot_generation, $9),
			    error_code = NULL, error_message = NULL, updated_at = now()
			WHERE id = $10 AND salon_id = $11 AND reconciliation_status = $12
			  AND (
			      (provider_location_id IS NULL AND provider_snapshot_generation IS NULL)
			      OR (
			          provider_location_id = $8
			          AND provider_snapshot_generation IS NOT NULL
			          AND provider_snapshot_generation > 0
			      )
			  )
		`, finalStatus, mirror.ProviderAppointmentID, mirror.ProviderAppointmentVersion, ProviderOutcomeSucceeded, RetryPolicyNone,
			ReconciliationResolved, ReconciliationActionProviderAttached, mirror.ProviderLocationID, mirror.ProviderSnapshotGeneration, attemptID, salonID,
			ReconciliationRequired)
		if err := requireExactlyOneRow(result, err); err != nil {
			return nil, err
		}
		if item.OperationType == BookingActionBook && mirror.BookingAttemptID != attemptID {
			result, err := tx.ExecContext(ctx, `
				UPDATE booking_attempts
				SET superseded_by_attempt_id = $1,
				    superseded_at = COALESCE(superseded_at, now()),
				    updated_at = now()
				WHERE id = $2
				  AND salon_id = $3
				  AND (superseded_by_attempt_id IS NULL OR superseded_by_attempt_id = $1)
				`, attemptID, mirror.BookingAttemptID, salonID)
			if err := requireExactlyOneRow(result, err); err != nil {
				return nil, err
			}
			if err := closeSupersededReconciliationTaskTx(ctx, tx, salonID, mirror.BookingAttemptID, attemptID); err != nil {
				return nil, err
			}
			result, err = tx.ExecContext(ctx, `
				UPDATE appointments
				SET booking_attempt_id = $1, updated_at = now()
				WHERE id = $2 AND salon_id = $3 AND pos_sync_status = 'synced'
				`, attemptID, mirror.AppointmentID, salonID)
			if err := requireExactlyOneRow(result, err); err != nil {
				return nil, err
			}
		}
	default:
		return nil, ErrValidation
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE booking_reconciliation_tasks
		SET status = $1, resolution = $2, resolved_by_user_id = $3,
		    resolution_note = NULLIF($4, ''),
		    resolved_at = CASE WHEN $1 = 'resolved' THEN now() ELSE NULL END,
		    updated_at = now()
		WHERE id = $5
	`, taskStatus, resolution, ownerUserID, req.Note, item.TaskID)
	if err := requireExactlyOneRow(result, err); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_events (
			salon_id, booking_attempt_id, reconciliation_task_id, actor_user_id,
			action_key, payload_fingerprint, action, provider_appointment_id,
			provider_appointment_version, provider_status, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, ''), NULLIF($11, ''))
	`, salonID, attemptID, item.TaskID, ownerUserID, req.ActionKey, req.PayloadFingerprint, req.Action,
		req.ProviderAppointmentID, req.ProviderAppointmentVersion, string(providerStatus), req.Note); err != nil {
		return nil, err
	}
	payload := ownerNotificationPayload("booking_reconciliation_resolved", salonID, attemptID, "", map[string]any{
		"action":      req.Action,
		"action_key":  req.ActionKey,
		"task_status": taskStatus,
		"resolution":  resolution,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (
			salon_id, booking_attempt_id, type, status, title, message, dedupe_key, payload
		)
		VALUES ($1, $2, 'booking_reconciliation_resolved', 'pending', $3, $4, $5, $6::jsonb)
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, salonID, attemptID, "Booking reconciliation updated", defaultString(req.Note, "The booking reconciliation task was updated."), "booking-reconciliation-resolution:"+attemptID+":"+req.ActionKey, payload); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getReconciliationTaskForOwner(ctx, salonID, ownerUserID, attemptID)
}

func (r *Repository) ListCalendarAppointments(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]Appointment, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
		       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
		       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
		       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
		       COALESCE(confirmation_source, ''), status, customer_name, customer_phone, COALESCE(customer_email, ''), COALESCE(service_id::text, ''),
		       COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''),
		       COALESCE(pos_sync_status, ''), last_pos_synced_at, COALESCE(pos_sync_error, ''), created_at, updated_at
		FROM appointments
		WHERE salon_id = $1
		  AND start_time < $3
		  AND end_time > $2
		ORDER BY start_time ASC, id ASC
	`, salonID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Appointment, 0)
	for rows.Next() {
		var item Appointment
		var lastPOSSyncedAt sql.NullTime
		var confirmedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.BookingAttemptID,
			&item.POSProvider,
			&item.POSAppointmentID,
			&item.POSAppointmentVersion,
			&item.SchedulingAuthority,
			&item.AuthorityProvider,
			&item.AuthorityAppointmentID,
			&item.AuthorityAppointmentVersion,
			&item.AuthorityCustomerID,
			&confirmedAt,
			&item.ConfirmedByUserID,
			&item.ConfirmationSource,
			&item.Status,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.StartTime,
			&item.EndTime,
			&item.Notes,
			&item.POSSyncStatus,
			&lastPOSSyncedAt,
			&item.POSSyncError,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastPOSSyncedAt.Valid {
			item.LastPOSSyncedAt = &lastPOSSyncedAt.Time
		}
		if confirmedAt.Valid {
			item.ConfirmedAt = &confirmedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachAppointmentSegments(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListCalendarPendingRequests(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]BookingAttempt, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ba.id::text, ba.salon_id::text, ba.source, ba.status, COALESCE(ba.pos_provider, ''), COALESCE(ba.pos_booking_id, ''),
		       COALESCE(ba.pos_booking_version, 0), ba.scheduling_authority, COALESCE(ba.authority_provider, ''),
		       COALESCE(ba.authority_appointment_id, ''), COALESCE(ba.authority_appointment_version, 0),
		       COALESCE(ba.target_authority_appointment_version, 0),
		       COALESCE(ba.operation_type, 'book'), COALESCE(ba.provider_outcome, 'not_started'),
		       COALESCE(ba.retry_policy, 'none'), COALESCE(ba.reconciliation_status, 'not_required'), ba.processing_lease_expires_at,
		       ba.customer_name, ba.customer_phone, COALESCE(ba.customer_email, ''), COALESCE(ba.service_id::text, ''),
		       COALESCE(ba.staff_id::text, ''), COALESCE(ba.staff_selection_mode, 'specific'), ba.requested_start_time, ba.requested_end_time, COALESCE(ba.notes, ''),
		       COALESCE(ba.error_code, ''), COALESCE(ba.error_message, ''), ba.created_at, ba.updated_at,
		       COALESCE(ba.target_appointment_id::text, note.appointment_id::text, ''), COALESCE(note.type, ''), COALESCE(note.status, '')
		FROM booking_attempts ba
		LEFT JOIN LATERAL (
			SELECT appointment_id, type, status
			FROM owner_notifications
			WHERE booking_attempt_id = ba.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) note ON TRUE
		WHERE ba.salon_id = $1
		  AND ba.status IN ('fallback_pending', 'pos_pending', 'provider_pending')
		  AND ba.requested_start_time < $3
		  AND ba.requested_end_time > $2
		ORDER BY ba.requested_start_time ASC, ba.id ASC
	`, salonID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BookingAttempt, 0)
	for rows.Next() {
		var item BookingAttempt
		var processingLease sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.Source,
			&item.Status,
			&item.POSProvider,
			&item.POSBookingID,
			&item.POSBookingVersion,
			&item.SchedulingAuthority,
			&item.AuthorityProvider,
			&item.AuthorityAppointmentID,
			&item.AuthorityAppointmentVersion,
			&item.TargetAuthorityAppointmentVersion,
			&item.OperationType,
			&item.ProviderOutcome,
			&item.RetryPolicy,
			&item.Reconciliation,
			&processingLease,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.RequestedStartTime,
			&item.RequestedEndTime,
			&item.Notes,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.TargetAppointmentID,
			&item.NotificationType,
			&item.NotificationStatus,
		); err != nil {
			return nil, err
		}
		if processingLease.Valid {
			value := processingLease.Time
			item.ProcessingLeaseEnds = &value
		}
		item.BookingAction = bookingActionForAttempt(item.Status, item.NotificationType)
		if item.OperationType != "" {
			item.BookingAction = item.OperationType
		}
		annotateBookingAttemptPolicy(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachBookingAttemptSegments(ctx, items); err != nil {
		return nil, err
	}
	if err := r.attachAttemptTargetAppointments(ctx, salonID, items); err != nil {
		return nil, err
	}
	attempts := make([]*BookingAttempt, 0, len(items))
	for index := range items {
		attempts = append(attempts, &items[index])
	}
	if err := r.annotateBookingAttemptPoliciesCurrent(ctx, attempts...); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListCalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor CalendarEventCursor, limit int) ([]CalendarEvent, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT n.id::text, n.salon_id::text, n.type, n.status, n.title, n.message,
		       COALESCE(n.booking_attempt_id::text, ''), COALESCE(n.appointment_id::text, ''),
		       COALESCE(ba.source, ''), COALESCE(ba.status, ''), COALESCE(ba.customer_name, ''),
		       COALESCE(ba.service_id::text, ''), COALESCE(ba.staff_id::text, ''),
		       COALESCE(a.start_time, ba.requested_start_time, n.created_at),
		       COALESCE(a.end_time, ba.requested_end_time, n.created_at),
		       n.created_at
		FROM owner_notifications n
		LEFT JOIN booking_attempts ba
		  ON ba.id = n.booking_attempt_id
		 AND ba.salon_id = n.salon_id
		LEFT JOIN appointments a
		  ON a.id = n.appointment_id
		 AND a.salon_id = n.salon_id
		WHERE n.salon_id = $1
		  AND n.type IN ($2, $3)
		  AND (
		        n.created_at > $4
		        OR (n.created_at = $4 AND n.id::text > $5)
		      )
		ORDER BY n.created_at ASC, n.id::text ASC
		LIMIT $6
	`, salonID, NotificationTypeBookingConfirmed, NotificationTypeBookingFallback, cursor.CreatedAt, cursor.ID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CalendarEvent, 0)
	for rows.Next() {
		var item CalendarEvent
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.Type,
			&item.NotificationStatus,
			&item.Title,
			&item.Message,
			&item.BookingAttemptID,
			&item.AppointmentID,
			&item.Source,
			&item.BookingStatus,
			&item.CustomerName,
			&item.ServiceID,
			&item.StaffID,
			&item.StartTime,
			&item.EndTime,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) UpsertCalendarAppointments(ctx context.Context, salonID string, provider string, fence pos.ProviderFence, items []CalendarAppointmentImport) (CalendarSyncSummary, error) {
	summary := CalendarSyncSummary{}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, tx, salonID); err != nil {
		return summary, err
	}
	if err := lockAndValidateCalendarProviderFenceTx(ctx, tx, salonID, provider, fence); err != nil {
		return summary, err
	}

	for _, item := range items {
		if strings.TrimSpace(item.POSAppointmentID) == "" || item.StartTime.IsZero() || !item.EndTime.After(item.StartTime) {
			summary.Skipped++
			continue
		}
		item.Provider = defaultString(strings.TrimSpace(item.Provider), provider)
		item.Status = normalizeImportedAppointmentStatus(item.Status)
		if item.POSAppointmentVersion < 0 {
			item.POSAppointmentVersion = 0
		}
		var currentAppointmentVersion int
		err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(pos_appointment_version, 0)
			FROM appointments
			WHERE salon_id = $1
			  AND scheduling_authority = 'external_provider'
			  AND pos_provider = $2
			  AND pos_appointment_id = $3
			FOR UPDATE
		`, salonID, item.Provider, item.POSAppointmentID).Scan(&currentAppointmentVersion)
		if errors.Is(err, sql.ErrNoRows) {
			var protectedAppointmentID string
			collisionErr := tx.QueryRowContext(ctx, `
				SELECT id::text
				FROM appointments
				WHERE salon_id = $1
				  AND scheduling_authority <> 'external_provider'
				  AND pos_provider = $2
				  AND pos_appointment_id = $3
				FOR UPDATE
			`, salonID, item.Provider, item.POSAppointmentID).Scan(&protectedAppointmentID)
			if collisionErr == nil {
				summary.Skipped++
				continue
			}
			if !errors.Is(collisionErr, sql.ErrNoRows) {
				return summary, collisionErr
			}
		}
		if err == nil && item.POSAppointmentVersion < currentAppointmentVersion {
			summary.Skipped++
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return summary, err
		}
		if strings.TrimSpace(item.POSCustomerID) != "" {
			var customerName string
			var customerPhone string
			var customerEmail string
			err := tx.QueryRowContext(ctx, `
				SELECT customer.name, COALESCE(customer.phone, ''), COALESCE(customer.email, '')
				FROM pos_entity_links link
				JOIN customers customer
				  ON customer.id = link.entity_id
				 AND customer.salon_id = link.salon_id
				WHERE link.salon_id = $1
				  AND link.entity_type = $2
				  AND link.provider = $3
				  AND link.provider_entity_id = $4
				  AND link.sync_status = $5
				  AND customer.active = true
				  AND customer.archived_at IS NULL
				LIMIT 1
			`, salonID, pos.EntityTypeCustomer, item.Provider, item.POSCustomerID, pos.SyncStatusSynced).Scan(&customerName, &customerPhone, &customerEmail)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return summary, err
			}
			if err == nil {
				item = mergeCalendarCustomerDetails(item, customerName, customerPhone, customerEmail)
			}
		}
		item.CustomerName = defaultString(strings.TrimSpace(item.CustomerName), "Square customer")
		item.CustomerPhone = validation.NormalizePhone(item.CustomerPhone)
		segments, err := r.resolveCalendarImportSegments(ctx, tx, salonID, item.Provider, item)
		if err != nil {
			return summary, err
		}
		primary := segments[0]
		providerOutcome, retryPolicy, reconciliation := calendarImportAttemptPolicy(item.Status)
		mirrorReconciliation := reconciliation
		var existingAppointmentID string
		var existingAttemptID string
		var existingAppointmentVersion int
		var existingOriginLocationID string
		err = tx.QueryRowContext(ctx, `
			SELECT appointment.id::text, appointment.booking_attempt_id::text,
			       COALESCE(appointment.pos_appointment_version, 0),
			       COALESCE(origin.provider_location_id, '')
			FROM appointments appointment
			JOIN booking_attempts origin ON origin.id = appointment.booking_attempt_id
			WHERE appointment.salon_id = $1
			  AND appointment.scheduling_authority = 'external_provider'
			  AND origin.scheduling_authority = 'external_provider'
			  AND appointment.pos_provider = $2
			  AND appointment.pos_appointment_id = $3
			FOR UPDATE OF appointment, origin
			`, salonID, item.Provider, item.POSAppointmentID).Scan(&existingAppointmentID, &existingAttemptID, &existingAppointmentVersion, &existingOriginLocationID)
		if errors.Is(err, sql.ErrNoRows) {
			var existingAttemptVersion int
			err = tx.QueryRowContext(ctx, `
				SELECT id::text, COALESCE(pos_booking_version, 0)
				FROM booking_attempts
				WHERE salon_id = $1
				  AND scheduling_authority = 'external_provider'
				  AND pos_provider = $2
				  AND pos_booking_id = $3
				  AND operation_type = $4
				  AND reconciliation_status = $5
				  AND superseded_at IS NULL
				ORDER BY created_at ASC, id ASC
				LIMIT 1
				FOR UPDATE
			`, salonID, item.Provider, item.POSAppointmentID, BookingActionBook, ReconciliationRequired).Scan(&existingAttemptID, &existingAttemptVersion)
			if errors.Is(err, sql.ErrNoRows) {
				if err := tx.QueryRowContext(ctx, `
					INSERT INTO booking_attempts (
						salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version, customer_name, customer_phone,
						customer_email, service_id, staff_id, staff_selection_mode, requested_start_time,
						requested_end_time, notes, operation_type, provider_outcome, retry_policy,
						reconciliation_status, provider_location_id, provider_snapshot_generation,
						scheduling_authority, authority_provider, authority_appointment_id,
						authority_appointment_version, target_authority_appointment_version,
						authority_location_id, authority_snapshot_generation
					)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12, $13, $14, NULLIF($15, ''),
					        $16, $17, $18, $19, $20, $21,
					        $22, $4, $5, $6, 0, $20, $21)
					RETURNING id::text
				`, salonID, SourcePOSCalendarSync, item.Status, item.Provider, item.POSAppointmentID, item.POSAppointmentVersion, item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceID(), primary.staffID(), primary.staffSelectionMode(), item.StartTime, item.EndTime, item.Notes,
					BookingActionBook, providerOutcome, retryPolicy, reconciliation, fence.LocationID, fence.SnapshotGeneration,
					SchedulingAuthorityExternalProvider).Scan(&existingAttemptID); err != nil {
					return summary, err
				}
			} else if err != nil {
				return summary, err
			} else if item.POSAppointmentVersion < existingAttemptVersion {
				summary.Skipped++
				continue
			} else if err := updateCalendarBookingAttemptTx(ctx, tx, salonID, existingAttemptID, item, primary, providerOutcome, retryPolicy, reconciliation, fence); err != nil {
				return summary, err
			}
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO appointments (
					salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
					pos_customer_id, status, customer_name, customer_phone, customer_email, service_id, staff_id,
					staff_selection_mode, start_time, end_time, notes, pos_sync_status, last_pos_synced_at,
					pos_sync_error, scheduling_authority, authority_provider, authority_appointment_id,
					authority_appointment_version, authority_customer_id
				)
				VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, $15, NULLIF($16, ''), 'synced', now(), NULL,
				        $17, $3, $4, $5, NULLIF($6, ''))
				RETURNING id::text
			`, salonID, existingAttemptID, item.Provider, item.POSAppointmentID, item.POSAppointmentVersion, item.POSCustomerID, item.Status, item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceID(), primary.staffID(), primary.staffSelectionMode(), item.StartTime, item.EndTime, item.Notes, SchedulingAuthorityExternalProvider).Scan(&existingAppointmentID); err != nil {
				return summary, err
			}
			summary.Imported++
		} else if err != nil {
			return summary, err
		} else {
			if existingOriginLocationID != "" && strings.TrimSpace(existingOriginLocationID) != fence.LocationID {
				return summary, pos.ErrStaleProviderFence
			}
			if item.POSAppointmentVersion < existingAppointmentVersion {
				summary.Skipped++
				continue
			}
			if item.POSAppointmentVersion == existingAppointmentVersion {
				snapshot, err := loadCalendarAppointmentSnapshotTx(ctx, tx, salonID, existingAppointmentID, item.Provider, item.POSAppointmentID)
				if err != nil {
					return summary, err
				}
				if !equalVersionCalendarSnapshotMatches(snapshot, item, segments, fence) {
					summary.Skipped++
					continue
				}
				enriched, err := enrichEqualVersionCalendarAppointmentTx(ctx, tx, salonID, existingAppointmentID, existingAttemptID, item, primary, segments, snapshot, fence)
				if err != nil {
					return summary, err
				}
				if err := resolveCalendarAppointmentActionTx(ctx, tx, salonID, existingAppointmentID, existingAttemptID, item.Provider, item.POSAppointmentID); err != nil {
					return summary, err
				}
				if enriched {
					summary.Updated++
				} else {
					summary.Skipped++
				}
				continue
			}
			if reconciliation == ReconciliationRequired {
				hasActionReconciliation, err := hasActiveAppointmentActionReconciliationTx(ctx, tx, salonID, existingAppointmentID, existingAttemptID, item.Provider, item.POSAppointmentID)
				if err != nil {
					return summary, err
				}
				if hasActionReconciliation {
					mirrorReconciliation = ReconciliationNotRequired
				}
			}
			if err := updateCalendarBookingAttemptTx(ctx, tx, salonID, existingAttemptID, item, primary, providerOutcome, retryPolicy, mirrorReconciliation, fence); err != nil {
				return summary, err
			}
			result, err := tx.ExecContext(ctx, `
				UPDATE appointments
				SET pos_appointment_version = $4,
				    authority_appointment_version = $4,
				    status = $5,
				    pos_customer_id = COALESCE(NULLIF($15, ''), pos_customer_id),
				    authority_customer_id = COALESCE(NULLIF($15, ''), authority_customer_id),
				    customer_name = CASE WHEN $6 = 'Square customer' AND customer_name <> '' THEN customer_name ELSE $6 END,
				    customer_phone = CASE WHEN NULLIF($7, '') IS NULL AND customer_phone <> '' THEN customer_phone ELSE $7 END,
				    customer_email = COALESCE(NULLIF($8, ''), customer_email),
				    service_id = $9,
				    staff_id = $10,
				    staff_selection_mode = $11,
				    start_time = $12,
				    end_time = $13,
				    notes = COALESCE(NULLIF($14, ''), notes),
				    pos_sync_status = 'synced',
				    last_pos_synced_at = now(),
				    pos_sync_error = NULL,
				    updated_at = now()
				WHERE salon_id = $1
				  AND id = $2
				  AND scheduling_authority = 'external_provider'
				  AND pos_provider = $3
			`, salonID, existingAppointmentID, item.Provider, item.POSAppointmentVersion, item.Status, item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceID(), primary.staffID(), primary.staffSelectionMode(), item.StartTime, item.EndTime, item.Notes, item.POSCustomerID)
			if err != nil {
				return summary, err
			}
			rowsAffected, err := result.RowsAffected()
			if err != nil {
				return summary, err
			}
			if rowsAffected != 1 {
				return summary, ErrOperationConflict
			}
			summary.Updated++
		}
		if err := r.replaceCalendarAppointmentSegments(ctx, tx, salonID, existingAppointmentID, segments); err != nil {
			return summary, err
		}
		if mirrorReconciliation == ReconciliationRequired {
			reason := "Provider calendar returned an appointment status that is not accepted."
			if item.Status == StatusUnknown {
				reason = "Provider calendar returned an unrecognized appointment status."
			}
			if err := ensureReconciliationTaskTx(ctx, tx, salonID, existingAttemptID, reason); err != nil {
				return summary, err
			}
		} else {
			if err := resolveCalendarReconciliationTaskTx(ctx, tx, salonID, existingAttemptID, item.POSAppointmentID, item.POSAppointmentVersion, item.Status); err != nil {
				return summary, err
			}
		}
		if err := resolveCalendarAppointmentActionTx(ctx, tx, salonID, existingAppointmentID, existingAttemptID, item.Provider, item.POSAppointmentID); err != nil {
			return summary, err
		}
	}
	if err := tx.Commit(); err != nil {
		return summary, err
	}
	return summary, nil
}

func lockAndValidateCalendarProviderFenceTx(ctx context.Context, tx *sql.Tx, salonID string, provider string, fence pos.ProviderFence) error {
	provider = strings.TrimSpace(provider)
	fence.LocationID = strings.TrimSpace(fence.LocationID)
	if provider == "" || !validProviderFence(fence) {
		return pos.ErrStaleProviderFence
	}

	var activeProvider string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(BTRIM(active_pos_provider), ''), 'square')
		FROM salons
		WHERE id = $1
		FOR SHARE
	`, salonID).Scan(&activeProvider)
	if errors.Is(err, sql.ErrNoRows) {
		return pos.ErrStaleProviderFence
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(activeProvider) != provider {
		return pos.ErrStaleProviderFence
	}

	var status string
	var locationID string
	var generation int64
	var lastSyncAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT status, COALESCE(location_id, ''), snapshot_generation, last_sync_at
		FROM pos_connections
		WHERE salon_id = $1
		  AND provider = $2
		FOR SHARE
	`, salonID, provider).Scan(&status, &locationID, &generation, &lastSyncAt)
	if errors.Is(err, sql.ErrNoRows) {
		return pos.ErrStaleProviderFence
	}
	if err != nil {
		return err
	}
	if status != pos.StatusActive || !lastSyncAt.Valid || strings.TrimSpace(locationID) != fence.LocationID || generation != fence.SnapshotGeneration {
		return pos.ErrStaleProviderFence
	}
	return nil
}

type calendarAppointmentSegmentSnapshot struct {
	ServiceID          string
	POSServiceID       string
	POSServiceVersion  int64
	StaffID            string
	POSStaffID         string
	StaffSelectionMode string
	DurationMinutes    int
	SortOrder          int
}

type calendarAppointmentSnapshot struct {
	AppointmentID               string
	BookingAttemptID            string
	OriginSource                string
	OriginSuperseded            bool
	SchedulingAuthority         string
	AuthorityProvider           string
	AuthorityAppointmentID      string
	AuthorityAppointmentVersion int
	AuthorityCustomerID         string
	ConfirmedAt                 *time.Time
	ConfirmedByUserID           string
	ConfirmationSource          string
	Provider                    string
	ProviderLocationID          string
	ProviderGeneration          int64
	POSAppointmentID            string
	POSAppointmentVersion       int
	POSCustomerID               string
	Status                      string
	CustomerName                string
	CustomerPhone               string
	CustomerEmail               string
	ServiceID                   string
	StaffID                     string
	StaffSelectionMode          string
	StartTime                   time.Time
	EndTime                     time.Time
	Notes                       string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	Segments                    []calendarAppointmentSegmentSnapshot
}

func loadCalendarAppointmentSnapshotTx(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, provider string, providerAppointmentID string) (calendarAppointmentSnapshot, error) {
	var snapshot calendarAppointmentSnapshot
	var confirmedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT appointment.id::text,
		       appointment.booking_attempt_id::text,
		       origin.source,
		       origin.superseded_at IS NOT NULL,
		       appointment.scheduling_authority,
		       COALESCE(appointment.authority_provider, ''),
		       COALESCE(appointment.authority_appointment_id, ''),
		       COALESCE(appointment.authority_appointment_version, 0),
		       COALESCE(appointment.authority_customer_id, ''),
		       appointment.confirmed_at,
		       COALESCE(appointment.confirmed_by_user_id::text, ''),
		       COALESCE(appointment.confirmation_source, ''),
		       appointment.pos_provider,
		       COALESCE(origin.provider_location_id, ''),
		       COALESCE(origin.provider_snapshot_generation, 0),
		       appointment.pos_appointment_id,
		       COALESCE(appointment.pos_appointment_version, 0),
		       COALESCE(appointment.pos_customer_id, ''),
		       appointment.status,
		       appointment.customer_name,
		       appointment.customer_phone,
		       COALESCE(appointment.customer_email, ''),
		       COALESCE(appointment.service_id::text, ''),
		       COALESCE(appointment.staff_id::text, ''),
		       COALESCE(appointment.staff_selection_mode, 'specific'),
		       appointment.start_time,
		       appointment.end_time,
		       COALESCE(appointment.notes, ''),
		       appointment.created_at,
		       appointment.updated_at
		FROM appointments appointment
		JOIN booking_attempts origin ON origin.id = appointment.booking_attempt_id
		WHERE appointment.salon_id = $1
		  AND appointment.id = $2
		  AND appointment.scheduling_authority = 'external_provider'
		  AND origin.scheduling_authority = 'external_provider'
		  AND appointment.pos_provider = $3
		  AND appointment.pos_appointment_id = $4
		FOR UPDATE OF appointment, origin
	`, salonID, appointmentID, provider, providerAppointmentID).Scan(
		&snapshot.AppointmentID,
		&snapshot.BookingAttemptID,
		&snapshot.OriginSource,
		&snapshot.OriginSuperseded,
		&snapshot.SchedulingAuthority,
		&snapshot.AuthorityProvider,
		&snapshot.AuthorityAppointmentID,
		&snapshot.AuthorityAppointmentVersion,
		&snapshot.AuthorityCustomerID,
		&confirmedAt,
		&snapshot.ConfirmedByUserID,
		&snapshot.ConfirmationSource,
		&snapshot.Provider,
		&snapshot.ProviderLocationID,
		&snapshot.ProviderGeneration,
		&snapshot.POSAppointmentID,
		&snapshot.POSAppointmentVersion,
		&snapshot.POSCustomerID,
		&snapshot.Status,
		&snapshot.CustomerName,
		&snapshot.CustomerPhone,
		&snapshot.CustomerEmail,
		&snapshot.ServiceID,
		&snapshot.StaffID,
		&snapshot.StaffSelectionMode,
		&snapshot.StartTime,
		&snapshot.EndTime,
		&snapshot.Notes,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
	); err != nil {
		return calendarAppointmentSnapshot{}, err
	}
	if confirmedAt.Valid {
		value := confirmedAt.Time
		snapshot.ConfirmedAt = &value
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(service_id::text, ''), pos_service_id, COALESCE(pos_service_version, 0),
		       COALESCE(staff_id::text, ''), COALESCE(pos_staff_id, ''),
		       COALESCE(staff_selection_mode, 'specific'), duration_minutes, sort_order
		FROM appointment_services
		WHERE salon_id = $1
		  AND appointment_id = $2
		  AND scheduling_authority = 'external_provider'
		ORDER BY sort_order ASC, id ASC
		FOR UPDATE
	`, salonID, appointmentID)
	if err != nil {
		return calendarAppointmentSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var segment calendarAppointmentSegmentSnapshot
		if err := rows.Scan(
			&segment.ServiceID,
			&segment.POSServiceID,
			&segment.POSServiceVersion,
			&segment.StaffID,
			&segment.POSStaffID,
			&segment.StaffSelectionMode,
			&segment.DurationMinutes,
			&segment.SortOrder,
		); err != nil {
			return calendarAppointmentSnapshot{}, err
		}
		snapshot.Segments = append(snapshot.Segments, segment)
	}
	if err := rows.Err(); err != nil {
		return calendarAppointmentSnapshot{}, err
	}
	return snapshot, nil
}

func equalVersionCalendarSnapshotMatches(snapshot calendarAppointmentSnapshot, item CalendarAppointmentImport, segments []calendarImportSegmentRef, fence pos.ProviderFence) bool {
	if snapshot.Provider != item.Provider ||
		(snapshot.ProviderLocationID != "" && strings.TrimSpace(snapshot.ProviderLocationID) != strings.TrimSpace(fence.LocationID)) ||
		snapshot.POSAppointmentID != item.POSAppointmentID ||
		snapshot.POSAppointmentVersion != item.POSAppointmentVersion ||
		snapshot.Status != item.Status ||
		!snapshot.StartTime.Equal(item.StartTime) ||
		!snapshot.EndTime.Equal(item.EndTime) ||
		(snapshot.POSCustomerID != "" && snapshot.POSCustomerID != strings.TrimSpace(item.POSCustomerID)) ||
		len(snapshot.Segments) != len(segments) {
		return false
	}
	for index, persisted := range snapshot.Segments {
		incoming := segments[index]
		if persisted.POSServiceID != incoming.POSServiceID ||
			persisted.POSServiceVersion != incoming.POSServiceVersion ||
			(persisted.POSStaffID != "" && persisted.POSStaffID != strings.TrimSpace(incoming.POSStaffID)) ||
			persisted.DurationMinutes != incoming.DurationMinutes ||
			persisted.SortOrder != incoming.SortOrder {
			return false
		}
	}
	return true
}

func enrichEqualVersionCalendarAppointmentTx(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	appointmentID string,
	attemptID string,
	item CalendarAppointmentImport,
	primary calendarImportSegmentRef,
	segments []calendarImportSegmentRef,
	snapshot calendarAppointmentSnapshot,
	fence pos.ProviderFence,
) (bool, error) {
	changed := false
	customerIdentityMatches := snapshot.POSCustomerID != "" && snapshot.POSCustomerID == strings.TrimSpace(item.POSCustomerID)
	primaryStaffIdentityMatches := len(snapshot.Segments) > 0 &&
		snapshot.Segments[0].POSStaffID != "" &&
		snapshot.Segments[0].POSStaffID == strings.TrimSpace(primary.POSStaffID)
	originIdentityProven := customerIdentityMatches && len(snapshot.Segments) == len(segments) && len(segments) > 0
	if originIdentityProven {
		for index, segment := range segments {
			persistedStaffID := strings.TrimSpace(snapshot.Segments[index].POSStaffID)
			if persistedStaffID == "" || persistedStaffID != strings.TrimSpace(segment.POSStaffID) {
				originIdentityProven = false
				break
			}
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET customer_name = CASE
		        WHEN $12 AND (customer_name = '' OR customer_name = 'Square customer') AND $6 <> 'Square customer' THEN $6
		        ELSE customer_name
		    END,
		    customer_phone = CASE WHEN $12 AND customer_phone = '' AND $7 <> '' THEN $7 ELSE customer_phone END,
		    customer_email = CASE WHEN $12 AND COALESCE(customer_email, '') = '' AND $8 <> '' THEN $8 ELSE customer_email END,
		    service_id = COALESCE(service_id, NULLIF($9, '')::uuid),
		    staff_id = CASE WHEN $13 THEN COALESCE(staff_id, NULLIF($10, '')::uuid) ELSE staff_id END,
		    staff_selection_mode = CASE
		        WHEN $13 AND staff_id IS NULL AND NULLIF($10, '') IS NOT NULL THEN $11
		        ELSE staff_selection_mode
		    END,
		    updated_at = now()
		WHERE salon_id = $1
		  AND id = $2
		  AND scheduling_authority = 'external_provider'
		  AND pos_provider = $3
		  AND pos_appointment_id = $4
		  AND COALESCE(pos_appointment_version, 0) = $5
		  AND (
		      ($12 AND (customer_name = '' OR customer_name = 'Square customer') AND $6 <> 'Square customer')
		      OR ($12 AND customer_phone = '' AND $7 <> '')
		      OR ($12 AND COALESCE(customer_email, '') = '' AND $8 <> '')
		      OR (service_id IS NULL AND NULLIF($9, '') IS NOT NULL)
		      OR ($13 AND staff_id IS NULL AND NULLIF($10, '') IS NOT NULL)
		  )
	`, salonID, appointmentID, item.Provider, item.POSAppointmentID, item.POSAppointmentVersion,
		item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceIDString(), primary.staffIDString(), primary.staffSelectionMode(), customerIdentityMatches, primaryStaffIdentityMatches)
	if err != nil {
		return false, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected > 0 {
		changed = true
	}

	result, err = tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET customer_name = CASE
		        WHEN $11 AND (customer_name = '' OR customer_name = 'Square customer') AND $5 <> 'Square customer' THEN $5
		        ELSE customer_name
		    END,
		    customer_phone = CASE WHEN $11 AND customer_phone = '' AND $6 <> '' THEN $6 ELSE customer_phone END,
		    customer_email = CASE WHEN $11 AND COALESCE(customer_email, '') = '' AND $7 <> '' THEN $7 ELSE customer_email END,
		    service_id = COALESCE(service_id, NULLIF($8, '')::uuid),
		    staff_id = CASE WHEN $12 THEN COALESCE(staff_id, NULLIF($9, '')::uuid) ELSE staff_id END,
		    staff_selection_mode = CASE
		        WHEN $12 AND staff_id IS NULL AND NULLIF($9, '') IS NOT NULL THEN $10
		        ELSE staff_selection_mode
		    END,
		    provider_location_id = CASE WHEN $15 THEN COALESCE(provider_location_id, NULLIF($13, '')) ELSE provider_location_id END,
		    provider_snapshot_generation = CASE WHEN $15 THEN COALESCE(provider_snapshot_generation, NULLIF($14, 0)) ELSE provider_snapshot_generation END,
		    authority_location_id = CASE WHEN $15 THEN COALESCE(authority_location_id, NULLIF($13, '')) ELSE authority_location_id END,
		    authority_snapshot_generation = CASE WHEN $15 THEN COALESCE(authority_snapshot_generation, NULLIF($14, 0)) ELSE authority_snapshot_generation END,
		    updated_at = now()
		WHERE salon_id = $1
		  AND id = $2
		  AND scheduling_authority = 'external_provider'
		  AND pos_provider = $3
		  AND COALESCE(pos_booking_version, 0) = $4
		  AND (
		      ($11 AND (customer_name = '' OR customer_name = 'Square customer') AND $5 <> 'Square customer')
		      OR ($11 AND customer_phone = '' AND $6 <> '')
		      OR ($11 AND COALESCE(customer_email, '') = '' AND $7 <> '')
		      OR (service_id IS NULL AND NULLIF($8, '') IS NOT NULL)
		      OR ($12 AND staff_id IS NULL AND NULLIF($9, '') IS NOT NULL)
		      OR ($15 AND provider_location_id IS NULL AND provider_snapshot_generation IS NULL)
		  )
	`, salonID, attemptID, item.Provider, item.POSAppointmentVersion,
		item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceIDString(), primary.staffIDString(), primary.staffSelectionMode(), customerIdentityMatches, primaryStaffIdentityMatches, fence.LocationID, fence.SnapshotGeneration, originIdentityProven)
	if err != nil {
		return false, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected > 0 {
		changed = true
	}

	for index, segment := range segments {
		staffIdentityMatches := index < len(snapshot.Segments) &&
			snapshot.Segments[index].POSStaffID != "" &&
			snapshot.Segments[index].POSStaffID == strings.TrimSpace(segment.POSStaffID)
		result, err := tx.ExecContext(ctx, `
			UPDATE appointment_services
			SET service_id = COALESCE(service_id, NULLIF($2, '')::uuid),
			    staff_id = CASE WHEN $9 THEN COALESCE(staff_id, NULLIF($3, '')::uuid) ELSE staff_id END,
			    staff_selection_mode = CASE
			        WHEN $9 AND staff_id IS NULL AND NULLIF($3, '') IS NOT NULL THEN $4
			        ELSE staff_selection_mode
			    END,
			    name = CASE
			        WHEN service_id IS NULL AND NULLIF($2, '') IS NOT NULL AND name IN ('Square appointment', 'Square service') THEN $5
			        ELSE name
			    END,
			    price_from = COALESCE(price_from, NULLIF($6, 0))
			WHERE salon_id = $10
			  AND appointment_id = $1
			  AND scheduling_authority = 'external_provider'
			  AND sort_order = $7
			  AND pos_service_id = $8
			  AND (
			      (service_id IS NULL AND NULLIF($2, '') IS NOT NULL)
			      OR ($9 AND staff_id IS NULL AND NULLIF($3, '') IS NOT NULL)
			  )
		`, appointmentID, segment.serviceIDString(), segment.staffIDString(), segment.staffSelectionMode(), segment.Name, segment.PriceFrom, segment.SortOrder, segment.POSServiceID, staffIdentityMatches, salonID)
		if err != nil {
			return false, err
		}
		if affected, err := result.RowsAffected(); err != nil {
			return false, err
		} else if affected > 0 {
			changed = true
		}
	}
	return changed, nil
}

func updateCalendarBookingAttemptTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, item CalendarAppointmentImport, primary calendarImportSegmentRef, providerOutcome string, retryPolicy string, reconciliation string, fence pos.ProviderFence) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status = $4,
		    pos_booking_id = $5,
		    pos_booking_version = $6,
		    authority_appointment_id = $5,
		    authority_appointment_version = $6,
		    customer_name = CASE WHEN $7 = 'Square customer' AND customer_name <> '' THEN customer_name ELSE $7 END,
		    customer_phone = CASE WHEN NULLIF($8, '') IS NULL AND customer_phone <> '' THEN customer_phone ELSE $8 END,
		    customer_email = COALESCE(NULLIF($9, ''), customer_email),
		    service_id = $10,
		    staff_id = $11,
		    staff_selection_mode = $12,
		    requested_start_time = $13,
		    requested_end_time = $14,
		    notes = COALESCE(NULLIF($15, ''), notes),
		    operation_type = $16,
		    provider_outcome = $17,
		    retry_policy = $18,
		    reconciliation_status = CASE
		        WHEN reconciliation_status = 'required' AND $19 = 'not_required' THEN reconciliation_status
		        ELSE $19
		    END,
		    provider_location_id = COALESCE(provider_location_id, NULLIF($20, '')),
		    provider_snapshot_generation = COALESCE(provider_snapshot_generation, NULLIF($21, 0)),
		    authority_location_id = COALESCE(authority_location_id, NULLIF($20, '')),
		    authority_snapshot_generation = COALESCE(authority_snapshot_generation, NULLIF($21, 0)),
		    updated_at = now()
		WHERE salon_id = $1
		  AND id = $2
		  AND scheduling_authority = 'external_provider'
		  AND pos_provider = $3
		  AND (
		      (provider_location_id IS NULL AND provider_snapshot_generation IS NULL)
		      OR provider_location_id = $20
		  )
	`, salonID, attemptID, item.Provider, item.Status, item.POSAppointmentID, item.POSAppointmentVersion, item.CustomerName, item.CustomerPhone, item.CustomerEmail, primary.serviceID(), primary.staffID(), primary.staffSelectionMode(), item.StartTime, item.EndTime, item.Notes,
		BookingActionBook, providerOutcome, retryPolicy, reconciliation, fence.LocationID, fence.SnapshotGeneration)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return ErrOperationConflict
	}
	return nil
}

func calendarImportAttemptPolicy(status string) (string, string, string) {
	if status == StatusProviderPending {
		return ProviderOutcomeSucceeded, RetryPolicyBlocked, ReconciliationRequired
	}
	if status == StatusUnknown {
		return ProviderOutcomeUnknown, RetryPolicyBlocked, ReconciliationRequired
	}
	return ProviderOutcomeSucceeded, RetryPolicyNone, ReconciliationNotRequired
}

func hasActiveAppointmentActionReconciliationTx(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, mirrorAttemptID string, provider string, providerAppointmentID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM booking_attempts
			WHERE salon_id = $1
			  AND id <> $2
			  AND target_appointment_id = $3
			  AND scheduling_authority = 'external_provider'
			  AND pos_provider = $4
			  AND pos_booking_id = $5
			  AND operation_type IN ($6, $7)
			  AND reconciliation_status = $8
			  AND superseded_at IS NULL
		)
	`, salonID, mirrorAttemptID, appointmentID, provider, providerAppointmentID, BookingActionReschedule, BookingActionCancel, ReconciliationRequired).Scan(&exists)
	return exists, err
}

func resolveCalendarAppointmentActionTx(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, mirrorAttemptID string, provider string, providerAppointmentID string) error {
	snapshot, err := loadCalendarAppointmentSnapshotTx(ctx, tx, salonID, appointmentID, provider, providerAppointmentID)
	if err != nil {
		return err
	}
	if snapshot.Status != StatusCancelled && snapshot.Status != StatusConfirmed && snapshot.Status != StatusRescheduled {
		return nil
	}
	type candidate struct {
		AttemptID               string
		OperationType           string
		TargetPOSBookingVersion int
		StartTime               time.Time
		EndTime                 time.Time
		SegmentsMatch           bool
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT attempt.id::text, attempt.operation_type,
		       COALESCE(attempt.target_pos_booking_version, 0),
		       attempt.requested_start_time, attempt.requested_end_time,
		       EXISTS (
		           SELECT 1
		           FROM booking_attempt_segments segment
		           WHERE segment.booking_attempt_id = attempt.id
		       ) AND EXISTS (
		           SELECT 1
		           FROM appointment_services segment
		           WHERE segment.appointment_id = $3
		       ) AND COALESCE((
		           SELECT jsonb_agg(jsonb_build_array(
		               COALESCE(segment.service_id::text, ''),
		               COALESCE(segment.staff_id::text, '')
		           ) ORDER BY segment.sort_order, segment.id)
		           FROM booking_attempt_segments segment
		           WHERE segment.booking_attempt_id = attempt.id
		       ), '[]'::jsonb) = COALESCE((
		           SELECT jsonb_agg(jsonb_build_array(
		               COALESCE(segment.service_id::text, ''),
		               COALESCE(segment.staff_id::text, '')
		           ) ORDER BY segment.sort_order, segment.id)
		           FROM appointment_services segment
		           WHERE segment.appointment_id = $3
		       ), '[]'::jsonb) AS segments_match
		FROM booking_attempts attempt
		WHERE attempt.salon_id = $1
		  AND attempt.id <> $2
		  AND attempt.target_appointment_id = $3
		  AND attempt.scheduling_authority = 'external_provider'
		  AND attempt.pos_provider = $4
		  AND attempt.pos_booking_id = $5
		  AND attempt.reconciliation_status = $6
		  AND attempt.superseded_at IS NULL
		ORDER BY attempt.created_at ASC, attempt.id ASC
		FOR UPDATE
	`, salonID, mirrorAttemptID, appointmentID, snapshot.Provider, snapshot.POSAppointmentID, ReconciliationRequired)
	if err != nil {
		return err
	}
	candidates := make([]candidate, 0, 2)
	for rows.Next() {
		var current candidate
		if err := rows.Scan(&current.AttemptID, &current.OperationType, &current.TargetPOSBookingVersion, &current.StartTime, &current.EndTime, &current.SegmentsMatch); err != nil {
			rows.Close()
			return err
		}
		matches := current.OperationType == BookingActionCancel &&
			snapshot.Status == StatusCancelled &&
			snapshot.POSAppointmentVersion > current.TargetPOSBookingVersion
		if current.OperationType == BookingActionReschedule && (snapshot.Status == StatusConfirmed || snapshot.Status == StatusRescheduled) {
			matches = snapshot.POSAppointmentVersion > current.TargetPOSBookingVersion &&
				current.StartTime.Equal(snapshot.StartTime) &&
				current.EndTime.Equal(snapshot.EndTime) &&
				current.SegmentsMatch
		}
		if matches {
			candidates = append(candidates, current)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(candidates) != 1 {
		return nil
	}
	matched := candidates[0]
	finalStatus := StatusRescheduled
	if matched.OperationType == BookingActionCancel {
		finalStatus = StatusCancelled
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_version = $2,
		    authority_appointment_version = $2,
		    provider_outcome = $3,
		    retry_policy = $4,
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $5
		  AND salon_id = $6
		  AND scheduling_authority = 'external_provider'
		  AND reconciliation_status = $7
	`, finalStatus, snapshot.POSAppointmentVersion, ProviderOutcomeSucceeded, RetryPolicyNone, matched.AttemptID, salonID, ReconciliationRequired)
	if err := requireExactlyOneRow(result, err); err != nil {
		return err
	}
	return resolveCalendarReconciliationTaskTx(ctx, tx, salonID, matched.AttemptID, snapshot.POSAppointmentID, snapshot.POSAppointmentVersion, snapshot.Status)
}

func resolveCalendarReconciliationTaskTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, providerAppointmentID string, providerAppointmentVersion int, providerStatus string) error {
	var reconciliationStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT reconciliation_status
		FROM booking_attempts
		WHERE id = $1
		  AND salon_id = $2
		  AND scheduling_authority = 'external_provider'
		FOR UPDATE
	`, attemptID, salonID).Scan(&reconciliationStatus); err != nil {
		return err
	}
	var taskID string
	var taskStatus string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM booking_reconciliation_tasks
		WHERE salon_id = $1
		  AND booking_attempt_id = $2
		FOR UPDATE
	`, salonID, attemptID).Scan(&taskID, &taskStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if taskStatus == "resolved" {
		return nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE booking_reconciliation_tasks
		SET status = 'resolved', resolution = $1,
		    resolution_note = 'Resolved by provider calendar synchronization.',
		    resolved_at = now(), updated_at = now()
		WHERE id = $2 AND salon_id = $3 AND status <> 'resolved'
	`, ReconciliationActionProviderAttached, taskID, salonID)
	if err := requireExactlyOneRow(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET reconciliation_status = $1,
		    reconciliation_resolution = $2,
		    reconciliation_resolved_at = now(),
		    retry_policy = $3,
		    updated_at = now()
		WHERE id = $4 AND salon_id = $5 AND reconciliation_status = $6
		  AND scheduling_authority = 'external_provider'
	`, ReconciliationResolved, ReconciliationActionProviderAttached, RetryPolicyNone, attemptID, salonID, ReconciliationRequired)
	if err := requireExactlyOneRow(result, err); err != nil {
		return err
	}
	actionKey := fmt.Sprintf("calendar:%s:%d:%s", providerAppointmentID, providerAppointmentVersion, providerStatus)
	payloadFingerprint := fingerprintJSON(struct {
		Action                     string `json:"action"`
		ProviderAppointmentID      string `json:"provider_appointment_id"`
		ProviderAppointmentVersion int    `json:"provider_appointment_version"`
		ProviderStatus             string `json:"provider_status"`
	}{
		Action:                     ReconciliationActionProviderAttached,
		ProviderAppointmentID:      providerAppointmentID,
		ProviderAppointmentVersion: providerAppointmentVersion,
		ProviderStatus:             providerStatus,
	})
	_, err = tx.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_events (
			salon_id, booking_attempt_id, reconciliation_task_id, action_key,
			payload_fingerprint, action, provider_appointment_id,
			provider_appointment_version, provider_status, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'Resolved by provider calendar synchronization.')
		ON CONFLICT (reconciliation_task_id, action_key) DO NOTHING
	`, salonID, attemptID, taskID, actionKey, payloadFingerprint, ReconciliationActionProviderAttached, providerAppointmentID, providerAppointmentVersion, providerStatus)
	return err
}

func mergeCalendarCustomerDetails(item CalendarAppointmentImport, name string, phone string, email string) CalendarAppointmentImport {
	if strings.TrimSpace(item.CustomerName) == "" || strings.EqualFold(strings.TrimSpace(item.CustomerName), "Square customer") {
		item.CustomerName = strings.TrimSpace(name)
	}
	if validation.NormalizePhone(item.CustomerPhone) == "" {
		item.CustomerPhone = validation.NormalizePhone(phone)
	}
	if strings.TrimSpace(item.CustomerEmail) == "" {
		item.CustomerEmail = strings.TrimSpace(email)
	}
	return item
}

type calendarImportSegmentRef struct {
	Service           *ServiceRef
	Staff             *StaffRef
	POSServiceID      string
	POSServiceVersion int64
	POSStaffID        string
	Name              string
	DurationMinutes   int
	PriceFrom         float64
	SortOrder         int
}

func (s calendarImportSegmentRef) serviceID() any {
	if s.serviceIDString() == "" {
		return nil
	}
	return s.serviceIDString()
}

func (s calendarImportSegmentRef) serviceIDString() string {
	if s.Service == nil {
		return ""
	}
	return strings.TrimSpace(s.Service.ID)
}

func (s calendarImportSegmentRef) staffID() any {
	if s.staffIDString() == "" {
		return nil
	}
	return s.staffIDString()
}

func (s calendarImportSegmentRef) staffIDString() string {
	if s.Staff == nil {
		return ""
	}
	return strings.TrimSpace(s.Staff.ID)
}

func (s calendarImportSegmentRef) staffSelectionMode() string {
	if s.Staff == nil || strings.TrimSpace(s.Staff.ID) == "" {
		return StaffSelectionAnyone
	}
	return StaffSelectionSpecific
}

func (r *Repository) resolveCalendarImportSegments(ctx context.Context, tx *sql.Tx, salonID string, provider string, item CalendarAppointmentImport) ([]calendarImportSegmentRef, error) {
	rawSegments := item.Segments
	if len(rawSegments) == 0 {
		rawSegments = []CalendarAppointmentSegmentImport{{
			DurationMinutes: int(item.EndTime.Sub(item.StartTime).Minutes()),
		}}
	}
	segments := make([]calendarImportSegmentRef, 0, len(rawSegments))
	for idx, raw := range rawSegments {
		service, err := r.findCalendarServiceRef(ctx, tx, salonID, provider, raw.POSServiceID)
		if err != nil {
			return nil, err
		}
		staff, err := r.findCalendarStaffRef(ctx, tx, salonID, provider, raw.POSStaffID)
		if err != nil {
			return nil, err
		}
		name := "Square appointment"
		duration := raw.DurationMinutes
		price := float64(0)
		if service != nil {
			name = service.Name
			if duration <= 0 {
				duration = service.DurationMinutes
			}
			price = service.PriceFrom
		} else if strings.TrimSpace(raw.POSServiceID) != "" {
			name = "Square service"
		}
		if duration <= 0 {
			duration = int(item.EndTime.Sub(item.StartTime).Minutes())
		}
		segments = append(segments, calendarImportSegmentRef{
			Service:           service,
			Staff:             staff,
			POSServiceID:      defaultString(strings.TrimSpace(raw.POSServiceID), "unmapped"),
			POSServiceVersion: raw.POSServiceVersion,
			POSStaffID:        strings.TrimSpace(raw.POSStaffID),
			Name:              name,
			DurationMinutes:   duration,
			PriceFrom:         price,
			SortOrder:         idx + 1,
		})
	}
	return segments, nil
}

func (r *Repository) findCalendarServiceRef(ctx context.Context, tx *sql.Tx, salonID string, provider string, posServiceID string) (*ServiceRef, error) {
	posServiceID = strings.TrimSpace(posServiceID)
	if posServiceID == "" {
		return nil, nil
	}
	var item ServiceRef
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, pos_provider, pos_service_id, COALESCE(pos_service_version, 0),
		       name, duration_minutes, COALESCE(price_from, 0)
		FROM services
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND pos_service_id = $3
		ORDER BY archived_at IS NULL DESC, updated_at DESC
		LIMIT 1
	`, salonID, provider, posServiceID).Scan(&item.ID, &item.POSProvider, &item.POSServiceID, &item.POSServiceVersion, &item.Name, &item.DurationMinutes, &item.PriceFrom)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) findCalendarStaffRef(ctx context.Context, tx *sql.Tx, salonID string, provider string, posStaffID string) (*StaffRef, error) {
	posStaffID = strings.TrimSpace(posStaffID)
	if posStaffID == "" {
		return nil, nil
	}
	var item StaffRef
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, pos_provider, pos_staff_id, name
		FROM staff
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND pos_staff_id = $3
		ORDER BY archived_at IS NULL DESC, updated_at DESC
		LIMIT 1
	`, salonID, provider, posStaffID).Scan(&item.ID, &item.POSProvider, &item.POSStaffID, &item.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) replaceCalendarAppointmentSegments(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, segments []calendarImportSegmentRef) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM appointment_services
		WHERE salon_id = $1 AND appointment_id = $2
	`, salonID, appointmentID); err != nil {
		return err
	}
	for _, segment := range segments {
		var price any
		if segment.PriceFrom > 0 {
			price = segment.PriceFrom
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				salon_id, appointment_id, service_id, staff_id, staff_selection_mode, pos_service_id,
				pos_service_version, pos_staff_id, name, duration_minutes, price_from, sort_order,
				scheduling_authority, authority_provider, authority_service_id,
				authority_service_version, authority_staff_id
			)
			SELECT appointment.salon_id, appointment.id, $3, $4, $5, $6, NULLIF($7::bigint, 0), NULLIF($8, ''), $9, $10, $11, $12,
			       appointment.scheduling_authority, appointment.pos_provider, $6,
			       NULLIF($7::bigint, 0), NULLIF($8, '')
			FROM appointments appointment
			WHERE appointment.salon_id = $1
			  AND appointment.id = $2
			  AND appointment.scheduling_authority = 'external_provider'
		`, salonID, appointmentID, segment.serviceID(), segment.staffID(), segment.staffSelectionMode(), segment.POSServiceID, segment.POSServiceVersion, segment.POSStaffID, segment.Name, segment.DurationMinutes, price, segment.SortOrder); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) CreateCalendarSyncLog(ctx context.Context, salonID string, provider string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO pos_sync_logs (salon_id, provider, sync_type, status)
		VALUES ($1, $2, 'calendar_import', 'started')
		RETURNING id::text
	`, salonID, provider).Scan(&id)
	return id, err
}

func (r *Repository) CompleteCalendarSyncLog(ctx context.Context, id string, status string, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pos_sync_logs
		SET status = $2, message = NULLIF($3, ''), completed_at = now()
		WHERE id = $1
	`, id, status, message)
	return err
}

func (r *Repository) LogPOSError(ctx context.Context, salonID string, provider string, operation string, providerErr error) error {
	if providerErr == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, salonID, provider, operation, posErrorCode(providerErr), providerErr.Error())
	return err
}

func (r *Repository) attachAttemptTargetAppointments(ctx context.Context, salonID string, items []BookingAttempt) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item.TargetAppointmentID == "" || seen[item.TargetAppointmentID] {
			continue
		}
		seen[item.TargetAppointmentID] = true
		ids = append(ids, item.TargetAppointmentID)
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, COALESCE(pos_provider, ''), COALESCE(pos_appointment_id, ''),
		       COALESCE(pos_appointment_version, 0), scheduling_authority, COALESCE(authority_provider, ''),
		       COALESCE(authority_appointment_id, ''), COALESCE(authority_appointment_version, 0),
		       COALESCE(authority_customer_id, ''), confirmed_at, COALESCE(confirmed_by_user_id::text, ''),
		       COALESCE(confirmation_source, ''), status, customer_name, customer_phone,
		       COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''),
		       COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''), created_at, updated_at
		FROM appointments
		WHERE salon_id = $1
		  AND id = ANY($2::uuid[])
	`, salonID, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	appointments := make([]Appointment, 0, len(ids))
	for rows.Next() {
		var item Appointment
		var confirmedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.BookingAttemptID,
			&item.POSProvider,
			&item.POSAppointmentID,
			&item.POSAppointmentVersion,
			&item.SchedulingAuthority,
			&item.AuthorityProvider,
			&item.AuthorityAppointmentID,
			&item.AuthorityAppointmentVersion,
			&item.AuthorityCustomerID,
			&confirmedAt,
			&item.ConfirmedByUserID,
			&item.ConfirmationSource,
			&item.Status,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.StartTime,
			&item.EndTime,
			&item.Notes,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return err
		}
		if confirmedAt.Valid {
			item.ConfirmedAt = &confirmedAt.Time
		}
		appointments = append(appointments, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := r.attachAppointmentSegments(ctx, appointments); err != nil {
		return err
	}

	appointmentByID := make(map[string]Appointment, len(appointments))
	for _, appointment := range appointments {
		appointmentByID[appointment.ID] = appointment
	}
	for index := range items {
		appointment, ok := appointmentByID[items[index].TargetAppointmentID]
		if !ok {
			continue
		}
		appointmentCopy := appointment
		items[index].Appointment = &appointmentCopy
	}
	return nil
}

func bookingActionForAttempt(status string, notificationType string) string {
	switch notificationType {
	case NotificationTypeRescheduleFallback:
		return BookingActionReschedule
	case NotificationTypeCancellationFallback:
		return BookingActionCancel
	case NotificationTypeBookingFallback:
		return BookingActionBook
	}
	switch status {
	case StatusRescheduled:
		return BookingActionReschedule
	case StatusCancelled:
		return BookingActionCancel
	default:
		return BookingActionBook
	}
}

func (r *Repository) attachAppointmentSegments(ctx context.Context, items []Appointment) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for index, item := range items {
		ids = append(ids, item.ID)
		indexByID[item.ID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT aps.appointment_id::text, aps.id::text,
		       aps.scheduling_authority, COALESCE(aps.authority_provider, ''),
		       COALESCE(aps.authority_service_id, ''), COALESCE(aps.authority_service_version, 0),
		       COALESCE(aps.authority_staff_id, ''),
		       COALESCE(aps.service_id::text, ''), COALESCE(aps.pos_service_id, ''), COALESCE(aps.pos_service_version, 0), aps.name,
		       COALESCE(aps.staff_id::text, ''), COALESCE(aps.pos_staff_id, ''), COALESCE(st.name, ''),
		       COALESCE(aps.staff_selection_mode, 'specific'),
		       COALESCE(aps.guest_reference, ''), aps.plan_version,
		       aps.scheduled_start_time, aps.scheduled_end_time,
		       aps.occupied_start_time, aps.occupied_end_time,
		       aps.buffer_before_minutes, aps.buffer_after_minutes,
		       COALESCE(aps.duration_minutes, 0), aps.sort_order
		FROM appointment_services aps
		JOIN appointments appointment
		  ON appointment.id = aps.appointment_id
		 AND appointment.salon_id = aps.salon_id
		LEFT JOIN staff st ON st.id = aps.staff_id
		WHERE aps.appointment_id = ANY($1::uuid[])
		  AND (
		      aps.scheduling_authority <> 'manleai_calendar'
		      OR (
		          appointment.scheduling_authority = 'manleai_calendar'
		          AND aps.plan_version = appointment.authority_appointment_version
		          AND aps.released_at IS NULL
		      )
		  )
		ORDER BY aps.appointment_id, aps.sort_order ASC, aps.id ASC
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	type segmentLocation struct {
		appointmentIndex int
		segmentIndex     int
	}
	internalSegmentLocations := make(map[string]segmentLocation)
	internalSegmentIDs := make([]string, 0)
	for rows.Next() {
		var appointmentID string
		var segment BookingSegmentSnapshot
		var scheduledStart sql.NullTime
		var scheduledEnd sql.NullTime
		var occupiedStart sql.NullTime
		var occupiedEnd sql.NullTime
		var bufferBefore sql.NullInt64
		var bufferAfter sql.NullInt64
		if err := rows.Scan(
			&appointmentID,
			&segment.AppointmentServiceID,
			&segment.SchedulingAuthority,
			&segment.AuthorityProvider,
			&segment.AuthorityServiceID,
			&segment.AuthorityServiceVersion,
			&segment.AuthorityStaffID,
			&segment.ServiceID,
			&segment.POSServiceID,
			&segment.POSServiceVersion,
			&segment.ServiceName,
			&segment.StaffID,
			&segment.POSStaffID,
			&segment.StaffName,
			&segment.StaffSelectionMode,
			&segment.GuestReference,
			&segment.PlanVersion,
			&scheduledStart,
			&scheduledEnd,
			&occupiedStart,
			&occupiedEnd,
			&bufferBefore,
			&bufferAfter,
			&segment.DurationMinutes,
			&segment.SortOrder,
		); err != nil {
			return err
		}
		segment.Quantity = 1
		if scheduledStart.Valid {
			value := scheduledStart.Time
			segment.ScheduledStartTime = &value
		}
		if scheduledEnd.Valid {
			value := scheduledEnd.Time
			segment.ScheduledEndTime = &value
		}
		if occupiedStart.Valid {
			value := occupiedStart.Time
			segment.OccupiedStartTime = &value
		}
		if occupiedEnd.Valid {
			value := occupiedEnd.Time
			segment.OccupiedEndTime = &value
		}
		if bufferBefore.Valid {
			value := int(bufferBefore.Int64)
			segment.BufferBeforeMinutes = &value
		}
		if bufferAfter.Valid {
			value := int(bufferAfter.Int64)
			segment.BufferAfterMinutes = &value
		}
		if index, ok := indexByID[appointmentID]; ok {
			items[index].Segments = append(items[index].Segments, segment)
			if segment.SchedulingAuthority == SchedulingAuthorityManleAICalendar {
				internalSegmentIDs = append(internalSegmentIDs, segment.AppointmentServiceID)
				internalSegmentLocations[segment.AppointmentServiceID] = segmentLocation{
					appointmentIndex: index,
					segmentIndex:     len(items[index].Segments) - 1,
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(internalSegmentIDs) == 0 {
		return nil
	}
	resourceRows, err := r.db.QueryContext(ctx, `
		SELECT allocation.appointment_service_id::text,
		       pool.id::text, pool.name, allocation.units_allocated
		FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN manleai_calendar_resource_pools pool
		  ON pool.id = allocation.resource_pool_id
		 AND pool.salon_id = allocation.salon_id
		WHERE allocation.appointment_service_id = ANY($1::uuid[])
		  AND allocation.released_at IS NULL
		ORDER BY allocation.appointment_service_id, pool.id
	`, pq.Array(internalSegmentIDs))
	if err != nil {
		return err
	}
	defer resourceRows.Close()
	for resourceRows.Next() {
		var segmentID string
		var allocation AvailabilityResourceAllocation
		if err := resourceRows.Scan(
			&segmentID,
			&allocation.ResourcePoolID,
			&allocation.ResourceName,
			&allocation.UnitsAllocated,
		); err != nil {
			return err
		}
		location, ok := internalSegmentLocations[segmentID]
		if !ok {
			continue
		}
		segment := &items[location.appointmentIndex].Segments[location.segmentIndex]
		segment.ResourceAllocations = append(segment.ResourceAllocations, allocation)
	}
	return resourceRows.Err()
}

func (r *Repository) attachBookingAttemptSegments(ctx context.Context, items []BookingAttempt) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for index, item := range items {
		ids = append(ids, item.ID)
		indexByID[item.ID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT bas.booking_attempt_id::text,
		       bas.scheduling_authority, COALESCE(bas.authority_provider, ''),
		       COALESCE(bas.authority_service_id, ''), COALESCE(bas.authority_service_version, 0),
		       COALESCE(bas.authority_staff_id, ''),
		       COALESCE(bas.service_id::text, ''), COALESCE(bas.pos_service_id, ''), COALESCE(bas.pos_service_version, 0), bas.name,
		       COALESCE(bas.staff_id::text, ''), COALESCE(bas.pos_staff_id, ''), COALESCE(st.name, ''),
		       COALESCE(bas.staff_selection_mode, 'specific'),
		       COALESCE(bas.duration_minutes, 0), bas.sort_order
		FROM booking_attempt_segments bas
		LEFT JOIN staff st ON st.id = bas.staff_id
		WHERE bas.booking_attempt_id = ANY($1::uuid[])
		ORDER BY bas.booking_attempt_id, bas.sort_order ASC
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var attemptID string
		var segment BookingSegmentSnapshot
		if err := rows.Scan(
			&attemptID,
			&segment.SchedulingAuthority,
			&segment.AuthorityProvider,
			&segment.AuthorityServiceID,
			&segment.AuthorityServiceVersion,
			&segment.AuthorityStaffID,
			&segment.ServiceID,
			&segment.POSServiceID,
			&segment.POSServiceVersion,
			&segment.ServiceName,
			&segment.StaffID,
			&segment.POSStaffID,
			&segment.StaffName,
			&segment.StaffSelectionMode,
			&segment.DurationMinutes,
			&segment.SortOrder,
		); err != nil {
			return err
		}
		if index, ok := indexByID[attemptID]; ok {
			items[index].Segments = append(items[index].Segments, segment)
		}
	}
	return rows.Err()
}

type appointmentScanner interface {
	Scan(dest ...any) error
}

func scanAppointment(row appointmentScanner) (*Appointment, error) {
	var item Appointment
	var confirmedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.BookingAttemptID,
		&item.POSProvider,
		&item.POSAppointmentID,
		&item.POSAppointmentVersion,
		&item.SchedulingAuthority,
		&item.AuthorityProvider,
		&item.AuthorityAppointmentID,
		&item.AuthorityAppointmentVersion,
		&item.AuthorityCustomerID,
		&confirmedAt,
		&item.ConfirmedByUserID,
		&item.ConfirmationSource,
		&item.Status,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.ServiceID,
		&item.StaffID,
		&item.StaffSelectionMode,
		&item.StartTime,
		&item.EndTime,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if confirmedAt.Valid {
		value := confirmedAt.Time
		item.ConfirmedAt = &value
	}
	return &item, nil
}

func authorityBookingSegmentSnapshots(segments []BookingSegmentRecord) []BookingSegmentSnapshot {
	snapshots := bookingSegmentSnapshots(segments)
	for index := range snapshots {
		provider := strings.TrimSpace(segments[index].Service.POSProvider)
		if provider == "" {
			provider = strings.TrimSpace(segments[index].Staff.POSProvider)
		}
		snapshots[index].SchedulingAuthority = SchedulingAuthorityExternalProvider
		snapshots[index].AuthorityProvider = provider
		snapshots[index].AuthorityServiceID = segments[index].Service.POSServiceID
		snapshots[index].AuthorityServiceVersion = segments[index].Service.POSServiceVersion
		snapshots[index].AuthorityStaffID = segments[index].Staff.POSStaffID
	}
	return snapshots
}

func insertBookingAttemptSegments(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, segments []BookingSegmentRecord) error {
	for index, segment := range segments {
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		mode := segment.StaffSelectionMode
		if mode == "" {
			mode = StaffSelectionSpecific
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments (
				salon_id, booking_attempt_id, service_id, staff_id, staff_selection_mode, pos_service_id,
				pos_service_version, pos_staff_id, name, duration_minutes, price_from, sort_order,
				scheduling_authority, authority_provider, authority_service_id,
				authority_service_version, authority_staff_id
			)
			SELECT attempt.salon_id, attempt.id, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6,
			       NULLIF($7::bigint, 0), NULLIF($8, ''), $9, $10, NULLIF($11, 0), $12,
			       attempt.scheduling_authority, attempt.pos_provider, $6,
			       NULLIF($7::bigint, 0), NULLIF($8, '')
			FROM booking_attempts attempt
			WHERE attempt.salon_id = $1
			  AND attempt.id = $2
			  AND attempt.scheduling_authority = 'external_provider'
		`, salonID, attemptID, segment.Service.ID, segment.Staff.ID, mode, segment.Service.POSServiceID, segment.Service.POSServiceVersion, segment.Staff.POSStaffID, segment.Service.Name, segment.Service.DurationMinutes, segment.Service.PriceFrom, sortOrder); err != nil {
			return err
		}
	}
	return nil
}

func insertAppointmentServices(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, segments []BookingSegmentRecord) error {
	for index, segment := range segments {
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		mode := segment.StaffSelectionMode
		if mode == "" {
			mode = StaffSelectionSpecific
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				salon_id, appointment_id, service_id, staff_id, staff_selection_mode, pos_service_id,
				pos_service_version, pos_staff_id, name, duration_minutes, price_from, sort_order,
				scheduling_authority, authority_provider, authority_service_id,
				authority_service_version, authority_staff_id
			)
			SELECT appointment.salon_id, appointment.id, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6,
			       NULLIF($7::bigint, 0), NULLIF($8, ''), $9, $10, NULLIF($11, 0), $12,
			       appointment.scheduling_authority, appointment.pos_provider, $6,
			       NULLIF($7::bigint, 0), NULLIF($8, '')
			FROM appointments appointment
			WHERE appointment.salon_id = $1
			  AND appointment.id = $2
			  AND appointment.scheduling_authority = 'external_provider'
		`, salonID, appointmentID, segment.Service.ID, segment.Staff.ID, mode, segment.Service.POSServiceID, segment.Service.POSServiceVersion, segment.Staff.POSStaffID, segment.Service.Name, segment.Service.DurationMinutes, segment.Service.PriceFrom, sortOrder); err != nil {
			return err
		}
	}
	return nil
}

func fallbackNotificationMessage(record FallbackBookingRecord) string {
	when := record.StartTime.Format(time.RFC3339)
	message := fmt.Sprintf("%s requested %s with %s at %s. POS returned %s: %s", record.CustomerName, record.Service.Name, record.Staff.Name, when, record.ErrorCode, record.ErrorMessage)
	if record.Status == StatusProviderPending {
		message += " The provider has not accepted this appointment; do not treat it as confirmed."
	} else if record.Reconciliation == ReconciliationRequired {
		message += " The provider result is unknown; verify it in the POS before retrying."
	}
	return message
}

func fallbackNotificationTitle(record FallbackBookingRecord) string {
	if record.Status == StatusProviderPending {
		return "Booking is pending provider acceptance"
	}
	if record.Reconciliation == ReconciliationRequired {
		return "Booking result needs POS reconciliation"
	}
	return "Booking request needs review"
}

func confirmedNotificationMessage(record ConfirmedBookingRecord) string {
	when := record.StartTime.Format(time.RFC3339)
	return fmt.Sprintf("%s booked %s with %s at %s. The active POS returned a booking ID.", record.CustomerName, record.Service.Name, record.Staff.Name, when)
}

func appointmentActionFallbackTitle(record AppointmentActionFallbackRecord) string {
	if record.Status == StatusProviderPending {
		return "Appointment action is pending provider acceptance"
	}
	if record.Reconciliation == ReconciliationRequired {
		return "Appointment action needs POS reconciliation"
	}
	switch record.NotificationType {
	case NotificationTypeRescheduleFallback:
		return "Reschedule request needs review"
	case NotificationTypeCancellationFallback:
		return "Cancellation request needs review"
	default:
		return "Appointment request needs review"
	}
}

func appointmentActionFallbackMessage(record AppointmentActionFallbackRecord) string {
	when := record.RequestedStartTime.Format(time.RFC3339)
	message := ""
	switch record.NotificationType {
	case NotificationTypeRescheduleFallback:
		message = fmt.Sprintf("%s requested reschedule for %s with %s at %s. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, record.Appointment.Staff.Name, when, record.ErrorCode, record.ErrorMessage)
	case NotificationTypeCancellationFallback:
		message = fmt.Sprintf("%s requested cancellation for %s at %s. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, when, record.ErrorCode, record.ErrorMessage)
	default:
		message = fmt.Sprintf("%s appointment action for %s at %s needs review. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, when, record.ErrorCode, record.ErrorMessage)
	}
	if record.Status == StatusProviderPending {
		message += " The provider has not accepted this change; do not treat it as complete."
	} else if record.Reconciliation == ReconciliationRequired {
		message += " The provider result is unknown; verify it in the POS before retrying."
	}
	return message
}
