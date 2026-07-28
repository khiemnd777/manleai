-- Owner-authorized Platform support for salon-scoped Services, AI Training,
-- and Calls workflows. Platform role/assignment capability is necessary but
-- never sufficient for these features: one active owner approval is also
-- required. Existing rows receive no implicit authorization.

INSERT INTO permissions (name, display_name, scope, delegation_scope)
VALUES
    ('services.read', 'Read salon services, categories, and aliases', 'tenant', 'salon'),
    ('services.write', 'Manage salon services, categories, and aliases', 'tenant', 'salon'),
    ('training.read', 'Read salon AI Training', 'tenant', 'salon'),
    ('training.write', 'Manage salon AI Training', 'tenant', 'salon'),
    ('calls.read', 'Read salon calls and transcripts', 'tenant', 'salon'),
    ('calls.manage', 'Manage salon call review and corrections', 'tenant', 'salon'),
    ('calls.simulate', 'Run salon call simulator sessions', 'tenant', 'salon'),
    ('calls.redact', 'Irreversibly redact salon call personal data', 'tenant', 'salon')
ON CONFLICT (name) DO UPDATE
SET display_name = EXCLUDED.display_name,
    scope = EXCLUDED.scope,
    delegation_scope = EXCLUDED.delegation_scope,
    updated_at = now();

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
JOIN permissions permission
  ON (
      role.name IN ('tenant_owner', 'tenant_business_manager')
      AND permission.name IN (
          'services.read', 'services.write',
          'training.read', 'training.write',
          'calls.read', 'calls.manage', 'calls.simulate'
      )
  )
  OR (
      role.name = 'tenant_owner'
      AND permission.name = 'calls.redact'
  )
  OR (
      role.name = 'platform_admin'
      AND permission.name IN (
          'services.read', 'services.write',
          'training.read', 'training.write',
          'calls.read', 'calls.manage', 'calls.simulate', 'calls.redact'
      )
  )
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS platform_support_access_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    platform_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending_owner_review'
        CHECK (status IN ('pending_owner_review', 'approved', 'rejected', 'cancelled', 'revoked')),
    reason TEXT NOT NULL
        CHECK (reason ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
    requested_expires_at TIMESTAMPTZ NOT NULL,
    approved_expires_at TIMESTAMPTZ,
    decision_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    decision_at TIMESTAMPTZ,
    revoked_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (requested_expires_at > created_at),
    CHECK (requested_expires_at <= created_at + interval '30 days'),
    CHECK (approved_expires_at IS NULL OR approved_expires_at <= requested_expires_at),
    CHECK ((status = 'approved') = (approved_expires_at IS NOT NULL)),
    CHECK (decision_at IS NULL OR decision_by_user_id IS NOT NULL),
    CHECK (revoked_at IS NULL OR revoked_by_user_id IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_platform_support_one_pending_request
    ON platform_support_access_requests (salon_id, platform_user_id)
    WHERE status = 'pending_owner_review';

CREATE INDEX IF NOT EXISTS idx_platform_support_effective_access
    ON platform_support_access_requests (platform_user_id, salon_id, approved_expires_at)
    WHERE status = 'approved';

CREATE TABLE IF NOT EXISTS platform_support_access_request_permissions (
    request_id UUID NOT NULL
        REFERENCES platform_support_access_requests(id) ON DELETE CASCADE,
    permission_id UUID REFERENCES permissions(id) ON DELETE RESTRICT,
    pii_scope TEXT CHECK (pii_scope IN ('calls')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((permission_id IS NOT NULL) <> (pii_scope IS NOT NULL)),
    UNIQUE NULLS NOT DISTINCT (request_id, permission_id, pii_scope)
);

CREATE INDEX IF NOT EXISTS idx_platform_support_permissions_capability
    ON platform_support_access_request_permissions (permission_id, request_id)
    WHERE permission_id IS NOT NULL;

ALTER TABLE platform_pii_access_grants
    ADD COLUMN IF NOT EXISTS support_access_request_id UUID
        REFERENCES platform_support_access_requests(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_platform_pii_grant_support_request_scope
    ON platform_pii_access_grants (support_access_request_id, scope)
    WHERE support_access_request_id IS NOT NULL;

CREATE OR REPLACE FUNCTION public.validate_platform_support_permission()
RETURNS trigger
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, public
AS $$
DECLARE
    permission_name TEXT;
    requested_at TIMESTAMPTZ;
    requested_expires_at TIMESTAMPTZ;
BEGIN
    SELECT request.created_at, request.requested_expires_at
    INTO requested_at, requested_expires_at
    FROM public.platform_support_access_requests request
    WHERE request.id = NEW.request_id;

    IF requested_at IS NULL THEN
        RAISE EXCEPTION 'platform support request does not exist';
    END IF;

    IF NEW.permission_id IS NOT NULL THEN
        SELECT permission.name INTO permission_name
        FROM public.permissions permission
        WHERE permission.id = NEW.permission_id;
        IF permission_name NOT IN (
            'services.read', 'services.write',
            'training.read', 'training.write',
            'calls.read', 'calls.manage', 'calls.simulate', 'calls.redact'
        ) THEN
            RAISE EXCEPTION 'permission is not an owner-authorized support capability';
        END IF;
    ELSIF NEW.pii_scope = 'calls'
          AND requested_expires_at > requested_at + interval '24 hours' THEN
        RAISE EXCEPTION 'calls PII authorization cannot exceed 24 hours';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS platform_support_permission_guard
    ON platform_support_access_request_permissions;
CREATE TRIGGER platform_support_permission_guard
BEFORE INSERT OR UPDATE ON platform_support_access_request_permissions
FOR EACH ROW EXECUTE FUNCTION public.validate_platform_support_permission();

CREATE OR REPLACE FUNCTION public.app_active_support_authorization(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.platform_support_access_requests request
        JOIN public.platform_support_access_request_permissions child
          ON child.request_id = request.id
        JOIN public.permissions permission
          ON permission.id = child.permission_id
        WHERE request.platform_user_id = target_actor_id
          AND request.salon_id = target_salon_id
          AND request.status = 'approved'
          AND request.approved_expires_at > now()
          AND permission.name = required_capability
          AND EXISTS (
              SELECT 1
              FROM public.users account
              JOIN public.platform_role_assignments role_assignment
                ON role_assignment.user_id = account.id
               AND role_assignment.status = 'active'
              JOIN public.roles role
                ON role.id = role_assignment.role_id
              WHERE account.id = target_actor_id
                AND account.status = 'active'
                AND account.principal_scope = 'platform'
                AND (
                    (
                        role.name = 'platform_admin'
                        AND EXISTS (
                            SELECT 1
                            FROM public.role_permissions role_permission
                            JOIN public.permissions base_permission
                              ON base_permission.id = role_permission.permission_id
                            WHERE role_permission.role_id = role.id
                              AND base_permission.name = required_capability
                        )
                    )
                    OR (
                        role.name = 'platform_ops'
                        AND EXISTS (
                            SELECT 1
                            FROM public.platform_salon_assignments salon_assignment
                            JOIN public.platform_salon_assignment_permissions assignment_permission
                              ON assignment_permission.assignment_id = salon_assignment.id
                            JOIN public.permissions base_permission
                              ON base_permission.id = assignment_permission.permission_id
                            WHERE salon_assignment.user_id = target_actor_id
                              AND salon_assignment.salon_id = target_salon_id
                              AND salon_assignment.status = 'active'
                              AND base_permission.name = required_capability
                        )
                    )
                )
          )
    )
$$;

CREATE OR REPLACE FUNCTION public.app_active_support_pii_grant(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT,
    required_pii_scope TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.platform_pii_access_grants grant_record
        JOIN public.platform_support_access_requests request
          ON request.id = grant_record.support_access_request_id
        JOIN public.platform_support_access_request_permissions capability_child
          ON capability_child.request_id = request.id
        JOIN public.permissions permission
          ON permission.id = capability_child.permission_id
        JOIN public.platform_support_access_request_permissions pii_child
          ON pii_child.request_id = request.id
         AND pii_child.pii_scope = required_pii_scope
        WHERE grant_record.user_id = target_actor_id
          AND grant_record.salon_id = target_salon_id
          AND grant_record.scope = required_pii_scope
          AND grant_record.revoked_at IS NULL
          AND grant_record.expires_at > now()
          AND request.status = 'approved'
          AND request.approved_expires_at > now()
          AND permission.name = required_capability
          AND public.app_active_support_authorization(
              target_actor_id, target_salon_id, required_capability
          )
    )
$$;

CREATE OR REPLACE FUNCTION public.app_actor_feature_access(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT,
    required_pii_scope TEXT DEFAULT NULL
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
BEGIN
    IF target_actor_id IS NULL OR target_salon_id IS NULL THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.salon_memberships membership
          ON membership.user_id = account.id
         AND membership.salon_id = target_salon_id
         AND membership.status = 'active'
        JOIN public.role_permissions role_permission
          ON role_permission.role_id = membership.role_id
        JOIN public.permissions permission
          ON permission.id = role_permission.permission_id
         AND permission.name = required_capability
        WHERE account.id = target_actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'tenant'
    ) THEN
        RETURN true;
    END IF;

    IF NOT public.app_active_support_authorization(target_actor_id, target_salon_id, required_capability) THEN
        RETURN false;
    END IF;
    IF required_pii_scope IS NULL OR required_pii_scope = '' THEN
        RETURN true;
    END IF;
    RETURN public.app_active_support_pii_grant(
        target_actor_id, target_salon_id, required_capability, required_pii_scope
    );
END
$$;

CREATE OR REPLACE FUNCTION public.app_rls_feature_access(
    target_salon_id UUID,
    required_capability TEXT,
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
BEGIN
    RETURN public.app_actor_feature_access(
        actor_id, target_salon_id, required_capability, required_pii_scope
    );
END
$$;

REVOKE ALL ON FUNCTION public.app_active_support_authorization(UUID, UUID, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_active_support_pii_grant(UUID, UUID, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_actor_feature_access(UUID, UUID, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.app_rls_feature_access(UUID, TEXT, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_active_support_authorization(UUID, UUID, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_active_support_pii_grant(UUID, UUID, TEXT, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_actor_feature_access(UUID, UUID, TEXT, TEXT) TO PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_rls_feature_access(UUID, TEXT, TEXT) TO PUBLIC;

-- Calls data stays behind both the exact feature capability and the linked,
-- Owner-approved Calls PII grant. This replaces V72's legacy direct-grant
-- policies so a Platform Administrator cannot bypass Owner authorization.
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
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''calls.read'', ''calls'') END)',
            prefix || '_select', target.table_name
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_insert', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_update', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_delete', target.table_name);
    END LOOP;
END
$$;

CREATE POLICY saas_rls_call_sessions_insert ON call_sessions
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls') END
);
CREATE POLICY saas_rls_call_sessions_update ON call_sessions
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);
CREATE POLICY saas_rls_call_sessions_delete ON call_sessions
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);

CREATE POLICY saas_rls_call_transcript_messages_insert ON call_transcript_messages
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls') END
);
CREATE POLICY saas_rls_call_transcript_messages_update ON call_transcript_messages
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'calls.simulate', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.redact', 'calls') END
);
CREATE POLICY saas_rls_call_transcript_messages_delete ON call_transcript_messages
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true WHEN 'provider' THEN true
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
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') END)',
            prefix || '_insert', target.table_name
        );
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') OR public.app_rls_feature_access(salon_id, ''calls.manage'', ''calls'') END) WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''calls.simulate'', ''calls'') OR public.app_rls_feature_access(salon_id, ''calls.manage'', ''calls'') END)',
            prefix || '_update', target.table_name
        );
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''calls.redact'', ''calls'') END)',
            prefix || '_delete', target.table_name
        );
    END LOOP;
