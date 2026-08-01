-- External-provider Atomic Slot Commit.
--
-- These rows are an outbound concurrency fence for ManleAI-originated writes.
-- They are not an external calendar mirror and never manufacture provider
-- availability or provider confirmation evidence.

ALTER TABLE salon_integration_configs
    ADD CONSTRAINT salon_integration_configs_salon_id_id_key UNIQUE (salon_id, id);

ALTER TABLE booking_attempts
    ADD COLUMN external_slot_claim_required BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE external_provider_scheduling_capability_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    integration_config_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (length(trim(provider)) BETWEEN 1 AND 100),
    provider_location_id TEXT NOT NULL CHECK (length(trim(provider_location_id)) BETWEEN 1 AND 255),
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    verification_contract_version TEXT NOT NULL
        CHECK (verification_contract_version = 'external-slot-commit-v1'),
    verification_source TEXT NOT NULL
        CHECK (verification_source IN ('provider_contract', 'provider_live_verification')),
    atomic_create_no_overlap BOOLEAN NOT NULL DEFAULT false,
    atomic_reschedule_no_overlap BOOLEAN NOT NULL DEFAULT false,
    concrete_staff_assignment BOOLEAN NOT NULL DEFAULT false,
    resource_capacity_enforced BOOLEAN NOT NULL DEFAULT false,
    atomic_party_create BOOLEAN NOT NULL DEFAULT false,
    verified_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_provider_capability_config_tenant_fk
        FOREIGN KEY (salon_id, integration_config_id)
        REFERENCES salon_integration_configs(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT external_provider_capability_time_check
        CHECK (expires_at > verified_at),
    CONSTRAINT external_provider_capability_evidence_object_check
        CHECK (jsonb_typeof(evidence) = 'object'),
    UNIQUE (salon_id, provider, provider_location_id, config_version)
);

CREATE INDEX idx_external_provider_capability_current
    ON external_provider_scheduling_capability_evidence (
        salon_id, provider, provider_location_id, expires_at DESC
    );

ALTER TABLE external_provider_scheduling_capability_evidence
    ADD CONSTRAINT external_provider_capability_salon_id_id_key UNIQUE (salon_id, id);

