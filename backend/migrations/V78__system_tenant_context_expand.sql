-- Expand phase for provider/worker tenant binding.
--
-- This migration is deliberately forward-compatible with the pre-V78
-- application image: existing provider/worker RLS behavior is not tightened
-- here. The application can begin carrying app.system_salon_id and can use the
-- narrow locator functions before a later release changes the base policies.

CREATE OR REPLACE FUNCTION public.app_request_system_salon_id()
RETURNS UUID
LANGUAGE plpgsql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    raw_salon TEXT;
BEGIN
    raw_salon := NULLIF(current_setting('app.system_salon_id', true), '');
    IF raw_salon IS NULL THEN
        RETURN NULL;
    END IF;
    RETURN raw_salon::UUID;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END
$$;

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
      AND NULLIF(BTRIM(target_provider), '') IS NOT NULL
      AND NULLIF(BTRIM(target_provider_call_id), '') IS NOT NULL
      AND session.provider = BTRIM(target_provider)
      AND session.provider_call_id = BTRIM(target_provider_call_id)
    ORDER BY session.id
    LIMIT 1
$$;

CREATE OR REPLACE FUNCTION public.app_provider_voice_phone_salon(target_phone TEXT)
RETURNS UUID
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT salon.id
    FROM public.salons salon
    WHERE public.app_database_scope() = 'provider'
      AND NULLIF(BTRIM(target_phone), '') IS NOT NULL
      AND regexp_replace(COALESCE(salon.phone, ''), '[^0-9]', '', 'g') =
          regexp_replace(target_phone, '[^0-9]', '', 'g')
      AND COALESCE(salon.phone, '') <> ''
    ORDER BY salon.created_at, salon.id
    LIMIT 1
$$;

CREATE OR REPLACE FUNCTION public.app_provider_owner_message_salon(
    target_provider TEXT,
    target_provider_message_id TEXT
)
RETURNS UUID
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT notification.salon_id
    FROM public.owner_notifications notification
    WHERE public.app_database_scope() = 'provider'
      AND NULLIF(BTRIM(target_provider), '') IS NOT NULL
      AND NULLIF(BTRIM(target_provider_message_id), '') IS NOT NULL
      AND notification.delivery_provider = BTRIM(target_provider)
      AND notification.provider_message_id = BTRIM(target_provider_message_id)
    ORDER BY notification.id
    LIMIT 1
$$;

CREATE OR REPLACE FUNCTION public.app_provider_customer_message_salon(
    target_provider TEXT,
    target_provider_message_id TEXT
)
RETURNS UUID
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT delivery.salon_id
    FROM public.customer_notification_deliveries delivery
    WHERE public.app_database_scope() = 'provider'
      AND NULLIF(BTRIM(target_provider), '') IS NOT NULL
      AND NULLIF(BTRIM(target_provider_message_id), '') IS NOT NULL
      AND delivery.delivery_provider = BTRIM(target_provider)
      AND delivery.provider_message_id = BTRIM(target_provider_message_id)
    ORDER BY delivery.id
    LIMIT 1
$$;

CREATE OR REPLACE FUNCTION public.app_provider_square_webhook_targets(
    target_provider TEXT,
    target_merchant_id TEXT,
    target_location_id TEXT,
    target_statuses TEXT[]
)
RETURNS TABLE (salon_id UUID)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT connection.salon_id
    FROM public.pos_connections connection
    JOIN public.salons salon ON salon.id = connection.salon_id
    WHERE public.app_database_scope() = 'provider'
      AND connection.provider = BTRIM(target_provider)
      AND connection.merchant_id = BTRIM(target_merchant_id)
      AND connection.location_id = BTRIM(target_location_id)
      AND salon.active_pos_provider = BTRIM(target_provider)
      AND connection.status = ANY(target_statuses)
    ORDER BY connection.salon_id
    LIMIT 2
$$;

REVOKE ALL ON FUNCTION public.app_request_system_salon_id() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_provider_voice_phone_salon(TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_provider_owner_message_salon(TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_provider_customer_message_salon(TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_provider_square_webhook_targets(TEXT, TEXT, TEXT, TEXT[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_request_system_salon_id() TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_voice_phone_salon(TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_owner_message_salon(TEXT, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_customer_message_salon(TEXT, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_provider_square_webhook_targets(TEXT, TEXT, TEXT, TEXT[]) TO PUBLIC;

COMMENT ON FUNCTION public.app_request_system_salon_id() IS
'Returns the server-bound provider/worker salon UUID for the current database operation.';
COMMENT ON FUNCTION public.app_provider_voice_route_salon(TEXT, TEXT) IS
'Provider-only locator returning only the salon UUID for an existing voice call route.';
COMMENT ON FUNCTION public.app_provider_voice_phone_salon(TEXT) IS
'Provider-only locator returning only the salon UUID for the inbound salon phone route.';
COMMENT ON FUNCTION public.app_provider_owner_message_salon(TEXT, TEXT) IS
'Provider-only locator returning only the salon UUID for an owner-notification callback.';
COMMENT ON FUNCTION public.app_provider_customer_message_salon(TEXT, TEXT) IS
'Provider-only locator returning only the salon UUID for a customer-notification callback.';
COMMENT ON FUNCTION public.app_provider_square_webhook_targets(TEXT, TEXT, TEXT, TEXT[]) IS
'Provider-only locator returning at most two salon UUIDs so ambiguous Square targets fail closed.';
