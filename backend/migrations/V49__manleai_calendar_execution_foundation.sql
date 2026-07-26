-- Phase 4A adds durable ManleAI Calendar execution evidence without registering
-- an executor. Existing external-provider rows retain their legacy shape; new
-- internal rows must leave every POS/provider compatibility field NULL.

ALTER TABLE booking_attempts
    ALTER COLUMN pos_provider DROP NOT NULL,
    ALTER COLUMN target_pos_booking_version DROP NOT NULL,
    ADD COLUMN scheduling_authority_version BIGINT,
    ADD COLUMN authority_config_version BIGINT,
    ADD COLUMN party_size SMALLINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT booking_attempts_salon_id_id_key UNIQUE (salon_id, id),
    ADD CONSTRAINT booking_attempts_party_size_check
        CHECK (party_size BETWEEN 1 AND 100),
    ADD CONSTRAINT booking_attempts_execution_fence_check
        CHECK (
            (scheduling_authority = 'external_provider'
                AND scheduling_authority_version IS NULL
                AND authority_config_version IS NULL)
            OR (
                scheduling_authority = 'manleai_calendar'
                AND scheduling_authority_version >= 1
                AND authority_config_version >= 1
            )
        ),
    ADD CONSTRAINT booking_attempts_manleai_calendar_shape_check
        CHECK (
            scheduling_authority <> 'manleai_calendar'
            OR (
                pos_provider IS NULL
                AND pos_booking_id IS NULL
                AND pos_booking_version IS NULL
                AND target_pos_booking_version IS NULL
                AND pos_idempotency_key IS NULL
                AND provider_location_id IS NULL
                AND provider_snapshot_generation IS NULL
                AND processing_token IS NULL
                AND processing_lease_expires_at IS NULL
                AND authority_provider IS NULL
                AND authority_location_id IS NULL
                AND authority_snapshot_generation IS NULL
                AND provider_outcome = 'not_started'
                AND retry_policy = 'none'
                AND reconciliation_status = 'not_required'
                AND reconciliation_resolution IS NULL
                AND reconciliation_resolved_at IS NULL
                AND superseded_at IS NULL
                AND superseded_by_attempt_id IS NULL
                AND operation_key IS NOT NULL
                AND length(trim(operation_key)) BETWEEN 1 AND 256
                AND request_fingerprint ~ '^[0-9a-f]{64}$'
                AND authority_idempotency_key = operation_key
                AND authority_appointment_id IS NOT NULL
                AND authority_appointment_version >= 1
                AND status IN ('confirmed', 'rescheduled', 'cancelled')
                AND (
                    (
                        operation_type = 'book'
                        AND status = 'confirmed'
                        AND target_appointment_id IS NULL
                        AND target_authority_appointment_version IS NULL
                        AND availability_quote_id IS NOT NULL
                    )
                    OR (
                        operation_type = 'reschedule'
                        AND status = 'rescheduled'
                        AND target_appointment_id IS NOT NULL
                        AND target_authority_appointment_version >= 1
                        AND availability_quote_id IS NOT NULL
                    )
                    OR (
                        operation_type = 'cancel'
                        AND status = 'cancelled'
                        AND target_appointment_id IS NOT NULL
                        AND target_authority_appointment_version >= 1
                        AND availability_quote_id IS NULL
                        AND availability_slot_fingerprint IS NULL
                    )
                )
            )
        ),
    ADD CONSTRAINT booking_attempts_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                pos_provider IS NOT NULL
                AND target_pos_booking_version IS NOT NULL
            )
        ),
    ADD CONSTRAINT booking_attempts_execution_authority_check
        CHECK (scheduling_authority IN ('manleai_calendar', 'external_provider'));

