-- Tenant-bound OpenAI runtime identity and durable live-verification evidence.
--
-- Active credentials remain encrypted in salon_integration_configs. The HMAC
-- value below is a non-reversible uniqueness identity derived by application
-- code with a purpose-separated key. It is never returned by an API or copied
-- into an audit/event row.

ALTER TABLE public.salon_integration_configs
    ADD COLUMN credential_fingerprint_hmac TEXT,
    ADD COLUMN credential_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN destination_profile TEXT;

ALTER TABLE public.salon_integration_configs
    ADD CONSTRAINT salon_integration_configs_openai_identity_check CHECK (
        CASE WHEN provider = 'openai' THEN
            credential_revision >= 0
            AND (credential_fingerprint_hmac IS NULL OR credential_fingerprint_hmac ~ '^[0-9a-f]{64}$')
            AND (destination_profile IS NULL OR destination_profile = 'openai_public')
        ELSE
            credential_fingerprint_hmac IS NULL
            AND credential_revision = 0
            AND destination_profile IS NULL
        END
    );

CREATE UNIQUE INDEX idx_openai_unique_credential_identity
    ON public.salon_integration_configs (credential_fingerprint_hmac)
    WHERE provider = 'openai' AND credential_fingerprint_hmac IS NOT NULL;

-- Configuration Transfer v10 excludes OpenAI operational identity and live
-- evidence. Extend the existing reviewed-run contract without rewriting data;
-- v9 and v8 history remain valid inputs and immutable audit values.
ALTER TABLE public.configuration_transfer_runs
    DROP CONSTRAINT configuration_transfer_runs_schema_version_check;
ALTER TABLE public.configuration_transfer_runs
    ADD CONSTRAINT configuration_transfer_runs_schema_version_check CHECK (
        schema_version IN (
            'manleai.salon_configuration.v8',
            'manleai.salon_configuration.v9',
            'manleai.salon_configuration.v10'
        )
    );

ALTER TABLE public.salon_integration_configs
    ADD CONSTRAINT salon_integration_configs_id_salon_unique UNIQUE (id, salon_id);

CREATE TABLE public.openai_runtime_verification_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES public.salons(id) ON DELETE RESTRICT,
    integration_config_id UUID NOT NULL,
    actor_user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    action_key TEXT NOT NULL CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    credential_revision BIGINT NOT NULL CHECK (credential_revision > 0),
    destination_policy_version TEXT NOT NULL CHECK (destination_policy_version = 'openai-public-v1'),
    verification_contract_version TEXT NOT NULL CHECK (verification_contract_version = 'openai-voice-v1'),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'claimed', 'succeeded', 'failed', 'stale')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 3),
    claim_token UUID,
    lease_expires_at TIMESTAMPTZ,
    error_code TEXT CHECK (error_code IS NULL OR error_code ~ '^[A-Za-z0-9._:-]{1,128}$'),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, actor_user_id, action_key),
    UNIQUE (id, salon_id),
    CONSTRAINT openai_verification_config_tenant_fk
        FOREIGN KEY (integration_config_id, salon_id)
        REFERENCES public.salon_integration_configs(id, salon_id)
        ON DELETE RESTRICT,
    CONSTRAINT openai_verification_claim_shape_check CHECK (
        (status = 'claimed' AND claim_token IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (status <> 'claimed' AND claim_token IS NULL AND lease_expires_at IS NULL)
    )
);

CREATE INDEX idx_openai_verification_runs_salon_created
    ON public.openai_runtime_verification_runs (salon_id, created_at DESC, id DESC);

CREATE INDEX idx_openai_verification_runs_claimable
    ON public.openai_runtime_verification_runs (status, lease_expires_at, created_at, id)
    WHERE status IN ('queued', 'claimed');