END
$$;

-- Interactive users never write provider-owned voice evidence directly.
CREATE POLICY saas_rls_voice_audio_outputs_insert ON voice_audio_outputs
FOR INSERT WITH CHECK (public.app_database_scope() IN ('worker', 'provider'));
CREATE POLICY saas_rls_voice_audio_outputs_update ON voice_audio_outputs
FOR UPDATE USING (public.app_database_scope() IN ('worker', 'provider'))
WITH CHECK (public.app_database_scope() IN ('worker', 'provider'));
CREATE POLICY saas_rls_voice_audio_outputs_delete ON voice_audio_outputs
FOR DELETE USING (public.app_database_scope() IN ('worker', 'provider'));
CREATE POLICY saas_rls_voice_webhook_events_insert ON voice_webhook_events
FOR INSERT WITH CHECK (public.app_database_scope() IN ('worker', 'provider'));
CREATE POLICY saas_rls_voice_webhook_events_update ON voice_webhook_events
FOR UPDATE USING (public.app_database_scope() IN ('worker', 'provider'))
WITH CHECK (public.app_database_scope() IN ('worker', 'provider'));
CREATE POLICY saas_rls_voice_webhook_events_delete ON voice_webhook_events
FOR DELETE USING (public.app_database_scope() IN ('worker', 'provider'));

