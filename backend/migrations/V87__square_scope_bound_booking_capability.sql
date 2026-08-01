-- Scope-bound Square scheduling capability evidence.
--
-- This migration does not widen Square scheduling behavior by itself. It
-- records immutable reviewed evidence for the exact connection/config/location
-- fence. Only buyer-level APPOINTMENTS_WRITE without APPOINTMENTS_ALL_WRITE may
-- prove single-create safety; reschedule, party, and resource capacity remain
-- fail closed.

CREATE FUNCTION square_oauth_scope_fingerprint(input_scopes TEXT[])
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT encode(
        digest(
            COALESCE((
                SELECT string_agg(scope_value, ' ' ORDER BY scope_value)
                FROM (
                    SELECT DISTINCT upper(btrim(raw_scope)) AS scope_value
                    FROM unnest(COALESCE(input_scopes, ARRAY[]::TEXT[])) raw_scope
                    WHERE btrim(raw_scope) <> ''
                ) normalized
            ), ''),
            'sha256'
        ),
        'hex'
    )
$$;

ALTER TABLE pos_connections
    ADD COLUMN booking_write_capability_version BIGINT NOT NULL DEFAULT 1
        CHECK (booking_write_capability_version > 0);

ALTER TABLE pos_connections
    ADD CONSTRAINT pos_connections_salon_id_id_key UNIQUE (salon_id, id);

CREATE FUNCTION fence_pos_connection_booking_write_capability()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.scopes IS DISTINCT FROM NEW.scopes
       OR OLD.access_token_encrypted IS DISTINCT FROM NEW.access_token_encrypted
       OR OLD.refresh_token_encrypted IS DISTINCT FROM NEW.refresh_token_encrypted
       OR OLD.merchant_id IS DISTINCT FROM NEW.merchant_id
       OR OLD.location_id IS DISTINCT FROM NEW.location_id
       OR OLD.status IS DISTINCT FROM NEW.status THEN
        NEW.booking_write_capability_version := OLD.booking_write_capability_version + 1;
    ELSE
        NEW.booking_write_capability_version := OLD.booking_write_capability_version;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER pos_connections_booking_write_capability_fence
BEFORE UPDATE ON pos_connections
FOR EACH ROW EXECUTE FUNCTION fence_pos_connection_booking_write_capability();

DO $$
DECLARE target_constraint TEXT;
BEGIN
    SELECT constraint_row.conname INTO target_constraint
    FROM pg_constraint constraint_row
    WHERE constraint_row.conrelid = 'external_provider_scheduling_capability_evidence'::regclass
      AND constraint_row.contype = 'u'
      AND pg_get_constraintdef(constraint_row.oid) = 'UNIQUE (salon_id, provider, provider_location_id, config_version)';
    IF target_constraint IS NULL THEN
        RAISE EXCEPTION 'V86 external capability unique fence is missing';
    END IF;
    EXECUTE format(
        'ALTER TABLE external_provider_scheduling_capability_evidence DROP CONSTRAINT %I',
        target_constraint
    );

    SELECT constraint_row.conname INTO target_constraint
    FROM pg_constraint constraint_row
    WHERE constraint_row.conrelid = 'external_provider_scheduling_capability_evidence'::regclass
      AND constraint_row.contype = 'c'
      AND pg_get_constraintdef(constraint_row.oid) LIKE '%verification_contract_version%external-slot-commit-v1%';
    IF target_constraint IS NULL THEN
        RAISE EXCEPTION 'V86 external capability verification contract guard is missing';
    END IF;
    EXECUTE format(
        'ALTER TABLE external_provider_scheduling_capability_evidence DROP CONSTRAINT %I',
        target_constraint
    );
END
$$;

