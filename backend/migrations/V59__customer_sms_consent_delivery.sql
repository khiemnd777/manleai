-- Phase 6A2: explicit customer SMS consent, salon policy, and transactional
-- customer notification delivery. The legacy sms_confirmation_enabled flag is
-- not consent and is deliberately not reused by this contract.

ALTER TABLE salon_settings
    ADD COLUMN customer_sms_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN customer_sms_quiet_start TIME,
    ADD COLUMN customer_sms_quiet_end TIME,
    ADD COLUMN customer_sms_policy_version BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT salon_settings_customer_sms_policy_version_check
        CHECK (customer_sms_policy_version >= 1),
    ADD CONSTRAINT salon_settings_customer_sms_quiet_shape_check
        CHECK (
            (customer_sms_quiet_start IS NULL AND customer_sms_quiet_end IS NULL)
            OR
            (customer_sms_quiet_start IS NOT NULL
                AND customer_sms_quiet_end IS NOT NULL
                AND customer_sms_quiet_start <> customer_sms_quiet_end)
        ),
    ADD CONSTRAINT salon_settings_customer_sms_enablement_check
        CHECK (
            NOT customer_sms_enabled
            OR (customer_sms_quiet_start IS NOT NULL AND customer_sms_quiet_end IS NOT NULL)
        );

CREATE FUNCTION enforce_customer_sms_policy_version()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.customer_sms_enabled IS DISTINCT FROM NEW.customer_sms_enabled
       OR OLD.customer_sms_quiet_start IS DISTINCT FROM NEW.customer_sms_quiet_start
       OR OLD.customer_sms_quiet_end IS DISTINCT FROM NEW.customer_sms_quiet_end THEN
        NEW.customer_sms_policy_version := OLD.customer_sms_policy_version + 1;
    ELSIF OLD.customer_sms_policy_version IS DISTINCT FROM NEW.customer_sms_policy_version THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'customer SMS policy version is managed by the database',
            CONSTRAINT = 'salon_settings_customer_sms_policy_version_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER salon_settings_customer_sms_policy_version_guard
BEFORE UPDATE OF customer_sms_enabled, customer_sms_quiet_start,
                 customer_sms_quiet_end, customer_sms_policy_version
ON salon_settings
FOR EACH ROW
EXECUTE FUNCTION enforce_customer_sms_policy_version();

-- US salon scope accepts explicit E.164 and canonical 10/11 digit US values.
-- Any other value stays unrecognized instead of being guessed.
CREATE FUNCTION normalize_customer_sms_destination(raw_value TEXT)
RETURNS TEXT AS $$
DECLARE
    trimmed TEXT := btrim(COALESCE(raw_value, ''));
    digits TEXT;
BEGIN
    IF trimmed = '' THEN
        RETURN NULL;
    END IF;
    IF trimmed !~ '^\+?[0-9(). -]+$'
       OR (position('+' IN trimmed) > 1)
       OR length(trimmed) - length(replace(trimmed, '+', '')) > 1 THEN
        RETURN NULL;
    END IF;
    digits := regexp_replace(trimmed, '[^0-9]', '', 'g');
    IF trimmed LIKE '+%' AND digits ~ '^[1-9][0-9]{7,14}$' THEN
        RETURN '+' || digits;
    END IF;
    IF digits ~ '^[2-9][0-9]{9}$' THEN
        RETURN '+1' || digits;
    END IF;
    IF digits ~ '^1[2-9][0-9]{9}$' THEN
        RETURN '+' || digits;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE FUNCTION mask_customer_sms_destination(destination TEXT)
