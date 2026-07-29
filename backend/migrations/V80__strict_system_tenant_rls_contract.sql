-- Strict provider/worker tenant contract.
--
-- V78 introduced app.system_salon_id and provider-only tenant locators. V79
-- moved every global worker discovery path behind bounded worker-only
-- functions. Ordinary base-table access now fails closed unless the
-- server-bound system tenant matches the row tenant.

CREATE OR REPLACE FUNCTION public.app_rls_system_salon_allowed(target_salon_id UUID)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, public, pg_temp
AS $$
    SELECT CASE public.app_database_scope()
        WHEN 'worker' THEN COALESCE(public.app_request_system_salon_id() = target_salon_id, false)
        WHEN 'provider' THEN COALESCE(public.app_request_system_salon_id() = target_salon_id, false)
        ELSE false
    END
$$;

REVOKE ALL ON FUNCTION public.app_rls_system_salon_allowed(UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_system_salon_allowed(UUID) TO PUBLIC;

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
        WHEN 'worker' THEN public.app_rls_system_salon_allowed(target_salon_id)
        WHEN 'provider' THEN public.app_rls_system_salon_allowed(target_salon_id)
        WHEN 'public' THEN false
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
        WHEN 'worker' THEN public.app_rls_system_salon_allowed(target_salon_id)
        WHEN 'provider' THEN public.app_rls_system_salon_allowed(target_salon_id)
        ELSE public.app_rls_actor_salon_access(target_salon_id, required_pii_scope)
    END
$$;

-- V75 installed Calls-specific policies after the generic V68/V72 policies.
-- Preserve the actor capability contract while replacing the broad system
-- branches with the exact system tenant predicate.
DO $$
DECLARE
    target RECORD;
    prefix TEXT;
BEGIN
    FOR target IN
        SELECT * FROM (VALUES
            ('call_sessions'),
            ('call_transcript_messages'),
            ('voice_audio_outputs'),
            ('voice_webhook_events'),
            ('handoff_requests'),
            ('party_booking_requests')
        ) AS calls_table(table_name)
        WHERE to_regclass('public.' || calls_table.table_name) IS NOT NULL
    LOOP
        prefix := 'saas_rls_' || target.table_name;
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''calls.read'', ''calls'') END)',
            prefix || '_select', target.table_name
        );
    END LOOP;
END
$$;

DROP POLICY IF EXISTS saas_rls_call_sessions_insert ON public.call_sessions;
CREATE POLICY saas_rls_call_sessions_insert ON public.call_sessions
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_call_sessions_update ON public.call_sessions;
CREATE POLICY saas_rls_call_sessions_update ON public.call_sessions
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_call_sessions_delete ON public.call_sessions;
CREATE POLICY saas_rls_call_sessions_delete ON public.call_sessions
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);

DROP POLICY IF EXISTS saas_rls_call_transcript_messages_insert ON public.call_transcript_messages;
CREATE POLICY saas_rls_call_transcript_messages_insert ON public.call_transcript_messages
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_call_transcript_messages_update ON public.call_transcript_messages;
CREATE POLICY saas_rls_call_transcript_messages_update ON public.call_transcript_messages
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_call_transcript_messages_delete ON public.call_transcript_messages;
CREATE POLICY saas_rls_call_transcript_messages_delete ON public.call_transcript_messages
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);

DO $$
DECLARE
    target RECORD;
    prefix TEXT;
BEGIN
    FOR target IN
        SELECT * FROM (VALUES ('handoff_requests'), ('party_booking_requests')) AS mutable_call_child(table_name)
        WHERE to_regclass('public.' || mutable_call_child.table_name) IS NOT NULL
    LOOP
        prefix := 'saas_rls_' || target.table_name;
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_insert', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') END)',
            prefix || '_insert', target.table_name
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_update', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') OR public.app_rls_feature_access(salon_id, ''calls.manage'', ''calls'') END) WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') OR public.app_rls_feature_access(salon_id, ''calls.manage'', ''calls'') END)',
            prefix || '_update', target.table_name
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_delete', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''calls.redact'', ''calls'') END)',
            prefix || '_delete', target.table_name
        );
    END LOOP;
END
$$;