ALTER TABLE external_provider_scheduling_capability_evidence
    ADD COLUMN connection_id UUID,
    ADD COLUMN connection_capability_version BIGINT CHECK (connection_capability_version > 0),
    ADD COLUMN provider_api_version TEXT CHECK (provider_api_version IS NULL OR length(btrim(provider_api_version)) BETWEEN 1 AND 100),
    ADD COLUMN oauth_scope_fingerprint TEXT CHECK (oauth_scope_fingerprint IS NULL OR oauth_scope_fingerprint ~ '^[0-9a-f]{64}$'),
    ADD COLUMN write_permission_mode TEXT CHECK (write_permission_mode IS NULL OR write_permission_mode IN ('buyer_write', 'seller_write', 'unsupported')),
    ADD COLUMN reviewer_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN action_key TEXT CHECK (action_key IS NULL OR length(btrim(action_key)) BETWEEN 1 AND 256),
    ADD COLUMN blocker_code TEXT CHECK (blocker_code IS NULL OR blocker_code ~ '^[A-Z0-9_]{1,100}$'),
    ADD COLUMN reconnect_required BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT external_provider_capability_verification_contract_check
        CHECK (verification_contract_version IN ('external-slot-commit-v1', 'square-buyer-single-create-v1')),
    ADD CONSTRAINT external_provider_capability_connection_tenant_fk
        FOREIGN KEY (salon_id, connection_id)
        REFERENCES pos_connections(salon_id, id)
        ON DELETE RESTRICT;

CREATE UNIQUE INDEX external_provider_capability_review_unique
    ON external_provider_scheduling_capability_evidence (salon_id, action_key)
    WHERE action_key IS NOT NULL;

CREATE INDEX idx_external_provider_capability_exact_fence
    ON external_provider_scheduling_capability_evidence (
        salon_id, provider, connection_id, connection_capability_version,
        integration_config_id, config_version, provider_location_id,
        provider_api_version, expires_at DESC
    );

CREATE TABLE external_provider_scheduling_capability_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    action_key TEXT NOT NULL CHECK (length(btrim(action_key)) BETWEEN 1 AND 256),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    evidence_id UUID NOT NULL,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    response JSONB NOT NULL CHECK (jsonb_typeof(response) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_provider_capability_actions_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT external_provider_capability_actions_action_key_unique UNIQUE (salon_id, action_key),
    CONSTRAINT external_provider_capability_actions_evidence_tenant_fk
        FOREIGN KEY (salon_id, evidence_id)
        REFERENCES external_provider_scheduling_capability_evidence(salon_id, id)
        ON DELETE RESTRICT
);

CREATE FUNCTION prevent_external_provider_capability_history_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- Preserve tenant deletion through the existing ON DELETE CASCADE graph,
    -- while rejecting direct history deletion and every update.
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        CONSTRAINT = 'external_provider_capability_history_immutable_guard',
        MESSAGE = 'external provider capability history is immutable';
END
$$;

CREATE TRIGGER external_provider_capability_evidence_immutable_guard
BEFORE UPDATE OR DELETE ON external_provider_scheduling_capability_evidence
FOR EACH ROW EXECUTE FUNCTION prevent_external_provider_capability_history_mutation();

CREATE TRIGGER external_provider_capability_actions_immutable_guard
BEFORE UPDATE OR DELETE ON external_provider_scheduling_capability_actions
FOR EACH ROW EXECUTE FUNCTION prevent_external_provider_capability_history_mutation();

ALTER TABLE external_provider_scheduling_capability_actions ENABLE ROW LEVEL SECURITY;

CREATE POLICY external_provider_capability_actions_select
    ON external_provider_scheduling_capability_actions FOR SELECT
    USING (public.app_rls_feature_access(salon_id, 'technical.read', NULL));

CREATE POLICY external_provider_capability_actions_insert
    ON external_provider_scheduling_capability_actions FOR INSERT
    WITH CHECK (public.app_rls_feature_access(salon_id, 'technical.write', NULL));

COMMENT ON COLUMN pos_connections.booking_write_capability_version IS
    'Monotonic fence advanced when Square credential identity, scopes, merchant, location, or booking-relevant connection state changes.';
COMMENT ON TABLE external_provider_scheduling_capability_actions IS
    'Immutable Platform action replay ledger for scope-bound scheduling capability evaluation; contains no token, customer, transcript, or provider payload.';
