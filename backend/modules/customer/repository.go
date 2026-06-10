package customer

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
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

func (r *Repository) ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Record, error) {
	rows, err := r.db.QueryContext(ctx, customerListQuery, salonID, ownerUserID, limit)
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

func scanRecord(row interface {
	Scan(dest ...any) error
}) (*Record, error) {
	var item Record
	var latestAppointmentAt sql.NullTime
	var latestRequestAt sql.NullTime
	err := row.Scan(
		&item.Key,
		&item.Name,
		&item.Phone,
		&item.Email,
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

const customerListQuery = `
WITH events AS (
	SELECT a.salon_id,
	       a.id::text AS record_id,
	       'appointment' AS source,
	       a.status AS outcome,
	       NULLIF(trim(a.customer_name), '') AS customer_name,
	       NULLIF(trim(a.customer_phone), '') AS customer_phone,
	       NULLIF(trim(a.customer_email), '') AS customer_email,
	       regexp_replace(COALESCE(a.customer_phone, ''), '[^0-9+]', '', 'g') AS phone_key,
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
	       regexp_replace(COALESCE(ba.customer_phone, ''), '[^0-9+]', '', 'g') AS phone_key,
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
	       regexp_replace(COALESCE(cs.customer_phone, ''), '[^0-9+]', '', 'g') AS phone_key,
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
	       regexp_replace(COALESCE(hr.customer_phone, ''), '[^0-9+]', '', 'g') AS phone_key,
	       hr.created_at AS activity_at
	FROM handoff_requests hr
	WHERE hr.salon_id = $1
),
owned_events AS (
	SELECT e.*,
	       CASE
	         WHEN NULLIF(e.phone_key, '') IS NOT NULL THEN 'phone:' || e.phone_key
	         WHEN e.customer_email IS NOT NULL THEN 'email:' || lower(e.customer_email)
	         WHEN e.customer_name IS NOT NULL THEN 'name:' || lower(e.customer_name)
	         ELSE ''
	       END AS customer_key
	FROM events e
	JOIN salons s ON s.id = e.salon_id
	WHERE e.salon_id = $1
	  AND s.owner_user_id = $2
	  AND (e.customer_name IS NOT NULL OR e.customer_phone IS NOT NULL OR e.customer_email IS NOT NULL)
),
ranked AS (
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
	FROM owned_events
	WHERE customer_key <> ''
),
grouped AS (
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
	FROM ranked
	GROUP BY customer_key
)
SELECT customer_key, name, phone, email, last_activity_at, last_activity_source, last_outcome,
       confirmed_appointments, pending_requests, call_count, handoff_count,
       appointment_ids, booking_attempt_ids, call_session_ids, latest_appointment_at, latest_request_at
FROM grouped
ORDER BY last_activity_at DESC
LIMIT $3
`
