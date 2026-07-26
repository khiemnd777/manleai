-- Phase 6C: bounded, worker-driven PII redaction for scheduling and notification
-- records. This migration adds eligibility evidence and one-way guards only;
-- it never redacts a row while the migration is running.

CREATE FUNCTION retention_safe_audit_payload(input_payload JSONB)
RETURNS JSONB AS $$
    SELECT COALESCE(jsonb_object_agg(item.key, item.value), '{}'::jsonb)
           || jsonb_build_object('redacted', true, 'redaction_version', 1)
    FROM jsonb_each(COALESCE(input_payload, '{}'::jsonb)) item
    WHERE item.key = ANY (ARRAY[
        'action', 'appointment_id', 'authority_version', 'booking_attempt_id',
        'error_code', 'event_status', 'failure_phase', 'from_status',
        'operation_type', 'pos_booking_id', 'pos_booking_version', 'pos_provider',
        'provider', 'provider_booking_id', 'provider_booking_version',
        'provider_outcome', 'reconciliation_status', 'request_version',
        'result_status', 'scheduling_authority',
        'scheduling_request_id', 'source', 'status', 'target_scheduling_authority',
        'task_status', 'to_status'
    ])
$$ LANGUAGE SQL IMMUTABLE;

ALTER TABLE scheduling_requests
    ADD COLUMN retention_expires_at TIMESTAMPTZ,
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN redaction_version INTEGER;

UPDATE scheduling_requests
SET retention_expires_at = COALESCE(resolved_at, dismissed_at, updated_at) + INTERVAL '90 days'
WHERE status IN ('resolved', 'dismissed')
  AND retention_expires_at IS NULL;

ALTER TABLE scheduling_requests
    DROP CONSTRAINT scheduling_requests_customer_name_nonempty_check,
    DROP CONSTRAINT scheduling_requests_customer_phone_nonempty_check,
    ADD CONSTRAINT scheduling_requests_customer_identity_retention_check
        CHECK (
            (redacted_at IS NULL
                AND length(trim(customer_name)) > 0
                AND length(trim(customer_phone)) > 0)
            OR
            (redacted_at IS NOT NULL
                AND customer_name = '[redacted]'
                AND customer_phone = '[redacted]'
                AND customer_email IS NULL
                AND notes IS NULL
                AND resolution_reason IS NULL)
        ),
    ADD CONSTRAINT scheduling_requests_retention_shape_check
        CHECK (
            (status IN ('resolved', 'dismissed')) = (retention_expires_at IS NOT NULL)
            AND (
                retention_expires_at IS NULL
                OR retention_expires_at >= COALESCE(resolved_at, dismissed_at, updated_at)
            )
        ),
    ADD CONSTRAINT scheduling_requests_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR
            (redacted_at IS NOT NULL
                AND redaction_version = 1
                AND status IN ('resolved', 'dismissed')
                AND retention_expires_at IS NOT NULL
                AND redacted_at >= retention_expires_at)
        );

CREATE FUNCTION set_scheduling_request_retention_expiry()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status IN ('resolved', 'dismissed') THEN
        NEW.retention_expires_at :=
            COALESCE(NEW.resolved_at, NEW.dismissed_at, NEW.updated_at) + INTERVAL '90 days';
    ELSE
        NEW.retention_expires_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_requests_retention_expiry_guard
BEFORE INSERT OR UPDATE OF status, resolved_at, dismissed_at, updated_at, retention_expires_at
ON scheduling_requests
FOR EACH ROW EXECUTE FUNCTION set_scheduling_request_retention_expiry();

CREATE OR REPLACE FUNCTION enforce_scheduling_request_immutable_core()
RETURNS TRIGGER AS $$
DECLARE
    redacting BOOLEAN := OLD.redacted_at IS NULL AND NEW.redacted_at IS NOT NULL;