CREATE TABLE external_slot_claims (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    booking_attempt_id UUID NOT NULL,
    scheduling_authority TEXT NOT NULL DEFAULT 'external_provider'
        CHECK (scheduling_authority = 'external_provider'),
    provider TEXT NOT NULL CHECK (length(trim(provider)) BETWEEN 1 AND 100),
    provider_location_id TEXT NOT NULL CHECK (length(trim(provider_location_id)) BETWEEN 1 AND 255),
    operation_type TEXT NOT NULL CHECK (operation_type IN ('book', 'reschedule')),
    target_appointment_id UUID,
    expected_target_authority_version INTEGER,
    replaces_claim_id UUID,
    state TEXT NOT NULL CHECK (state IN (
        'claimed_pre_dispatch', 'dispatch_started', 'confirmed',
        'definite_failure', 'dispatched_unknown',
        'reconciliation_required', 'released'
    )),
    provider_capability_evidence_id UUID NOT NULL,
    provider_config_version BIGINT NOT NULL CHECK (provider_config_version > 0),
    processing_token TEXT,
    lease_expires_at TIMESTAMPTZ,
    dispatch_started_at TIMESTAMPTZ,
    provider_booking_id TEXT,
    provider_booking_version INTEGER,
    reconciliation_task_id UUID REFERENCES booking_reconciliation_tasks(id) ON DELETE SET NULL,
    released_at TIMESTAMPTZ,
    release_reason TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_slot_claims_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT external_slot_claims_attempt_unique UNIQUE (salon_id, booking_attempt_id),
    CONSTRAINT external_slot_claims_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_slot_claims_target_tenant_fk
        FOREIGN KEY (salon_id, target_appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT external_slot_claims_replaces_tenant_fk
        FOREIGN KEY (salon_id, replaces_claim_id)
        REFERENCES external_slot_claims(salon_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT external_slot_claims_capability_tenant_fk
        FOREIGN KEY (salon_id, provider_capability_evidence_id)
        REFERENCES external_provider_scheduling_capability_evidence(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_slot_claims_operation_target_check CHECK (
        (operation_type = 'book'
            AND target_appointment_id IS NULL
            AND expected_target_authority_version IS NULL
            AND replaces_claim_id IS NULL)
        OR
        (operation_type = 'reschedule'
            AND target_appointment_id IS NOT NULL
            AND expected_target_authority_version >= 1)
    ),
    CONSTRAINT external_slot_claims_lease_check CHECK (
        (state = 'claimed_pre_dispatch'
            AND processing_token IS NOT NULL
            AND lease_expires_at IS NOT NULL
            AND dispatch_started_at IS NULL)
        OR
        (state <> 'claimed_pre_dispatch')
    ),
    CONSTRAINT external_slot_claims_dispatch_check CHECK (
        state NOT IN ('dispatch_started', 'confirmed', 'dispatched_unknown', 'reconciliation_required')
        OR dispatch_started_at IS NOT NULL
    ),
    CONSTRAINT external_slot_claims_provider_result_check CHECK (
        (provider_booking_version IS NULL OR provider_booking_version >= 0)
        AND (state <> 'confirmed' OR (
            provider_booking_id IS NOT NULL
            AND length(trim(provider_booking_id)) > 0
            AND provider_booking_version IS NOT NULL
        ))
    ),
    CONSTRAINT external_slot_claims_release_check CHECK (
        (state IN ('definite_failure', 'released')
            AND released_at IS NOT NULL
            AND release_reason IS NOT NULL
            AND length(trim(release_reason)) BETWEEN 1 AND 100)
        OR
        (state NOT IN ('definite_failure', 'released')
            AND released_at IS NULL
            AND release_reason IS NULL)
    )
);

CREATE INDEX idx_external_slot_claims_active_state
    ON external_slot_claims (salon_id, state, created_at)
    WHERE released_at IS NULL;

CREATE INDEX idx_external_slot_claims_reconciliation
    ON external_slot_claims (salon_id, updated_at)
    WHERE state IN ('dispatched_unknown', 'reconciliation_required');

CREATE TABLE external_slot_claim_intervals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    claim_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (length(trim(provider)) BETWEEN 1 AND 100),
    provider_location_id TEXT NOT NULL CHECK (length(trim(provider_location_id)) BETWEEN 1 AND 255),
    resource_kind TEXT NOT NULL CHECK (resource_kind IN ('staff', 'exclusive_resource', 'capacity_unit')),
    resource_id TEXT NOT NULL CHECK (length(trim(resource_id)) BETWEEN 1 AND 255),
    source_segment_indexes INTEGER[] NOT NULL,
    occupied_start_time TIMESTAMPTZ NOT NULL,
    occupied_end_time TIMESTAMPTZ NOT NULL,
    resource_capacity_version BIGINT,
    activation_pending BOOLEAN NOT NULL DEFAULT false,
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_slot_claim_intervals_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT external_slot_claim_intervals_claim_tenant_fk
        FOREIGN KEY (salon_id, claim_id)
        REFERENCES external_slot_claims(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_slot_claim_intervals_range_check
        CHECK (occupied_end_time > occupied_start_time),
    CONSTRAINT external_slot_claim_intervals_indexes_check
        CHECK (cardinality(source_segment_indexes) BETWEEN 1 AND 100),
    CONSTRAINT external_slot_claim_intervals_capacity_version_check
        CHECK (resource_capacity_version IS NULL OR resource_capacity_version > 0)
);

ALTER TABLE external_slot_claim_intervals
    ADD CONSTRAINT external_slot_claim_intervals_no_overlap
    EXCLUDE USING gist (
        salon_id WITH =,
        provider WITH =,
        provider_location_id WITH =,
        resource_kind WITH =,
        resource_id WITH =,
        (tstzrange(occupied_start_time, occupied_end_time, '[)')) WITH &&
    )
    WHERE (released_at IS NULL AND activation_pending = false)
    DEFERRABLE INITIALLY IMMEDIATE;

CREATE INDEX idx_external_slot_claim_intervals_active_lookup
    ON external_slot_claim_intervals (
        salon_id, provider, provider_location_id, resource_kind, resource_id,
        occupied_start_time, occupied_end_time
    )
    WHERE released_at IS NULL AND activation_pending = false;

CREATE TABLE external_slot_claim_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    claim_id UUID NOT NULL,
    booking_attempt_id UUID NOT NULL,
    action_key TEXT NOT NULL CHECK (length(trim(action_key)) BETWEEN 1 AND 256),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'claim_acquired', 'claim_conflict', 'dispatch_started', 'provider_confirmed',
        'provider_definite_failure', 'provider_outcome_unknown',
        'reconciliation_required', 'reconciliation_confirmed',
        'reconciliation_not_created', 'claim_released',
        'cancel_confirmed', 'reschedule_replaced'
    )),
    from_state TEXT,
    to_state TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_slot_claim_events_claim_tenant_fk
        FOREIGN KEY (salon_id, claim_id)
        REFERENCES external_slot_claims(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_slot_claim_events_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_slot_claim_events_payload_check
        CHECK (jsonb_typeof(payload) = 'object'),
    UNIQUE (claim_id, action_key)
);

CREATE FUNCTION validate_external_slot_claim_release_graph()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE target_claim_id UUID;
DECLARE target_salon_id UUID;
DECLARE root_state TEXT;
DECLARE root_released_at TIMESTAMPTZ;
DECLARE active_interval_count INTEGER;
BEGIN
    IF TG_TABLE_NAME = 'external_slot_claims' THEN
        target_claim_id := NEW.id;
        target_salon_id := NEW.salon_id;
    ELSE
        target_claim_id := NEW.claim_id;
        target_salon_id := NEW.salon_id;
    END IF;

    SELECT state, released_at
      INTO root_state, root_released_at
    FROM external_slot_claims
    WHERE id = target_claim_id AND salon_id = target_salon_id;

    SELECT count(*) INTO active_interval_count
    FROM external_slot_claim_intervals
    WHERE claim_id = target_claim_id
      AND salon_id = target_salon_id
      AND released_at IS NULL;

    IF root_state IN ('definite_failure', 'released') THEN
        IF root_released_at IS NULL OR active_interval_count <> 0 THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                CONSTRAINT = 'external_slot_claim_release_graph_guard',
                MESSAGE = 'released external slot claim retains active intervals';
        END IF;
    ELSIF root_released_at IS NOT NULL OR active_interval_count = 0 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            CONSTRAINT = 'external_slot_claim_release_graph_guard',
            MESSAGE = 'active external slot claim must retain occupied intervals';
    END IF;
    RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER external_slot_claims_release_graph_guard
AFTER INSERT OR UPDATE ON external_slot_claims
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION validate_external_slot_claim_release_graph();

CREATE CONSTRAINT TRIGGER external_slot_claim_intervals_release_graph_guard
AFTER INSERT OR UPDATE ON external_slot_claim_intervals
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION validate_external_slot_claim_release_graph();

CREATE FUNCTION prevent_external_slot_claim_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        CONSTRAINT = 'external_slot_claim_events_immutable_guard',
        MESSAGE = 'external slot claim events are immutable';
END
$$;

CREATE TRIGGER external_slot_claim_events_immutable_guard
BEFORE UPDATE OR DELETE ON external_slot_claim_events
FOR EACH ROW EXECUTE FUNCTION prevent_external_slot_claim_event_mutation();

DO $$
DECLARE target_table TEXT;
DECLARE policy_prefix TEXT;
BEGIN
    FOREACH target_table IN ARRAY ARRAY[
        'external_slot_claims',
        'external_slot_claim_intervals',
        'external_slot_claim_events'
    ] LOOP
        policy_prefix := 'saas_rls_' || target_table;
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target_table);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id, ''appointments'', false))',
            policy_prefix || '_select', target_table
        );
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id, ''appointments''))',
            policy_prefix || '_insert', target_table
        );
        IF target_table <> 'external_slot_claim_events' THEN
            EXECUTE format(
                'CREATE POLICY %I ON public.%I FOR UPDATE USING (public.app_rls_salon_write_allowed(salon_id, ''appointments'')) WITH CHECK (public.app_rls_salon_write_allowed(salon_id, ''appointments''))',
                policy_prefix || '_update', target_table
            );
        END IF;
    END LOOP;
