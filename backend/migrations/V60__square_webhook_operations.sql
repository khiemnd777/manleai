-- Phase 6G: owner-visible Square webhook operations without exposing provider
-- identity, payload, signature, token, or raw error data.

ALTER TABLE square_booking_webhook_events
    ADD COLUMN IF NOT EXISTS last_error_class TEXT,
    ADD COLUMN IF NOT EXISTS last_error_code TEXT,
    ADD COLUMN IF NOT EXISTS dead_lettered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS requeue_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE square_calendar_repair_state
    ADD COLUMN IF NOT EXISTS last_error_class TEXT,
    ADD COLUMN IF NOT EXISTS last_error_code TEXT;

-- The V41 status constraint does not know the new terminal token, so remove it
-- before classifying legacy rows. The expanded constraint is installed below
-- in the same migration transaction.
ALTER TABLE square_booking_webhook_events
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_processing_status_check;

-- Legacy rows at or beyond the old ten-attempt claim ceiling were no longer
-- claimable. Make that terminal state explicit before installing the new
-- constraints. Any active old claim is fenced because its token is cleared.
UPDATE square_booking_webhook_events
SET processing_status = 'dead_letter',
    processing_token = NULL,
    processing_lease_expires_at = NULL,
    dead_lettered_at = COALESCE(dead_lettered_at, now()),
    last_error_class = 'processing',
    last_error_code = 'SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED',
    last_error = NULL,
    updated_at = now()
WHERE processing_attempts >= 10
  AND processing_status IN ('pending', 'processing', 'failed');

-- Preserve only bounded diagnostics for non-terminal legacy failures. Raw
-- last_error text may contain provider details and is deliberately discarded.
UPDATE square_booking_webhook_events
SET last_error_class = CASE
		WHEN processing_status = 'dead_letter' THEN COALESCE(last_error_class, 'processing')
        WHEN processing_status = 'failed' THEN 'dependency'
        ELSE NULL
    END,
    last_error_code = CASE
		WHEN processing_status = 'dead_letter' THEN COALESCE(last_error_code, 'SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED')
        WHEN processing_status = 'failed' THEN 'SQUARE_WEBHOOK_PROCESSING_FAILED'
        ELSE NULL
    END,
    last_error = NULL,
    dead_lettered_at = CASE
        WHEN processing_status = 'dead_letter' THEN COALESCE(dead_lettered_at, now())
        ELSE NULL
    END,
    processing_token = CASE WHEN processing_status = 'processing' THEN processing_token ELSE NULL END,
    processing_lease_expires_at = CASE WHEN processing_status = 'processing' THEN processing_lease_expires_at ELSE NULL END;

UPDATE square_calendar_repair_state
SET last_error_class = CASE WHEN last_error IS NOT NULL THEN 'dependency' ELSE NULL END,
    last_error_code = CASE WHEN last_error IS NOT NULL THEN 'SQUARE_CALENDAR_REPAIR_FAILED' ELSE NULL END,
    last_error = NULL;

ALTER TABLE square_booking_webhook_events
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_processing_status_check,
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_processing_claim_shape_check,
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_dead_letter_shape_check,
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_error_class_check,
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_error_code_check,
    DROP CONSTRAINT IF EXISTS square_booking_webhook_events_requeue_count_check;

ALTER TABLE square_booking_webhook_events
    ADD CONSTRAINT square_booking_webhook_events_processing_status_check
        CHECK (processing_status IN ('pending', 'processing', 'succeeded', 'failed', 'ignored', 'dead_letter')),
    ADD CONSTRAINT square_booking_webhook_events_processing_claim_shape_check
        CHECK (
            (processing_status = 'processing'
                AND processing_token IS NOT NULL
                AND processing_lease_expires_at IS NOT NULL)
            OR
            (processing_status <> 'processing'
                AND processing_token IS NULL
                AND processing_lease_expires_at IS NULL)
        ),
    ADD CONSTRAINT square_booking_webhook_events_dead_letter_shape_check
        CHECK ((processing_status = 'dead_letter') = (dead_lettered_at IS NOT NULL)),
    ADD CONSTRAINT square_booking_webhook_events_error_class_check
        CHECK (last_error_class IS NULL OR last_error_class ~ '^[a-z][a-z0-9_]{0,31}$'),
    ADD CONSTRAINT square_booking_webhook_events_error_code_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    ADD CONSTRAINT square_booking_webhook_events_requeue_count_check
        CHECK (requeue_count BETWEEN 0 AND 3);

ALTER TABLE square_calendar_repair_state
    DROP CONSTRAINT IF EXISTS square_calendar_repair_state_error_class_check,
    DROP CONSTRAINT IF EXISTS square_calendar_repair_state_error_code_check;

ALTER TABLE square_calendar_repair_state
    ADD CONSTRAINT square_calendar_repair_state_error_class_check
        CHECK (last_error_class IS NULL OR last_error_class ~ '^[a-z][a-z0-9_]{0,31}$'),
    ADD CONSTRAINT square_calendar_repair_state_error_code_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[A-Z][A-Z0-9_]{0,63}$');