BEGIN
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.salon_id IS DISTINCT FROM NEW.salon_id
       OR OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority
       OR OLD.operation_key IS DISTINCT FROM NEW.operation_key
       OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
       OR OLD.operation_type IS DISTINCT FROM NEW.operation_type
       OR OLD.source IS DISTINCT FROM NEW.source
       OR OLD.call_session_id IS DISTINCT FROM NEW.call_session_id
       OR OLD.target_appointment_id IS DISTINCT FROM NEW.target_appointment_id
       OR OLD.target_scheduling_authority IS DISTINCT FROM NEW.target_scheduling_authority
       OR OLD.requested_timezone IS DISTINCT FROM NEW.requested_timezone
       OR OLD.party_size IS DISTINCT FROM NEW.party_size
       OR OLD.requested_start_time IS DISTINCT FROM NEW.requested_start_time
       OR OLD.requested_end_time IS DISTINCT FROM NEW.requested_end_time
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling request core fields are immutable',
            CONSTRAINT = 'scheduling_requests_immutable_core_guard';
    END IF;

    IF OLD.target_description IS DISTINCT FROM NEW.target_description
       OR OLD.customer_name IS DISTINCT FROM NEW.customer_name
       OR OLD.customer_phone IS DISTINCT FROM NEW.customer_phone
       OR OLD.customer_email IS DISTINCT FROM NEW.customer_email
       OR OLD.notes IS DISTINCT FROM NEW.notes THEN
        IF NOT redacting
           OR NEW.customer_name <> '[redacted]'
           OR NEW.customer_phone <> '[redacted]'
           OR NEW.customer_email IS NOT NULL
           OR NEW.notes IS NOT NULL
	           OR NEW.target_description IS DISTINCT FROM (CASE
	                WHEN NEW.operation_type IN ('reschedule', 'cancel')
	                     AND NEW.target_appointment_id IS NULL THEN '[redacted]'
	                ELSE NULL
	              END) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'scheduling request PII may change only through exact retention redaction',
                CONSTRAINT = 'scheduling_requests_immutable_core_guard';
        END IF;
    END IF;

    IF OLD.redacted_at IS NOT NULL
       AND (OLD.redacted_at IS DISTINCT FROM NEW.redacted_at
            OR OLD.redaction_version IS DISTINCT FROM NEW.redaction_version) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling request redaction is irreversible',
            CONSTRAINT = 'scheduling_requests_immutable_core_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE INDEX idx_scheduling_requests_retention_due
    ON scheduling_requests(retention_expires_at, id)
    WHERE redacted_at IS NULL AND status IN ('resolved', 'dismissed');

ALTER TABLE scheduling_request_segments
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN redaction_version INTEGER,
    ADD CONSTRAINT scheduling_request_segments_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR (redacted_at IS NOT NULL AND redaction_version = 1 AND guest_reference IS NULL)
        );

CREATE OR REPLACE FUNCTION reject_scheduling_request_segment_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE'
       AND OLD.redacted_at IS NULL
       AND NEW.redacted_at IS NOT NULL
       AND NEW.redaction_version = 1
       AND NEW.guest_reference IS NULL
       AND OLD.id IS NOT DISTINCT FROM NEW.id
       AND OLD.salon_id IS NOT DISTINCT FROM NEW.salon_id
       AND OLD.scheduling_request_id IS NOT DISTINCT FROM NEW.scheduling_request_id
       AND OLD.service_id IS NOT DISTINCT FROM NEW.service_id
       AND OLD.service_name IS NOT DISTINCT FROM NEW.service_name
       AND OLD.quantity IS NOT DISTINCT FROM NEW.quantity
       AND OLD.staff_id IS NOT DISTINCT FROM NEW.staff_id
       AND OLD.staff_name IS NOT DISTINCT FROM NEW.staff_name
       AND OLD.staff_selection_mode IS NOT DISTINCT FROM NEW.staff_selection_mode
       AND OLD.duration_minutes IS NOT DISTINCT FROM NEW.duration_minutes
       AND OLD.requested_start_time IS NOT DISTINCT FROM NEW.requested_start_time
       AND OLD.requested_end_time IS NOT DISTINCT FROM NEW.requested_end_time
       AND OLD.sort_order IS NOT DISTINCT FROM NEW.sort_order
       AND OLD.created_at IS NOT DISTINCT FROM NEW.created_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling request segments are immutable outside exact retention redaction',
        CONSTRAINT = 'scheduling_request_segments_immutable_guard';
