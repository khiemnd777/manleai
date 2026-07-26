-- SaaS Phase 8 database defense-in-depth.
--
-- Application authorization remains mandatory. These policies add an
-- independent salon-row boundary for a non-owner, non-BYPASSRLS runtime role.
-- The contextual database connector sets app.actor_user_id for authenticated
-- requests and one allowlisted app.database_scope for public/provider/worker
-- execution. Migration/table owners intentionally remain outside the runtime
-- role so PostgreSQL does not bypass enabled RLS in production.

CREATE OR REPLACE FUNCTION public.app_request_actor_user_id()
RETURNS UUID
LANGUAGE plpgsql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    raw_actor TEXT;
BEGIN
    raw_actor := NULLIF(current_setting('app.actor_user_id', true), '');
    IF raw_actor IS NULL THEN
        RETURN NULL;
    END IF;
    RETURN raw_actor::UUID;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END
$$;

CREATE OR REPLACE FUNCTION public.app_database_scope()
RETURNS TEXT
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT CASE current_setting('app.database_scope', true)
        WHEN 'public' THEN 'public'
        WHEN 'provider' THEN 'provider'
        WHEN 'worker' THEN 'worker'
        ELSE ''
    END
$$;

CREATE OR REPLACE FUNCTION public.app_rls_public_salon_visible(target_salon_id UUID)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.salons salon
        WHERE salon.id = target_salon_id
          AND salon.public_catalog_enabled
          AND NULLIF(salon.public_slug, '') IS NOT NULL
    )
$$;

CREATE OR REPLACE FUNCTION public.app_rls_actor_salon_access(
    target_salon_id UUID,
    required_pii_scope TEXT DEFAULT NULL
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    actor_id UUID := public.app_request_actor_user_id();
    platform_allowed BOOLEAN := false;
BEGIN
    IF actor_id IS NULL OR target_salon_id IS NULL THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.salon_memberships membership
          ON membership.user_id = account.id
         AND membership.salon_id = target_salon_id
         AND membership.status = 'active'
        JOIN public.roles role
          ON role.id = membership.role_id
         AND role.scope = 'tenant'
         AND role.name IN ('tenant_owner', 'tenant_business_manager')
        WHERE account.id = actor_id
          AND account.status = 'active'
    ) THEN
        RETURN true;
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments assignment
          ON assignment.user_id = account.id
         AND assignment.status = 'active'
        JOIN public.roles role ON role.id = assignment.role_id
        WHERE account.id = actor_id
          AND account.status = 'active'
          AND (
              role.name = 'platform_admin'
              OR (
                  role.name = 'platform_ops'
                  AND EXISTS (
                      SELECT 1
                      FROM public.platform_salon_assignments salon_assignment
                      JOIN public.platform_salon_assignment_permissions assignment_permission
                        ON assignment_permission.assignment_id = salon_assignment.id
                      WHERE salon_assignment.user_id = actor_id
                        AND salon_assignment.salon_id = target_salon_id
                        AND salon_assignment.status = 'active'
                  )
              )
          )
    ) INTO platform_allowed;

    IF NOT platform_allowed THEN
        RETURN false;
    END IF;
    IF required_pii_scope IS NULL OR required_pii_scope = '' THEN
        RETURN true;
    END IF;
    IF required_pii_scope NOT IN ('customers', 'calls', 'appointments', 'notifications') THEN
        RETURN false;
    END IF;

    RETURN EXISTS (
        SELECT 1
        FROM public.platform_pii_access_grants grant_record
        WHERE grant_record.salon_id = target_salon_id
          AND grant_record.user_id = actor_id
          AND grant_record.scope = required_pii_scope
          AND grant_record.revoked_at IS NULL
          AND grant_record.expires_at > now()
    );
END
$$;