-- Interactive actors still cannot mutate provider-owned voice evidence.
DROP POLICY IF EXISTS saas_rls_voice_audio_outputs_insert ON public.voice_audio_outputs;
CREATE POLICY saas_rls_voice_audio_outputs_insert ON public.voice_audio_outputs
FOR INSERT WITH CHECK (public.app_rls_system_salon_allowed(salon_id));
DROP POLICY IF EXISTS saas_rls_voice_audio_outputs_update ON public.voice_audio_outputs;
CREATE POLICY saas_rls_voice_audio_outputs_update ON public.voice_audio_outputs
FOR UPDATE USING (public.app_rls_system_salon_allowed(salon_id))
WITH CHECK (public.app_rls_system_salon_allowed(salon_id));
DROP POLICY IF EXISTS saas_rls_voice_audio_outputs_delete ON public.voice_audio_outputs;
CREATE POLICY saas_rls_voice_audio_outputs_delete ON public.voice_audio_outputs
FOR DELETE USING (public.app_rls_system_salon_allowed(salon_id));
DROP POLICY IF EXISTS saas_rls_voice_webhook_events_insert ON public.voice_webhook_events;
CREATE POLICY saas_rls_voice_webhook_events_insert ON public.voice_webhook_events
FOR INSERT WITH CHECK (public.app_rls_system_salon_allowed(salon_id));
DROP POLICY IF EXISTS saas_rls_voice_webhook_events_update ON public.voice_webhook_events;
CREATE POLICY saas_rls_voice_webhook_events_update ON public.voice_webhook_events
FOR UPDATE USING (public.app_rls_system_salon_allowed(salon_id))
WITH CHECK (public.app_rls_system_salon_allowed(salon_id));
DROP POLICY IF EXISTS saas_rls_voice_webhook_events_delete ON public.voice_webhook_events;
CREATE POLICY saas_rls_voice_webhook_events_delete ON public.voice_webhook_events
FOR DELETE USING (public.app_rls_system_salon_allowed(salon_id));

-- Recreate the V75 feature policies with the same actor capabilities and an
-- exact tenant match for provider/worker access.
DO $$
DECLARE
    target RECORD;
    prefix TEXT;
BEGIN
    FOR target IN
        SELECT * FROM (VALUES
            ('knowledge_items', 'training.read', 'training.write', NULL::TEXT),
            ('services', 'services.read', 'services.write', NULL::TEXT),
            ('service_categories', 'services.read', 'services.write', NULL::TEXT),
            ('service_consultation_profiles', 'services.read', 'services.write', NULL::TEXT),
            ('service_aliases', 'services.read', 'services.write', NULL::TEXT),
            ('service_category_aliases', 'services.read', 'services.write', NULL::TEXT),
            ('owner_corrections', 'training.read', 'training.write', 'calls')
        ) AS feature_table(table_name, read_capability, write_capability, pii_scope)
        WHERE to_regclass('public.' || feature_table.table_name) IS NOT NULL
    LOOP
        prefix := 'saas_rls_' || target.table_name;
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_select', target.table_name, target.read_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_insert', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_insert', target.table_name, target.write_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_update', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, %L, %L) END) WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_update', target.table_name, target.write_capability, target.pii_scope, target.write_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_delete', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_delete', target.table_name, target.write_capability, target.pii_scope
        );
    END LOOP;
END
$$;

-- The Calls workspace retains its read-only service-label access.
DO $$
DECLARE
    target RECORD;
    prefix TEXT;
BEGIN
    FOR target IN
        SELECT * FROM (VALUES
            ('services'),
            ('service_categories'),
            ('service_consultation_profiles')
        ) AS service_table(table_name)
        WHERE to_regclass('public.' || service_table.table_name) IS NOT NULL
    LOOP
        prefix := 'saas_rls_' || target.table_name;
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN public.app_rls_system_salon_allowed(salon_id) WHEN ''provider'' THEN public.app_rls_system_salon_allowed(salon_id) ELSE public.app_rls_feature_access(salon_id, ''services.read'') OR public.app_rls_feature_access(salon_id, ''calls.read'', ''calls'') END)',
            prefix || '_select', target.table_name
        );
    END LOOP;
END
$$;

DROP POLICY IF EXISTS saas_rls_service_aliases_select ON public.service_aliases;
CREATE POLICY saas_rls_service_aliases_select ON public.service_aliases
FOR SELECT USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'services.read')
      OR public.app_rls_feature_access(salon_id, 'training.read') END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_insert ON public.service_aliases;
CREATE POLICY saas_rls_service_aliases_insert ON public.service_aliases
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write') END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_update ON public.service_aliases;
CREATE POLICY saas_rls_service_aliases_update ON public.service_aliases
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write') END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_delete ON public.service_aliases;
CREATE POLICY saas_rls_service_aliases_delete ON public.service_aliases
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write') END
);