END;
$$ LANGUAGE plpgsql;

ALTER TABLE scheduling_request_events
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN redaction_version INTEGER,
    ADD CONSTRAINT scheduling_request_events_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR
            (redacted_at IS NOT NULL
                AND redaction_version = 1
                AND payload = retention_safe_audit_payload(payload))
        );

CREATE OR REPLACE FUNCTION reject_scheduling_request_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE'
       AND OLD.redacted_at IS NULL
       AND NEW.redacted_at IS NOT NULL
       AND NEW.redaction_version = 1
       AND NEW.payload = retention_safe_audit_payload(OLD.payload)
       AND OLD.id IS NOT DISTINCT FROM NEW.id
       AND OLD.salon_id IS NOT DISTINCT FROM NEW.salon_id
       AND OLD.scheduling_request_id IS NOT DISTINCT FROM NEW.scheduling_request_id
       AND OLD.action_key IS NOT DISTINCT FROM NEW.action_key
       AND OLD.action_fingerprint IS NOT DISTINCT FROM NEW.action_fingerprint
       AND OLD.event_type IS NOT DISTINCT FROM NEW.event_type
       AND OLD.request_version IS NOT DISTINCT FROM NEW.request_version
       AND OLD.actor_user_id IS NOT DISTINCT FROM NEW.actor_user_id
       AND OLD.created_at IS NOT DISTINCT FROM NEW.created_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling request events are immutable outside exact retention redaction',
        CONSTRAINT = 'scheduling_request_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

ALTER TABLE owner_notifications
    ADD COLUMN retention_expires_at TIMESTAMPTZ,
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN redaction_version INTEGER,
    ADD CONSTRAINT owner_notifications_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR
            (redacted_at IS NOT NULL
                AND redaction_version = 1
                AND retention_expires_at IS NOT NULL
                AND redacted_at >= retention_expires_at
                AND message = '[redacted]'
                AND payload = retention_safe_audit_payload(payload)
                AND delivery_destination_masked IS NULL
                AND last_delivery_error IS NULL)
        );

WITH eligible AS (
    SELECT notification.id,
           GREATEST(
               CASE
                   WHEN request.id IS NOT NULL
                       THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                   WHEN attempt.id IS NOT NULL
                       THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
               END,
               COALESCE(notification.delivered_at, notification.dead_lettered_at,
                        notification.last_provider_event_at, notification.created_at)
           ) AS terminal_at
    FROM owner_notifications notification
    LEFT JOIN scheduling_requests request
      ON request.salon_id = notification.salon_id
     AND request.id = notification.scheduling_request_id
    LEFT JOIN booking_attempts attempt
      ON attempt.salon_id = notification.salon_id
     AND attempt.id = notification.booking_attempt_id
    LEFT JOIN booking_reconciliation_tasks task
      ON task.salon_id = notification.salon_id
     AND task.booking_attempt_id = attempt.id
    WHERE notification.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'disabled')
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
)
UPDATE owner_notifications notification
SET retention_expires_at = eligible.terminal_at + INTERVAL '90 days'
FROM eligible
WHERE notification.id = eligible.id
  AND notification.retention_expires_at IS NULL;

CREATE FUNCTION clear_owner_notification_retention_when_live()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.delivery_status NOT IN ('delivered', 'undelivered', 'dead_letter', 'disabled')
       OR NEW.delivery_claim_token IS NOT NULL
       OR NEW.delivery_lease_expires_at IS NOT NULL THEN
        NEW.retention_expires_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER owner_notifications_live_retention_guard