ALTER TABLE booking_attempts
    ADD CONSTRAINT booking_attempts_target_appointment_tenant_fk
        FOREIGN KEY (salon_id, target_appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE appointments
    ALTER COLUMN pos_provider DROP NOT NULL,
    ALTER COLUMN pos_appointment_id DROP NOT NULL,
    ALTER COLUMN pos_appointment_version DROP NOT NULL,
    ALTER COLUMN pos_sync_status DROP NOT NULL,
    ADD COLUMN scheduling_authority_version BIGINT,
    ADD COLUMN authority_config_version BIGINT,
    ADD COLUMN party_size SMALLINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT appointments_party_size_check
        CHECK (party_size BETWEEN 1 AND 100),
    ADD CONSTRAINT appointments_manleai_calendar_shape_check
        CHECK (
            scheduling_authority <> 'manleai_calendar'
            OR (
                pos_provider IS NULL
                AND pos_appointment_id IS NULL
                AND pos_appointment_version IS NULL
                AND pos_customer_id IS NULL
                AND pos_sync_status IS NULL
                AND last_pos_synced_at IS NULL
                AND pos_sync_error IS NULL
                AND authority_provider IS NULL
                AND authority_customer_id IS NULL
                AND authority_appointment_id = id::text
                AND authority_appointment_version >= 1
                AND scheduling_authority_version >= 1
                AND authority_config_version >= 1
                AND status IN ('confirmed', 'rescheduled', 'cancelled')
                AND confirmed_at IS NOT NULL
                AND confirmation_source = 'manleai_calendar'
                AND confirmed_by_user_id IS NULL
            )
        ),
    ADD CONSTRAINT appointments_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                pos_provider IS NOT NULL
                AND pos_appointment_id IS NOT NULL
                AND pos_appointment_version IS NOT NULL
                AND pos_sync_status IS NOT NULL
                AND scheduling_authority_version IS NULL
                AND authority_config_version IS NULL
            )
        ),
    ADD CONSTRAINT appointments_execution_authority_check
        CHECK (scheduling_authority IN ('manleai_calendar', 'external_provider')),
    ADD CONSTRAINT appointments_booking_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE booking_attempt_segments
    ALTER COLUMN pos_service_id DROP NOT NULL,
    ADD COLUMN salon_id UUID,
    ADD COLUMN guest_reference TEXT,
    ADD COLUMN scheduled_start_time TIMESTAMPTZ,
    ADD COLUMN scheduled_end_time TIMESTAMPTZ,
    ADD COLUMN buffer_before_minutes SMALLINT,
    ADD COLUMN buffer_after_minutes SMALLINT,
    ADD COLUMN occupied_start_time TIMESTAMPTZ,
    ADD COLUMN occupied_end_time TIMESTAMPTZ;

UPDATE booking_attempt_segments segment
SET salon_id = attempt.salon_id
FROM booking_attempts attempt
WHERE attempt.id = segment.booking_attempt_id;

ALTER TABLE booking_attempt_segments
    ALTER COLUMN salon_id SET NOT NULL,
    ADD CONSTRAINT booking_attempt_segments_salon_id_id_key UNIQUE (salon_id, id),
    ADD CONSTRAINT booking_attempt_segments_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE CASCADE,
    ADD CONSTRAINT booking_attempt_segments_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT booking_attempt_segments_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT booking_attempt_segments_guest_reference_check
        CHECK (guest_reference IS NULL OR length(trim(guest_reference)) BETWEEN 1 AND 200),
    ADD CONSTRAINT booking_attempt_segments_manleai_calendar_shape_check
        CHECK (
            scheduling_authority <> 'manleai_calendar'
            OR (
                service_id IS NOT NULL
                AND staff_id IS NOT NULL
                AND pos_service_id IS NULL
                AND pos_service_version IS NULL
                AND pos_staff_id IS NULL
                AND authority_provider IS NULL
                AND authority_service_id = service_id::text
                AND authority_service_version IS NULL
                AND authority_staff_id = staff_id::text
                AND duration_minutes > 0
                AND sort_order >= 1
                AND scheduled_start_time IS NOT NULL
                AND scheduled_end_time = scheduled_start_time + make_interval(mins => duration_minutes)
                AND buffer_before_minutes BETWEEN 0 AND 1440
                AND buffer_after_minutes BETWEEN 0 AND 1440
                AND occupied_start_time = scheduled_start_time - make_interval(mins => buffer_before_minutes)
                AND occupied_end_time = scheduled_end_time + make_interval(mins => buffer_after_minutes)
                AND occupied_end_time > occupied_start_time
            )
        ),
    ADD CONSTRAINT booking_attempt_segments_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                pos_service_id IS NOT NULL
                AND guest_reference IS NULL
                AND scheduled_start_time IS NULL
                AND scheduled_end_time IS NULL
                AND buffer_before_minutes IS NULL
                AND buffer_after_minutes IS NULL
                AND occupied_start_time IS NULL
                AND occupied_end_time IS NULL
            )
        ),
    ADD CONSTRAINT booking_attempt_segments_execution_authority_check
        CHECK (scheduling_authority IN ('manleai_calendar', 'external_provider'));

