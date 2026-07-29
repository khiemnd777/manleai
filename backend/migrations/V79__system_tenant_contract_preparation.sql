-- Contract-preparation release for system tenant isolation.
--
-- This migration is additive: it moves unbound worker discovery/claim paths
-- behind narrow SECURITY DEFINER functions and strengthens call-child tenant
-- integrity. It deliberately does not tighten provider/worker base RLS. The
-- later contract migration remains a separate release after deployment
-- observation proves every system path supplies app.system_salon_id.

CREATE OR REPLACE FUNCTION public.app_worker_discovery_allowed()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, public, pg_temp
AS $$
    SELECT public.app_database_scope() = 'worker'
       AND public.app_request_system_salon_id() IS NULL
$$;

-- Historical call children must already agree with their parent tenant. A
-- mismatch is a release blocker; this migration never reparents or deletes it.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.call_transcript_messages child
        JOIN public.call_sessions parent ON parent.id = child.session_id
        WHERE parent.salon_id <> child.salon_id
        UNION ALL
        SELECT 1
        FROM public.handoff_requests child
        JOIN public.call_sessions parent ON parent.id = child.call_session_id
        WHERE parent.salon_id <> child.salon_id
        UNION ALL
        SELECT 1
        FROM public.voice_webhook_events child
        JOIN public.call_sessions parent ON parent.id = child.call_session_id
        WHERE child.salon_id IS NULL OR parent.salon_id <> child.salon_id
        UNION ALL
        SELECT 1
        FROM public.voice_audio_outputs child
        JOIN public.call_sessions parent ON parent.id = child.call_session_id
        WHERE child.salon_id IS NULL OR parent.salon_id <> child.salon_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'call child tenant preflight failed',
            CONSTRAINT = 'call_children_salon_call_session_preflight';
    END IF;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE public.call_transcript_messages
    DROP CONSTRAINT IF EXISTS call_transcript_messages_session_id_fkey,
    ADD CONSTRAINT call_transcript_messages_salon_session_fkey
        FOREIGN KEY (salon_id, session_id)
        REFERENCES public.call_sessions(salon_id, id)
        ON DELETE CASCADE
        NOT VALID;

ALTER TABLE public.handoff_requests
    DROP CONSTRAINT IF EXISTS handoff_requests_call_session_id_fkey,
    ADD CONSTRAINT handoff_requests_salon_call_session_fkey
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES public.call_sessions(salon_id, id)
        ON DELETE CASCADE
        NOT VALID;

ALTER TABLE public.voice_webhook_events
    DROP CONSTRAINT IF EXISTS voice_webhook_events_call_session_id_fkey,
    ADD CONSTRAINT voice_webhook_events_session_requires_salon_check
        CHECK (call_session_id IS NULL OR salon_id IS NOT NULL)
        NOT VALID,
    ADD CONSTRAINT voice_webhook_events_salon_call_session_fkey
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES public.call_sessions(salon_id, id)
        ON DELETE SET NULL (call_session_id)
        NOT VALID;

ALTER TABLE public.voice_audio_outputs
    DROP CONSTRAINT IF EXISTS voice_audio_outputs_call_session_id_fkey,
    ADD CONSTRAINT voice_audio_outputs_session_requires_salon_check
        CHECK (call_session_id IS NULL OR salon_id IS NOT NULL)
        NOT VALID,
    ADD CONSTRAINT voice_audio_outputs_salon_call_session_fkey
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES public.call_sessions(salon_id, id)
        ON DELETE CASCADE
        NOT VALID;

ALTER TABLE public.call_transcript_messages
    VALIDATE CONSTRAINT call_transcript_messages_salon_session_fkey;
ALTER TABLE public.handoff_requests
    VALIDATE CONSTRAINT handoff_requests_salon_call_session_fkey;
ALTER TABLE public.voice_webhook_events
    VALIDATE CONSTRAINT voice_webhook_events_session_requires_salon_check;
ALTER TABLE public.voice_webhook_events
    VALIDATE CONSTRAINT voice_webhook_events_salon_call_session_fkey;
ALTER TABLE public.voice_audio_outputs
    VALIDATE CONSTRAINT voice_audio_outputs_session_requires_salon_check;
ALTER TABLE public.voice_audio_outputs
    VALIDATE CONSTRAINT voice_audio_outputs_salon_call_session_fkey;