-- Calls shows backend-validated scheduling outcomes. Calls authorization may
-- read only the scheduling rows that are durably linked to an authorized call
-- session; it does not grant general Appointments access or any scheduling
-- write permission.
CREATE OR REPLACE FUNCTION public.app_calls_linked_scheduling_row(
    target_salon_id UUID,
    record_kind TEXT,
    record_id UUID
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
BEGIN
    IF target_salon_id IS NULL OR record_id IS NULL
       OR NOT public.app_rls_feature_access(target_salon_id, 'calls.read', 'calls') THEN
        RETURN false;
    END IF;

    CASE record_kind
    WHEN 'scheduling_request' THEN
        RETURN EXISTS (
            SELECT 1 FROM public.call_sessions session
            WHERE session.salon_id = target_salon_id
              AND session.scheduling_request_id = record_id
        );
    WHEN 'booking_attempt' THEN
        RETURN EXISTS (
            SELECT 1
            FROM public.call_sessions session
            LEFT JOIN public.booking_attempts attempt
              ON attempt.salon_id = session.salon_id AND attempt.id = record_id
            WHERE session.salon_id = target_salon_id
              AND (
                  session.booking_attempt_id = record_id
                  OR COALESCE(session.party_plan->'split_booking_attempt_ids', '[]'::jsonb)
                       @> jsonb_build_array(record_id::text)
                  OR attempt.operation_key LIKE 'conversation:' || session.id::text || ':%'
              )
        );
    WHEN 'booking_attempt_segment' THEN
        RETURN EXISTS (
            SELECT 1
            FROM public.booking_attempt_segments segment
            JOIN public.call_sessions session ON session.salon_id = segment.salon_id
            LEFT JOIN public.booking_attempts attempt
              ON attempt.salon_id = segment.salon_id AND attempt.id = segment.booking_attempt_id
            WHERE segment.salon_id = target_salon_id
              AND segment.id = record_id
              AND (
                  session.booking_attempt_id = segment.booking_attempt_id
                  OR COALESCE(session.party_plan->'split_booking_attempt_ids', '[]'::jsonb)
                       @> jsonb_build_array(segment.booking_attempt_id::text)
                  OR attempt.operation_key LIKE 'conversation:' || session.id::text || ':%'
              )
        );
    WHEN 'appointment' THEN
        RETURN EXISTS (
            SELECT 1 FROM public.call_sessions session
            WHERE session.salon_id = target_salon_id
              AND (
                  session.appointment_id = record_id
                  OR COALESCE(session.party_plan->'split_appointment_ids', '[]'::jsonb)
                       @> jsonb_build_array(record_id::text)
              )
        );
    WHEN 'appointment_service' THEN
        RETURN EXISTS (
            SELECT 1
            FROM public.appointment_services child
            JOIN public.call_sessions session ON session.salon_id = child.salon_id
            WHERE child.salon_id = target_salon_id
              AND child.id = record_id
              AND (
                  session.appointment_id = child.appointment_id
                  OR COALESCE(session.party_plan->'split_appointment_ids', '[]'::jsonb)
                       @> jsonb_build_array(child.appointment_id::text)
              )
        );
    WHEN 'calendar_execution_event' THEN
        RETURN EXISTS (
            SELECT 1
            FROM public.manleai_calendar_execution_events event
            JOIN public.call_sessions session ON session.salon_id = event.salon_id
            WHERE event.salon_id = target_salon_id
              AND event.id = record_id
              AND (
                  session.booking_attempt_id = event.booking_attempt_id
                  OR session.appointment_id = event.appointment_id
                  OR COALESCE(session.party_plan->'split_booking_attempt_ids', '[]'::jsonb)
                       @> jsonb_build_array(event.booking_attempt_id::text)
                  OR COALESCE(session.party_plan->'split_appointment_ids', '[]'::jsonb)
                       @> jsonb_build_array(event.appointment_id::text)
              )
        );
    ELSE
        RETURN false;
    END CASE;
END
$$;

REVOKE ALL ON FUNCTION public.app_calls_linked_scheduling_row(UUID, TEXT, UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_calls_linked_scheduling_row(UUID, TEXT, UUID) TO PUBLIC;

DROP POLICY IF EXISTS saas_rls_scheduling_requests_calls_select ON scheduling_requests;
CREATE POLICY saas_rls_scheduling_requests_calls_select ON scheduling_requests
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'scheduling_request', id));
DROP POLICY IF EXISTS saas_rls_booking_attempts_calls_select ON booking_attempts;
CREATE POLICY saas_rls_booking_attempts_calls_select ON booking_attempts
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'booking_attempt', id));
DROP POLICY IF EXISTS saas_rls_booking_attempt_segments_calls_select ON booking_attempt_segments;
CREATE POLICY saas_rls_booking_attempt_segments_calls_select ON booking_attempt_segments
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'booking_attempt_segment', id));
DROP POLICY IF EXISTS saas_rls_appointments_calls_select ON appointments;
CREATE POLICY saas_rls_appointments_calls_select ON appointments
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'appointment', id));
DROP POLICY IF EXISTS saas_rls_appointment_services_calls_select ON appointment_services;
CREATE POLICY saas_rls_appointment_services_calls_select ON appointment_services
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'appointment_service', id));
DROP POLICY IF EXISTS saas_rls_manleai_calendar_execution_events_calls_select ON manleai_calendar_execution_events;
CREATE POLICY saas_rls_manleai_calendar_execution_events_calls_select ON manleai_calendar_execution_events
FOR SELECT USING (public.app_calls_linked_scheduling_row(salon_id, 'calendar_execution_event', id));

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
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_select', target.table_name, target.read_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_insert', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_insert', target.table_name, target.write_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_update', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, %L, %L) END) WITH CHECK (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_update', target.table_name, target.write_capability, target.pii_scope, target.write_capability, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', prefix || '_delete', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, %L, %L) END)',
            prefix || '_delete', target.table_name, target.write_capability, target.pii_scope
        );
    END LOOP;
