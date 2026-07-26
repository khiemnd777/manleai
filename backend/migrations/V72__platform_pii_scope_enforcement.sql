-- SaaS Phase 8/10 PII defense-in-depth.
--
-- V68 established the tenant row boundary. This migration completes the
-- Platform grant boundary by assigning every PII-bearing operational table to
-- one of the four durable Platform PII scopes. Tenant members retain access to
-- their own salon, while Platform actors need an active, unexpired grant for
-- the exact scope. Provider and worker execution keep their narrowly routed
-- system scope because those runtimes do not represent an interactive user.

DO $$
DECLARE
    target RECORD;
    policy_prefix TEXT;
BEGIN
    FOR target IN
        SELECT * FROM (VALUES
            ('customers', 'customers'),

            ('call_sessions', 'calls'),
            ('call_transcript_messages', 'calls'),
            ('voice_audio_outputs', 'calls'),
            ('voice_webhook_events', 'calls'),
            ('handoff_requests', 'calls'),
            ('party_booking_requests', 'calls'),
            ('owner_corrections', 'calls'),

            ('appointments', 'appointments'),
            ('appointment_services', 'appointments'),
            ('booking_attempts', 'appointments'),
            ('booking_attempt_segments', 'appointments'),
            ('booking_attempt_segment_resource_allocations', 'appointments'),
            ('availability_quotes', 'appointments'),
            ('availability_quote_slots', 'appointments'),
            ('availability_quote_slot_segments', 'appointments'),
            ('availability_quote_slot_resource_allocations', 'appointments'),
            ('booking_reconciliation_tasks', 'appointments'),
            ('booking_reconciliation_events', 'appointments'),
            ('manleai_calendar_appointment_resource_allocations', 'appointments'),
            ('manleai_calendar_execution_events', 'appointments'),
            ('scheduling_requests', 'appointments'),
            ('scheduling_request_segments', 'appointments'),
            ('scheduling_request_events', 'appointments'),

            ('owner_notifications', 'notifications'),
            ('owner_notification_delivery_attempts', 'notifications'),
            ('owner_notification_delivery_events', 'notifications'),
            ('owner_notification_delivery_actions', 'notifications'),
            ('customer_sms_consents', 'notifications'),
            ('customer_sms_consent_events', 'notifications'),
            ('customer_notification_deliveries', 'notifications'),
            ('customer_notification_delivery_attempts', 'notifications'),
            ('customer_notification_delivery_events', 'notifications'),
            ('customer_notification_delivery_actions', 'notifications')
        ) AS scoped(table_name, pii_scope)
        WHERE to_regclass('public.' || scoped.table_name) IS NOT NULL
        ORDER BY scoped.table_name
    LOOP
        policy_prefix := 'saas_rls_' || target.table_name;

        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', target.table_name);
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_select', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR SELECT USING (public.app_rls_salon_select_allowed(salon_id, %L, false))',
            policy_prefix || '_select', target.table_name, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_insert', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR INSERT WITH CHECK (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_insert', target.table_name, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_update', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR UPDATE USING (public.app_rls_salon_write_allowed(salon_id, %L)) WITH CHECK (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_update', target.table_name, target.pii_scope, target.pii_scope
        );
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I', policy_prefix || '_delete', target.table_name);
        EXECUTE format(
            'CREATE POLICY %I ON public.%I FOR DELETE USING (public.app_rls_salon_write_allowed(salon_id, %L))',
            policy_prefix || '_delete', target.table_name, target.pii_scope
        );
    END LOOP;
END
$$;

COMMENT ON TABLE platform_pii_access_grants IS
'Time-bounded Platform access to customers, calls, appointments, or notifications. V72 enforces these scopes on all corresponding PII-bearing salon tables.';
