ALTER TABLE booking_attempts
    ADD COLUMN retry_of_attempt_id UUID REFERENCES booking_attempts(id) ON DELETE SET NULL,
    ADD COLUMN superseded_by_attempt_id UUID REFERENCES booking_attempts(id) ON DELETE SET NULL,
    ADD COLUMN retry_sequence INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN superseded_at TIMESTAMPTZ,
    ADD COLUMN pos_booking_version INTEGER,
    ADD COLUMN target_pos_booking_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN reconciliation_resolution TEXT,
    ADD COLUMN reconciliation_resolved_at TIMESTAMPTZ;

-- Persist provider-native identities alongside canonical links. Canonical UUIDs
-- can be re-linked after a catalog sync, so they are not sufficient evidence for
-- equal-version calendar updates or safe retry decisions.
ALTER TABLE appointments
    ADD COLUMN pos_customer_id TEXT;

ALTER TABLE appointment_services
    ADD COLUMN pos_staff_id TEXT;

ALTER TABLE booking_attempt_segments
    ADD COLUMN pos_staff_id TEXT;

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_retry_sequence_check
    CHECK (retry_sequence >= 0),
    ADD CONSTRAINT booking_attempts_retry_not_self_check
    CHECK (retry_of_attempt_id IS NULL OR retry_of_attempt_id <> id),
    ADD CONSTRAINT booking_attempts_superseded_not_self_check
    CHECK (superseded_by_attempt_id IS NULL OR superseded_by_attempt_id <> id),
    ADD CONSTRAINT booking_attempts_pos_booking_version_check
    CHECK (pos_booking_version IS NULL OR pos_booking_version >= 0),
    ADD CONSTRAINT booking_attempts_target_pos_booking_version_check
    CHECK (target_pos_booking_version >= 0),
    ADD CONSTRAINT booking_attempts_reconciliation_resolution_check
    CHECK (reconciliation_resolution IS NULL OR reconciliation_resolution IN ('provider_attached', 'not_created', 'escalated', 'superseded'));

UPDATE booking_attempts attempt
SET pos_booking_version = appointment.pos_appointment_version
FROM appointments appointment
WHERE appointment.booking_attempt_id = attempt.id
  AND attempt.pos_booking_version IS NULL;

UPDATE booking_attempts attempt
SET target_pos_booking_version = appointment.pos_appointment_version
FROM appointments appointment
WHERE appointment.id = attempt.target_appointment_id
  AND attempt.operation_type IN ('reschedule', 'cancel');

ALTER TABLE booking_attempts
    DROP CONSTRAINT IF EXISTS booking_attempts_status_check;

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_status_check
    CHECK (status IN (
        'started', 'pos_pending', 'provider_pending', 'confirmed', 'rescheduled',
        'cancelled', 'declined', 'no_show', 'unknown', 'fallback_pending', 'failed'
    ));

ALTER TABLE appointments
    DROP CONSTRAINT IF EXISTS appointments_status_check;

ALTER TABLE appointments
    ADD CONSTRAINT appointments_status_check
    CHECK (status IN ('provider_pending', 'confirmed', 'rescheduled', 'cancelled', 'declined', 'no_show', 'unknown'));

-- Older single-service rows predate the ordered segment ledger. Backfill only
-- rows with a provider-mapped service; anything that cannot be reconstructed
-- remains intentionally ineligible for exact provider reconciliation.
INSERT INTO booking_attempt_segments (
    booking_attempt_id, service_id, staff_id, staff_selection_mode,
    pos_service_id, pos_service_version, name, duration_minutes, price_from,
    sort_order
)
SELECT attempt.id, service.id, attempt.staff_id,
       COALESCE(attempt.staff_selection_mode, 'specific'),
       service.pos_service_id, service.pos_service_version, service.name,
       service.duration_minutes, service.price_from, 1
FROM booking_attempts attempt
JOIN services service
  ON service.id = attempt.service_id
 AND service.salon_id = attempt.salon_id
WHERE NOT EXISTS (
    SELECT 1
    FROM booking_attempt_segments segment
    WHERE segment.booking_attempt_id = attempt.id
)
  AND COALESCE(service.pos_service_id, '') <> '';

UPDATE appointment_services segment
SET staff_id = appointment.staff_id,
    staff_selection_mode = COALESCE(appointment.staff_selection_mode, 'specific')