CREATE OR REPLACE FUNCTION public.app_worker_claim_pos_sync_jobs(target_limit INTEGER)
RETURNS TABLE (
    job_id UUID,
    salon_id UUID,
    provider TEXT,
    entity_type TEXT,
    entity_id UUID,
    operation TEXT,
    status TEXT,
    attempt_count INTEGER,
    max_attempts INTEGER,
    next_attempt_at TIMESTAMPTZ,
    locked_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH ranked AS (
        SELECT job.id, job.next_attempt_at, job.created_at,
               row_number() OVER (
                   PARTITION BY job.salon_id
                   ORDER BY job.next_attempt_at, job.created_at, job.id
               ) AS tenant_rank,
               COALESCE(limits.worker_claims_per_batch, 2) AS tenant_limit
        FROM public.pos_sync_jobs job
        LEFT JOIN public.tenant_runtime_limits limits ON limits.salon_id = job.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND job.status IN ('queued', 'failed')
          AND job.attempt_count < job.max_attempts
          AND job.next_attempt_at <= now()
    ), candidates AS (
        SELECT job.id
        FROM public.pos_sync_jobs job
        JOIN ranked ON ranked.id = job.id
        WHERE ranked.tenant_rank <= ranked.tenant_limit
        ORDER BY ranked.next_attempt_at, ranked.created_at, job.id
        FOR UPDATE OF job SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 10), 1), 100)
    )
    UPDATE public.pos_sync_jobs job
    SET status = 'running',
        attempt_count = job.attempt_count + 1,
        locked_at = now(),
        completed_at = NULL,
        updated_at = now()
    FROM candidates
    WHERE job.id = candidates.id
    RETURNING job.id, job.salon_id, job.provider, job.entity_type, job.entity_id,
              job.operation, job.status, job.attempt_count, job.max_attempts,
              job.next_attempt_at, job.locked_at, job.completed_at,
              COALESCE(job.last_error, ''), job.created_at, job.updated_at
$$;

CREATE OR REPLACE FUNCTION public.app_worker_expired_booking_leases(target_limit INTEGER)
RETURNS TABLE (
    attempt_id UUID,
    salon_id UUID,
    provider TEXT,
    operation_type TEXT,
    target_appointment_id UUID,
    pos_booking_id TEXT
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT attempt.id, attempt.salon_id, attempt.pos_provider,
           attempt.operation_type, attempt.target_appointment_id,
           attempt.pos_booking_id
    FROM public.booking_attempts attempt
    WHERE public.app_worker_discovery_allowed()
      AND attempt.scheduling_authority = 'external_provider'
      AND attempt.status = 'pos_pending'
      AND attempt.provider_outcome IN ('not_started', 'in_flight')
      AND attempt.superseded_at IS NULL
      AND attempt.processing_lease_expires_at IS NOT NULL
      AND attempt.processing_lease_expires_at <= now()
    ORDER BY attempt.processing_lease_expires_at, attempt.id
    LIMIT LEAST(GREATEST(COALESCE(target_limit, 50), 1), 200)
$$;

CREATE OR REPLACE FUNCTION public.app_worker_cleanup_availability_quotes(
    unconsumed_expired_before TIMESTAMPTZ,
    consumed_before TIMESTAMPTZ,
    target_limit INTEGER
)
RETURNS INTEGER
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH candidates AS MATERIALIZED (
        SELECT quote.id
        FROM public.availability_quotes quote
        WHERE public.app_worker_discovery_allowed()
          AND quote.consumed_by_attempt_id IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM public.booking_attempts attempt
              WHERE attempt.availability_quote_id = quote.id
          )
          AND (
              (quote.consumed_at IS NULL AND quote.expires_at <= unconsumed_expired_before)
              OR
              (quote.consumed_at IS NOT NULL AND quote.consumed_at <= consumed_before)
          )
        ORDER BY COALESCE(quote.consumed_at, quote.expires_at), quote.id
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 100), 1), 500)
        FOR UPDATE OF quote SKIP LOCKED
    ), deleted AS (
        DELETE FROM public.availability_quotes quote
        USING candidates
        WHERE quote.id = candidates.id
        RETURNING quote.id
    )
    SELECT count(*)::INTEGER FROM deleted
$$;

