package customer

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Repository struct {
	db *sql.DB
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
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) ([]Record, error) {
	rows, err := r.db.QueryContext(ctx, customerListQuery, salonID, ownerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Record, 0)
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CustomerSummary(ctx context.Context, salonID string, ownerUserID string) (Summary, error) {
	var summary Summary
	var lastActivityAt sql.NullTime
	err := r.db.QueryRowContext(ctx, customerSummaryQuery, salonID, ownerUserID).Scan(
		&summary.TotalKnownCustomers,
		&summary.ActiveCustomers,
		&summary.POSLinkedCustomers,
		&summary.ConfirmedAppointments,
		&summary.PendingRequests,
		&summary.CustomersWithCalls,
		&lastActivityAt,
	)
	if err != nil {
		return Summary{}, err
	}
	if lastActivityAt.Valid {
		value := lastActivityAt.Time
		summary.LastCustomerActivityAt = &value
	}
	return summary, nil
}

func (r *Repository) CreateCustomer(ctx context.Context, salonID string, ownerUserID string, input Mutation) (*Record, error) {
	if err := r.ensureUniqueCustomer(ctx, salonID, "", input); err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var customerID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO customers (
			salon_id, name, phone, normalized_phone, email, normalized_email, notes, active,
			sync_status, source
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, 'local_only', 'local')
		RETURNING id::text
	`, salonID, input.Name, input.Phone, input.NormalizedPhone, input.Email, input.NormalizedEmail, input.Notes, input.Active).Scan(&customerID)
	if err != nil {
		return nil, mapCustomerWriteError(err)
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
	`, salonID, customerID, pos.ProviderSquare); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getCustomerForOwner(ctx, salonID, ownerUserID, customerID)
}

func (r *Repository) UpdateCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string, input Mutation) (*Record, error) {
	current, err := r.getCustomerForOwner(ctx, salonID, ownerUserID, customerID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return nil, ErrValidation
	}
	if err := r.ensureUniqueCustomer(ctx, salonID, customerID, input); err != nil {
		return nil, err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE customers
		SET name = $1,
		    phone = NULLIF($2, ''),
		    normalized_phone = NULLIF($3, ''),
		    email = NULLIF($4, ''),
		    normalized_email = NULLIF($5, ''),
		    notes = NULLIF($6, ''),
		    active = $7,
		    updated_at = now()
		WHERE id = $8
		  AND salon_id = $9
	`, input.Name, input.Phone, input.NormalizedPhone, input.Email, input.NormalizedEmail, input.Notes, input.Active, customerID, salonID)
	if err != nil {
		return nil, mapCustomerWriteError(err)
	}
	return r.getCustomerForOwner(ctx, salonID, ownerUserID, customerID)
}

func (r *Repository) ArchiveCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string) (*Record, error) {
	current, err := r.getCustomerForOwner(ctx, salonID, ownerUserID, customerID)
	if err != nil {
		return nil, err
	}
	if current.ArchivedAt != nil {
		return current, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET active = false,
		    sync_status = 'archived',
		    archived_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND salon_id = $2
	`, customerID, salonID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET sync_status = 'archived',
		    updated_at = now()
		WHERE salon_id = $1
		  AND entity_type = 'customer'
		  AND entity_id = $2
	`, salonID, customerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getCustomerForOwner(ctx, salonID, ownerUserID, customerID)
}

func scanRecord(row interface {
	Scan(dest ...any) error
}) (*Record, error) {
	var item Record
	var archivedAt sql.NullTime
	var lastSyncedAt sql.NullTime
	var latestAppointmentAt sql.NullTime
	var latestRequestAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.Key,
		&item.Name,
		&item.Phone,
		&item.Email,
		&item.Notes,
		&item.Active,
		&item.SyncStatus,
		&archivedAt,
		&lastSyncedAt,
		&item.SyncError,
		&item.Source,
		&item.POSLinked,
		&item.LastActivityAt,
		&item.LastActivitySource,
		&item.LastOutcome,
		&item.ConfirmedAppointments,
		&item.PendingRequests,
		&item.CallCount,
		&item.HandoffCount,
		pq.Array(&item.AppointmentIDs),
		pq.Array(&item.BookingAttemptIDs),
		pq.Array(&item.CallSessionIDs),
		&latestAppointmentAt,
		&latestRequestAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archivedAt.Valid {
		value := archivedAt.Time
		item.ArchivedAt = &value
	}
	if lastSyncedAt.Valid {
		value := lastSyncedAt.Time
		item.LastSyncedAt = &value
	}
	if latestAppointmentAt.Valid {
		value := latestAppointmentAt.Time
		item.LatestAppointmentAt = &value
	}
	if latestRequestAt.Valid {
		value := latestRequestAt.Time
		item.LatestRequestAt = &value
	}
	return &item, nil
}

func (r *Repository) getCustomerForOwner(ctx context.Context, salonID string, ownerUserID string, customerID string) (*Record, error) {
	row := r.db.QueryRowContext(ctx, customerByIDQuery, salonID, ownerUserID, customerID)
	return scanRecord(row)
}

func (r *Repository) ensureUniqueCustomer(ctx context.Context, salonID string, customerID string, input Mutation) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM customers
			WHERE salon_id = $1
			  AND archived_at IS NULL
			  AND id <> COALESCE(NULLIF($2, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
			  AND (
			    (NULLIF($3, '') IS NOT NULL AND normalized_phone = NULLIF($3, ''))
			    OR (NULLIF($4, '') IS NOT NULL AND normalized_email = NULLIF($4, ''))
			  )
		)
	`, salonID, customerID, input.NormalizedPhone, input.NormalizedEmail).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicate
	}
	return nil
}

