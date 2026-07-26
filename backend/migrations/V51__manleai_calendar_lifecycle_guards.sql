-- Phase 4C extends the V49/V50 internal execution graph with lifecycle
-- history.  The released plan remains the durable prior-plan snapshot; its
-- release owner is the immutable lifecycle attempt that replaced or cancelled
-- it.  Resource release ownership is derived through appointment_services.

ALTER TABLE appointment_services
    ADD COLUMN released_by_attempt_id UUID,
    ADD CONSTRAINT appointment_services_release_owner_pair_check
        CHECK (
            (released_at IS NULL AND released_by_attempt_id IS NULL)
            OR (released_at IS NOT NULL AND released_by_attempt_id IS NOT NULL)
        ),
    ADD CONSTRAINT appointment_services_released_by_attempt_tenant_fk
        FOREIGN KEY (salon_id, released_by_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE manleai_calendar_execution_events
    ADD CONSTRAINT manleai_calendar_execution_events_appointment_version_key
        UNIQUE (salon_id, appointment_id, authority_appointment_version);

CREATE OR REPLACE FUNCTION enforce_manleai_calendar_appointment_history_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME = 'appointment_services' THEN
        IF OLD.scheduling_authority <> 'manleai_calendar' THEN
            IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
            RETURN NEW;
        END IF;
        IF TG_OP = 'DELETE'
           OR OLD.released_at IS NOT NULL
           OR OLD.released_by_attempt_id IS NOT NULL
           OR NEW.released_at IS NULL
           OR NEW.released_by_attempt_id IS NULL
           OR (to_jsonb(NEW) - ARRAY['released_at', 'released_by_attempt_id'])
                IS DISTINCT FROM
              (to_jsonb(OLD) - ARRAY['released_at', 'released_by_attempt_id']) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI Calendar appointment plan history is immutable except for one owned release',
                CONSTRAINT = 'manleai_calendar_appointment_history_immutable_guard';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE'
       OR OLD.released_at IS NOT NULL
       OR NEW.released_at IS NULL
       OR (to_jsonb(NEW) - 'released_at') IS DISTINCT FROM (to_jsonb(OLD) - 'released_at') THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar resource allocation history is immutable except for release',
            CONSTRAINT = 'manleai_calendar_appointment_history_immutable_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_lifecycle_root_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.scheduling_authority <> 'manleai_calendar' THEN
        RETURN NEW;
    END IF;

    IF OLD.salon_id IS DISTINCT FROM NEW.salon_id
       OR OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority
       OR OLD.scheduling_authority_version IS DISTINCT FROM NEW.scheduling_authority_version
       OR OLD.authority_provider IS DISTINCT FROM NEW.authority_provider
       OR OLD.authority_appointment_id IS DISTINCT FROM NEW.authority_appointment_id
       OR OLD.authority_customer_id IS DISTINCT FROM NEW.authority_customer_id
       OR OLD.customer_name IS DISTINCT FROM NEW.customer_name
       OR OLD.customer_phone IS DISTINCT FROM NEW.customer_phone
       OR OLD.customer_email IS DISTINCT FROM NEW.customer_email
       OR OLD.party_size IS DISTINCT FROM NEW.party_size
       OR OLD.created_at IS DISTINCT FROM NEW.created_at
       OR OLD.confirmed_at IS DISTINCT FROM NEW.confirmed_at
       OR OLD.confirmation_source IS DISTINCT FROM NEW.confirmation_source
       OR OLD.confirmed_by_user_id IS DISTINCT FROM NEW.confirmed_by_user_id THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment tenant, origin, customer, party, and confirmation provenance are immutable',
            CONSTRAINT = 'manleai_calendar_lifecycle_root_immutable_guard';
    END IF;

    IF OLD.status = 'cancelled'
       OR OLD.status NOT IN ('confirmed', 'rescheduled')
       OR NEW.status NOT IN ('rescheduled', 'cancelled')
       OR NEW.authority_appointment_version <> OLD.authority_appointment_version + 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment lifecycle must advance one version and cancelled appointments are terminal',
            CONSTRAINT = 'manleai_calendar_lifecycle_root_transition_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER appointments_manleai_calendar_lifecycle_root_immutable_guard
BEFORE UPDATE ON appointments
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_root_immutable();

CREATE FUNCTION validate_manleai_calendar_lifecycle_graph(target_appointment_id UUID)
RETURNS VOID AS $$
DECLARE
    root_item RECORD;
    attempt_item RECORD;
    quote_item RECORD;
    expected_event_type TEXT;
    expected_operation_type TEXT;
    event_count INTEGER;
BEGIN
    SELECT appointment.*
    INTO root_item
    FROM appointments appointment
    WHERE appointment.id = target_appointment_id;

    IF NOT FOUND OR root_item.scheduling_authority <> 'manleai_calendar' THEN
        RETURN;
    END IF;

    IF root_item.authority_appointment_id IS DISTINCT FROM root_item.id::TEXT
       OR root_item.authority_appointment_version < 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment root must retain its internal origin',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;

    expected_operation_type := CASE root_item.status
        WHEN 'confirmed' THEN 'book'
        WHEN 'rescheduled' THEN 'reschedule'
        WHEN 'cancelled' THEN 'cancel'
        ELSE NULL
    END;
    expected_event_type := CASE root_item.status
        WHEN 'confirmed' THEN 'appointment_confirmed'
        WHEN 'rescheduled' THEN 'appointment_rescheduled'
        WHEN 'cancelled' THEN 'appointment_cancelled'
        ELSE NULL
    END;

    SELECT attempt.*
    INTO attempt_item
    FROM booking_attempts attempt
    WHERE attempt.salon_id = root_item.salon_id
      AND attempt.id = root_item.booking_attempt_id;

    IF NOT FOUND
       OR expected_operation_type IS NULL
       OR attempt_item.scheduling_authority <> 'manleai_calendar'
       OR attempt_item.operation_type <> expected_operation_type
       OR attempt_item.status <> root_item.status
       OR attempt_item.authority_appointment_id IS DISTINCT FROM root_item.id::TEXT
       OR attempt_item.authority_appointment_version <> root_item.authority_appointment_version
       OR attempt_item.scheduling_authority_version <> root_item.scheduling_authority_version
       OR attempt_item.authority_config_version <> root_item.authority_config_version
	   OR attempt_item.party_size <> root_item.party_size
	   OR attempt_item.customer_name IS DISTINCT FROM root_item.customer_name
	   OR attempt_item.customer_phone IS DISTINCT FROM root_item.customer_phone
	   OR attempt_item.customer_email IS DISTINCT FROM root_item.customer_email
       OR (root_item.authority_appointment_version = 1
           AND (attempt_item.target_appointment_id IS NOT NULL
                OR attempt_item.target_authority_appointment_version IS NOT NULL))
       OR (root_item.authority_appointment_version > 1
           AND (attempt_item.target_appointment_id IS DISTINCT FROM root_item.id
                OR attempt_item.target_authority_appointment_version <> root_item.authority_appointment_version - 1)) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment root must point to its latest exact lifecycle attempt',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;

    SELECT count(*)
    INTO event_count
    FROM manleai_calendar_execution_events event
    WHERE event.salon_id = root_item.salon_id
      AND event.appointment_id = root_item.id;

    IF event_count <> root_item.authority_appointment_version
       OR EXISTS (
            SELECT 1
            FROM generate_series(1, root_item.authority_appointment_version) version_item(version)
            LEFT JOIN manleai_calendar_execution_events event
              ON event.salon_id = root_item.salon_id
             AND event.appointment_id = root_item.id
             AND event.authority_appointment_version = version_item.version
            WHERE event.id IS NULL
       )
       OR NOT EXISTS (
            SELECT 1
            FROM manleai_calendar_execution_events event
            WHERE event.salon_id = root_item.salon_id
              AND event.appointment_id = root_item.id
              AND event.booking_attempt_id = attempt_item.id
              AND event.event_type = expected_event_type
              AND event.authority_appointment_version = root_item.authority_appointment_version
              AND event.scheduling_authority_version = root_item.scheduling_authority_version
              AND event.authority_config_version = root_item.authority_config_version
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar appointment lifecycle requires one durable event for every version',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;

    IF root_item.authority_appointment_version = 1 THEN
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM appointment_services segment
        WHERE segment.salon_id = root_item.salon_id
          AND segment.appointment_id = root_item.id
          AND segment.plan_version = root_item.authority_appointment_version - 1
    ) OR EXISTS (
        SELECT 1
        FROM appointment_services segment
        WHERE segment.salon_id = root_item.salon_id
          AND segment.appointment_id = root_item.id
          AND segment.plan_version = root_item.authority_appointment_version - 1
          AND (segment.released_at IS NULL
               OR segment.released_by_attempt_id IS DISTINCT FROM attempt_item.id)
    ) OR EXISTS (
        SELECT 1
        FROM manleai_calendar_appointment_resource_allocations allocation
        JOIN appointment_services segment
          ON segment.salon_id = allocation.salon_id
         AND segment.id = allocation.appointment_service_id
        WHERE segment.salon_id = root_item.salon_id
          AND segment.appointment_id = root_item.id
          AND segment.plan_version = root_item.authority_appointment_version - 1
          AND (allocation.released_at IS DISTINCT FROM segment.released_at
               OR segment.released_by_attempt_id IS DISTINCT FROM attempt_item.id)
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar lifecycle must release every prior plan and resource through its lifecycle attempt',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;

    IF root_item.status = 'cancelled' THEN
        IF NOT EXISTS (
            SELECT 1
            FROM manleai_calendar_execution_events event
            WHERE event.salon_id = root_item.salon_id
              AND event.appointment_id = root_item.id
              AND event.authority_appointment_version = root_item.authority_appointment_version - 1
              AND event.authority_config_version = attempt_item.authority_config_version
        ) OR EXISTS (
            SELECT 1
            FROM appointment_services segment
            WHERE segment.appointment_id = root_item.id
              AND segment.released_at IS NULL
		) OR EXISTS (
			SELECT 1
			FROM appointment_services segment
			WHERE segment.appointment_id = root_item.id
			  AND segment.plan_version = root_item.authority_appointment_version
        ) OR EXISTS (
            SELECT 1
            FROM (
                (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                        segment.staff_selection_mode, segment.guest_reference,
                        segment.duration_minutes, segment.scheduled_start_time,
                        segment.scheduled_end_time, segment.buffer_before_minutes,
                        segment.buffer_after_minutes, segment.occupied_start_time,
                        segment.occupied_end_time
                 FROM appointment_services segment
                 WHERE segment.appointment_id = root_item.id
                   AND segment.plan_version = root_item.authority_appointment_version - 1)
                EXCEPT
                (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                        segment.staff_selection_mode, segment.guest_reference,
                        segment.duration_minutes, segment.scheduled_start_time,
                        segment.scheduled_end_time, segment.buffer_before_minutes,
                        segment.buffer_after_minutes, segment.occupied_start_time,
                        segment.occupied_end_time
                 FROM booking_attempt_segments segment
                 WHERE segment.booking_attempt_id = attempt_item.id)
            ) missing_cancel_snapshot
        ) OR EXISTS (
            SELECT 1
            FROM (
                (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                        segment.staff_selection_mode, segment.guest_reference,
                        segment.duration_minutes, segment.scheduled_start_time,
                        segment.scheduled_end_time, segment.buffer_before_minutes,
                        segment.buffer_after_minutes, segment.occupied_start_time,
                        segment.occupied_end_time
                 FROM booking_attempt_segments segment
                 WHERE segment.booking_attempt_id = attempt_item.id)
                EXCEPT
                (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                        segment.staff_selection_mode, segment.guest_reference,
                        segment.duration_minutes, segment.scheduled_start_time,
                        segment.scheduled_end_time, segment.buffer_before_minutes,
                        segment.buffer_after_minutes, segment.occupied_start_time,
                        segment.occupied_end_time
                 FROM appointment_services segment
                 WHERE segment.appointment_id = root_item.id
                   AND segment.plan_version = root_item.authority_appointment_version - 1)
            ) extra_cancel_snapshot
        ) OR EXISTS (
            SELECT 1
            FROM (
                (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                 FROM appointment_services segment
                 JOIN manleai_calendar_appointment_resource_allocations allocation
                   ON allocation.appointment_service_id = segment.id
                 WHERE segment.appointment_id = root_item.id
                   AND segment.plan_version = root_item.authority_appointment_version - 1)
                EXCEPT
                (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                 FROM booking_attempt_segments segment
                 JOIN booking_attempt_segment_resource_allocations allocation
                   ON allocation.booking_attempt_segment_id = segment.id
                 WHERE segment.booking_attempt_id = attempt_item.id)
            ) missing_cancel_resource_snapshot
        ) OR EXISTS (
            SELECT 1
            FROM (
                (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                 FROM booking_attempt_segments segment
                 JOIN booking_attempt_segment_resource_allocations allocation
                   ON allocation.booking_attempt_segment_id = segment.id
                 WHERE segment.booking_attempt_id = attempt_item.id)
                EXCEPT
                (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                 FROM appointment_services segment
                 JOIN manleai_calendar_appointment_resource_allocations allocation
                   ON allocation.appointment_service_id = segment.id
                 WHERE segment.appointment_id = root_item.id
                   AND segment.plan_version = root_item.authority_appointment_version - 1)
            ) extra_cancel_resource_snapshot
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI Calendar cancellation requires its exact prior-plan snapshot and no active plan',
                CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
        END IF;
        RETURN;
    END IF;

    SELECT quote.*
    INTO quote_item
    FROM availability_quotes quote
    WHERE quote.salon_id = root_item.salon_id
      AND quote.id = attempt_item.availability_quote_id;

    IF NOT FOUND
       OR quote_item.scheduling_authority <> 'manleai_calendar'
       OR quote_item.operation_type <> 'reschedule'
       OR quote_item.target_appointment_id IS DISTINCT FROM root_item.id
       OR quote_item.target_authority_appointment_version <> root_item.authority_appointment_version - 1
       OR quote_item.scheduling_authority_version <> attempt_item.scheduling_authority_version
       OR quote_item.authority_config_version <> attempt_item.authority_config_version
       OR quote_item.consumed_at IS NULL
       OR quote_item.consumed_at >= quote_item.expires_at
       OR quote_item.consumed_by_attempt_id IS DISTINCT FROM attempt_item.id
       OR quote_item.party_size <> attempt_item.party_size
       OR NOT EXISTS (
            SELECT 1
            FROM manleai_calendar_configs config
            WHERE config.salon_id = root_item.salon_id
              AND config.version = quote_item.authority_config_version
              AND config.activated_version = config.version
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar reschedule requires its exact current activated quote',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;

    PERFORM validate_manleai_calendar_quote_resource_integrity(quote_item.id);

    IF NOT EXISTS (
        SELECT 1
        FROM appointment_services segment
        WHERE segment.appointment_id = root_item.id
          AND segment.released_at IS NULL
          AND segment.plan_version = root_item.authority_appointment_version
    ) OR EXISTS (
        SELECT 1
        FROM appointment_services segment
        WHERE segment.appointment_id = root_item.id
          AND segment.released_at IS NULL
          AND segment.plan_version <> root_item.authority_appointment_version
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM availability_quote_slot_segments segment
             JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
             WHERE slot.quote_id = quote_item.id
               AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint)
            EXCEPT
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM booking_attempt_segments segment
             WHERE segment.booking_attempt_id = attempt_item.id)
        ) missing_reschedule_attempt_segment
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM booking_attempt_segments segment
             WHERE segment.booking_attempt_id = attempt_item.id)
            EXCEPT
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM availability_quote_slot_segments segment
             JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
             WHERE slot.quote_id = quote_item.id
               AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint)
        ) extra_reschedule_quote_segment
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM booking_attempt_segments segment
             WHERE segment.booking_attempt_id = attempt_item.id)
            EXCEPT
            (SELECT segment.sort_order, segment.service_id, segment.staff_id,
                    segment.staff_selection_mode, segment.guest_reference,
                    segment.duration_minutes, segment.scheduled_start_time,
                    segment.scheduled_end_time, segment.buffer_before_minutes,
                    segment.buffer_after_minutes, segment.occupied_start_time,
                    segment.occupied_end_time
             FROM appointment_services segment
             WHERE segment.appointment_id = root_item.id
               AND segment.plan_version = root_item.authority_appointment_version
               AND segment.released_at IS NULL)
        ) extra_reschedule_attempt_segment
    ) OR EXISTS (
        SELECT 1
        FROM appointment_services segment
        WHERE segment.appointment_id = root_item.id
          AND segment.plan_version = root_item.authority_appointment_version
          AND segment.released_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM booking_attempt_segments attempt_segment
              WHERE attempt_segment.booking_attempt_id = attempt_item.id
                AND attempt_segment.sort_order = segment.sort_order
                AND attempt_segment.service_id = segment.service_id
                AND attempt_segment.staff_id = segment.staff_id
                AND attempt_segment.staff_selection_mode = segment.staff_selection_mode
                AND attempt_segment.guest_reference IS NOT DISTINCT FROM segment.guest_reference
                AND attempt_segment.duration_minutes = segment.duration_minutes
                AND attempt_segment.scheduled_start_time = segment.scheduled_start_time
                AND attempt_segment.scheduled_end_time = segment.scheduled_end_time
                AND attempt_segment.buffer_before_minutes = segment.buffer_before_minutes
                AND attempt_segment.buffer_after_minutes = segment.buffer_after_minutes
                AND attempt_segment.occupied_start_time = segment.occupied_start_time
                AND attempt_segment.occupied_end_time = segment.occupied_end_time
          )
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM booking_attempt_segments segment
             JOIN booking_attempt_segment_resource_allocations allocation
               ON allocation.booking_attempt_segment_id = segment.id
             WHERE segment.booking_attempt_id = attempt_item.id)
            EXCEPT
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM availability_quote_slot_segments segment
             JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
             JOIN availability_quote_slot_resource_allocations allocation
               ON allocation.quote_slot_segment_id = segment.id
             WHERE slot.quote_id = quote_item.id
               AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint)
        ) extra_reschedule_quote_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM availability_quote_slot_segments segment
             JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
             JOIN availability_quote_slot_resource_allocations allocation
               ON allocation.quote_slot_segment_id = segment.id
             WHERE slot.quote_id = quote_item.id
               AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint)
            EXCEPT
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM booking_attempt_segments segment
             JOIN booking_attempt_segment_resource_allocations allocation
               ON allocation.booking_attempt_segment_id = segment.id
             WHERE segment.booking_attempt_id = attempt_item.id)
        ) missing_reschedule_attempt_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM booking_attempt_segments segment
             JOIN booking_attempt_segment_resource_allocations allocation
               ON allocation.booking_attempt_segment_id = segment.id
             WHERE segment.booking_attempt_id = attempt_item.id)
            EXCEPT
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM appointment_services segment
             JOIN manleai_calendar_appointment_resource_allocations allocation
               ON allocation.appointment_service_id = segment.id
             WHERE segment.appointment_id = root_item.id
               AND segment.plan_version = root_item.authority_appointment_version
               AND segment.released_at IS NULL)
        ) extra_reschedule_attempt_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM appointment_services segment
             JOIN manleai_calendar_appointment_resource_allocations allocation
               ON allocation.appointment_service_id = segment.id
             WHERE segment.appointment_id = root_item.id
               AND segment.plan_version = root_item.authority_appointment_version
               AND segment.released_at IS NULL)
            EXCEPT
            (SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
             FROM availability_quote_slot_segments segment
             JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
             JOIN availability_quote_slot_resource_allocations allocation
               ON allocation.quote_slot_segment_id = segment.id
             WHERE slot.quote_id = quote_item.id
               AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint)
        ) extra_reschedule_plan_resource
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar reschedule quote, attempt, and new plan must match exactly',
            CONSTRAINT = 'manleai_calendar_lifecycle_graph_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_lifecycle_graph()