CREATE OR REPLACE FUNCTION public.app_worker_claim_square_booking_webhooks(
    target_limit INTEGER,
    max_attempts_per_cycle INTEGER
)
RETURNS TABLE (
    webhook_id UUID,
    salon_id UUID,
    event_id TEXT,
    event_type TEXT,
    merchant_id TEXT,
    location_id TEXT,
    pos_booking_id TEXT,
    pos_booking_version INTEGER,
    booking_status TEXT,
    booking_start_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    processing_attempts INTEGER,
    processing_token TEXT,
    owner_user_id UUID
)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH exhausted AS (
        UPDATE public.square_booking_webhook_events event
        SET processing_status = 'dead_letter',
            processing_token = NULL,
            processing_lease_expires_at = NULL,
            dead_lettered_at = COALESCE(event.dead_lettered_at, now()),
            last_error = NULL,
            last_error_class = 'processing',
            last_error_code = 'SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED',
            updated_at = now()
        WHERE public.app_worker_discovery_allowed()
          AND event.processing_attempts >= (event.requeue_count + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
          AND (
              event.processing_status IN ('pending', 'failed')
              OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
          )
        RETURNING event.id
    ), ranked AS (
        SELECT event.id, event.next_attempt_at, event.created_at,
               row_number() OVER (
                   PARTITION BY event.salon_id
                   ORDER BY event.next_attempt_at, event.created_at, event.id
               ) AS tenant_rank,
               COALESCE(limits.worker_claims_per_batch, 2) AS tenant_limit
        FROM public.square_booking_webhook_events event
        LEFT JOIN public.tenant_runtime_limits limits ON limits.salon_id = event.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND event.processing_attempts < (event.requeue_count + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
          AND (
              (event.processing_status IN ('pending', 'failed') AND event.next_attempt_at <= now())
              OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
          )
    ), candidates AS (
        SELECT event.id
        FROM public.square_booking_webhook_events event
        JOIN ranked ON ranked.id = event.id
        WHERE ranked.tenant_rank <= ranked.tenant_limit
        ORDER BY ranked.next_attempt_at, ranked.created_at, event.id
        FOR UPDATE OF event SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 20), 1), 100)
    ), claimed AS (
        UPDATE public.square_booking_webhook_events event
        SET processing_status = 'processing',
            processing_attempts = event.processing_attempts + 1,
            processing_token = gen_random_uuid()::TEXT,
            processing_lease_expires_at = now() + interval '4 minutes',
            dead_lettered_at = NULL,
            last_error = NULL,
            last_error_class = NULL,
            last_error_code = NULL,
            updated_at = now()
        FROM candidates
        WHERE event.id = candidates.id
        RETURNING event.id, event.salon_id, event.event_id, event.event_type,
                  event.merchant_id, event.location_id, event.pos_booking_id,
                  COALESCE(event.pos_booking_version, 0), COALESCE(event.booking_status, ''),
                  event.booking_start_at, event.delivered_at, event.processing_attempts,
                  event.processing_token
    )
    SELECT claimed.*, salon.owner_user_id
    FROM claimed
    JOIN public.salons salon ON salon.id = claimed.salon_id
    ORDER BY claimed.event_id
$$;

CREATE OR REPLACE FUNCTION public.app_worker_claim_square_calendar_repairs(
    target_provider TEXT,
    target_limit INTEGER
)
RETURNS TABLE (salon_id UUID, owner_user_id UUID, lease_token TEXT)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH seeded AS (
        INSERT INTO public.square_calendar_repair_state (salon_id)
        SELECT connection.salon_id
        FROM public.pos_connections connection
        JOIN public.salons salon ON salon.id = connection.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND connection.provider = BTRIM(target_provider)
          AND salon.active_pos_provider = BTRIM(target_provider)
          AND connection.status = 'active'
          AND COALESCE(connection.merchant_id, '') <> ''
          AND COALESCE(connection.location_id, '') <> ''
        ON CONFLICT (salon_id) DO NOTHING
        RETURNING salon_id
    ), candidates AS (
        SELECT state.salon_id
        FROM public.square_calendar_repair_state state
        JOIN public.pos_connections connection
          ON connection.salon_id = state.salon_id
         AND connection.provider = BTRIM(target_provider)
        JOIN public.salons salon
          ON salon.id = connection.salon_id
         AND salon.active_pos_provider = BTRIM(target_provider)
        WHERE public.app_worker_discovery_allowed()
          AND state.next_repair_at <= now()
          AND (state.lease_expires_at IS NULL OR state.lease_expires_at < now())
          AND connection.status = 'active'
        ORDER BY state.next_repair_at, state.salon_id
        FOR UPDATE OF state SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 2), 1), 20)
    ), claimed AS (
        UPDATE public.square_calendar_repair_state state
        SET lease_expires_at = now() + interval '5 minutes',
            lease_token = gen_random_uuid()::TEXT,
            next_repair_at = now() + interval '5 minutes',
            repair_attempts = state.repair_attempts + 1,
            updated_at = now()
        FROM candidates
        WHERE state.salon_id = candidates.salon_id
        RETURNING state.salon_id, state.lease_token
    )
    SELECT claimed.salon_id, salon.owner_user_id, claimed.lease_token
    FROM claimed
    JOIN public.salons salon ON salon.id = claimed.salon_id
    ORDER BY claimed.salon_id
