-- Expand-only tenant-bound Twilio Voice routing contract.
-- The integration-config UUID is routing identity, not an authentication secret.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'salon_integration_configs_twilio_voice_number_e164_check'
          AND conrelid = 'public.salon_integration_configs'::regclass
    ) THEN
        ALTER TABLE public.salon_integration_configs
            ADD CONSTRAINT salon_integration_configs_twilio_voice_number_e164_check
            CHECK (
                provider <> 'twilio'
                OR COALESCE(settings->>'voice_inbound_number', '') = ''
                OR settings->>'voice_inbound_number' ~ '^\+[1-9][0-9]{7,14}$'
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'salon_integration_configs_twilio_voice_enabled_check'
          AND conrelid = 'public.salon_integration_configs'::regclass
    ) THEN
        ALTER TABLE public.salon_integration_configs
            ADD CONSTRAINT salon_integration_configs_twilio_voice_enabled_check
            CHECK (
                provider <> 'twilio'
                OR COALESCE(settings->>'voice_routing_enabled', 'false') IN ('true', 'false')
            );
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_twilio_voice_active_inbound_number
    ON public.salon_integration_configs ((settings->>'voice_inbound_number'))
    WHERE provider = 'twilio'
      AND enabled = true
      AND settings->>'voice_routing_enabled' = 'true'
      AND COALESCE(settings->>'voice_inbound_number', '') <> '';

CREATE OR REPLACE FUNCTION public.app_provider_twilio_voice_route_salon(target_route_id UUID)
RETURNS UUID
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT integration.salon_id
    FROM public.salon_integration_configs integration
    WHERE public.app_database_scope() = 'provider'
      AND public.app_request_system_salon_id() IS NULL
      AND integration.id = target_route_id
      AND integration.provider = 'twilio'
      AND integration.enabled = true
      AND integration.settings->>'voice_routing_enabled' = 'true'
      AND integration.settings->>'voice_inbound_number' ~ '^\+[1-9][0-9]{7,14}$'
    LIMIT 1
$$;

REVOKE ALL ON FUNCTION public.app_provider_twilio_voice_route_salon(UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_twilio_voice_route_salon(UUID) TO PUBLIC;

COMMENT ON FUNCTION public.app_provider_twilio_voice_route_salon(UUID) IS
'Provider-only locator returning only the salon UUID for one enabled tenant-bound Twilio Voice integration route.';

-- Shared-route rollback compatibility still needs provider-wide CallSid
-- discovery, but only before a tenant has been selected. Tenant-bound paths
-- query the exact bound salon directly and cannot use this locator to rebind.
CREATE OR REPLACE FUNCTION public.app_provider_voice_route_salon(
    target_provider TEXT,
    target_provider_call_id TEXT
)
RETURNS UUID
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT session.salon_id
    FROM public.call_sessions session
    WHERE public.app_database_scope() = 'provider'
      AND public.app_request_system_salon_id() IS NULL
      AND NULLIF(BTRIM(target_provider), '') IS NOT NULL
      AND NULLIF(BTRIM(target_provider_call_id), '') IS NOT NULL
      AND session.provider = BTRIM(target_provider)
      AND session.provider_call_id = BTRIM(target_provider_call_id)
    ORDER BY session.id
    LIMIT 1
$$;

REVOKE ALL ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) TO PUBLIC;

COMMENT ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) IS
'Provider-only legacy CallSid locator available only before a provider request is tenant-bound.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_voice_webhook_events_twilio_route_verified
    ON public.voice_webhook_events (provider, provider_call_id, event_type)
    WHERE provider = 'twilio'
      AND provider_call_id IS NOT NULL
      AND event_type = 'twilio_inbound_route_verified';

CREATE INDEX IF NOT EXISTS idx_voice_webhook_events_twilio_route_fingerprint
    ON public.voice_webhook_events (
        salon_id,
        (payload->>'routing_fingerprint'),
        created_at DESC
    )
    WHERE provider = 'twilio'
      AND event_type = 'twilio_inbound_route_verified';