DROP POLICY IF EXISTS saas_rls_owner_corrections_select ON public.owner_corrections;
CREATE POLICY saas_rls_owner_corrections_select ON public.owner_corrections
FOR SELECT USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'training.read', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.read', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_insert ON public.owner_corrections;
CREATE POLICY saas_rls_owner_corrections_insert ON public.owner_corrections
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_update ON public.owner_corrections;
CREATE POLICY saas_rls_owner_corrections_update ON public.owner_corrections
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls') END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_delete ON public.owner_corrections;
CREATE POLICY saas_rls_owner_corrections_delete ON public.owner_corrections
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN public.app_rls_system_salon_allowed(salon_id)
    WHEN 'provider' THEN public.app_rls_system_salon_allowed(salon_id)
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls') END
);

-- V76 owns the current Platform support policies. System access remains
-- available only for the same row tenant; provider still cannot create or
-- mutate support grants.
DROP POLICY IF EXISTS platform_support_requests_select ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_select ON public.platform_support_access_requests
FOR SELECT USING (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR platform_user_id = public.app_request_actor_user_id()
    OR public.app_rls_system_salon_allowed(salon_id)
);
DROP POLICY IF EXISTS platform_support_requests_insert ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_insert ON public.platform_support_access_requests
FOR INSERT WITH CHECK (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR (
        public.app_database_scope() = 'worker'
        AND public.app_rls_system_salon_allowed(salon_id)
    )
);
DROP POLICY IF EXISTS platform_support_requests_update ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_update ON public.platform_support_access_requests
FOR UPDATE USING (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR (
        public.app_database_scope() = 'worker'
        AND public.app_rls_system_salon_allowed(salon_id)
    )
) WITH CHECK (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR (
        public.app_database_scope() = 'worker'
        AND public.app_rls_system_salon_allowed(salon_id)
    )
);

DROP POLICY IF EXISTS platform_support_permissions_all ON public.platform_support_access_request_permissions;
CREATE POLICY platform_support_permissions_all ON public.platform_support_access_request_permissions
FOR ALL USING (
    EXISTS (
        SELECT 1
        FROM public.platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              request.platform_user_id = public.app_request_actor_user_id()
              OR public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
              OR public.app_rls_system_salon_allowed(request.salon_id)
          )
    )
) WITH CHECK (
    EXISTS (
        SELECT 1
        FROM public.platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
              OR (
                  public.app_database_scope() = 'worker'
                  AND public.app_rls_system_salon_allowed(request.salon_id)
              )
          )
    )
);

-- Fail the migration if a direct provider/worker policy branch remains and
-- does not pass through the exact tenant predicate. Narrow SECURITY DEFINER
-- locator/discovery functions are intentionally outside pg_policies.
DO $$
DECLARE
    unsafe_policy RECORD;
BEGIN
    SELECT policy.schemaname, policy.tablename, policy.policyname, expression.value
    INTO unsafe_policy
    FROM pg_catalog.pg_policies policy
    CROSS JOIN LATERAL (
        VALUES (policy.qual), (policy.with_check)
    ) AS expression(value)
    WHERE policy.schemaname = 'public'
      AND expression.value IS NOT NULL
      AND expression.value LIKE '%app_database_scope()%'
      AND (
          expression.value LIKE '%''worker''%'
          OR expression.value LIKE '%''provider''%'
      )
      AND expression.value NOT LIKE '%app_rls_system_salon_allowed%'
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'strict system tenant RLS policy audit failed',
            DETAIL = format('%I.%I policy %I', unsafe_policy.schemaname, unsafe_policy.tablename, unsafe_policy.policyname),
            CONSTRAINT = 'strict_system_tenant_rls_policy_audit';
    END IF;
END
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.app_rls_system_salon_allowed(UUID) IS
'Provider/worker base-table predicate requiring app.system_salon_id to match the row salon exactly.';
COMMENT ON FUNCTION public.app_rls_salon_select_allowed(UUID, TEXT, BOOLEAN) IS
'Base-table tenant selection is actor-authorized or exact provider/worker system-tenant bound; public reads use the safe catalog projection.';
COMMENT ON FUNCTION public.app_rls_salon_write_allowed(UUID, TEXT) IS
'Base-table tenant writes are actor-authorized or exact provider/worker system-tenant bound.';