RETURNS TEXT AS $$
BEGIN
    IF destination IS NULL OR length(destination) < 4 THEN
        RETURN NULL;
    END IF;
    RETURN '••••' || right(destination, 4);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE TABLE customer_sms_consents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    normalized_destination TEXT NOT NULL,
    destination_masked TEXT NOT NULL,
    status TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    source TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    evidence_reference TEXT NOT NULL,
    call_session_id UUID,
    actor_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    requested_at TIMESTAMPTZ,
    consented_at TIMESTAMPTZ,
    declined_at TIMESTAMPTZ,
    opted_out_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_sms_consents_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT customer_sms_consents_destination_key UNIQUE (salon_id, normalized_destination),
    CONSTRAINT customer_sms_consents_destination_check
        CHECK (normalized_destination ~ '^\+[1-9][0-9]{7,14}$'),
    CONSTRAINT customer_sms_consents_destination_mask_check
        CHECK (destination_masked ~ '^.{4}[0-9]{4}$'),
    CONSTRAINT customer_sms_consents_status_check
        CHECK (status IN ('pending', 'consented', 'declined', 'opted_out')),
    CONSTRAINT customer_sms_consents_version_check CHECK (version >= 1),
    CONSTRAINT customer_sms_consents_source_check
        CHECK (source IN ('conversation_explicit', 'owner_attested', 'twilio_advanced_opt_out')),
    CONSTRAINT customer_sms_consents_evidence_check
        CHECK (length(btrim(evidence_type)) BETWEEN 1 AND 64
            AND length(btrim(evidence_reference)) BETWEEN 1 AND 256),
    CONSTRAINT customer_sms_consents_call_session_tenant_fk
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES call_sessions(salon_id, id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT customer_sms_consents_state_shape_check
        CHECK (
            (status = 'pending' AND requested_at IS NOT NULL
                AND consented_at IS NULL AND declined_at IS NULL AND opted_out_at IS NULL)
            OR
            (status = 'consented' AND consented_at IS NOT NULL AND opted_out_at IS NULL)
            OR
            (status = 'declined' AND declined_at IS NOT NULL AND consented_at IS NULL AND opted_out_at IS NULL)
            OR
            (status = 'opted_out' AND opted_out_at IS NOT NULL)
        ),
    CONSTRAINT customer_sms_consents_owner_evidence_check
        CHECK (source <> 'owner_attested' OR actor_user_id IS NOT NULL),
    CONSTRAINT customer_sms_consents_conversation_evidence_check
        CHECK (source <> 'conversation_explicit' OR call_session_id IS NOT NULL)
);

CREATE INDEX idx_customer_sms_consents_salon_status
    ON customer_sms_consents(salon_id, status, updated_at DESC, id);