$$;

CREATE OR REPLACE FUNCTION public.app_worker_claim_owner_notifications(
    max_attempts_per_cycle INTEGER,
    target_limit INTEGER,
    lease_milliseconds BIGINT
)
RETURNS TABLE (
    notification_id UUID,
    salon_id UUID,
    notification_type TEXT,
    message TEXT,
    claim_token UUID,
    attempt_number INTEGER,
    requeue_count INTEGER,
    created_at TIMESTAMPTZ
)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH ranked AS (
        SELECT notification.id, notification.next_delivery_at, notification.created_at,
               row_number() OVER (
                   PARTITION BY notification.salon_id
                   ORDER BY notification.next_delivery_at, notification.created_at, notification.id
               ) AS tenant_rank,
               COALESCE(limits.worker_claims_per_batch, 2) AS tenant_limit
        FROM public.owner_notifications notification
        LEFT JOIN public.tenant_runtime_limits limits ON limits.salon_id = notification.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND notification.delivery_status IN ('queued', 'failed')
          AND notification.next_delivery_at <= now()
          AND notification.delivery_attempts::BIGINT <
              (notification.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
    ), candidates AS (
        SELECT notification.id
        FROM public.owner_notifications notification
        JOIN ranked ON ranked.id = notification.id
        WHERE ranked.tenant_rank <= ranked.tenant_limit
        ORDER BY ranked.next_delivery_at, ranked.created_at, notification.id
        FOR UPDATE OF notification SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 1), 1), 100)
    ), claimed AS (
        UPDATE public.owner_notifications notification
        SET delivery_status = 'delivering',
            delivery_provider = 'twilio',
            delivery_claim_token = gen_random_uuid(),
            delivery_claimed_at = now(),
            delivery_lease_expires_at = now() +
                (LEAST(GREATEST(COALESCE(lease_milliseconds, 1), 1), 3600000) * interval '1 millisecond'),
            delivery_dispatch_started_at = NULL,
            delivery_attempts = notification.delivery_attempts + 1,
            dead_lettered_at = NULL,
            last_delivery_error = NULL,
            last_delivery_error_code = NULL
        FROM candidates
        WHERE notification.id = candidates.id
        RETURNING notification.id, notification.salon_id, notification.type,
                  notification.message, notification.delivery_claim_token,
                  notification.delivery_attempts, notification.requeue_count,
                  notification.created_at
    ), attempts AS (
        INSERT INTO public.owner_notification_delivery_attempts (
            salon_id, owner_notification_id, attempt_number, claim_token, provider, outcome
        )
        SELECT claimed.salon_id, claimed.id, claimed.delivery_attempts,
               claimed.delivery_claim_token, 'twilio', 'leased'
        FROM claimed
        RETURNING owner_notification_id
    ), events AS (
        INSERT INTO public.owner_notification_delivery_events (
            salon_id, owner_notification_id, event_key, event_fingerprint,
            event_type, delivery_status
        )
        SELECT claimed.salon_id, claimed.id,
               'claim:' || claimed.delivery_claim_token::TEXT,
               encode(digest('claimed' || chr(31) || 'delivering' || chr(31) || chr(31), 'sha256'), 'hex'),
               'claimed', 'delivering'
        FROM claimed
        RETURNING owner_notification_id
    )
    SELECT claimed.id, claimed.salon_id, claimed.type, claimed.message,
           claimed.delivery_claim_token, claimed.delivery_attempts,
           claimed.requeue_count, claimed.created_at
    FROM claimed
$$;

