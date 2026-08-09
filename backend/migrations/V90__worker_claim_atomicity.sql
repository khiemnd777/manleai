-- Recheck queue eligibility on the exact row version that each worker locks
-- and updates. V79/V84 ranked eligible rows from a statement snapshot, then
-- joined the ranked IDs back to the live queue. A competing worker could
-- commit after that snapshot but before the second worker acquired its row
-- lock; an ID-only UPDATE predicate could then claim the newer ineligible row.
--
-- Keep tenant fairness and SKIP LOCKED discovery unchanged. The duplicated
-- eligibility predicates are intentional: candidates reject a row whose live
-- version already changed, and the final UPDATE remains the atomic authority.

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
        WHERE public.app_worker_discovery_allowed()
          AND ranked.tenant_rank <= ranked.tenant_limit
          AND job.status IN ('queued', 'failed')
          AND job.attempt_count < job.max_attempts
          AND job.next_attempt_at <= now()
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
      AND public.app_worker_discovery_allowed()
      AND job.status IN ('queued', 'failed')
      AND job.attempt_count < job.max_attempts
      AND job.next_attempt_at <= now()
    RETURNING job.id, job.salon_id, job.provider, job.entity_type, job.entity_id,
              job.operation, job.status, job.attempt_count, job.max_attempts,
              job.next_attempt_at, job.locked_at, job.completed_at,
              COALESCE(job.last_error, ''), job.created_at, job.updated_at
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
        WHERE public.app_worker_discovery_allowed()
          AND ranked.tenant_rank <= ranked.tenant_limit
          AND event.processing_attempts < (event.requeue_count + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
          AND (
              (event.processing_status IN ('pending', 'failed') AND event.next_attempt_at <= now())
              OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
          )
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
          AND public.app_worker_discovery_allowed()
          AND event.processing_attempts < (event.requeue_count + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
          AND (
              (event.processing_status IN ('pending', 'failed') AND event.next_attempt_at <= now())
              OR (event.processing_status = 'processing' AND event.processing_lease_expires_at < now())
          )
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
        WHERE public.app_worker_discovery_allowed()
          AND ranked.tenant_rank <= ranked.tenant_limit
          AND notification.delivery_status IN ('queued', 'failed')
          AND notification.next_delivery_at <= now()
          AND notification.delivery_attempts::BIGINT <
              (notification.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
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
          AND public.app_worker_discovery_allowed()
          AND notification.delivery_status IN ('queued', 'failed')
          AND notification.next_delivery_at <= now()
          AND notification.delivery_attempts::BIGINT <
              (notification.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
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
        WHERE public.app_worker_discovery_allowed()
          AND ranked.tenant_rank <= ranked.tenant_limit
          AND delivery.delivery_status IN ('queued', 'quiet_hours', 'failed')
          AND delivery.next_delivery_at <= now()
          AND delivery.delivery_attempts::BIGINT <
              (delivery.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
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
          AND public.app_worker_discovery_allowed()
          AND delivery.delivery_status IN ('queued', 'quiet_hours', 'failed')
          AND delivery.next_delivery_at <= now()
          AND delivery.delivery_attempts::BIGINT <
              (delivery.requeue_count::BIGINT + 1) * LEAST(GREATEST(COALESCE(max_attempts_per_cycle, 1), 1), 100)
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

CREATE OR REPLACE FUNCTION public.app_worker_claim_openai_runtime_verifications(
    target_limit INTEGER,
    lease_milliseconds BIGINT
)
RETURNS TABLE (run_id UUID, salon_id UUID, claim_token UUID)
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    WITH ranked AS (
        SELECT run.id, run.salon_id, run.created_at,
               row_number() OVER (PARTITION BY run.salon_id ORDER BY run.created_at, run.id) AS tenant_rank,
               COALESCE(limits.worker_claims_per_batch, 2) AS tenant_limit
        FROM public.openai_runtime_verification_runs run
        LEFT JOIN public.tenant_runtime_limits limits ON limits.salon_id = run.salon_id
        WHERE public.app_worker_discovery_allowed()
          AND run.attempt_count < 3
          AND (run.status = 'queued' OR (run.status = 'claimed' AND run.lease_expires_at <= now()))
    ), candidates AS (
        SELECT run.id
        FROM public.openai_runtime_verification_runs run
        JOIN ranked ON ranked.id = run.id
        WHERE public.app_worker_discovery_allowed()
          AND ranked.tenant_rank <= ranked.tenant_limit
          AND run.attempt_count < 3
          AND (run.status = 'queued' OR (run.status = 'claimed' AND run.lease_expires_at <= now()))
        ORDER BY ranked.created_at, run.id
        FOR UPDATE OF run SKIP LOCKED
        LIMIT LEAST(GREATEST(COALESCE(target_limit, 1), 1), 25)
    )
    UPDATE public.openai_runtime_verification_runs run
    SET status = 'claimed',
        attempt_count = run.attempt_count + 1,
        claim_token = gen_random_uuid(),
        lease_expires_at = now() +
            (LEAST(GREATEST(COALESCE(lease_milliseconds, 1), 1), 900000) * interval '1 millisecond'),
        started_at = COALESCE(run.started_at, now()),
        completed_at = NULL,
        error_code = NULL,
        updated_at = now()
    FROM candidates
    WHERE run.id = candidates.id
      AND public.app_worker_discovery_allowed()
      AND run.attempt_count < 3
      AND (run.status = 'queued' OR (run.status = 'claimed' AND run.lease_expires_at <= now()))
    RETURNING run.id, run.salon_id, run.claim_token
$$;

COMMENT ON FUNCTION public.app_worker_claim_pos_sync_jobs(INTEGER) IS
    'V90 worker-only fair POS sync claim with lock-time and update-time eligibility rechecks.';
COMMENT ON FUNCTION public.app_worker_claim_square_booking_webhooks(INTEGER, INTEGER) IS
    'V90 worker-only fair Square webhook claim with lock-time and update-time eligibility rechecks.';
COMMENT ON FUNCTION public.app_worker_claim_owner_notifications(INTEGER, INTEGER, BIGINT) IS
    'V90 worker-only fair owner notification claim with lock-time and update-time eligibility rechecks.';
COMMENT ON FUNCTION public.app_worker_claim_customer_notifications(INTEGER, INTEGER, BIGINT) IS
    'V90 worker-only fair customer notification claim with lock-time and update-time eligibility rechecks.';
COMMENT ON FUNCTION public.app_worker_claim_openai_runtime_verifications(INTEGER, BIGINT) IS
    'V90 worker-only fair OpenAI verification claim with lock-time and update-time eligibility rechecks.';