CREATE TABLE customer_sms_consent_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    customer_sms_consent_id UUID NOT NULL,
    event_sequence INTEGER NOT NULL,
    consent_version INTEGER NOT NULL,
    event_key TEXT NOT NULL,
    event_fingerprint TEXT NOT NULL,
    event_type TEXT NOT NULL,
    source TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    evidence_reference TEXT NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    provider_message_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_sms_consent_events_tenant_fk
        FOREIGN KEY (salon_id, customer_sms_consent_id)
        REFERENCES customer_sms_consents(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_sms_consent_events_sequence_check CHECK (event_sequence >= 1),
    CONSTRAINT customer_sms_consent_events_version_check CHECK (consent_version >= 1),
    CONSTRAINT customer_sms_consent_events_event_key_check
        CHECK (event_key = btrim(event_key) AND length(event_key) BETWEEN 1 AND 256),
    CONSTRAINT customer_sms_consent_events_fingerprint_check
        CHECK (event_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT customer_sms_consent_events_type_check
        CHECK (event_type IN ('consent_requested', 'consented', 'declined', 'opt_out', 'opt_in', 'help')),
    CONSTRAINT customer_sms_consent_events_source_check
        CHECK (source IN ('conversation_explicit', 'owner_attested', 'twilio_advanced_opt_out')),
    CONSTRAINT customer_sms_consent_events_evidence_check
        CHECK (length(btrim(evidence_type)) BETWEEN 1 AND 64
            AND length(btrim(evidence_reference)) BETWEEN 1 AND 256),
    CONSTRAINT customer_sms_consent_events_sequence_key
        UNIQUE (customer_sms_consent_id, event_sequence),
    CONSTRAINT customer_sms_consent_events_action_key
        UNIQUE (salon_id, event_key)
);

CREATE INDEX idx_customer_sms_consent_events_consent
    ON customer_sms_consent_events(customer_sms_consent_id, event_sequence ASC);

CREATE FUNCTION reject_customer_sms_consent_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'customer SMS consent events are immutable',
        CONSTRAINT = 'customer_sms_consent_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER customer_sms_consent_events_immutable_guard
BEFORE UPDATE OR DELETE ON customer_sms_consent_events
FOR EACH ROW EXECUTE FUNCTION reject_customer_sms_consent_event_mutation();

CREATE TABLE customer_notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    customer_sms_consent_id UUID NOT NULL,
    scheduling_request_id UUID,
    appointment_id UUID,
    booking_attempt_id UUID,
    notification_type TEXT NOT NULL,
    source_version INTEGER NOT NULL,
    dedupe_key TEXT NOT NULL,
    message_body TEXT NOT NULL,
    destination_e164 TEXT NOT NULL,
    destination_masked TEXT NOT NULL,
    destination_hash TEXT NOT NULL,
    consent_version INTEGER NOT NULL,
    policy_version BIGINT NOT NULL,
    delivery_status TEXT NOT NULL DEFAULT 'queued',
    delivery_provider TEXT,
    delivery_attempts INTEGER NOT NULL DEFAULT 0,
    next_delivery_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivery_claim_token UUID,
    delivery_claimed_at TIMESTAMPTZ,
    delivery_lease_expires_at TIMESTAMPTZ,
    delivery_dispatch_started_at TIMESTAMPTZ,
    provider_message_id TEXT,
    provider_status TEXT,
    provider_status_rank INTEGER NOT NULL DEFAULT 0,
    last_provider_event_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    suppressed_at TIMESTAMPTZ,
    requeue_count INTEGER NOT NULL DEFAULT 0,
    last_delivery_error_code TEXT,
    redacted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_notification_deliveries_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT customer_notification_deliveries_consent_tenant_fk
        FOREIGN KEY (salon_id, customer_sms_consent_id)
        REFERENCES customer_sms_consents(salon_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_notification_deliveries_request_tenant_fk
        FOREIGN KEY (salon_id, scheduling_request_id)
        REFERENCES scheduling_requests(salon_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_notification_deliveries_appointment_tenant_fk
        FOREIGN KEY (salon_id, appointment_id)
        REFERENCES appointments(salon_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_notification_deliveries_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_notification_deliveries_type_check
        CHECK (notification_type IN ('request_received', 'confirmed', 'rescheduled', 'cancelled')),
    CONSTRAINT customer_notification_deliveries_source_shape_check
        CHECK (
            (notification_type = 'request_received'
                AND scheduling_request_id IS NOT NULL AND appointment_id IS NULL AND booking_attempt_id IS NULL)
            OR
            (notification_type IN ('confirmed', 'rescheduled', 'cancelled')
                AND scheduling_request_id IS NULL AND appointment_id IS NOT NULL AND booking_attempt_id IS NOT NULL)
        ),
    CONSTRAINT customer_notification_deliveries_source_version_check CHECK (source_version >= 0),
    CONSTRAINT customer_notification_deliveries_dedupe_check
        CHECK (dedupe_key = btrim(dedupe_key) AND length(dedupe_key) BETWEEN 1 AND 256),
    CONSTRAINT customer_notification_deliveries_message_check CHECK (length(btrim(message_body)) BETWEEN 1 AND 1000),
    CONSTRAINT customer_notification_deliveries_destination_check
        CHECK (destination_e164 ~ '^\+[1-9][0-9]{7,14}$'
            AND destination_masked ~ '^.{4}[0-9]{4}$'
            AND destination_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT customer_notification_deliveries_version_check
        CHECK (consent_version >= 1 AND policy_version >= 1),
    CONSTRAINT customer_notification_deliveries_status_check
        CHECK (delivery_status IN (
            'queued', 'quiet_hours', 'delivering', 'provider_accepted', 'sent',
            'delivered', 'failed', 'undelivered', 'dead_letter', 'suppressed'
        )),
    CONSTRAINT customer_notification_deliveries_provider_check
        CHECK (delivery_provider IS NULL OR delivery_provider = 'twilio'),
    CONSTRAINT customer_notification_deliveries_claim_shape_check
        CHECK (
            (delivery_status = 'delivering' AND delivery_claim_token IS NOT NULL
                AND delivery_claimed_at IS NOT NULL AND delivery_lease_expires_at IS NOT NULL
                AND delivery_lease_expires_at > delivery_claimed_at)
            OR
            (delivery_status <> 'delivering' AND delivery_claim_token IS NULL
                AND delivery_claimed_at IS NULL AND delivery_lease_expires_at IS NULL
                AND delivery_dispatch_started_at IS NULL)
        ),
    CONSTRAINT customer_notification_deliveries_provider_evidence_check
        CHECK (delivery_status NOT IN ('provider_accepted', 'sent', 'delivered', 'undelivered')
            OR (delivery_provider IS NOT NULL AND provider_message_id IS NOT NULL)),
    CONSTRAINT customer_notification_deliveries_status_rank_check CHECK (provider_status_rank >= 0),
    CONSTRAINT customer_notification_deliveries_attempts_check
        CHECK (delivery_attempts >= 0 AND requeue_count BETWEEN 0 AND 1),
    CONSTRAINT customer_notification_deliveries_terminal_shape_check
        CHECK ((delivery_status = 'dead_letter') = (dead_lettered_at IS NOT NULL)
            AND (delivery_status = 'suppressed') = (suppressed_at IS NOT NULL)),
    CONSTRAINT customer_notification_deliveries_redaction_check
        CHECK (redacted_at IS NULL OR redacted_at >= created_at),
    CONSTRAINT customer_notification_deliveries_dedupe_key UNIQUE (salon_id, dedupe_key)
);

CREATE UNIQUE INDEX idx_customer_notification_deliveries_provider_message
    ON customer_notification_deliveries(delivery_provider, provider_message_id)
    WHERE delivery_provider IS NOT NULL AND provider_message_id IS NOT NULL;
CREATE INDEX idx_customer_notification_delivery_queue
    ON customer_notification_deliveries(next_delivery_at, created_at, id)
    WHERE delivery_status IN ('queued', 'quiet_hours', 'failed');
CREATE INDEX idx_customer_notification_delivery_appointment
    ON customer_notification_deliveries(salon_id, appointment_id, created_at DESC)
    WHERE appointment_id IS NOT NULL;
CREATE INDEX idx_customer_notification_delivery_request
    ON customer_notification_deliveries(salon_id, scheduling_request_id, created_at DESC)
    WHERE scheduling_request_id IS NOT NULL;

CREATE TABLE customer_notification_delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    customer_notification_delivery_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    claim_token UUID NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK (provider = 'twilio'),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'leased', 'quiet_hours', 'safe_retry', 'provider_accepted', 'sent', 'delivered',
        'provider_failed', 'outcome_unknown', 'dead_letter', 'suppressed'
    )),
    provider_status TEXT,
    provider_message_id TEXT,
    error_code TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    dispatch_started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_notification_delivery_attempts_tenant_fk
        FOREIGN KEY (salon_id, customer_notification_delivery_id)
        REFERENCES customer_notification_deliveries(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_notification_delivery_attempts_number_key
        UNIQUE (customer_notification_delivery_id, attempt_number),
    CONSTRAINT customer_notification_delivery_attempts_finish_shape_check
        CHECK ((outcome = 'leased') = (finished_at IS NULL)),
    CONSTRAINT customer_notification_delivery_attempts_dispatch_shape_check
        CHECK (dispatch_started_at IS NULL OR dispatch_started_at >= started_at)
);

CREATE TABLE customer_notification_delivery_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    customer_notification_delivery_id UUID NOT NULL,
    event_key TEXT NOT NULL CHECK (event_key = btrim(event_key) AND length(event_key) BETWEEN 1 AND 256),
    event_fingerprint TEXT NOT NULL CHECK (event_fingerprint ~ '^[0-9a-f]{64}$'),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'enqueued', 'claimed', 'quiet_hours_scheduled', 'safe_retry_scheduled',
        'dispatch_started', 'provider_response', 'status_callback', 'dead_lettered',
        'suppressed', 'owner_requeued'
    )),
    delivery_status TEXT NOT NULL,
    provider_status TEXT,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_notification_delivery_events_tenant_fk
        FOREIGN KEY (salon_id, customer_notification_delivery_id)
        REFERENCES customer_notification_deliveries(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_notification_delivery_events_action_key UNIQUE (salon_id, event_key)
);