END
$$;

-- The Calls review workspace needs the canonical service labels and
-- consultation metadata used by its transcript/detected-detail renderer. That
-- read stays Calls-scoped and still requires the Owner-linked Calls PII grant;
-- it grants no Services mutation capability and does not expose the Services
-- management routes.
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
            'CREATE POLICY %I ON public.%I FOR SELECT USING (CASE public.app_database_scope() WHEN ''worker'' THEN true WHEN ''provider'' THEN true ELSE public.app_rls_feature_access(salon_id, ''services.read'') OR public.app_rls_feature_access(salon_id, ''calls.read'', ''calls'') END)',
            prefix || '_select', target.table_name
        );
    END LOOP;
END
$$;

-- Service aliases are a structured Services child and may also be written
-- when an authorized Training correction is explicitly applied as an alias.
DROP POLICY IF EXISTS saas_rls_service_aliases_select ON service_aliases;
CREATE POLICY saas_rls_service_aliases_select ON service_aliases
FOR SELECT USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'services.read')
      OR public.app_rls_feature_access(salon_id, 'training.read')
    END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_insert ON service_aliases;
CREATE POLICY saas_rls_service_aliases_insert ON service_aliases
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write')
    END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_update ON service_aliases;
CREATE POLICY saas_rls_service_aliases_update ON service_aliases
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write')
    END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write')
    END
);
DROP POLICY IF EXISTS saas_rls_service_aliases_delete ON service_aliases;
CREATE POLICY saas_rls_service_aliases_delete ON service_aliases
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'services.write')
      OR public.app_rls_feature_access(salon_id, 'training.write')
    END
);