RETURNS TRIGGER AS $$
DECLARE
    appointment_id_to_check UUID;
BEGIN
    IF TG_TABLE_NAME = 'appointments' THEN
        appointment_id_to_check := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'appointment_services' THEN
        appointment_id_to_check := COALESCE(NEW.appointment_id, OLD.appointment_id);
    ELSIF TG_TABLE_NAME = 'manleai_calendar_appointment_resource_allocations' THEN
        SELECT segment.appointment_id
        INTO appointment_id_to_check
        FROM appointment_services segment
        WHERE segment.id = COALESCE(NEW.appointment_service_id, OLD.appointment_service_id);
    ELSIF TG_TABLE_NAME = 'manleai_calendar_execution_events' THEN
        appointment_id_to_check := NEW.appointment_id;
    ELSIF TG_TABLE_NAME = 'booking_attempts' THEN
        SELECT appointment.id
        INTO appointment_id_to_check
        FROM appointments appointment
        WHERE appointment.salon_id = COALESCE(NEW.salon_id, OLD.salon_id)
          AND appointment.authority_appointment_id = COALESCE(NEW.authority_appointment_id, OLD.authority_appointment_id);
    ELSIF TG_TABLE_NAME = 'booking_attempt_segments' THEN
        SELECT appointment.id
        INTO appointment_id_to_check
        FROM booking_attempts attempt
        JOIN appointments appointment
          ON appointment.salon_id = attempt.salon_id
         AND appointment.authority_appointment_id = attempt.authority_appointment_id
        WHERE attempt.id = COALESCE(NEW.booking_attempt_id, OLD.booking_attempt_id);
    ELSE
        SELECT appointment.id
        INTO appointment_id_to_check
        FROM booking_attempt_segment_resource_allocations allocation
        JOIN booking_attempt_segments segment ON segment.id = allocation.booking_attempt_segment_id
        JOIN booking_attempts attempt ON attempt.id = segment.booking_attempt_id
        JOIN appointments appointment
          ON appointment.salon_id = attempt.salon_id
         AND appointment.authority_appointment_id = attempt.authority_appointment_id
        WHERE allocation.id = COALESCE(NEW.id, OLD.id);
    END IF;

    IF appointment_id_to_check IS NOT NULL THEN
        PERFORM validate_manleai_calendar_lifecycle_graph(appointment_id_to_check);
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER appointments_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE ON appointments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER appointment_services_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON appointment_services
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER appointment_resources_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_appointment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER booking_attempts_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE ON booking_attempts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER booking_attempt_segments_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON booking_attempt_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER booking_attempt_resources_manleai_calendar_lifecycle_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON booking_attempt_segment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();

CREATE CONSTRAINT TRIGGER execution_events_manleai_calendar_lifecycle_graph_guard
AFTER INSERT ON manleai_calendar_execution_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_lifecycle_graph();