CREATE UNIQUE INDEX IF NOT EXISTS idx_square_booking_webhook_events_salon_id_id
    ON square_booking_webhook_events(salon_id, id);

DROP INDEX IF EXISTS idx_square_booking_webhook_events_ready;
CREATE INDEX idx_square_booking_webhook_events_ready
    ON square_booking_webhook_events(next_attempt_at, created_at, id)
    WHERE processing_status IN ('pending', 'failed', 'processing');

CREATE INDEX IF NOT EXISTS idx_square_booking_webhook_events_owner_status
    ON square_booking_webhook_events(salon_id, processing_status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS square_booking_webhook_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    webhook_event_id UUID NOT NULL,
    action_key TEXT NOT NULL
        CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_fingerprint TEXT NOT NULL
        CHECK (action_fingerprint ~ '^[0-9a-f]{64}$'),
    action_type TEXT NOT NULL CHECK (action_type = 'requeue'),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    result_processing_status TEXT NOT NULL CHECK (result_processing_status = 'pending'),
    result_processing_attempts INTEGER NOT NULL CHECK (result_processing_attempts >= 0),
    result_requeue_count INTEGER NOT NULL CHECK (result_requeue_count BETWEEN 1 AND 3),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT square_booking_webhook_actions_tenant_fk
        FOREIGN KEY (salon_id, webhook_event_id)
        REFERENCES square_booking_webhook_events(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT square_booking_webhook_actions_key_unique UNIQUE (salon_id, action_key)
);

CREATE INDEX IF NOT EXISTS idx_square_booking_webhook_actions_event
    ON square_booking_webhook_actions(webhook_event_id, created_at DESC, id DESC);

-- Webhook provider evidence is ingress audit data. Requeue and worker
-- processing may change only operational fields, never tenant or provider
-- identity/version/status evidence from the signed delivery.
CREATE OR REPLACE FUNCTION square_booking_webhook_event_evidence_immutable_guard()
RETURNS trigger AS $$
BEGIN
    IF ROW(
        NEW.salon_id, NEW.event_id, NEW.event_type, NEW.merchant_id,
        NEW.location_id, NEW.pos_booking_id, NEW.pos_booking_version,
        NEW.booking_status, NEW.booking_start_at, NEW.delivered_at, NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.salon_id, OLD.event_id, OLD.event_type, OLD.merchant_id,
        OLD.location_id, OLD.pos_booking_id, OLD.pos_booking_version,
        OLD.booking_status, OLD.booking_start_at, OLD.delivered_at, OLD.created_at
    ) THEN
        RAISE EXCEPTION 'square webhook identity and provider evidence are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS square_booking_webhook_event_evidence_immutable_guard
    ON square_booking_webhook_events;
CREATE TRIGGER square_booking_webhook_event_evidence_immutable_guard
BEFORE UPDATE ON square_booking_webhook_events
FOR EACH ROW EXECUTE FUNCTION square_booking_webhook_event_evidence_immutable_guard();

-- Old workers may still send raw last_error during a rolling handoff. Drop the
-- text at the database boundary and retain only a stable safe diagnostic.
CREATE OR REPLACE FUNCTION square_webhook_operational_error_redaction_guard()
RETURNS trigger AS $$
BEGIN
    IF NEW.last_error IS NOT NULL THEN
        NEW.last_error := NULL;
        NEW.last_error_class := COALESCE(NEW.last_error_class, 'dependency');
        NEW.last_error_code := COALESCE(NEW.last_error_code, 'SQUARE_WEBHOOK_PROCESSING_FAILED');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS square_webhook_operational_error_redaction_guard
    ON square_booking_webhook_events;
CREATE TRIGGER square_webhook_operational_error_redaction_guard
BEFORE INSERT OR UPDATE ON square_booking_webhook_events
FOR EACH ROW EXECUTE FUNCTION square_webhook_operational_error_redaction_guard();

CREATE OR REPLACE FUNCTION square_calendar_repair_error_redaction_guard()
RETURNS trigger AS $$
BEGIN
    IF NEW.last_error IS NOT NULL THEN
        NEW.last_error := NULL;
        NEW.last_error_class := COALESCE(NEW.last_error_class, 'dependency');
        NEW.last_error_code := COALESCE(NEW.last_error_code, 'SQUARE_CALENDAR_REPAIR_FAILED');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS square_calendar_repair_error_redaction_guard
    ON square_calendar_repair_state;
CREATE TRIGGER square_calendar_repair_error_redaction_guard
BEFORE INSERT OR UPDATE ON square_calendar_repair_state
FOR EACH ROW EXECUTE FUNCTION square_calendar_repair_error_redaction_guard();

CREATE OR REPLACE FUNCTION square_booking_webhook_action_immutable_guard()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'square webhook actions are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS square_booking_webhook_action_immutable_guard
    ON square_booking_webhook_actions;
CREATE TRIGGER square_booking_webhook_action_immutable_guard
BEFORE UPDATE ON square_booking_webhook_actions
FOR EACH ROW EXECUTE FUNCTION square_booking_webhook_action_immutable_guard();