FROM appointments appointment
WHERE segment.appointment_id = appointment.id
  AND segment.staff_id IS NULL
  AND appointment.staff_id IS NOT NULL;

INSERT INTO appointment_services (
    appointment_id, service_id, staff_id, staff_selection_mode,
    pos_service_id, pos_service_version, name, duration_minutes, price_from,
    sort_order
)
SELECT appointment.id, service.id, appointment.staff_id,
       COALESCE(appointment.staff_selection_mode, 'specific'),
       service.pos_service_id, service.pos_service_version, service.name,
       service.duration_minutes, service.price_from, 1
FROM appointments appointment
JOIN services service
  ON service.id = appointment.service_id
 AND service.salon_id = appointment.salon_id
WHERE NOT EXISTS (
    SELECT 1
    FROM appointment_services segment
    WHERE segment.appointment_id = appointment.id
)
  AND COALESCE(service.pos_service_id, '') <> '';

-- Historical duplicates with the same normalized logical request retain their
-- provider outcome for audit. A dispatched/uncertain attempt takes precedence
-- over not-started rows; multiple dispatched rows require manual reconciliation
-- because a shared fingerprint does not prove that only one POS booking exists.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM booking_attempts
        WHERE request_fingerprint IS NOT NULL
          AND (
              status IN ('pos_pending', 'provider_pending')
              OR reconciliation_status = 'required'
          )
          AND superseded_at IS NULL
        GROUP BY salon_id, operation_type, request_fingerprint
        HAVING count(*) FILTER (WHERE provider_outcome <> 'not_started') > 1
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23505',
            MESSAGE = 'V39 cannot safely normalize multiple dispatched booking attempts with the same request fingerprint',
            HINT = 'Reconcile the provider outcomes before retrying this migration.';
    END IF;
END
$$;

WITH ranked AS (
    SELECT id, provider_outcome,
           first_value(id) OVER (
               PARTITION BY salon_id, operation_type, request_fingerprint
               ORDER BY
                   CASE WHEN provider_outcome <> 'not_started' THEN 0 ELSE 1 END,
                   created_at ASC,
                   id ASC
           ) AS canonical_attempt_id,
           row_number() OVER (
               PARTITION BY salon_id, operation_type, request_fingerprint
               ORDER BY
                   CASE WHEN provider_outcome <> 'not_started' THEN 0 ELSE 1 END,
                   created_at ASC,
                   id ASC
           ) AS duplicate_rank
    FROM booking_attempts
    WHERE request_fingerprint IS NOT NULL
      AND (
          status IN ('pos_pending', 'provider_pending')
          OR reconciliation_status = 'required'
      )
      AND superseded_at IS NULL
)
UPDATE booking_attempts attempt
SET superseded_by_attempt_id = ranked.canonical_attempt_id,
    superseded_at = now(),
    reconciliation_status = 'resolved',
    reconciliation_resolution = 'superseded',
    reconciliation_resolved_at = COALESCE(attempt.reconciliation_resolved_at, now()),
    updated_at = now()
FROM ranked
WHERE attempt.id = ranked.id
  AND ranked.duplicate_rank > 1
  AND ranked.provider_outcome = 'not_started';

CREATE UNIQUE INDEX idx_booking_attempts_active_fingerprint
    ON booking_attempts(salon_id, operation_type, request_fingerprint)
    WHERE request_fingerprint IS NOT NULL
      AND superseded_at IS NULL
      AND (
          status IN ('pos_pending', 'provider_pending')
          OR reconciliation_status = 'required'
      );

CREATE UNIQUE INDEX idx_booking_attempts_single_retry
    ON booking_attempts(retry_of_attempt_id)
    WHERE retry_of_attempt_id IS NOT NULL;

CREATE INDEX idx_booking_attempts_retry_lineage
    ON booking_attempts(retry_of_attempt_id, retry_sequence)
    WHERE retry_of_attempt_id IS NOT NULL;

CREATE TABLE availability_quotes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_location_id TEXT NOT NULL,
    provider_snapshot_generation BIGINT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    consumed_by_attempt_id UUID REFERENCES booking_attempts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (length(trim(provider_location_id)) > 0),
    CHECK (provider_snapshot_generation > 0),
    CHECK (consumed_by_attempt_id IS NULL OR consumed_at IS NOT NULL)
);