CREATE OR REPLACE FUNCTION public.app_worker_expired_owner_notification_leases(target_limit INTEGER)
RETURNS TABLE (
    notification_id UUID,
    salon_id UUID,
    claim_token UUID,
    dispatch_started BOOLEAN
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT notification.id, notification.salon_id,
           notification.delivery_claim_token,
           notification.delivery_dispatch_started_at IS NOT NULL
    FROM public.owner_notifications notification
    WHERE public.app_worker_discovery_allowed()
      AND notification.delivery_status = 'delivering'
      AND notification.delivery_claim_token IS NOT NULL
      AND notification.delivery_lease_expires_at <= now()
    ORDER BY notification.delivery_lease_expires_at, notification.id
    LIMIT LEAST(GREATEST(COALESCE(target_limit, 1), 1), 100)
$$;

CREATE OR REPLACE FUNCTION public.app_worker_claim_customer_notifications(
    max_attempts_per_cycle INTEGER,
    target_limit INTEGER,
    lease_milliseconds BIGINT
)
RETURNS TABLE (
    delivery_id UUID,
    salon_id UUID,
    consent_id UUID,
    notification_type TEXT,
    message_body TEXT,
    destination_e164 TEXT,
    destination_masked TEXT,
    consent_version INTEGER,
    policy_version BIGINT,
    claim_token UUID,
    attempt_number INTEGER,
    requeue_count INTEGER
)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH ranked AS (
        SELECT delivery.id, delivery.next_delivery_at, delivery.created_at,
               row_number() OVER (
                   PARTITION BY delivery.salon_id
                   ORDER BY delivery.next_delivery_at, delivery.created_at, delivery.id
               ) AS tenant_rank,
               COALESCE(limits.worker_claims_per_batch, 2) AS tenant_limit
        FROM public.customer_notification_deliveries delivery
        LEFT JOIN public.tenant_runtime_limits limits ON limits.salon_id = delivery.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND delivery.delivery_status IN ('queued', 'quiet_hours', 'failed')
          AND delivery.next_delivery_at <= now()
          AND delivery.delivery_attempts::BIGINT <
              (delivery.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
    ), candidates AS (
        SELECT delivery.id
        FROM public.customer_notification_deliveries delivery
        JOIN ranked ON ranked.id = delivery.id
        WHERE ranked.tenant_rank <= ranked.tenant_limit
        ORDER BY ranked.next_delivery_at, ranked.created_at, delivery.id
        FOR UPDATE OF delivery SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 1), 1), 100)
    ), claimed AS (
        UPDATE public.customer_notification_deliveries delivery
        SET delivery_status = 'delivering',
            delivery_provider = 'twilio',
            delivery_claim_token = gen_random_uuid(),
            delivery_claimed_at = now(),
            delivery_lease_expires_at = now() +
                (LEAST(GREATEST(COALESCE(lease_milliseconds, 1), 1), 3600000) * interval '1 millisecond'),
            delivery_dispatch_started_at = NULL,
            delivery_attempts = delivery.delivery_attempts + 1,
            dead_lettered_at = NULL,
            suppressed_at = NULL,
            last_delivery_error_code = NULL,
            updated_at = now()
        FROM candidates
        WHERE delivery.id = candidates.id
        RETURNING delivery.id, delivery.salon_id, delivery.customer_sms_consent_id,
                  delivery.notification_type, delivery.message_body,
                  delivery.destination_e164, delivery.destination_masked,
                  delivery.consent_version, delivery.policy_version,
                  delivery.delivery_claim_token, delivery.delivery_attempts,
                  delivery.requeue_count
    ), attempts AS (
        INSERT INTO public.customer_notification_delivery_attempts (
            salon_id, customer_notification_delivery_id, attempt_number,
            claim_token, provider, outcome
        )
        SELECT claimed.salon_id, claimed.id, claimed.delivery_attempts,
               claimed.delivery_claim_token, 'twilio', 'leased'
        FROM claimed
        RETURNING customer_notification_delivery_id
    ), events AS (
        INSERT INTO public.customer_notification_delivery_events (
            salon_id, customer_notification_delivery_id, event_key,
            event_fingerprint, event_type, delivery_status
        )
        SELECT claimed.salon_id, claimed.id,
               'claim:' || claimed.delivery_claim_token::TEXT,
               encode(digest(
                   convert_to(claimed.id::TEXT, 'UTF8') || decode('00', 'hex') ||
                   convert_to('claim:' || claimed.delivery_claim_token::TEXT, 'UTF8') || decode('00', 'hex') ||
                   convert_to('claimed', 'UTF8') || decode('00', 'hex') ||
                   convert_to('delivering', 'UTF8') || decode('0000', 'hex'),
                   'sha256'
               ), 'hex'),
               'claimed', 'delivering'
        FROM claimed
        RETURNING customer_notification_delivery_id
    )
    SELECT claimed.id, claimed.salon_id, claimed.customer_sms_consent_id,
           claimed.notification_type, claimed.message_body,
           claimed.destination_e164, claimed.destination_masked,
           claimed.consent_version, claimed.policy_version,
           claimed.delivery_claim_token, claimed.delivery_attempts,
           claimed.requeue_count
    FROM claimed
$$;