-- Corrections are visible from both Training review and Calls review. Either
-- workflow still requires the same active Calls PII grant.
DROP POLICY IF EXISTS saas_rls_owner_corrections_select ON owner_corrections;
CREATE POLICY saas_rls_owner_corrections_select ON owner_corrections
FOR SELECT USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'training.read', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.read', 'calls')
    END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_insert ON owner_corrections;
CREATE POLICY saas_rls_owner_corrections_insert ON owner_corrections
FOR INSERT WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
    END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_update ON owner_corrections;
CREATE POLICY saas_rls_owner_corrections_update ON owner_corrections
FOR UPDATE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
    END
) WITH CHECK (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
    END
);
DROP POLICY IF EXISTS saas_rls_owner_corrections_delete ON owner_corrections;
CREATE POLICY saas_rls_owner_corrections_delete ON owner_corrections
FOR DELETE USING (
    CASE public.app_database_scope()
    WHEN 'worker' THEN true
    WHEN 'provider' THEN true
    ELSE public.app_rls_feature_access(salon_id, 'training.write', 'calls')
      OR public.app_rls_feature_access(salon_id, 'calls.manage', 'calls')
    END
);

ALTER TABLE platform_support_access_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_support_access_request_permissions ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS platform_support_requests_select ON platform_support_access_requests;
CREATE POLICY platform_support_requests_select ON platform_support_access_requests
FOR SELECT USING (
    EXISTS (
        SELECT 1 FROM salon_memberships membership
        WHERE membership.salon_id = platform_support_access_requests.salon_id
          AND membership.user_id = public.app_request_actor_user_id()
          AND membership.status = 'active'
          AND membership.is_owner
    )
    OR platform_user_id = public.app_request_actor_user_id()
    OR requested_by_user_id = public.app_request_actor_user_id()
    OR EXISTS (
        SELECT 1 FROM platform_role_assignments role_assignment
        JOIN roles role ON role.id = role_assignment.role_id
        WHERE role_assignment.user_id = public.app_request_actor_user_id()
          AND role_assignment.status = 'active'
          AND role.name = 'platform_admin'
    )
    OR public.app_database_scope() IN ('worker', 'provider')
);