ALTER TABLE appointment_services
    ALTER COLUMN pos_service_id DROP NOT NULL,
    ADD COLUMN salon_id UUID,
    ADD COLUMN plan_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN guest_reference TEXT,
    ADD COLUMN scheduled_start_time TIMESTAMPTZ,
    ADD COLUMN scheduled_end_time TIMESTAMPTZ,
    ADD COLUMN buffer_before_minutes SMALLINT,
    ADD COLUMN buffer_after_minutes SMALLINT,
    ADD COLUMN occupied_start_time TIMESTAMPTZ,
    ADD COLUMN occupied_end_time TIMESTAMPTZ,
    ADD COLUMN released_at TIMESTAMPTZ;

UPDATE appointment_services segment
SET salon_id = appointment.salon_id
FROM appointments appointment
WHERE appointment.id = segment.appointment_id;

ALTER TABLE appointment_services
    ALTER COLUMN salon_id SET NOT NULL,
    ADD CONSTRAINT appointment_services_salon_id_id_key UNIQUE (salon_id, id),
    ADD CONSTRAINT appointment_services_appointment_tenant_fk
        FOREIGN KEY (salon_id, appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE CASCADE,
    ADD CONSTRAINT appointment_services_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT appointment_services_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT appointment_services_plan_version_check
        CHECK (plan_version >= 1),
    ADD CONSTRAINT appointment_services_guest_reference_check
        CHECK (guest_reference IS NULL OR length(trim(guest_reference)) BETWEEN 1 AND 200),
    ADD CONSTRAINT appointment_services_manleai_calendar_shape_check
        CHECK (
            scheduling_authority <> 'manleai_calendar'
            OR (
                service_id IS NOT NULL
                AND staff_id IS NOT NULL
                AND pos_service_id IS NULL
                AND pos_service_version IS NULL
                AND pos_staff_id IS NULL
                AND authority_provider IS NULL
                AND authority_service_id = service_id::text
                AND authority_service_version IS NULL
                AND authority_staff_id = staff_id::text
                AND duration_minutes > 0
                AND sort_order >= 1
                AND scheduled_start_time IS NOT NULL
                AND scheduled_end_time = scheduled_start_time + make_interval(mins => duration_minutes)
                AND buffer_before_minutes BETWEEN 0 AND 1440
                AND buffer_after_minutes BETWEEN 0 AND 1440
                AND occupied_start_time = scheduled_start_time - make_interval(mins => buffer_before_minutes)
                AND occupied_end_time = scheduled_end_time + make_interval(mins => buffer_after_minutes)
                AND occupied_end_time > occupied_start_time
                AND (released_at IS NULL OR released_at >= created_at)
            )
        ),
    ADD CONSTRAINT appointment_services_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                pos_service_id IS NOT NULL
                AND plan_version = 1
                AND guest_reference IS NULL
                AND scheduled_start_time IS NULL
                AND scheduled_end_time IS NULL
                AND buffer_before_minutes IS NULL
                AND buffer_after_minutes IS NULL
                AND occupied_start_time IS NULL
                AND occupied_end_time IS NULL
                AND released_at IS NULL
            )
        ),
    ADD CONSTRAINT appointment_services_execution_authority_check
        CHECK (scheduling_authority IN ('manleai_calendar', 'external_provider'));

CREATE UNIQUE INDEX idx_appointment_services_manleai_plan_order
    ON appointment_services(appointment_id, plan_version, sort_order)
    WHERE scheduling_authority = 'manleai_calendar';

CREATE UNIQUE INDEX idx_appointment_services_manleai_active_order
    ON appointment_services(appointment_id, sort_order)
    WHERE scheduling_authority = 'manleai_calendar' AND released_at IS NULL;

