-- SaaS Phase 9 noisy-neighbor controls and tenant usage evidence.

CREATE TABLE tenant_runtime_limits (
    salon_id UUID PRIMARY KEY REFERENCES salons(id) ON DELETE CASCADE,
    expensive_requests_per_minute INTEGER NOT NULL DEFAULT 60
        CHECK (expensive_requests_per_minute BETWEEN 1 AND 6000),
    scheduling_writes_per_minute INTEGER NOT NULL DEFAULT 120
        CHECK (scheduling_writes_per_minute BETWEEN 1 AND 6000),
    provider_writes_per_minute INTEGER NOT NULL DEFAULT 30
        CHECK (provider_writes_per_minute BETWEEN 1 AND 6000),
    voice_starts_per_minute INTEGER NOT NULL DEFAULT 30
        CHECK (voice_starts_per_minute BETWEEN 1 AND 6000),
    worker_claims_per_batch INTEGER NOT NULL DEFAULT 2
        CHECK (worker_claims_per_batch BETWEEN 1 AND 50),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_runtime_limit_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action_key TEXT NOT NULL
        CHECK (action_key=btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    result_snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, action_key)
);

CREATE TABLE tenant_runtime_limit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE RESTRICT,
    action_id UUID NOT NULL REFERENCES tenant_runtime_limit_actions(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    previous_version BIGINT NOT NULL CHECK (previous_version >= 1),
    result_version BIGINT NOT NULL CHECK (result_version = previous_version + 1),
    changed_fields TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_usage_minute_buckets (
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    bucket_start TIMESTAMPTZ NOT NULL,
    metric TEXT NOT NULL CHECK (metric IN (
        'expensive_request',
        'scheduling_write',
        'provider_write',
        'voice_start'
    )),
    used_count BIGINT NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    rejected_count BIGINT NOT NULL DEFAULT 0 CHECK (rejected_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (salon_id, bucket_start, metric),
    CHECK (date_trunc('minute', bucket_start) = bucket_start)
);

CREATE INDEX idx_tenant_usage_minute_buckets_recent
    ON tenant_usage_minute_buckets (salon_id, bucket_start DESC, metric);

INSERT INTO tenant_runtime_limits (salon_id)
SELECT id FROM salons
ON CONFLICT (salon_id) DO NOTHING;

CREATE OR REPLACE FUNCTION phase9_create_tenant_runtime_limits()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO tenant_runtime_limits (salon_id)
    VALUES (NEW.id)
    ON CONFLICT (salon_id) DO NOTHING;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS salons_create_tenant_runtime_limits ON salons;
CREATE TRIGGER salons_create_tenant_runtime_limits
AFTER INSERT ON salons
FOR EACH ROW EXECUTE FUNCTION phase9_create_tenant_runtime_limits();

CREATE OR REPLACE FUNCTION consume_tenant_runtime_quota(
    target_salon_id UUID,
    requested_metric TEXT,
    requested_units INTEGER DEFAULT 1
)
RETURNS TABLE (
    allowed BOOLEAN,
    quota_limit INTEGER,
    used_count BIGINT,
    remaining_count BIGINT,
    rejected_count BIGINT,
    reset_at TIMESTAMPTZ
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    current_limit INTEGER;
    current_used BIGINT;
    current_rejected BIGINT;
    current_bucket TIMESTAMPTZ := date_trunc('minute', clock_timestamp());
BEGIN
    IF target_salon_id IS NULL OR requested_units < 1 OR requested_units > 1000 THEN
        RAISE EXCEPTION 'invalid tenant quota request';
    END IF;

    SELECT CASE requested_metric
        WHEN 'expensive_request' THEN limits.expensive_requests_per_minute
        WHEN 'scheduling_write' THEN limits.scheduling_writes_per_minute
        WHEN 'provider_write' THEN limits.provider_writes_per_minute
        WHEN 'voice_start' THEN limits.voice_starts_per_minute
        ELSE NULL
    END
    INTO current_limit
    FROM tenant_runtime_limits limits
    WHERE limits.salon_id=target_salon_id;
    IF current_limit IS NULL THEN
        RAISE EXCEPTION 'tenant runtime limits unavailable';
    END IF;

    INSERT INTO tenant_usage_minute_buckets (
        salon_id,bucket_start,metric,used_count,rejected_count
    ) VALUES (
        target_salon_id,current_bucket,requested_metric,0,0
    ) ON CONFLICT (salon_id,bucket_start,metric) DO NOTHING;

    SELECT bucket.used_count,bucket.rejected_count
    INTO current_used,current_rejected
    FROM tenant_usage_minute_buckets bucket
    WHERE bucket.salon_id=target_salon_id
      AND bucket.bucket_start=current_bucket
      AND bucket.metric=requested_metric
    FOR UPDATE;

    IF current_used + requested_units <= current_limit THEN
        current_used := current_used + requested_units;
        UPDATE tenant_usage_minute_buckets bucket
        SET used_count=current_used,updated_at=now()
        WHERE bucket.salon_id=target_salon_id
          AND bucket.bucket_start=current_bucket
          AND bucket.metric=requested_metric;
        allowed := true;
    ELSE
        current_rejected := current_rejected + requested_units;
        UPDATE tenant_usage_minute_buckets bucket
        SET rejected_count=current_rejected,updated_at=now()
        WHERE bucket.salon_id=target_salon_id
          AND bucket.bucket_start=current_bucket
          AND bucket.metric=requested_metric;
        allowed := false;
    END IF;

    quota_limit := current_limit;
    used_count := current_used;
    remaining_count := GREATEST(current_limit::BIGINT-current_used,0);
    rejected_count := current_rejected;
    reset_at := current_bucket + interval '1 minute';
    RETURN NEXT;
END
$$;

REVOKE ALL ON FUNCTION consume_tenant_runtime_quota(UUID, TEXT, INTEGER) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION consume_tenant_runtime_quota(UUID, TEXT, INTEGER) TO PUBLIC;

CREATE OR REPLACE FUNCTION phase9_reject_runtime_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

DROP TRIGGER IF EXISTS tenant_runtime_limit_actions_immutable ON tenant_runtime_limit_actions;
CREATE TRIGGER tenant_runtime_limit_actions_immutable
BEFORE UPDATE OR DELETE ON tenant_runtime_limit_actions
FOR EACH ROW EXECUTE FUNCTION phase9_reject_runtime_ledger_mutation();

DROP TRIGGER IF EXISTS tenant_runtime_limit_events_immutable ON tenant_runtime_limit_events;
CREATE TRIGGER tenant_runtime_limit_events_immutable
BEFORE UPDATE OR DELETE ON tenant_runtime_limit_events
FOR EACH ROW EXECUTE FUNCTION phase9_reject_runtime_ledger_mutation();

DO $$
DECLARE
    table_name TEXT;
    prefix TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'tenant_runtime_limits',
        'tenant_runtime_limit_actions',
        'tenant_runtime_limit_events',
        'tenant_usage_minute_buckets'
    ] LOOP
        prefix := 'saas_rls_' || table_name;
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('CREATE POLICY %I ON public.%I FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id,NULL,false))', prefix || '_select', table_name);
        EXECUTE format('CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id,NULL))', prefix || '_insert', table_name);
        EXECUTE format('CREATE POLICY %I ON public.%I FOR UPDATE USING (public.app_rls_salon_write_allowed(salon_id,NULL)) WITH CHECK (public.app_rls_salon_write_allowed(salon_id,NULL))', prefix || '_update', table_name);
        EXECUTE format('CREATE POLICY %I ON public.%I FOR DELETE USING (public.app_rls_salon_write_allowed(salon_id,NULL))', prefix || '_delete', table_name);
    END LOOP;
END
$$;

COMMENT ON TABLE tenant_runtime_limits IS
'Per-salon noisy-neighbor limits. Platform Operations owns changes; runtime consumption is atomic in consume_tenant_runtime_quota.';
COMMENT ON TABLE tenant_usage_minute_buckets IS
'Bounded per-minute tenant usage and rejection evidence. It contains counts only, never request, provider, or customer payloads.';