CREATE INDEX idx_customer_notification_delivery_events_delivery
    ON customer_notification_delivery_events(customer_notification_delivery_id, created_at, id);

CREATE TABLE customer_notification_delivery_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    customer_notification_delivery_id UUID NOT NULL,
    action_key TEXT NOT NULL CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_fingerprint TEXT NOT NULL CHECK (action_fingerprint ~ '^[0-9a-f]{64}$'),
    action_type TEXT NOT NULL CHECK (action_type = 'requeue'),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    result_delivery_status TEXT NOT NULL CHECK (result_delivery_status = 'queued'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_notification_delivery_actions_tenant_fk
        FOREIGN KEY (salon_id, customer_notification_delivery_id)
        REFERENCES customer_notification_deliveries(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_notification_delivery_actions_key UNIQUE (salon_id, action_key)
);

CREATE FUNCTION reject_customer_notification_delivery_audit_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'customer notification delivery audit records are immutable',
        CONSTRAINT = 'customer_notification_delivery_audit_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER customer_notification_delivery_events_immutable_guard
BEFORE UPDATE OR DELETE ON customer_notification_delivery_events
FOR EACH ROW EXECUTE FUNCTION reject_customer_notification_delivery_audit_mutation();

CREATE TRIGGER customer_notification_delivery_actions_immutable_guard
BEFORE UPDATE OR DELETE ON customer_notification_delivery_actions
FOR EACH ROW EXECUTE FUNCTION reject_customer_notification_delivery_audit_mutation();

CREATE FUNCTION insert_customer_notification_event(
    p_salon_id UUID,
    p_delivery_id UUID,
    p_event_key TEXT,
    p_event_type TEXT,
    p_delivery_status TEXT
) RETURNS VOID AS $$
BEGIN
    INSERT INTO customer_notification_delivery_events (
        salon_id, customer_notification_delivery_id, event_key, event_fingerprint,
        event_type, delivery_status
    ) VALUES (
        p_salon_id, p_delivery_id, p_event_key,
        encode(digest(p_event_key || ':' || p_event_type || ':' || p_delivery_status, 'sha256'), 'hex'),
        p_event_type, p_delivery_status
    );
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enqueue_customer_notification(
    p_salon_id UUID,
    p_destination TEXT,
    p_notification_type TEXT,
    p_source_version INTEGER,
    p_dedupe_key TEXT,
    p_message_body TEXT,
    p_scheduling_request_id UUID,
    p_appointment_id UUID,
    p_booking_attempt_id UUID
) RETURNS VOID AS $$
DECLARE
    normalized TEXT := normalize_customer_sms_destination(p_destination);
    consent_row customer_sms_consents%ROWTYPE;
    policy_row RECORD;
    delivery_id UUID;
BEGIN
    IF normalized IS NULL THEN
        RETURN;
    END IF;
    SELECT settings.customer_sms_enabled, settings.customer_sms_policy_version,
           settings.customer_sms_quiet_start, settings.customer_sms_quiet_end, salon.timezone
    INTO policy_row
    FROM salon_settings settings
    JOIN salons salon ON salon.id = settings.salon_id
    WHERE settings.salon_id = p_salon_id;
    IF NOT FOUND OR NOT policy_row.customer_sms_enabled
       OR policy_row.customer_sms_quiet_start IS NULL
       OR policy_row.customer_sms_quiet_end IS NULL
       OR btrim(COALESCE(policy_row.timezone, '')) = '' THEN
        RETURN;
    END IF;
    SELECT * INTO consent_row
    FROM customer_sms_consents
    WHERE salon_id = p_salon_id AND normalized_destination = normalized
      AND status = 'consented';
    IF NOT FOUND THEN
        RETURN;
    END IF;
    INSERT INTO customer_notification_deliveries (
        salon_id, customer_sms_consent_id, scheduling_request_id, appointment_id,
        booking_attempt_id, notification_type, source_version, dedupe_key, message_body,
        destination_e164, destination_masked, destination_hash, consent_version, policy_version
    ) VALUES (
        p_salon_id, consent_row.id, p_scheduling_request_id, p_appointment_id,
        p_booking_attempt_id, p_notification_type, p_source_version, p_dedupe_key, p_message_body,
        normalized, consent_row.destination_masked,
        encode(digest(normalized, 'sha256'), 'hex'), consent_row.version,
        policy_row.customer_sms_policy_version
    )
    ON CONFLICT (salon_id, dedupe_key) DO NOTHING
    RETURNING id INTO delivery_id;
    IF delivery_id IS NOT NULL THEN
        PERFORM insert_customer_notification_event(
            p_salon_id, delivery_id, 'enqueue:' || p_dedupe_key, 'enqueued', 'queued'
        );
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enqueue_customer_request_received()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'pending' THEN
        PERFORM enqueue_customer_notification(
            NEW.salon_id, NEW.customer_phone, 'request_received', NEW.version,
            'request:' || NEW.id::text || ':v' || NEW.version::text || ':received',
            'We received your appointment request for owner review. It is not confirmed yet. Reply STOP to opt out.',
            NEW.id, NULL, NULL
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_requests_customer_notification_outbox
AFTER INSERT ON scheduling_requests
FOR EACH ROW EXECUTE FUNCTION enqueue_customer_request_received();

CREATE FUNCTION enqueue_customer_appointment_lifecycle()
RETURNS TRIGGER AS $$
DECLARE
    attempt_row RECORD;
    notification_kind TEXT;
    local_start TEXT;
    salon_name TEXT;
    message_copy TEXT;
BEGIN
    SELECT source, status, operation_type INTO attempt_row
    FROM booking_attempts
    WHERE id = NEW.booking_attempt_id AND salon_id = NEW.salon_id;
    IF NOT FOUND OR attempt_row.source = 'pos_calendar_sync' THEN
        RETURN NEW;
    END IF;
    IF attempt_row.operation_type = 'book' AND attempt_row.status = 'confirmed' AND NEW.status = 'confirmed' THEN
        notification_kind := 'confirmed';
    ELSIF attempt_row.operation_type = 'reschedule' AND attempt_row.status = 'rescheduled' AND NEW.status = 'rescheduled' THEN
        notification_kind := 'rescheduled';
    ELSIF attempt_row.operation_type = 'cancel' AND attempt_row.status = 'cancelled' AND NEW.status = 'cancelled' THEN
        notification_kind := 'cancelled';
    ELSE
        RETURN NEW;
    END IF;
    SELECT salon.name,
           to_char(NEW.start_time AT TIME ZONE salon.timezone, 'FMDay, FMMonth FMDD at FMHH12:MI AM')
    INTO salon_name, local_start
    FROM salons salon WHERE salon.id = NEW.salon_id;
    IF notification_kind = 'confirmed' THEN
        message_copy := format('Your appointment at %s is confirmed for %s. Reply STOP to opt out.', salon_name, local_start);
    ELSIF notification_kind = 'rescheduled' THEN
        message_copy := format('Your appointment at %s was rescheduled to %s. Reply STOP to opt out.', salon_name, local_start);
    ELSE
        message_copy := format('Your appointment at %s was cancelled. Reply STOP to opt out.', salon_name);
    END IF;
    PERFORM enqueue_customer_notification(
        NEW.salon_id, NEW.customer_phone, notification_kind,
        COALESCE(NEW.authority_appointment_version, 1),
        'appointment:' || NEW.id::text || ':v' || COALESCE(NEW.authority_appointment_version, 1)::text || ':' || notification_kind,
        message_copy, NULL, NEW.id, NEW.booking_attempt_id
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER appointments_customer_notification_outbox_insert
AFTER INSERT ON appointments
FOR EACH ROW EXECUTE FUNCTION enqueue_customer_appointment_lifecycle();

CREATE TRIGGER appointments_customer_notification_outbox_update
AFTER UPDATE OF booking_attempt_id, status, authority_appointment_version ON appointments
FOR EACH ROW
WHEN (
    OLD.booking_attempt_id IS DISTINCT FROM NEW.booking_attempt_id
    OR OLD.status IS DISTINCT FROM NEW.status
    OR OLD.authority_appointment_version IS DISTINCT FROM NEW.authority_appointment_version
)
EXECUTE FUNCTION enqueue_customer_appointment_lifecycle();