ALTER TABLE appointment_services
    ADD CONSTRAINT appointment_services_manleai_staff_no_overlap
    EXCLUDE USING gist (
        salon_id WITH =,
        staff_id WITH =,
        (tstzrange(occupied_start_time, occupied_end_time, '[)')) WITH &&
    )
    WHERE (scheduling_authority = 'manleai_calendar' AND released_at IS NULL)
    DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE availability_quotes
    ALTER COLUMN provider DROP NOT NULL,
    ALTER COLUMN provider_location_id DROP NOT NULL,
    ALTER COLUMN provider_snapshot_generation DROP NOT NULL,
    ADD COLUMN scheduling_authority_version BIGINT,
    ADD COLUMN authority_config_version BIGINT,
    ADD COLUMN operation_type TEXT,
    ADD COLUMN target_appointment_id UUID,
    ADD COLUMN target_authority_appointment_version INTEGER,
    ADD COLUMN party_size SMALLINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT availability_quotes_salon_id_id_key UNIQUE (salon_id, id),
    ADD CONSTRAINT availability_quotes_party_size_check
        CHECK (party_size BETWEEN 1 AND 100),
    ADD CONSTRAINT availability_quotes_operation_type_check
        CHECK (operation_type IS NULL OR operation_type IN ('book', 'reschedule')),
    ADD CONSTRAINT availability_quotes_target_version_check
        CHECK (target_authority_appointment_version IS NULL OR target_authority_appointment_version >= 1),
    ADD CONSTRAINT availability_quotes_manleai_calendar_shape_check
        CHECK (
            scheduling_authority <> 'manleai_calendar'
            OR (
                provider IS NULL
                AND provider_location_id IS NULL
                AND provider_snapshot_generation IS NULL
                AND authority_provider IS NULL
                AND authority_location_id IS NULL
                AND authority_snapshot_generation IS NULL
                AND scheduling_authority_version >= 1
                AND authority_config_version >= 1
                AND request_fingerprint ~ '^[0-9a-f]{64}$'
                AND (
                    (operation_type = 'book'
                        AND target_appointment_id IS NULL
                        AND target_authority_appointment_version IS NULL)
                    OR
                    (operation_type = 'reschedule'
                        AND target_appointment_id IS NOT NULL
                        AND target_authority_appointment_version >= 1)
                )
            )
        ),
    ADD CONSTRAINT availability_quotes_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                provider IS NOT NULL
                AND provider_location_id IS NOT NULL
                AND provider_snapshot_generation IS NOT NULL
                AND scheduling_authority_version IS NULL
                AND authority_config_version IS NULL
                AND operation_type IS NULL
                AND target_appointment_id IS NULL
                AND target_authority_appointment_version IS NULL
                AND party_size = 1
            )
        ),
    ADD CONSTRAINT availability_quotes_execution_authority_check
        CHECK (scheduling_authority IN ('manleai_calendar', 'external_provider')),
    ADD CONSTRAINT availability_quotes_target_appointment_tenant_fk
        FOREIGN KEY (salon_id, target_appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT availability_quotes_consumed_attempt_tenant_fk
        FOREIGN KEY (salon_id, consumed_by_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE SET NULL (consumed_by_attempt_id);

ALTER TABLE availability_quote_slots
    ADD COLUMN salon_id UUID;

UPDATE availability_quote_slots slot
SET salon_id = quote.salon_id
FROM availability_quotes quote
WHERE quote.id = slot.quote_id;

ALTER TABLE availability_quote_slots
    ALTER COLUMN salon_id SET NOT NULL,
    ADD CONSTRAINT availability_quote_slots_salon_id_id_key UNIQUE (salon_id, id),
    ADD CONSTRAINT availability_quote_slots_quote_tenant_fk
        FOREIGN KEY (salon_id, quote_id)
        REFERENCES availability_quotes(salon_id, id)
        ON DELETE CASCADE;

CREATE TABLE availability_quote_slot_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    quote_slot_id UUID NOT NULL,
    service_id UUID NOT NULL,
    staff_id UUID NOT NULL,
    staff_selection_mode TEXT NOT NULL DEFAULT 'specific',
    guest_reference TEXT,
    duration_minutes INTEGER NOT NULL,
    sort_order INTEGER NOT NULL,
    scheduled_start_time TIMESTAMPTZ NOT NULL,
    scheduled_end_time TIMESTAMPTZ NOT NULL,
    buffer_before_minutes SMALLINT NOT NULL,
    buffer_after_minutes SMALLINT NOT NULL,
    occupied_start_time TIMESTAMPTZ NOT NULL,
    occupied_end_time TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT availability_quote_slot_segments_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT availability_quote_slot_segments_order_key UNIQUE (quote_slot_id, sort_order),
    CONSTRAINT availability_quote_slot_segments_slot_tenant_fk
        FOREIGN KEY (salon_id, quote_slot_id)
        REFERENCES availability_quote_slots(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_quote_slot_segments_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT availability_quote_slot_segments_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT availability_quote_slot_segments_staff_mode_check
        CHECK (staff_selection_mode IN ('specific', 'anyone')),
    CONSTRAINT availability_quote_slot_segments_guest_reference_check
        CHECK (guest_reference IS NULL OR length(trim(guest_reference)) BETWEEN 1 AND 200),
    CONSTRAINT availability_quote_slot_segments_timing_check
        CHECK (
            duration_minutes > 0
            AND sort_order >= 1
            AND scheduled_end_time = scheduled_start_time + make_interval(mins => duration_minutes)
            AND buffer_before_minutes BETWEEN 0 AND 1440
            AND buffer_after_minutes BETWEEN 0 AND 1440
            AND occupied_start_time = scheduled_start_time - make_interval(mins => buffer_before_minutes)
            AND occupied_end_time = scheduled_end_time + make_interval(mins => buffer_after_minutes)
            AND occupied_end_time > occupied_start_time
        )
);

CREATE TABLE availability_quote_slot_resource_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    quote_slot_segment_id UUID NOT NULL,
    resource_pool_id UUID NOT NULL,
    units_allocated INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT availability_quote_slot_resource_allocations_key
        UNIQUE (quote_slot_segment_id, resource_pool_id),
    CONSTRAINT availability_quote_slot_resource_allocations_segment_tenant_fk
        FOREIGN KEY (salon_id, quote_slot_segment_id)
        REFERENCES availability_quote_slot_segments(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_quote_slot_resource_allocations_pool_tenant_fk
        FOREIGN KEY (salon_id, resource_pool_id)
        REFERENCES manleai_calendar_resource_pools(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT availability_quote_slot_resource_allocations_units_check
        CHECK (units_allocated BETWEEN 1 AND 1000)
);

CREATE TABLE booking_attempt_segment_resource_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    booking_attempt_segment_id UUID NOT NULL,
    resource_pool_id UUID NOT NULL,
    units_allocated INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT booking_attempt_segment_resource_allocations_key
        UNIQUE (booking_attempt_segment_id, resource_pool_id),
    CONSTRAINT booking_attempt_segment_resource_allocations_segment_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_segment_id)
        REFERENCES booking_attempt_segments(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT booking_attempt_segment_resource_allocations_pool_tenant_fk
        FOREIGN KEY (salon_id, resource_pool_id)
        REFERENCES manleai_calendar_resource_pools(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT booking_attempt_segment_resource_allocations_units_check
        CHECK (units_allocated BETWEEN 1 AND 1000)
);

CREATE TABLE manleai_calendar_appointment_resource_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    appointment_service_id UUID NOT NULL,
    resource_pool_id UUID NOT NULL,
    units_allocated INTEGER NOT NULL,
    plan_version INTEGER NOT NULL,
    occupied_start_time TIMESTAMPTZ NOT NULL,
    occupied_end_time TIMESTAMPTZ NOT NULL,
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_salon_id_id_key
        UNIQUE (salon_id, id),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_key
        UNIQUE (appointment_service_id, resource_pool_id),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_segment_tenant_fk
        FOREIGN KEY (salon_id, appointment_service_id)
        REFERENCES appointment_services(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT manleai_calendar_appointment_resource_allocations_pool_tenant_fk
        FOREIGN KEY (salon_id, resource_pool_id)
        REFERENCES manleai_calendar_resource_pools(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT manleai_calendar_appointment_resource_allocations_units_check
        CHECK (units_allocated BETWEEN 1 AND 1000),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_version_check
        CHECK (plan_version >= 1),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_range_check
        CHECK (occupied_end_time > occupied_start_time),
    CONSTRAINT manleai_calendar_appointment_resource_allocations_release_check
        CHECK (released_at IS NULL OR released_at >= created_at)
);

CREATE INDEX idx_manleai_calendar_appointment_resource_allocations_overlap
    ON manleai_calendar_appointment_resource_allocations
    USING gist (salon_id, resource_pool_id, tstzrange(occupied_start_time, occupied_end_time, '[)'))
    WHERE released_at IS NULL;

CREATE TABLE manleai_calendar_execution_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    booking_attempt_id UUID NOT NULL,
    appointment_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    scheduling_authority_version BIGINT NOT NULL,
    authority_config_version BIGINT NOT NULL,
    authority_appointment_version INTEGER NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT manleai_calendar_execution_events_attempt_key
        UNIQUE (salon_id, booking_attempt_id),
    CONSTRAINT manleai_calendar_execution_events_attempt_tenant_fk
        FOREIGN KEY (salon_id, booking_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT manleai_calendar_execution_events_appointment_tenant_fk
        FOREIGN KEY (salon_id, appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT manleai_calendar_execution_events_type_check
        CHECK (event_type IN ('appointment_confirmed', 'appointment_rescheduled', 'appointment_cancelled')),
    CONSTRAINT manleai_calendar_execution_events_version_check
        CHECK (
            scheduling_authority_version >= 1
            AND authority_config_version >= 1
            AND authority_appointment_version >= 1
        ),
    CONSTRAINT manleai_calendar_execution_events_payload_check
        CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 16384)
);

CREATE FUNCTION validate_manleai_calendar_quote_evidence(target_quote_id UUID)
RETURNS VOID AS $$
DECLARE
    quote_authority TEXT;
    invalid_slots INTEGER;
BEGIN
    SELECT scheduling_authority
    INTO quote_authority
    FROM availability_quotes
    WHERE id = target_quote_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF quote_authority = 'external_provider' THEN
        IF EXISTS (
            SELECT 1
            FROM availability_quote_slots slot
            JOIN availability_quote_slot_segments segment ON segment.quote_slot_id = slot.id
            WHERE slot.quote_id = target_quote_id
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'normalized slot evidence belongs only to ManleAI Calendar quotes',
                CONSTRAINT = 'availability_quotes_external_provider_evidence_guard';
        END IF;
        RETURN;
    END IF;

    SELECT count(*)
    INTO invalid_slots
    FROM availability_quote_slots slot
    WHERE slot.quote_id = target_quote_id
      AND (
          slot.segments <> '[]'::jsonb
          OR NOT EXISTS (
              SELECT 1
              FROM availability_quote_slot_segments segment
              WHERE segment.quote_slot_id = slot.id
          )
          OR slot.start_time IS DISTINCT FROM (
              SELECT min(segment.scheduled_start_time)
              FROM availability_quote_slot_segments segment
              WHERE segment.quote_slot_id = slot.id
          )
          OR slot.end_time IS DISTINCT FROM (
              SELECT max(segment.scheduled_end_time)
              FROM availability_quote_slot_segments segment
              WHERE segment.quote_slot_id = slot.id
          )
      );

    IF invalid_slots > 0 OR NOT EXISTS (
        SELECT 1 FROM availability_quote_slots WHERE quote_id = target_quote_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar quotes require normalized exact slot evidence',
            CONSTRAINT = 'availability_quotes_manleai_calendar_evidence_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_quote_evidence()
RETURNS TRIGGER AS $$
DECLARE
    quote_id_to_check UUID;
BEGIN
    IF TG_TABLE_NAME = 'availability_quotes' THEN
        quote_id_to_check := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'availability_quote_slots' THEN
        quote_id_to_check := COALESCE(NEW.quote_id, OLD.quote_id);
    ELSE
        SELECT quote_id
        INTO quote_id_to_check
        FROM availability_quote_slots
        WHERE id = COALESCE(NEW.quote_slot_id, OLD.quote_slot_id);
    END IF;

    IF quote_id_to_check IS NOT NULL THEN
        PERFORM validate_manleai_calendar_quote_evidence(quote_id_to_check);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER availability_quotes_manleai_calendar_evidence_guard
AFTER INSERT OR UPDATE ON availability_quotes
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_evidence();

CREATE CONSTRAINT TRIGGER availability_quote_slots_manleai_calendar_evidence_guard
AFTER INSERT OR UPDATE OR DELETE ON availability_quote_slots
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_evidence();

CREATE CONSTRAINT TRIGGER availability_quote_slot_segments_manleai_calendar_evidence_guard
AFTER INSERT OR UPDATE OR DELETE ON availability_quote_slot_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_evidence();

CREATE FUNCTION validate_manleai_calendar_appointment_plan(target_appointment_id UUID)
RETURNS VOID AS $$
DECLARE
    item RECORD;
    active_count INTEGER;
    invalid_count INTEGER;
    invalid_resource_count INTEGER;
    active_start TIMESTAMPTZ;
    active_end TIMESTAMPTZ;
BEGIN
    SELECT scheduling_authority, status, authority_appointment_version, start_time, end_time
    INTO item
    FROM appointments
    WHERE id = target_appointment_id;

    IF NOT FOUND OR item.scheduling_authority <> 'manleai_calendar' THEN
        RETURN;
    END IF;

    SELECT count(*),
           count(*) FILTER (WHERE plan_version <> item.authority_appointment_version),
           min(scheduled_start_time), max(scheduled_end_time)
    INTO active_count, invalid_count, active_start, active_end
    FROM appointment_services
    WHERE appointment_id = target_appointment_id
      AND scheduling_authority = 'manleai_calendar'
      AND released_at IS NULL;

    SELECT count(*)
    INTO invalid_resource_count
    FROM manleai_calendar_appointment_resource_allocations allocation
    JOIN appointment_services segment ON segment.id = allocation.appointment_service_id
    WHERE segment.appointment_id = target_appointment_id
      AND (
          allocation.salon_id <> segment.salon_id
          OR allocation.plan_version <> segment.plan_version
          OR allocation.occupied_start_time IS DISTINCT FROM segment.occupied_start_time
          OR allocation.occupied_end_time IS DISTINCT FROM segment.occupied_end_time
          OR allocation.released_at IS DISTINCT FROM segment.released_at
      );

    IF invalid_resource_count <> 0 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar resource history does not match its appointment plan',
            CONSTRAINT = 'appointments_manleai_calendar_plan_guard';
    END IF;

    IF item.status IN ('confirmed', 'rescheduled') THEN
        IF active_count = 0
           OR invalid_count <> 0
           OR active_start IS DISTINCT FROM item.start_time
           OR active_end IS DISTINCT FROM item.end_time THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'active ManleAI Calendar appointment plan does not match its root',
                CONSTRAINT = 'appointments_manleai_calendar_plan_guard';
        END IF;
    ELSIF item.status = 'cancelled' AND active_count <> 0 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'cancelled ManleAI Calendar appointments cannot retain active allocations',
            CONSTRAINT = 'appointments_manleai_calendar_plan_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_appointment_plan()
RETURNS TRIGGER AS $$
DECLARE
    appointment_id_to_check UUID;
BEGIN
    IF TG_TABLE_NAME = 'appointments' THEN
        appointment_id_to_check := COALESCE(NEW.id, OLD.id);
    ELSE
        appointment_id_to_check := COALESCE(NEW.appointment_id, OLD.appointment_id);
    END IF;
    PERFORM validate_manleai_calendar_appointment_plan(appointment_id_to_check);
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER appointments_manleai_calendar_plan_guard
AFTER INSERT OR UPDATE ON appointments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_appointment_plan();

CREATE CONSTRAINT TRIGGER appointment_services_manleai_calendar_plan_guard
AFTER INSERT OR UPDATE OR DELETE ON appointment_services
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_appointment_plan();

CREATE FUNCTION enforce_manleai_calendar_resource_allocation_match()
RETURNS TRIGGER AS $$
DECLARE
    segment RECORD;
BEGIN
    SELECT scheduling_authority, salon_id, plan_version,
           occupied_start_time, occupied_end_time, released_at
    INTO segment
    FROM appointment_services
    WHERE id = NEW.appointment_service_id;

    IF NOT FOUND
       OR segment.scheduling_authority <> 'manleai_calendar'
       OR segment.salon_id <> NEW.salon_id
       OR segment.plan_version <> NEW.plan_version
       OR segment.occupied_start_time IS DISTINCT FROM NEW.occupied_start_time
       OR segment.occupied_end_time IS DISTINCT FROM NEW.occupied_end_time
       OR segment.released_at IS DISTINCT FROM NEW.released_at THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'resource allocation must match its ManleAI Calendar appointment segment',
            CONSTRAINT = 'manleai_calendar_appointment_resource_allocations_segment_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER manleai_calendar_appointment_resource_allocations_segment_guard
AFTER INSERT OR UPDATE ON manleai_calendar_appointment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_resource_allocation_match();

CREATE FUNCTION enforce_manleai_calendar_execution_event()
RETURNS TRIGGER AS $$
DECLARE
    attempt RECORD;
    appointment RECORD;
    expected_event_type TEXT;
BEGIN
    SELECT scheduling_authority, status, operation_type, authority_appointment_id,
           authority_appointment_version, scheduling_authority_version,
           authority_config_version
    INTO attempt
    FROM booking_attempts
    WHERE id = NEW.booking_attempt_id AND salon_id = NEW.salon_id;

    SELECT scheduling_authority, status, authority_appointment_id,
           authority_appointment_version
    INTO appointment
    FROM appointments
    WHERE id = NEW.appointment_id AND salon_id = NEW.salon_id;

    expected_event_type := CASE attempt.operation_type
        WHEN 'book' THEN 'appointment_confirmed'
        WHEN 'reschedule' THEN 'appointment_rescheduled'
        WHEN 'cancel' THEN 'appointment_cancelled'
        ELSE NULL
    END;

    IF attempt.scheduling_authority IS DISTINCT FROM 'manleai_calendar'
       OR appointment.scheduling_authority IS DISTINCT FROM 'manleai_calendar'
       OR expected_event_type IS DISTINCT FROM NEW.event_type
       OR attempt.status IS DISTINCT FROM appointment.status
       OR attempt.authority_appointment_id IS DISTINCT FROM appointment.authority_appointment_id
       OR attempt.authority_appointment_version IS DISTINCT FROM appointment.authority_appointment_version
       OR attempt.scheduling_authority_version IS DISTINCT FROM NEW.scheduling_authority_version
       OR attempt.authority_config_version IS DISTINCT FROM NEW.authority_config_version
       OR attempt.authority_appointment_version IS DISTINCT FROM NEW.authority_appointment_version THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar execution event does not match committed lifecycle evidence',
            CONSTRAINT = 'manleai_calendar_execution_events_evidence_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER manleai_calendar_execution_events_evidence_guard
AFTER INSERT ON manleai_calendar_execution_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_event();

CREATE FUNCTION enforce_manleai_calendar_execution_event_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'ManleAI Calendar execution events are append-only',
        CONSTRAINT = 'manleai_calendar_execution_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manleai_calendar_execution_events_immutable_guard
BEFORE UPDATE OR DELETE ON manleai_calendar_execution_events
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_event_immutable();

CREATE FUNCTION enforce_manleai_calendar_attempt_committed_evidence()
RETURNS TRIGGER AS $$
DECLARE
    segment_count INTEGER;
    invalid_segment_count INTEGER;
    segment_start TIMESTAMPTZ;
    segment_end TIMESTAMPTZ;
    quote RECORD;
BEGIN
    IF NEW.scheduling_authority <> 'manleai_calendar' THEN
        RETURN NEW;
    END IF;

    SELECT count(*),
           count(*) FILTER (
               WHERE segment.salon_id <> NEW.salon_id
                  OR segment.scheduling_authority <> 'manleai_calendar'
           ),
           min(segment.scheduled_start_time),
           max(segment.scheduled_end_time)
    INTO segment_count, invalid_segment_count, segment_start, segment_end
    FROM booking_attempt_segments segment
    WHERE segment.booking_attempt_id = NEW.id;

    IF segment_count = 0
       OR invalid_segment_count <> 0
       OR segment_start IS DISTINCT FROM NEW.requested_start_time
       OR segment_end IS DISTINCT FROM NEW.requested_end_time THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'committed ManleAI Calendar attempt segments do not match the operation root',
            CONSTRAINT = 'booking_attempts_manleai_calendar_segment_guard';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM booking_attempt_segment_resource_allocations allocation
        JOIN booking_attempt_segments segment
          ON segment.id = allocation.booking_attempt_segment_id
        WHERE segment.booking_attempt_id = NEW.id
          AND (
              allocation.salon_id <> segment.salon_id
              OR segment.scheduling_authority <> 'manleai_calendar'
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'attempt resource evidence must belong to ManleAI Calendar segments',
            CONSTRAINT = 'booking_attempts_manleai_calendar_resource_guard';
    END IF;

    IF NEW.operation_type IN ('book', 'reschedule') THEN
        SELECT scheduling_authority, scheduling_authority_version,
               authority_config_version, operation_type, target_appointment_id,
               target_authority_appointment_version, consumed_at,
               consumed_by_attempt_id
        INTO quote
        FROM availability_quotes
        WHERE id = NEW.availability_quote_id
          AND salon_id = NEW.salon_id;

        IF NOT FOUND
           OR quote.scheduling_authority <> 'manleai_calendar'
           OR quote.scheduling_authority_version <> NEW.scheduling_authority_version
           OR quote.authority_config_version <> NEW.authority_config_version
           OR quote.operation_type <> NEW.operation_type
           OR quote.target_appointment_id IS DISTINCT FROM NEW.target_appointment_id
           OR quote.target_authority_appointment_version IS DISTINCT FROM NEW.target_authority_appointment_version
           OR quote.consumed_at IS NULL
           OR quote.consumed_by_attempt_id IS DISTINCT FROM NEW.id
           OR NOT EXISTS (
               SELECT 1
               FROM availability_quote_slots slot
               WHERE slot.quote_id = NEW.availability_quote_id
                 AND slot.salon_id = NEW.salon_id
                 AND slot.slot_fingerprint = NEW.availability_slot_fingerprint
           ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'committed ManleAI Calendar attempt requires its exact consumed quote fence',
                CONSTRAINT = 'booking_attempts_manleai_calendar_quote_guard';
        END IF;
    END IF;

    IF (NEW.operation_type = 'book' AND NEW.authority_appointment_version <> 1)
       OR (
           NEW.operation_type IN ('reschedule', 'cancel')
           AND NEW.authority_appointment_version <> NEW.target_authority_appointment_version + 1
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment versions must advance exactly once per committed operation',
            CONSTRAINT = 'booking_attempts_manleai_calendar_version_guard';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM manleai_calendar_execution_events event
        WHERE event.salon_id = NEW.salon_id
          AND event.booking_attempt_id = NEW.id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'committed ManleAI Calendar attempts require an execution event',
            CONSTRAINT = 'booking_attempts_manleai_calendar_event_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER booking_attempts_manleai_calendar_event_guard
AFTER INSERT OR UPDATE ON booking_attempts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_attempt_committed_evidence();