func mapCustomerWriteError(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrDuplicate
	}
	return err
}

const customerByIDQuery = `
SELECT c.id::text,
       c.salon_id::text,
       'customer:' || c.id::text,
       c.name,
       COALESCE(c.phone, ''),
       COALESCE(c.email, ''),
       COALESCE(c.notes, ''),
       c.active,
       c.sync_status,
       c.archived_at,
       c.last_synced_at,
       COALESCE(c.sync_error, ''),
       c.source,
       EXISTS (
           SELECT 1
           FROM pos_entity_links link
           WHERE link.salon_id = c.salon_id
             AND link.entity_type = 'customer'
             AND link.entity_id = c.id
             AND link.provider_entity_id IS NOT NULL
             AND link.sync_status = 'synced'
       ) AS pos_linked,
       c.updated_at,
       '',
       '',
       0,
       0,
       0,
       0,
       ARRAY[]::text[],
       ARRAY[]::text[],
       ARRAY[]::text[],
       NULL::timestamptz,
       NULL::timestamptz
FROM customers c
JOIN salons salon ON salon.id = c.salon_id
WHERE c.salon_id = $1
  AND salon.owner_user_id = $2
  AND c.id = $3
`

const customerRowsQuery = `
WITH canonical AS (
	SELECT c.*,
	       EXISTS (
	           SELECT 1
	           FROM pos_entity_links link
	           WHERE link.salon_id = c.salon_id
	             AND link.entity_type = 'customer'
	             AND link.entity_id = c.id
	             AND link.provider_entity_id IS NOT NULL
	             AND link.sync_status = 'synced'
	       ) AS pos_linked
	FROM customers c
	JOIN salons salon ON salon.id = c.salon_id
	WHERE c.salon_id = $1
	  AND salon.owner_user_id = $2
),
events AS (
	SELECT a.salon_id,
	       a.id::text AS record_id,
	       'appointment' AS source,
	       a.status AS outcome,
	       NULLIF(trim(a.customer_name), '') AS customer_name,
	       NULLIF(trim(a.customer_phone), '') AS customer_phone,
	       NULLIF(trim(a.customer_email), '') AS customer_email,
	       NULLIF(regexp_replace(COALESCE(a.customer_phone, ''), '[^0-9+]', '', 'g'), '') AS phone_key,
	       NULLIF(lower(trim(COALESCE(a.customer_email, ''))), '') AS email_key,
	       a.start_time AS activity_at
	FROM appointments a
	WHERE a.salon_id = $1
	UNION ALL
	SELECT ba.salon_id,
	       ba.id::text AS record_id,
	       'booking_attempt' AS source,
	       ba.status AS outcome,
	       NULLIF(trim(ba.customer_name), '') AS customer_name,
	       NULLIF(trim(ba.customer_phone), '') AS customer_phone,
	       NULLIF(trim(ba.customer_email), '') AS customer_email,
	       NULLIF(regexp_replace(COALESCE(ba.customer_phone, ''), '[^0-9+]', '', 'g'), '') AS phone_key,
	       NULLIF(lower(trim(COALESCE(ba.customer_email, ''))), '') AS email_key,
	       ba.created_at AS activity_at
	FROM booking_attempts ba
	WHERE ba.salon_id = $1
	UNION ALL
	SELECT cs.salon_id,
	       cs.id::text AS record_id,
	       'call' AS source,
	       cs.outcome AS outcome,
	       NULLIF(trim(cs.customer_name), '') AS customer_name,
	       NULLIF(trim(cs.customer_phone), '') AS customer_phone,
	       NULLIF(trim(cs.customer_email), '') AS customer_email,
	       NULLIF(regexp_replace(COALESCE(cs.customer_phone, ''), '[^0-9+]', '', 'g'), '') AS phone_key,
	       NULLIF(lower(trim(COALESCE(cs.customer_email, ''))), '') AS email_key,
	       cs.updated_at AS activity_at
	FROM call_sessions cs
	WHERE cs.salon_id = $1
	UNION ALL
	SELECT hr.salon_id,
	       hr.id::text AS record_id,
	       'handoff' AS source,
	       hr.status AS outcome,
	       NULLIF(trim(hr.customer_name), '') AS customer_name,
	       NULLIF(trim(hr.customer_phone), '') AS customer_phone,
	       NULL AS customer_email,
	       NULLIF(regexp_replace(COALESCE(hr.customer_phone, ''), '[^0-9+]', '', 'g'), '') AS phone_key,
	       NULL AS email_key,
	       hr.created_at AS activity_at
	FROM handoff_requests hr
	WHERE hr.salon_id = $1
),
owned_events AS (
	SELECT e.*
	FROM events e
	JOIN salons s ON s.id = e.salon_id
	WHERE e.salon_id = $1
	  AND s.owner_user_id = $2
	  AND (e.customer_name IS NOT NULL OR e.customer_phone IS NOT NULL OR e.customer_email IS NOT NULL)
),
canonical_ranked AS (
	SELECT c.id AS customer_id,
	       e.*,
	       row_number() OVER (PARTITION BY c.id ORDER BY e.activity_at DESC, e.source ASC, e.record_id DESC) AS recency_rank,
	       row_number() OVER (
	         PARTITION BY c.id
	         ORDER BY CASE WHEN e.customer_name IS NOT NULL THEN 0 ELSE 1 END, e.activity_at DESC, e.record_id DESC
	       ) AS name_rank,
	       row_number() OVER (
	         PARTITION BY c.id
	         ORDER BY CASE WHEN e.customer_phone IS NOT NULL THEN 0 ELSE 1 END, e.activity_at DESC, e.record_id DESC
	       ) AS phone_rank,
	       row_number() OVER (
	         PARTITION BY c.id
	         ORDER BY CASE WHEN e.customer_email IS NOT NULL THEN 0 ELSE 1 END, e.activity_at DESC, e.record_id DESC
	       ) AS email_rank
	FROM canonical c
	JOIN owned_events e
	  ON (c.normalized_phone IS NOT NULL AND e.phone_key = c.normalized_phone)
	  OR (c.normalized_email IS NOT NULL AND e.email_key = c.normalized_email)
),
canonical_activity AS (
	SELECT customer_id,
	       max(activity_at) AS last_activity_at,
	       COALESCE(max(source) FILTER (WHERE recency_rank = 1), '') AS last_activity_source,
	       COALESCE(max(outcome) FILTER (WHERE recency_rank = 1), '') AS last_outcome,
	       count(DISTINCT record_id) FILTER (WHERE source = 'appointment' AND outcome IN ('confirmed', 'rescheduled'))::int AS confirmed_appointments,
	       count(DISTINCT record_id) FILTER (WHERE source = 'booking_attempt' AND outcome = 'fallback_pending')::int AS pending_requests,
	       count(DISTINCT record_id) FILTER (WHERE source = 'call')::int AS call_count,
	       count(DISTINCT record_id) FILTER (WHERE source = 'handoff')::int AS handoff_count,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'appointment'), ARRAY[]::text[]) AS appointment_ids,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'booking_attempt'), ARRAY[]::text[]) AS booking_attempt_ids,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'call'), ARRAY[]::text[]) AS call_session_ids,
	       max(activity_at) FILTER (WHERE source = 'appointment') AS latest_appointment_at,
	       max(activity_at) FILTER (WHERE source = 'booking_attempt' AND outcome = 'fallback_pending') AS latest_request_at
	FROM canonical_ranked
	GROUP BY customer_id
),
activity_unmatched AS (
	SELECT e.*,
	       CASE
	         WHEN e.phone_key IS NOT NULL THEN 'phone:' || e.phone_key
	         WHEN e.email_key IS NOT NULL THEN 'email:' || e.email_key
	         WHEN e.customer_name IS NOT NULL THEN 'name:' || lower(e.customer_name)
	         ELSE ''
	       END AS customer_key
	FROM owned_events e
	WHERE NOT EXISTS (
		SELECT 1
		FROM canonical c
		WHERE (c.normalized_phone IS NOT NULL AND e.phone_key = c.normalized_phone)
		   OR (c.normalized_email IS NOT NULL AND e.email_key = c.normalized_email)
	)
),
activity_ranked AS (
	SELECT *,
	       row_number() OVER (PARTITION BY customer_key ORDER BY activity_at DESC, source ASC, record_id DESC) AS recency_rank,
	       row_number() OVER (
	       	PARTITION BY customer_key
	       	ORDER BY CASE WHEN customer_name IS NOT NULL THEN 0 ELSE 1 END, activity_at DESC, record_id DESC
	       ) AS name_rank,
	       row_number() OVER (
	       	PARTITION BY customer_key
	       	ORDER BY CASE WHEN customer_phone IS NOT NULL THEN 0 ELSE 1 END, activity_at DESC, record_id DESC
	       ) AS phone_rank,
	       row_number() OVER (
	       	PARTITION BY customer_key
	       	ORDER BY CASE WHEN customer_email IS NOT NULL THEN 0 ELSE 1 END, activity_at DESC, record_id DESC
	       ) AS email_rank
	FROM activity_unmatched
	WHERE customer_key <> ''
),
activity_grouped AS (
	SELECT customer_key,
	       COALESCE(max(customer_name) FILTER (WHERE name_rank = 1), '') AS name,
	       COALESCE(max(customer_phone) FILTER (WHERE phone_rank = 1), '') AS phone,
	       COALESCE(max(customer_email) FILTER (WHERE email_rank = 1), '') AS email,
	       max(activity_at) AS last_activity_at,
	       COALESCE(max(source) FILTER (WHERE recency_rank = 1), '') AS last_activity_source,
	       COALESCE(max(outcome) FILTER (WHERE recency_rank = 1), '') AS last_outcome,
	       count(DISTINCT record_id) FILTER (WHERE source = 'appointment' AND outcome IN ('confirmed', 'rescheduled'))::int AS confirmed_appointments,
	       count(DISTINCT record_id) FILTER (WHERE source = 'booking_attempt' AND outcome = 'fallback_pending')::int AS pending_requests,
	       count(DISTINCT record_id) FILTER (WHERE source = 'call')::int AS call_count,
	       count(DISTINCT record_id) FILTER (WHERE source = 'handoff')::int AS handoff_count,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'appointment'), ARRAY[]::text[]) AS appointment_ids,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'booking_attempt'), ARRAY[]::text[]) AS booking_attempt_ids,
	       COALESCE(array_agg(DISTINCT record_id) FILTER (WHERE source = 'call'), ARRAY[]::text[]) AS call_session_ids,
	       max(activity_at) FILTER (WHERE source = 'appointment') AS latest_appointment_at,
	       max(activity_at) FILTER (WHERE source = 'booking_attempt' AND outcome = 'fallback_pending') AS latest_request_at
	FROM activity_ranked
	GROUP BY customer_key
),
rows AS (
	SELECT c.id::text AS id,
	       c.salon_id::text AS salon_id,
	       'customer:' || c.id::text AS key,
	       c.name,
	       COALESCE(c.phone, '') AS phone,
	       COALESCE(c.email, '') AS email,
	       COALESCE(c.notes, '') AS notes,
	       c.active,
	       c.sync_status,
	       c.archived_at,
	       c.last_synced_at,
	       COALESCE(c.sync_error, '') AS sync_error,
	       c.source,
	       c.pos_linked,
	       COALESCE(a.last_activity_at, c.updated_at) AS last_activity_at,
	       COALESCE(a.last_activity_source, '') AS last_activity_source,
	       COALESCE(a.last_outcome, '') AS last_outcome,
	       COALESCE(a.confirmed_appointments, 0) AS confirmed_appointments,
	       COALESCE(a.pending_requests, 0) AS pending_requests,
	       COALESCE(a.call_count, 0) AS call_count,
	       COALESCE(a.handoff_count, 0) AS handoff_count,
	       COALESCE(a.appointment_ids, ARRAY[]::text[]) AS appointment_ids,
	       COALESCE(a.booking_attempt_ids, ARRAY[]::text[]) AS booking_attempt_ids,
	       COALESCE(a.call_session_ids, ARRAY[]::text[]) AS call_session_ids,
	       a.latest_appointment_at,
	       a.latest_request_at
	FROM canonical c
	LEFT JOIN canonical_activity a ON a.customer_id = c.id
	UNION ALL
	SELECT '' AS id,
	       $1::text AS salon_id,
	       g.customer_key AS key,
	       g.name,
	       g.phone,
	       g.email,
	       '' AS notes,
	       false AS active,
	       'unmapped' AS sync_status,
	       NULL::timestamptz AS archived_at,
	       NULL::timestamptz AS last_synced_at,
	       '' AS sync_error,
	       'activity' AS source,
	       false AS pos_linked,
	       g.last_activity_at,
	       g.last_activity_source,
	       g.last_outcome,
	       g.confirmed_appointments,
	       g.pending_requests,
	       g.call_count,
	       g.handoff_count,
	       g.appointment_ids,
	       g.booking_attempt_ids,
	       g.call_session_ids,
	       g.latest_appointment_at,
	       g.latest_request_at
	FROM activity_grouped g
)
`

const customerListQuery = customerRowsQuery + `
SELECT id, salon_id, key, name, phone, email, notes, active, sync_status, archived_at, last_synced_at,
       sync_error, source, pos_linked, last_activity_at, last_activity_source, last_outcome,
       confirmed_appointments, pending_requests, call_count, handoff_count,
       appointment_ids, booking_attempt_ids, call_session_ids, latest_appointment_at, latest_request_at
FROM rows
ORDER BY (archived_at IS NOT NULL) ASC, active DESC, last_activity_at DESC, key ASC
LIMIT $3
OFFSET $4
`

const customerSummaryQuery = customerRowsQuery + `
SELECT count(*)::int,
       (count(*) FILTER (WHERE id <> '' AND active = true AND archived_at IS NULL))::int,
       (count(*) FILTER (WHERE pos_linked = true))::int,
       COALESCE(sum(confirmed_appointments), 0)::int,
       COALESCE(sum(pending_requests), 0)::int,
       (count(*) FILTER (WHERE call_count > 0))::int,
       max(last_activity_at)
FROM rows
`