BEFORE UPDATE OF delivery_status, delivery_claim_token, delivery_lease_expires_at, retention_expires_at
ON owner_notifications
FOR EACH ROW EXECUTE FUNCTION clear_owner_notification_retention_when_live();

CREATE FUNCTION owner_notification_redaction_irreversible_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.redacted_at IS NOT NULL
       AND (OLD.redacted_at IS DISTINCT FROM NEW.redacted_at
            OR OLD.redaction_version IS DISTINCT FROM NEW.redaction_version
            OR OLD.retention_expires_at IS DISTINCT FROM NEW.retention_expires_at
            OR OLD.message IS DISTINCT FROM NEW.message
            OR OLD.payload IS DISTINCT FROM NEW.payload
            OR OLD.delivery_destination_masked IS DISTINCT FROM NEW.delivery_destination_masked
            OR OLD.last_delivery_error IS DISTINCT FROM NEW.last_delivery_error) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'owner notification redaction is irreversible',
            CONSTRAINT = 'owner_notifications_redaction_irreversible_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER owner_notifications_redaction_irreversible_guard
BEFORE UPDATE ON owner_notifications
FOR EACH ROW EXECUTE FUNCTION owner_notification_redaction_irreversible_guard();

CREATE INDEX idx_owner_notifications_retention_due
    ON owner_notifications(retention_expires_at, id)
    WHERE redacted_at IS NULL AND retention_expires_at IS NOT NULL;

ALTER TABLE customer_notification_deliveries
	ADD COLUMN retention_expires_at TIMESTAMPTZ,
	ADD COLUMN redaction_version INTEGER,
	ALTER COLUMN destination_e164 DROP NOT NULL,
	ALTER COLUMN destination_masked DROP NOT NULL,
	ALTER COLUMN destination_hash DROP NOT NULL,
    DROP CONSTRAINT customer_notification_deliveries_message_check,
    DROP CONSTRAINT customer_notification_deliveries_destination_check,
    DROP CONSTRAINT customer_notification_deliveries_redaction_check,
    ADD CONSTRAINT customer_notification_deliveries_content_retention_check
        CHECK (
			(redacted_at IS NULL
				AND length(btrim(message_body)) BETWEEN 1 AND 1000
				AND destination_e164 ~ '^\+[1-9][0-9]{7,14}$'
				AND destination_masked ~ '^.{4}[0-9]{4}$'
				AND destination_hash ~ '^[0-9a-f]{64}$')
			OR
			(redacted_at IS NOT NULL
				AND message_body = '[redacted]'
				AND destination_e164 IS NULL
				AND destination_masked IS NULL
				AND destination_hash IS NULL)
		),
	ADD CONSTRAINT customer_notification_deliveries_destination_hash_check
		CHECK (destination_hash IS NULL OR destination_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT customer_notification_deliveries_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR
            (redacted_at IS NOT NULL
                AND redaction_version = 1
                AND retention_expires_at IS NOT NULL
                AND redacted_at >= retention_expires_at)
        );

WITH eligible AS (
    SELECT delivery.id,
           GREATEST(
               CASE
                   WHEN request.id IS NOT NULL
                       THEN COALESCE(request.resolved_at, request.dismissed_at, request.updated_at)
                   WHEN attempt.id IS NOT NULL
                       THEN GREATEST(attempt.updated_at, COALESCE(task.resolved_at, attempt.updated_at))
               END,
               COALESCE(delivery.delivered_at, delivery.dead_lettered_at,
                        delivery.suppressed_at, delivery.last_provider_event_at, delivery.updated_at)
           ) AS terminal_at
    FROM customer_notification_deliveries delivery
    LEFT JOIN scheduling_requests request
      ON request.salon_id = delivery.salon_id
     AND request.id = delivery.scheduling_request_id
    LEFT JOIN booking_attempts attempt
      ON attempt.salon_id = delivery.salon_id
     AND attempt.id = delivery.booking_attempt_id
    LEFT JOIN booking_reconciliation_tasks task
      ON task.salon_id = delivery.salon_id
     AND task.booking_attempt_id = attempt.id
    WHERE delivery.delivery_status IN ('delivered', 'undelivered', 'dead_letter', 'suppressed')
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
)
UPDATE customer_notification_deliveries delivery
SET retention_expires_at = eligible.terminal_at + INTERVAL '90 days'
FROM eligible
WHERE delivery.id = eligible.id
  AND delivery.retention_expires_at IS NULL;