CREATE OR REPLACE FUNCTION public.app_worker_expired_customer_notification_leases(target_limit INTEGER)
RETURNS TABLE (
    delivery_id UUID,
    salon_id UUID,
    claim_token UUID,
    dispatch_started BOOLEAN,
    attempt_number INTEGER,
    requeue_count INTEGER
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT delivery.id, delivery.salon_id, delivery.delivery_claim_token,
           delivery.delivery_dispatch_started_at IS NOT NULL,
           delivery.delivery_attempts, delivery.requeue_count
    FROM public.customer_notification_deliveries delivery
    WHERE public.app_worker_discovery_allowed()
      AND delivery.delivery_status = 'delivering'
      AND delivery.delivery_claim_token IS NOT NULL
      AND delivery.delivery_lease_expires_at <= now()
    ORDER BY delivery.delivery_lease_expires_at, delivery.id
    LIMIT LEAST(GREATEST(COALESCE(target_limit, 1), 1), 100)
$$;

CREATE OR REPLACE FUNCTION public.app_worker_expired_call_sessions(target_limit INTEGER)
RETURNS TABLE (session_id UUID, salon_id UUID)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT session.id, session.salon_id
    FROM public.call_sessions session
    WHERE public.app_worker_discovery_allowed()
      AND session.lifecycle_status <> 'redacted'
      AND session.retention_expires_at <= now()
    ORDER BY session.retention_expires_at, session.id
    LIMIT LEAST(GREATEST(COALESCE(target_limit, 50), 1), 200)
$$;

CREATE OR REPLACE FUNCTION public.app_worker_scheduling_retention_candidate(target_kind TEXT)
RETURNS TABLE (salon_id UUID, item_id UUID, terminal_at TIMESTAMPTZ)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT candidate.salon_id, candidate.item_id, candidate.terminal_at
    FROM (
        SELECT notification.salon_id, notification.id AS item_id,
               GREATEST(
                   CASE
                       WHEN request.id IS NOT NULL THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                       WHEN attempt.id IS NOT NULL THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
                   END,
                   COALESCE(notification.delivered_at, notification.dead_lettered_at,
                            notification.last_provider_event_at, notification.created_at)
               ) AS terminal_at
        FROM public.owner_notifications notification
        LEFT JOIN public.scheduling_requests request
          ON request.salon_id = notification.salon_id
         AND request.id = notification.scheduling_request_id
        LEFT JOIN public.booking_attempts attempt
          ON attempt.salon_id = notification.salon_id
         AND attempt.id = notification.booking_attempt_id
        LEFT JOIN public.booking_reconciliation_tasks task
          ON task.salon_id = notification.salon_id
         AND task.booking_attempt_id = attempt.id
        WHERE target_kind = 'owner_notification_retention_expiry'
          AND notification.retention_expires_at IS NULL
          AND notification.redacted_at IS NULL
          AND notification.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'disabled')
          AND notification.delivery_claim_token IS NULL
          AND notification.delivery_lease_expires_at IS NULL
          AND (
              (request.id IS NOT NULL AND request.status IN ('resolved', 'dismissed'))
              OR
              (attempt.id IS NOT NULL
               AND attempt.status IN ('confirmed', 'rescheduled', 'cancelled', 'declined', 'no_show', 'failed')
               AND attempt.processing_token IS NULL
               AND attempt.processing_lease_expires_at IS NULL
               AND attempt.reconciliation_status IN ('not_required', 'resolved')
               AND (task.id IS NULL OR task.status = 'resolved'))
          )

        UNION ALL

        SELECT delivery.salon_id, delivery.id,
               GREATEST(
                   CASE
                       WHEN request.id IS NOT NULL THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                       WHEN attempt.id IS NOT NULL THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
                   END,
                   COALESCE(delivery.delivered_at, delivery.dead_lettered_at,
                            delivery.suppressed_at, delivery.last_provider_event_at, delivery.updated_at)
               ) AS terminal_at
        FROM public.customer_notification_deliveries delivery
        LEFT JOIN public.scheduling_requests request
          ON request.salon_id = delivery.salon_id
         AND request.id = delivery.scheduling_request_id
        LEFT JOIN public.booking_attempts attempt
          ON attempt.salon_id = delivery.salon_id
         AND attempt.id = delivery.booking_attempt_id
        LEFT JOIN public.booking_reconciliation_tasks task
          ON task.salon_id = delivery.salon_id
         AND task.booking_attempt_id = attempt.id
        WHERE target_kind = 'customer_notification_retention_expiry'
          AND delivery.retention_expires_at IS NULL
          AND delivery.redacted_at IS NULL
          AND delivery.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'suppressed')
          AND delivery.delivery_claim_token IS NULL
          AND delivery.delivery_lease_expires_at IS NULL
          AND (
              (request.id IS NOT NULL AND request.status IN ('resolved', 'dismissed'))
              OR
              (attempt.id IS NOT NULL
               AND attempt.status IN ('confirmed', 'rescheduled', 'cancelled', 'declined', 'no_show', 'failed')
               AND attempt.processing_token IS NULL
               AND attempt.processing_lease_expires_at IS NULL
               AND attempt.reconciliation_status IN ('not_required', 'resolved')
               AND (task.id IS NULL OR task.status = 'resolved'))
          )

        UNION ALL

        SELECT request.salon_id, request.id, request.retention_expires_at
        FROM public.scheduling_requests request
        WHERE target_kind = 'scheduling_request'
          AND request.status IN ('resolved', 'dismissed')
          AND request.redacted_at IS NULL
          AND request.retention_expires_at <= now()
          AND NOT EXISTS (
              SELECT 1 FROM public.owner_notifications notification
              WHERE notification.salon_id = request.salon_id
                AND notification.scheduling_request_id = request.id
                AND (notification.delivery_status NOT IN ('delivered', 'undelivered', 'dead_letter', 'disabled')
                     OR notification.delivery_claim_token IS NOT NULL
                     OR notification.delivery_lease_expires_at IS NOT NULL)
          )
          AND NOT EXISTS (
              SELECT 1 FROM public.customer_notification_deliveries delivery
              WHERE delivery.salon_id = request.salon_id
                AND delivery.scheduling_request_id = request.id
                AND (delivery.delivery_status NOT IN ('delivered', 'undelivered', 'dead_letter', 'suppressed')
                     OR delivery.delivery_claim_token IS NOT NULL
                     OR delivery.delivery_lease_expires_at IS NOT NULL)
          )

        UNION ALL

        SELECT notification.salon_id, notification.id, notification.retention_expires_at
        FROM public.owner_notifications notification
        LEFT JOIN public.scheduling_requests request
          ON request.salon_id = notification.salon_id
         AND request.id = notification.scheduling_request_id
        LEFT JOIN public.booking_attempts attempt
          ON attempt.salon_id = notification.salon_id
         AND attempt.id = notification.booking_attempt_id
        LEFT JOIN public.booking_reconciliation_tasks task
          ON task.salon_id = notification.salon_id
         AND task.booking_attempt_id = attempt.id
        WHERE target_kind = 'owner_notification'
          AND notification.redacted_at IS NULL
          AND notification.retention_expires_at <= now()
          AND notification.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'disabled')
          AND notification.delivery_claim_token IS NULL
          AND notification.delivery_lease_expires_at IS NULL
          AND (
              (request.id IS NOT NULL AND request.status IN ('resolved', 'dismissed'))
              OR
              (attempt.id IS NOT NULL
               AND attempt.status IN ('confirmed', 'rescheduled', 'cancelled', 'declined', 'no_show', 'failed')
               AND attempt.processing_token IS NULL
               AND attempt.processing_lease_expires_at IS NULL
               AND attempt.reconciliation_status IN ('not_required', 'resolved')
               AND (task.id IS NULL OR task.status = 'resolved'))
          )

        UNION ALL

        SELECT delivery.salon_id, delivery.id, delivery.retention_expires_at
        FROM public.customer_notification_deliveries delivery
        LEFT JOIN public.scheduling_requests request
          ON request.salon_id = delivery.salon_id
         AND request.id = delivery.scheduling_request_id
        LEFT JOIN public.booking_attempts attempt
          ON attempt.salon_id = delivery.salon_id
         AND attempt.id = delivery.booking_attempt_id
        LEFT JOIN public.booking_reconciliation_tasks task
          ON task.salon_id = delivery.salon_id
         AND task.booking_attempt_id = attempt.id
        WHERE target_kind = 'customer_notification'
          AND delivery.redacted_at IS NULL
          AND delivery.retention_expires_at <= now()
          AND delivery.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'suppressed')
          AND delivery.delivery_claim_token IS NULL
          AND delivery.delivery_lease_expires_at IS NULL
          AND (
              (request.id IS NOT NULL AND request.status IN ('resolved', 'dismissed'))
              OR
              (attempt.id IS NOT NULL
               AND attempt.status IN ('confirmed', 'rescheduled', 'cancelled', 'declined', 'no_show', 'failed')
               AND attempt.processing_token IS NULL
               AND attempt.processing_lease_expires_at IS NULL
               AND attempt.reconciliation_status IN ('not_required', 'resolved')
               AND (task.id IS NULL OR task.status = 'resolved'))
          )

        UNION ALL

        SELECT audio.salon_id, audio.id, audio.expires_at
        FROM public.voice_audio_outputs audio
        WHERE target_kind = 'voice_audio'
          AND audio.salon_id IS NOT NULL
          AND audio.redacted_at IS NULL
          AND audio.expires_at <= now()
    ) candidate
    WHERE public.app_worker_discovery_allowed()
    ORDER BY candidate.terminal_at, candidate.item_id
    LIMIT 1