CREATE INDEX idx_availability_quotes_salon_expiry
    ON availability_quotes(salon_id, expires_at DESC);

CREATE INDEX idx_availability_quotes_unconsumed_cleanup
    ON availability_quotes(expires_at, id)
    WHERE consumed_at IS NULL AND consumed_by_attempt_id IS NULL;

CREATE INDEX idx_availability_quotes_consumed_cleanup
    ON availability_quotes(consumed_at, id)
    WHERE consumed_at IS NOT NULL AND consumed_by_attempt_id IS NULL;

CREATE TABLE availability_quote_slots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_id UUID NOT NULL REFERENCES availability_quotes(id) ON DELETE CASCADE,
    slot_fingerprint TEXT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    segments JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (quote_id, slot_fingerprint),
    CHECK (length(slot_fingerprint) = 64),
    CHECK (jsonb_typeof(segments) = 'array'),
    CHECK (end_time > start_time)
);

-- Preserve the exact provider-backed slot after the offered-slot list is
-- cleared. There is deliberately no foreign key to availability_quotes so
-- retention cleanup can delete expired quotes without breaking call history.
ALTER TABLE call_sessions
    ADD COLUMN availability_quote_id UUID,
    ADD COLUMN availability_slot_fingerprint TEXT,
    ADD CONSTRAINT call_sessions_availability_quote_pair_check
    CHECK (
        (availability_quote_id IS NULL AND availability_slot_fingerprint IS NULL)
        OR (
            availability_quote_id IS NOT NULL
            AND availability_slot_fingerprint IS NOT NULL
            AND length(availability_slot_fingerprint) = 64
        )
    );

ALTER TABLE booking_attempts
    ADD COLUMN availability_quote_id UUID REFERENCES availability_quotes(id) ON DELETE SET NULL,
    ADD COLUMN availability_slot_fingerprint TEXT,
    ADD COLUMN provider_location_id TEXT,
    ADD COLUMN provider_snapshot_generation BIGINT;

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_availability_quote_pair_check
    CHECK (
        availability_quote_id IS NULL
        OR (
            availability_slot_fingerprint IS NOT NULL
            AND length(availability_slot_fingerprint) = 64
        )
    );

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_provider_fence_pair_check
    CHECK (
        (provider_location_id IS NULL AND provider_snapshot_generation IS NULL)
        OR (
            length(trim(provider_location_id)) > 0
            AND provider_snapshot_generation > 0
        )
    );

CREATE INDEX idx_booking_attempts_availability_quote
    ON booking_attempts(availability_quote_id)
    WHERE availability_quote_id IS NOT NULL;

CREATE TABLE booking_reconciliation_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    booking_attempt_id UUID NOT NULL REFERENCES booking_attempts(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'escalated')),
    resolution TEXT CHECK (resolution IS NULL OR resolution IN ('provider_attached', 'not_created', 'escalated', 'superseded')),
    assigned_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    resolution_note TEXT,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (booking_attempt_id),
    CHECK (
        (status = 'open' AND resolution IS NULL AND resolved_at IS NULL)
        OR (status = 'escalated' AND resolution = 'escalated' AND resolved_at IS NULL)
        OR (
            status = 'resolved'
            AND resolution IN ('provider_attached', 'not_created', 'superseded')
            AND resolved_at IS NOT NULL
        )
    )
);

CREATE INDEX idx_booking_reconciliation_tasks_salon_status
    ON booking_reconciliation_tasks(salon_id, status, created_at ASC);

CREATE TABLE booking_reconciliation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    booking_attempt_id UUID NOT NULL REFERENCES booking_attempts(id) ON DELETE CASCADE,
    reconciliation_task_id UUID NOT NULL REFERENCES booking_reconciliation_tasks(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action_key TEXT NOT NULL,
    payload_fingerprint TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('opened', 'provider_attached', 'not_created', 'escalated', 'superseded')),
    provider_appointment_id TEXT,
    provider_appointment_version INTEGER,
    provider_status TEXT,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (provider_appointment_version IS NULL OR provider_appointment_version >= 0)
);

CREATE UNIQUE INDEX idx_booking_reconciliation_events_action_key
    ON booking_reconciliation_events(reconciliation_task_id, action_key);

CREATE INDEX idx_booking_reconciliation_events_attempt
    ON booking_reconciliation_events(booking_attempt_id, created_at ASC);

INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
SELECT salon_id, id, 'open'
FROM booking_attempts
WHERE reconciliation_status = 'required'
  AND superseded_at IS NULL
ON CONFLICT (booking_attempt_id) DO NOTHING;

INSERT INTO booking_reconciliation_events (
    salon_id, booking_attempt_id, reconciliation_task_id, action_key,
    payload_fingerprint, action, note
)
SELECT task.salon_id, task.booking_attempt_id, task.id, 'opened:backfill',
       encode(digest(task.booking_attempt_id::text || ':opened:backfill', 'sha256'), 'hex'), 'opened',
       'Reconciliation task backfilled from an unknown provider outcome.'
FROM booking_reconciliation_tasks task
WHERE NOT EXISTS (
    SELECT 1
    FROM booking_reconciliation_events event
    WHERE event.reconciliation_task_id = task.id
);

-- Preserve an explicit terminal task for historical attempts that V39
-- superseded while normalizing duplicate logical requests. This makes the
-- reconciliation ledger truthful instead of relying only on queue filters.
INSERT INTO booking_reconciliation_tasks (
    salon_id, booking_attempt_id, status, resolution, resolution_note,
    resolved_at, created_at, updated_at
)
SELECT attempt.salon_id, attempt.id, 'resolved', 'superseded',
       'Booking attempt was superseded by a canonical attempt.',
       COALESCE(attempt.reconciliation_resolved_at, attempt.superseded_at, now()),
       attempt.created_at, now()
FROM booking_attempts attempt
WHERE attempt.superseded_at IS NOT NULL
  AND attempt.superseded_by_attempt_id IS NOT NULL
  AND attempt.reconciliation_status = 'resolved'
  AND attempt.reconciliation_resolution = 'superseded'
ON CONFLICT (booking_attempt_id) DO UPDATE
SET status = 'resolved',
    resolution = 'superseded',
    resolved_by_user_id = NULL,
    resolution_note = 'Booking attempt was superseded by a canonical attempt.',
    resolved_at = COALESCE(
        booking_reconciliation_tasks.resolved_at,
        EXCLUDED.resolved_at,
        now()
    ),
    updated_at = now();

INSERT INTO booking_reconciliation_events (
    salon_id, booking_attempt_id, reconciliation_task_id, action_key,
    payload_fingerprint, action, note
)
SELECT task.salon_id, task.booking_attempt_id, task.id,
       'superseded:' || attempt.superseded_by_attempt_id::text,
       encode(digest(
           task.booking_attempt_id::text || ':superseded:' || attempt.superseded_by_attempt_id::text,
           'sha256'
       ), 'hex'),
       'superseded', 'Booking attempt was superseded by a canonical attempt.'
FROM booking_reconciliation_tasks task
JOIN booking_attempts attempt ON attempt.id = task.booking_attempt_id
WHERE task.status = 'resolved'
  AND task.resolution = 'superseded'
  AND attempt.superseded_by_attempt_id IS NOT NULL
ON CONFLICT (reconciliation_task_id, action_key) DO NOTHING;

ALTER TABLE owner_notifications
    ADD COLUMN dedupe_key TEXT,
    ADD COLUMN payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN delivery_status TEXT NOT NULL DEFAULT 'queued',
    ADD COLUMN delivery_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN next_delivery_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN delivered_at TIMESTAMPTZ,
    ADD COLUMN last_delivery_error TEXT;

-- Historical in-product notifications predate the outbox payload contract and
-- must not become newly deliverable merely because this migration was applied.
UPDATE owner_notifications
SET delivery_status = 'disabled'
WHERE payload = '{}'::jsonb;

ALTER TABLE owner_notifications
    ADD CONSTRAINT owner_notifications_delivery_status_check
    CHECK (delivery_status IN ('queued', 'delivering', 'delivered', 'failed', 'disabled')),
    ADD CONSTRAINT owner_notifications_delivery_attempts_check
    CHECK (delivery_attempts >= 0),
    ADD CONSTRAINT owner_notifications_payload_object_check
    CHECK (jsonb_typeof(payload) = 'object');

CREATE UNIQUE INDEX idx_owner_notifications_salon_dedupe
    ON owner_notifications(salon_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL;

CREATE INDEX idx_owner_notifications_delivery_queue
    ON owner_notifications(next_delivery_at ASC, created_at ASC)
    WHERE delivery_status IN ('queued', 'failed');