CREATE FUNCTION clear_customer_notification_retention_when_live()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.delivery_status NOT IN ('delivered', 'undelivered', 'dead_letter', 'suppressed')
       OR NEW.delivery_claim_token IS NOT NULL
       OR NEW.delivery_lease_expires_at IS NOT NULL THEN
        NEW.retention_expires_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER customer_notification_deliveries_live_retention_guard
BEFORE UPDATE OF delivery_status, delivery_claim_token, delivery_lease_expires_at, retention_expires_at
ON customer_notification_deliveries
FOR EACH ROW EXECUTE FUNCTION clear_customer_notification_retention_when_live();

CREATE FUNCTION customer_notification_redaction_irreversible_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.redacted_at IS NOT NULL
       AND (OLD.redacted_at IS DISTINCT FROM NEW.redacted_at
            OR OLD.redaction_version IS DISTINCT FROM NEW.redaction_version
            OR OLD.retention_expires_at IS DISTINCT FROM NEW.retention_expires_at
            OR OLD.message_body IS DISTINCT FROM NEW.message_body
            OR OLD.destination_e164 IS DISTINCT FROM NEW.destination_e164
            OR OLD.destination_masked IS DISTINCT FROM NEW.destination_masked
            OR OLD.destination_hash IS DISTINCT FROM NEW.destination_hash) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'customer notification redaction is irreversible',
            CONSTRAINT = 'customer_notification_deliveries_redaction_irreversible_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER customer_notification_deliveries_redaction_irreversible_guard
BEFORE UPDATE ON customer_notification_deliveries
FOR EACH ROW EXECUTE FUNCTION customer_notification_redaction_irreversible_guard();

CREATE INDEX idx_customer_notification_deliveries_retention_due
    ON customer_notification_deliveries(retention_expires_at, id)
    WHERE redacted_at IS NULL AND retention_expires_at IS NOT NULL;

ALTER TABLE voice_audio_outputs
    ADD COLUMN redacted_at TIMESTAMPTZ,
    ADD COLUMN redaction_version INTEGER,
    ADD CONSTRAINT voice_audio_outputs_redaction_shape_check
        CHECK (
            (redacted_at IS NULL AND redaction_version IS NULL)
            OR
			(redacted_at IS NOT NULL
				AND redaction_version = 1
				AND octet_length(audio_data) = 0)
		);

CREATE FUNCTION voice_audio_redaction_irreversible_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.redacted_at IS NOT NULL
       AND (OLD.redacted_at IS DISTINCT FROM NEW.redacted_at
            OR OLD.redaction_version IS DISTINCT FROM NEW.redaction_version
            OR OLD.audio_data IS DISTINCT FROM NEW.audio_data) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'voice audio redaction is irreversible',
            CONSTRAINT = 'voice_audio_outputs_redaction_irreversible_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER voice_audio_outputs_redaction_irreversible_guard
BEFORE UPDATE ON voice_audio_outputs
FOR EACH ROW EXECUTE FUNCTION voice_audio_redaction_irreversible_guard();

CREATE INDEX idx_voice_audio_outputs_redaction_due
    ON voice_audio_outputs(expires_at, id)
    WHERE redacted_at IS NULL;