CREATE TABLE public.openai_runtime_verification_capabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES public.salons(id) ON DELETE RESTRICT,
    run_id UUID NOT NULL,
    capability TEXT NOT NULL CHECK (capability IN (
        'transcription', 'semantic_full', 'semantic_guidance', 'reply',
        'speech', 'speech_stream', 'realtime'
    )),
    required BOOLEAN NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'verified', 'failed', 'stale', 'not_required')),
    latency_ms BIGINT CHECK (latency_ms IS NULL OR latency_ms BETWEEN 0 AND 3600000),
    provider_request_id TEXT CHECK (provider_request_id IS NULL OR length(provider_request_id) BETWEEN 1 AND 128),
    error_code TEXT CHECK (error_code IS NULL OR error_code ~ '^[A-Za-z0-9._:-]{1,128}$'),
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, capability),
    CONSTRAINT openai_verification_capability_run_tenant_fk
        FOREIGN KEY (run_id, salon_id)
        REFERENCES public.openai_runtime_verification_runs(id, salon_id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_openai_verification_capabilities_salon_run
    ON public.openai_runtime_verification_capabilities (salon_id, run_id, capability);

CREATE TABLE public.openai_runtime_verification_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES public.salons(id) ON DELETE RESTRICT,
    run_id UUID NOT NULL,
    event_key TEXT NOT NULL CHECK (event_key = btrim(event_key) AND length(event_key) BETWEEN 1 AND 256),
    event_fingerprint TEXT NOT NULL CHECK (event_fingerprint ~ '^[0-9a-f]{64}$'),
    event_type TEXT NOT NULL CHECK (event_type IN ('queued', 'claimed', 'capability_completed', 'succeeded', 'failed', 'stale')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'claimed', 'succeeded', 'failed', 'stale')),
    capability TEXT CHECK (capability IS NULL OR capability IN (
        'transcription', 'semantic_full', 'semantic_guidance', 'reply',
        'speech', 'speech_stream', 'realtime'
    )),
    error_code TEXT CHECK (error_code IS NULL OR error_code ~ '^[A-Za-z0-9._:-]{1,128}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, event_key),
    CONSTRAINT openai_verification_event_run_tenant_fk
        FOREIGN KEY (run_id, salon_id)
        REFERENCES public.openai_runtime_verification_runs(id, salon_id)
        ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION public.reject_openai_verification_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER openai_runtime_verification_events_immutable
BEFORE UPDATE OR DELETE ON public.openai_runtime_verification_events
FOR EACH ROW EXECUTE FUNCTION public.reject_openai_verification_event_mutation();

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
        WHERE ranked.tenant_rank <= ranked.tenant_limit
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
    RETURNING run.id, run.salon_id, run.claim_token
$$;

REVOKE ALL ON FUNCTION public.app_worker_claim_openai_runtime_verifications(INTEGER, BIGINT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_worker_claim_openai_runtime_verifications(INTEGER, BIGINT) TO PUBLIC;

DO $$
DECLARE target_table TEXT;
BEGIN
    FOREACH target_table IN ARRAY ARRAY[
        'openai_runtime_verification_runs',
        'openai_runtime_verification_capabilities',
        'openai_runtime_verification_events'
    ] LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target_table);
        EXECUTE format('ALTER TABLE public.%I FORCE ROW LEVEL SECURITY', target_table);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN false ELSE public.app_rls_feature_access(salon_id, ''technical.read'', NULL) END)',
            'ov_' || target_table || '_select', target_table
        );
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN false ELSE public.app_rls_feature_access(salon_id, ''technical.write'', NULL) END)',
            'ov_' || target_table || '_insert', target_table
        );
    END LOOP;
END
$$;

CREATE POLICY openai_verification_runs_update
    ON public.openai_runtime_verification_runs FOR UPDATE
    USING (public.app_database_scope() = 'worker' AND public.app_rls_system_salon_allowed(salon_id))
    WITH CHECK (public.app_database_scope() = 'worker' AND public.app_rls_system_salon_allowed(salon_id));

CREATE POLICY openai_verification_capabilities_update
    ON public.openai_runtime_verification_capabilities FOR UPDATE
    USING (public.app_database_scope() = 'worker' AND public.app_rls_system_salon_allowed(salon_id))
    WITH CHECK (public.app_database_scope() = 'worker' AND public.app_rls_system_salon_allowed(salon_id));

COMMENT ON FUNCTION public.app_worker_claim_openai_runtime_verifications(INTEGER, BIGINT) IS
    'Bounded worker-only OpenAI verification discovery. Returned rows must be processed under the exact returned system_salon_id.';
