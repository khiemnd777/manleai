-- Phase 6A1: provider-neutral, salon-scoped owner notification delivery.
-- Existing rows are deliberately disabled: installing a delivery worker must
-- never infer owner consent or a destination from historical salon data.

UPDATE owner_notifications
SET delivery_status = 'disabled',
    last_delivery_error = NULL
WHERE delivery_status IN ('queued', 'delivering', 'delivered', 'failed');

ALTER TABLE owner_notifications
    DROP CONSTRAINT owner_notifications_delivery_status_check;

ALTER TABLE owner_notifications
    ADD COLUMN delivery_provider TEXT,
    ADD COLUMN delivery_claim_token UUID,
    ADD COLUMN delivery_claimed_at TIMESTAMPTZ,
    ADD COLUMN delivery_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN delivery_dispatch_started_at TIMESTAMPTZ,
    ADD COLUMN provider_message_id TEXT,
    ADD COLUMN provider_status TEXT,
    ADD COLUMN provider_status_rank INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN delivery_destination_masked TEXT,
    ADD COLUMN last_provider_event_at TIMESTAMPTZ,
    ADD COLUMN dead_lettered_at TIMESTAMPTZ,
    ADD COLUMN requeue_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN last_delivery_error_code TEXT,
    ADD CONSTRAINT owner_notifications_delivery_status_check
        CHECK (delivery_status IN (
            'queued', 'delivering', 'provider_accepted', 'sent', 'delivered',
            'failed', 'undelivered', 'dead_letter', 'disabled'
        )),
    ADD CONSTRAINT owner_notifications_delivery_provider_check
        CHECK (delivery_provider IS NULL OR delivery_provider = 'twilio'),
    ADD CONSTRAINT owner_notifications_delivery_claim_shape_check
        CHECK (
            (delivery_status = 'delivering'
                AND delivery_claim_token IS NOT NULL
                AND delivery_claimed_at IS NOT NULL
                AND delivery_lease_expires_at IS NOT NULL
                AND delivery_lease_expires_at > delivery_claimed_at)
            OR
            (delivery_status <> 'delivering'
                AND delivery_claim_token IS NULL
                AND delivery_claimed_at IS NULL
                AND delivery_lease_expires_at IS NULL
                AND delivery_dispatch_started_at IS NULL)
        ),
    ADD CONSTRAINT owner_notifications_provider_evidence_check
        CHECK (
            delivery_status NOT IN ('provider_accepted', 'sent', 'delivered', 'undelivered')
            OR (delivery_provider IS NOT NULL AND provider_message_id IS NOT NULL)
        ),
    ADD CONSTRAINT owner_notifications_delivery_status_rank_check
        CHECK (provider_status_rank >= 0),
    ADD CONSTRAINT owner_notifications_destination_mask_check
        CHECK (delivery_destination_masked IS NULL OR delivery_destination_masked ~ '^.{4}[0-9]{4}$'),
    ADD CONSTRAINT owner_notifications_requeue_count_check
        CHECK (requeue_count >= 0),
    ADD CONSTRAINT owner_notifications_dead_letter_shape_check
        CHECK ((delivery_status = 'dead_letter') = (dead_lettered_at IS NOT NULL));

CREATE UNIQUE INDEX idx_owner_notifications_salon_id_id
    ON owner_notifications(salon_id, id);

CREATE UNIQUE INDEX idx_owner_notifications_provider_message
    ON owner_notifications(delivery_provider, provider_message_id)
    WHERE delivery_provider IS NOT NULL AND provider_message_id IS NOT NULL;

DROP INDEX IF EXISTS idx_owner_notifications_delivery_queue;
CREATE INDEX idx_owner_notifications_delivery_queue
    ON owner_notifications(next_delivery_at ASC, created_at ASC, id ASC)
    WHERE delivery_status IN ('queued', 'failed');

CREATE INDEX idx_owner_notifications_expired_delivery_leases
    ON owner_notifications(delivery_lease_expires_at ASC, id ASC)
    WHERE delivery_status = 'delivering';

CREATE TABLE owner_notification_delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    owner_notification_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    claim_token UUID NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK (provider = 'twilio'),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'leased', 'safe_retry', 'provider_accepted', 'sent', 'delivered',
        'provider_failed', 'outcome_unknown', 'dead_letter', 'disabled'
    )),
    provider_status TEXT,
    provider_message_id TEXT,
    error_code TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    dispatch_started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT owner_notification_delivery_attempts_tenant_fk
        FOREIGN KEY (salon_id, owner_notification_id)
        REFERENCES owner_notifications(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT owner_notification_delivery_attempts_number_unique
        UNIQUE (owner_notification_id, attempt_number),
    CONSTRAINT owner_notification_delivery_attempts_finish_shape_check
        CHECK ((outcome = 'leased') = (finished_at IS NULL)),
    CONSTRAINT owner_notification_delivery_attempts_dispatch_shape_check
        CHECK (dispatch_started_at IS NULL OR dispatch_started_at >= started_at)
);

CREATE INDEX idx_owner_notification_delivery_attempts_notification
    ON owner_notification_delivery_attempts(owner_notification_id, attempt_number DESC);

CREATE TABLE owner_notification_delivery_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    owner_notification_id UUID NOT NULL,
    event_key TEXT NOT NULL CHECK (event_key = btrim(event_key) AND length(event_key) BETWEEN 1 AND 256),
    event_fingerprint TEXT NOT NULL CHECK (event_fingerprint ~ '^[0-9a-f]{64}$'),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'claimed', 'safe_retry_scheduled', 'dispatch_started', 'provider_response',
        'status_callback', 'dead_lettered', 'delivery_disabled', 'owner_requeued'
    )),
    delivery_status TEXT NOT NULL,
    provider_status TEXT,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT owner_notification_delivery_events_tenant_fk
        FOREIGN KEY (salon_id, owner_notification_id)
        REFERENCES owner_notifications(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT owner_notification_delivery_events_action_unique
        UNIQUE (salon_id, event_key)
);

CREATE INDEX idx_owner_notification_delivery_events_notification
    ON owner_notification_delivery_events(owner_notification_id, created_at ASC, id ASC);

CREATE TABLE owner_notification_delivery_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    owner_notification_id UUID NOT NULL,
    action_key TEXT NOT NULL CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_fingerprint TEXT NOT NULL CHECK (action_fingerprint ~ '^[0-9a-f]{64}$'),
    action_type TEXT NOT NULL CHECK (action_type = 'requeue'),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    result_delivery_status TEXT NOT NULL CHECK (result_delivery_status = 'queued'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT owner_notification_delivery_actions_tenant_fk
        FOREIGN KEY (salon_id, owner_notification_id)
        REFERENCES owner_notifications(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT owner_notification_delivery_actions_key_unique
        UNIQUE (salon_id, action_key)
);

CREATE INDEX idx_owner_notification_delivery_actions_notification
    ON owner_notification_delivery_actions(owner_notification_id, created_at DESC);