DROP POLICY IF EXISTS platform_support_requests_insert ON platform_support_access_requests;
CREATE POLICY platform_support_requests_insert ON platform_support_access_requests
FOR INSERT WITH CHECK (
    requested_by_user_id = public.app_request_actor_user_id()
    OR public.app_database_scope() = 'worker'
);

DROP POLICY IF EXISTS platform_support_requests_update ON platform_support_access_requests;
CREATE POLICY platform_support_requests_update ON platform_support_access_requests
FOR UPDATE USING (
    requested_by_user_id = public.app_request_actor_user_id()
    OR EXISTS (
        SELECT 1 FROM salon_memberships membership
        WHERE membership.salon_id = platform_support_access_requests.salon_id
          AND membership.user_id = public.app_request_actor_user_id()
          AND membership.status = 'active'
          AND membership.is_owner
    )
    OR public.app_database_scope() = 'worker'
);

DROP POLICY IF EXISTS platform_support_permissions_all ON platform_support_access_request_permissions;
CREATE POLICY platform_support_permissions_all ON platform_support_access_request_permissions
FOR ALL USING (
    EXISTS (
        SELECT 1 FROM platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              request.platform_user_id = public.app_request_actor_user_id()
              OR request.requested_by_user_id = public.app_request_actor_user_id()
              OR EXISTS (
                  SELECT 1 FROM platform_role_assignments role_assignment
                  JOIN roles role ON role.id = role_assignment.role_id
                  WHERE role_assignment.user_id = public.app_request_actor_user_id()
                    AND role_assignment.status = 'active'
                    AND role.name = 'platform_admin'
              )
              OR EXISTS (
                  SELECT 1 FROM salon_memberships membership
                  WHERE membership.salon_id = request.salon_id
                    AND membership.user_id = public.app_request_actor_user_id()
                    AND membership.status = 'active'
                    AND membership.is_owner
              )
              OR public.app_database_scope() = 'worker'
          )
    )
) WITH CHECK (
    EXISTS (
        SELECT 1 FROM platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              request.requested_by_user_id = public.app_request_actor_user_id()
              OR public.app_database_scope() = 'worker'
          )
    )
);

COMMENT ON TABLE platform_support_access_requests IS
'Owner-reviewed, time-bounded authorization for one Platform principal to support one salon. Expiry is derived from approved_expires_at; revoked and expired access never revives.';
COMMENT ON TABLE platform_support_access_request_permissions IS
'Exact capability and calls-PII children requested and approved with the parent Platform support authorization.';