$$;

-- Legacy unowned audio cannot be tenant-bound. Keep its irreversible expiry
-- cleanup inside one narrow worker-only function so future strict RLS does not
-- require broad base-table access for this exceptional shape.
CREATE OR REPLACE FUNCTION public.app_worker_redact_unowned_voice_audio(redaction_policy_version INTEGER)
RETURNS BOOLEAN
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH candidate AS (
        SELECT audio.id
        FROM public.voice_audio_outputs audio
        WHERE public.app_worker_discovery_allowed()
          AND audio.salon_id IS NULL
          AND audio.call_session_id IS NULL
          AND audio.redacted_at IS NULL
          AND audio.expires_at <= now()
        ORDER BY audio.expires_at, audio.id
        FOR UPDATE OF audio SKIP LOCKED
        LIMIT 1
    ), changed AS (
        UPDATE public.voice_audio_outputs audio
        SET audio_data = ''::BYTEA,
            redacted_at = now(),
            redaction_version = redaction_policy_version
        FROM candidate
        WHERE audio.id = candidate.id
          AND audio.redacted_at IS NULL
        RETURNING audio.id
    )
    SELECT EXISTS(SELECT 1 FROM changed)
$$;

REVOKE ALL ON FUNCTION public.app_worker_discovery_allowed() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_claim_pos_sync_jobs(INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_expired_booking_leases(INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_cleanup_availability_quotes(TIMESTAMPTZ, TIMESTAMPTZ, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_claim_square_booking_webhooks(INTEGER, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_claim_square_calendar_repairs(TEXT, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_claim_owner_notifications(INTEGER, INTEGER, BIGINT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_expired_owner_notification_leases(INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_claim_customer_notifications(INTEGER, INTEGER, BIGINT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_expired_customer_notification_leases(INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_expired_call_sessions(INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_scheduling_retention_candidate(TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_worker_redact_unowned_voice_audio(INTEGER) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION public.app_worker_discovery_allowed() TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_pos_sync_jobs(INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_expired_booking_leases(INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_cleanup_availability_quotes(TIMESTAMPTZ, TIMESTAMPTZ, INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_square_booking_webhooks(INTEGER, INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_square_calendar_repairs(TEXT, INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_owner_notifications(INTEGER, INTEGER, BIGINT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_expired_owner_notification_leases(INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_customer_notifications(INTEGER, INTEGER, BIGINT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_expired_customer_notification_leases(INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_expired_call_sessions(INTEGER) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_scheduling_retention_candidate(TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_redact_unowned_voice_audio(INTEGER) TO PUBLIC;

COMMENT ON FUNCTION public.app_worker_discovery_allowed() IS
'True only for an unbound server-selected worker scope.';
COMMENT ON FUNCTION public.app_worker_claim_pos_sync_jobs(INTEGER) IS
'Worker-only bounded fair POS sync claim; subsequent item work must bind the returned salon.';
COMMENT ON FUNCTION public.app_worker_expired_booking_leases(INTEGER) IS
'Worker-only bounded booking lease locator returning only recovery identity and tenant fields.';
COMMENT ON FUNCTION public.app_worker_cleanup_availability_quotes(TIMESTAMPTZ, TIMESTAMPTZ, INTEGER) IS
'Worker-only bounded atomic cleanup of unreferenced expired availability quotes.';
COMMENT ON FUNCTION public.app_worker_claim_square_booking_webhooks(INTEGER, INTEGER) IS
'Worker-only bounded fair Square webhook claim with claim-token fencing.';
COMMENT ON FUNCTION public.app_worker_claim_square_calendar_repairs(TEXT, INTEGER) IS
'Worker-only bounded Square calendar-repair claim with per-salon leases.';
COMMENT ON FUNCTION public.app_worker_claim_owner_notifications(INTEGER, INTEGER, BIGINT) IS
'Worker-only bounded owner-notification claim that atomically writes attempt and event evidence.';
COMMENT ON FUNCTION public.app_worker_claim_customer_notifications(INTEGER, INTEGER, BIGINT) IS
'Worker-only bounded customer-notification claim that atomically writes attempt and event evidence.';
COMMENT ON FUNCTION public.app_worker_scheduling_retention_candidate(TEXT) IS
'Worker-only locator returning one minimal tenant-bound scheduling-retention work identity.';