CREATE OR REPLACE FUNCTION public.app_rls_salon_select_allowed(
    target_salon_id UUID,
    required_pii_scope TEXT,
    public_catalog_table BOOLEAN
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT CASE public.app_database_scope()
        WHEN 'worker' THEN true
        WHEN 'provider' THEN true
        WHEN 'public' THEN public_catalog_table
                           AND required_pii_scope IS NULL
                           AND public.app_rls_public_salon_visible(target_salon_id)
        ELSE public.app_rls_actor_salon_access(target_salon_id, required_pii_scope)
    END
$$;

CREATE OR REPLACE FUNCTION public.app_rls_salon_write_allowed(
    target_salon_id UUID,
    required_pii_scope TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT CASE public.app_database_scope()
        WHEN 'worker' THEN true
        WHEN 'provider' THEN true
        ELSE public.app_rls_actor_salon_access(target_salon_id, required_pii_scope)
    END
$$;

REVOKE ALL ON FUNCTION public.app_request_actor_user_id() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_database_scope() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_rls_public_salon_visible(UUID) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_rls_actor_salon_access(UUID, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_rls_salon_select_allowed(UUID, TEXT, BOOLEAN) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_rls_salon_write_allowed(UUID, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_request_actor_user_id() TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_database_scope() TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_public_salon_visible(UUID) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_actor_salon_access(UUID, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_salon_select_allowed(UUID, TEXT, BOOLEAN) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_salon_write_allowed(UUID, TEXT) TO PUBLIC;

DO $$
DECLARE
    target RECORD;
    pii_scope TEXT;
    public_visible BOOLEAN;
    policy_prefix TEXT;
BEGIN
    FOR target IN
        SELECT table_class.relname AS table_name
        FROM pg_class table_class
        JOIN pg_namespace table_namespace ON table_namespace.oid = table_class.relnamespace
        JOIN pg_attribute salon_column
          ON salon_column.attrelid = table_class.oid
         AND salon_column.attname = 'salon_id'
         AND NOT salon_column.attisdropped
        WHERE table_namespace.nspname = 'public'
          AND table_class.relkind IN ('r', 'p')
          AND table_class.relname NOT IN (
              'salon_memberships',
              'platform_salon_assignments',
              'platform_pii_access_grants',
              'access_control_actions',
              'access_control_events'
          )
        ORDER BY table_class.relname
    LOOP
        pii_scope := CASE target.table_name
            WHEN 'customers' THEN 'customers'
            WHEN 'call_transcript_messages' THEN 'calls'
            WHEN 'voice_audio_outputs' THEN 'calls'
            ELSE NULL
        END;
        public_visible := target.table_name IN (
            'salon_settings',
            'services',
            'staff',
            'salon_business_hour_periods',
            'manleai_calendar_configs',
            'manleai_calendar_service_policies',
            'manleai_calendar_service_staff',
            'manleai_calendar_staff_weekly_periods',
            'pos_connections',
            'pos_entity_links'
        );
        policy_prefix := 'saas_rls_' || target.table_name;

        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id, %L, %L))',
            policy_prefix || '_select', target.table_name, pii_scope, public_visible
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_insert', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_insert', target.table_name, pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_update', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (public.app_rls_salon_write_allowed(salon_id, %L)) WITH CHECK (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_update', target.table_name, pii_scope, pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_delete', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_delete', target.table_name, pii_scope
        );
    END LOOP;
END
$$;

ALTER TABLE public.salons ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS saas_rls_salons_select ON public.salons;
CREATE POLICY saas_rls_salons_select ON public.salons
FOR SELECT USING (
    public.app_rls_salon_select_allowed(id, NULL, true)
);
DROP POLICY IF EXISTS saas_rls_salons_insert ON public.salons;
CREATE POLICY saas_rls_salons_insert ON public.salons
FOR INSERT WITH CHECK (
    owner_user_id = public.app_request_actor_user_id()
);
DROP POLICY IF EXISTS saas_rls_salons_update ON public.salons;
CREATE POLICY saas_rls_salons_update ON public.salons
FOR UPDATE USING (
    public.app_rls_salon_write_allowed(id, NULL)
) WITH CHECK (
    public.app_rls_salon_write_allowed(id, NULL)
);
DROP POLICY IF EXISTS saas_rls_salons_delete ON public.salons;
CREATE POLICY saas_rls_salons_delete ON public.salons
FOR DELETE USING (
    public.app_rls_salon_write_allowed(id, NULL)
);

COMMENT ON FUNCTION public.app_rls_actor_salon_access(UUID, TEXT) IS
'RLS isolation predicate for a non-owner runtime role. It preserves the actual actor, exact salon, and optional active Platform PII grant.';

COMMENT ON TABLE public.salons IS
'Tenant root. RLS is enabled; production enforcement additionally requires the application runtime role to be distinct from the migration/table owner and to lack BYPASSRLS.';