END
$$;

ALTER TABLE external_provider_scheduling_capability_evidence ENABLE ROW LEVEL SECURITY;
CREATE POLICY external_provider_capability_select
    ON external_provider_scheduling_capability_evidence FOR SELECT
    USING (
        public.app_rls_salon_write_allowed(salon_id, 'appointments')
        OR CASE public.app_database_scope()
            WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
            WHEN 'provider' THEN false
            ELSE public.app_rls_feature_access(salon_id, 'technical.read', NULL)
        END
    );
CREATE POLICY external_provider_capability_insert
    ON external_provider_scheduling_capability_evidence FOR INSERT
    WITH CHECK (
        CASE public.app_database_scope()
            WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
            WHEN 'provider' THEN false
            ELSE public.app_rls_feature_access(salon_id, 'technical.write', NULL)
        END
    );
CREATE POLICY external_provider_capability_update
    ON external_provider_scheduling_capability_evidence FOR UPDATE
    USING (
        CASE public.app_database_scope()
            WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
            WHEN 'provider' THEN false
            ELSE public.app_rls_feature_access(salon_id, 'technical.write', NULL)
        END
    )
    WITH CHECK (
        CASE public.app_database_scope()
            WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
            WHEN 'provider' THEN false
            ELSE public.app_rls_feature_access(salon_id, 'technical.write', NULL)
        END
    );

COMMENT ON TABLE external_slot_claims IS
    'Durable outbound concurrency fence for ManleAI-originated external-provider writes; not an external calendar source of truth.';
COMMENT ON TABLE external_provider_scheduling_capability_evidence IS
    'Version-fenced provider proof required before external confirmed-booking dispatch; absence or expiry fails closed.';
