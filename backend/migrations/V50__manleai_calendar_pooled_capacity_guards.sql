-- Phase 4B keeps V48 configuration and V49 execution evidence as the source of
-- truth. This migration adds aggregate resource-capacity and exact committed
-- book-evidence guards without introducing a second reservation ledger.

CREATE INDEX idx_manleai_calendar_resource_capacity_exceptions
    ON manleai_calendar_exceptions
    USING gist (
        salon_id,
        resource_pool_id,
        tstzrange(starts_at, ends_at, '[)')
    )
    WHERE (
        scope_type = 'resource'
        AND effect = 'capacity_override'
        AND cancelled_at IS NULL
    );

CREATE FUNCTION lock_manleai_calendar_resource_pools(
    target_salon_id UUID,
    target_pool_ids UUID[]
)
RETURNS VOID AS $$
DECLARE
    expected_pool_count INTEGER;
    locked_pool_count INTEGER := 0;
    locked_pool_id UUID;
BEGIN
    IF target_salon_id IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'resource pool locking requires a salon',
            CONSTRAINT = 'manleai_calendar_resource_pool_lock_guard';
    END IF;

    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar resource claims require READ COMMITTED isolation',
            CONSTRAINT = 'manleai_calendar_resource_pool_lock_guard';
    END IF;

    PERFORM pg_advisory_xact_lock(
        hashtextextended('booking-calendar-reconciliation:' || target_salon_id::TEXT, 0)
    );

    SELECT count(*)
    INTO expected_pool_count
    FROM (
        SELECT DISTINCT pool_id
        FROM unnest(COALESCE(target_pool_ids, ARRAY[]::UUID[])) AS requested(pool_id)
        WHERE pool_id IS NOT NULL
    ) requested;

    FOR locked_pool_id IN
        SELECT pool.id
        FROM manleai_calendar_resource_pools pool
        JOIN (
            SELECT DISTINCT pool_id
            FROM unnest(COALESCE(target_pool_ids, ARRAY[]::UUID[])) AS requested(pool_id)
            WHERE pool_id IS NOT NULL
        ) requested ON requested.pool_id = pool.id
        WHERE pool.salon_id = target_salon_id
          AND pool.archived_at IS NULL
        ORDER BY pool.id
        FOR UPDATE OF pool
    LOOP
        locked_pool_count := locked_pool_count + 1;
    END LOOP;

    IF locked_pool_count <> expected_pool_count THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'resource pools must be active and belong to the booking salon',
            CONSTRAINT = 'manleai_calendar_resource_pool_lock_guard';
    END IF;
END;
$$ LANGUAGE plpgsql VOLATILE;

CREATE FUNCTION validate_manleai_calendar_quote_resource_integrity(target_quote_id UUID)
RETURNS VOID AS $$
DECLARE
    quote_item RECORD;
BEGIN
    SELECT quote.salon_id, quote.scheduling_authority,
           quote.scheduling_authority_version, quote.authority_config_version,
           quote.operation_type, quote.party_size
    INTO quote_item
    FROM availability_quotes quote
    WHERE quote.id = target_quote_id;

    IF NOT FOUND OR quote_item.scheduling_authority <> 'manleai_calendar' THEN
        RETURN;
    END IF;

    PERFORM validate_manleai_calendar_quote_evidence(target_quote_id);

    IF NOT EXISTS (
        SELECT 1
        FROM manleai_calendar_configs config
        WHERE config.salon_id = quote_item.salon_id
          AND config.version = quote_item.authority_config_version
          AND config.activated_version = config.version
    ) OR (
        quote_item.operation_type = 'book'
        AND NOT EXISTS (
            SELECT 1
            FROM salon_settings settings
            WHERE settings.salon_id = quote_item.salon_id
              AND settings.scheduling_authority = 'manleai_calendar'
              AND settings.scheduling_authority_version = quote_item.scheduling_authority_version
        )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar quote configuration fence is stale',
            CONSTRAINT = 'availability_quotes_manleai_calendar_resource_guard';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM availability_quote_slots slot
        LEFT JOIN availability_quote_slot_segments segment
          ON segment.quote_slot_id = slot.id
        WHERE slot.quote_id = target_quote_id
        GROUP BY slot.id
        HAVING count(segment.id) = 0
            OR min(segment.sort_order) <> 1
            OR max(segment.sort_order) <> count(segment.id)
            OR (
                count(*) FILTER (
                    WHERE segment.guest_reference IS NOT NULL
                ) > 0
                AND (
                    count(*) FILTER (
                        WHERE segment.guest_reference IS NULL
                           OR length(trim(segment.guest_reference)) = 0
                    ) > 0
                    OR count(DISTINCT trim(segment.guest_reference)) <> quote_item.party_size
                )
            )
            OR (
                count(*) FILTER (
                    WHERE segment.guest_reference IS NOT NULL
                ) = 0
                AND quote_item.party_size <> 1
            )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar quote party evidence is incomplete',
            CONSTRAINT = 'availability_quotes_manleai_calendar_party_guard';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM availability_quote_slots slot
        JOIN availability_quote_slot_segments segment ON segment.quote_slot_id = slot.id
        LEFT JOIN services service
          ON service.salon_id = segment.salon_id AND service.id = segment.service_id
        LEFT JOIN staff staff_member
          ON staff_member.salon_id = segment.salon_id AND staff_member.id = segment.staff_id
        LEFT JOIN manleai_calendar_service_policies policy
          ON policy.salon_id = segment.salon_id AND policy.service_id = segment.service_id
        WHERE slot.quote_id = target_quote_id
          AND (
              segment.salon_id <> quote_item.salon_id
              OR service.id IS NULL
              OR service.active = false
              OR service.ai_bookable = false
              OR service.archived_at IS NOT NULL
              OR staff_member.id IS NULL
              OR staff_member.active = false
              OR staff_member.ai_bookable = false
              OR staff_member.archived_at IS NOT NULL
              OR policy.enabled IS DISTINCT FROM true
              OR policy.capacity_mode NOT IN ('staff_only', 'pooled')
              OR (
                  policy.capacity_mode = 'staff_only'
                  AND EXISTS (
                      SELECT 1
                      FROM manleai_calendar_service_resources requirement
                      WHERE requirement.salon_id = segment.salon_id
                        AND requirement.service_id = segment.service_id
                  )
              )
              OR (
                  policy.capacity_mode = 'pooled'
                  AND NOT EXISTS (
                      SELECT 1
                      FROM manleai_calendar_service_resources requirement
                      WHERE requirement.salon_id = segment.salon_id
                        AND requirement.service_id = segment.service_id
                  )
              )
              OR NOT EXISTS (
                  SELECT 1
                  FROM manleai_calendar_service_staff service_staff
                  WHERE service_staff.salon_id = segment.salon_id
                    AND service_staff.service_id = segment.service_id
                    AND service_staff.staff_id = segment.staff_id
              )
          )
    ) OR EXISTS (
        SELECT 1
        FROM availability_quote_slots slot
        JOIN availability_quote_slot_segments segment ON segment.quote_slot_id = slot.id
        JOIN availability_quote_slot_resource_allocations allocation
          ON allocation.quote_slot_segment_id = segment.id
        LEFT JOIN manleai_calendar_service_resources requirement
          ON requirement.salon_id = allocation.salon_id
         AND requirement.service_id = segment.service_id
         AND requirement.resource_pool_id = allocation.resource_pool_id
        LEFT JOIN manleai_calendar_resource_pools pool
          ON pool.salon_id = allocation.salon_id
         AND pool.id = allocation.resource_pool_id
        WHERE slot.quote_id = target_quote_id
          AND (
              requirement.resource_pool_id IS NULL
              OR requirement.units_required <> allocation.units_allocated
              OR pool.id IS NULL
              OR pool.archived_at IS NOT NULL
          )
    ) OR EXISTS (
        SELECT 1
        FROM availability_quote_slots slot
        JOIN availability_quote_slot_segments segment ON segment.quote_slot_id = slot.id
        JOIN manleai_calendar_service_resources requirement
          ON requirement.salon_id = segment.salon_id
         AND requirement.service_id = segment.service_id
        LEFT JOIN availability_quote_slot_resource_allocations allocation
          ON allocation.salon_id = requirement.salon_id
         AND allocation.quote_slot_segment_id = segment.id
         AND allocation.resource_pool_id = requirement.resource_pool_id
         AND allocation.units_allocated = requirement.units_required
        WHERE slot.quote_id = target_quote_id
          AND allocation.id IS NULL
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar quote resources must match active service requirements',
            CONSTRAINT = 'availability_quotes_manleai_calendar_resource_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_quote_resource_integrity()
RETURNS TRIGGER AS $$
DECLARE
    quote_id_to_check UUID;
BEGIN
    IF TG_TABLE_NAME = 'availability_quotes' THEN
        quote_id_to_check := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'availability_quote_slots' THEN
        quote_id_to_check := COALESCE(NEW.quote_id, OLD.quote_id);
    ELSIF TG_TABLE_NAME = 'availability_quote_slot_segments' THEN
        SELECT slot.quote_id
        INTO quote_id_to_check
        FROM availability_quote_slots slot
        WHERE slot.id = COALESCE(NEW.quote_slot_id, OLD.quote_slot_id);
    ELSE
        SELECT slot.quote_id
        INTO quote_id_to_check
        FROM availability_quote_slot_segments segment
        JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
        WHERE segment.id = COALESCE(NEW.quote_slot_segment_id, OLD.quote_slot_segment_id);
    END IF;

    IF quote_id_to_check IS NOT NULL THEN
        PERFORM validate_manleai_calendar_quote_resource_integrity(quote_id_to_check);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER availability_quotes_manleai_calendar_resource_guard
AFTER INSERT OR UPDATE ON availability_quotes
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_resource_integrity();

CREATE CONSTRAINT TRIGGER availability_quote_slots_manleai_calendar_resource_guard
AFTER INSERT OR UPDATE OR DELETE ON availability_quote_slots
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_resource_integrity();

CREATE CONSTRAINT TRIGGER availability_quote_slot_segments_manleai_calendar_resource_guard
AFTER INSERT OR UPDATE OR DELETE ON availability_quote_slot_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_resource_integrity();

CREATE CONSTRAINT TRIGGER availability_quote_slot_resource_allocations_manleai_calendar_guard
AFTER INSERT OR UPDATE OR DELETE ON availability_quote_slot_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_quote_resource_integrity();

CREATE FUNCTION validate_manleai_calendar_resource_capacity(
    target_salon_id UUID,
    target_appointment_id UUID
)
RETURNS VOID AS $$
DECLARE
    target_pool_ids UUID[];
BEGIN
    SELECT array_agg(DISTINCT allocation.resource_pool_id ORDER BY allocation.resource_pool_id)
    INTO target_pool_ids
    FROM manleai_calendar_appointment_resource_allocations allocation
    JOIN appointment_services segment
      ON segment.salon_id = allocation.salon_id
     AND segment.id = allocation.appointment_service_id
    WHERE allocation.salon_id = target_salon_id
      AND segment.appointment_id = target_appointment_id
      AND allocation.released_at IS NULL;

    IF COALESCE(cardinality(target_pool_ids), 0) = 0 THEN
        RETURN;
    END IF;

    PERFORM lock_manleai_calendar_resource_pools(target_salon_id, target_pool_ids);

    IF EXISTS (
        WITH candidate_claims AS (
            SELECT allocation.resource_pool_id,
                   tstzrange(allocation.occupied_start_time, allocation.occupied_end_time, '[)') AS claim_range
            FROM manleai_calendar_appointment_resource_allocations allocation
            JOIN appointment_services segment
              ON segment.salon_id = allocation.salon_id
             AND segment.id = allocation.appointment_service_id
            WHERE allocation.salon_id = target_salon_id
              AND segment.appointment_id = target_appointment_id
              AND allocation.released_at IS NULL
        ),
        candidate_windows AS (
            SELECT DISTINCT resource_pool_id, claim_range
            FROM candidate_claims
        ),
        relevant_allocations AS (
            SELECT allocation.resource_pool_id,
                   allocation.units_allocated,
                   tstzrange(allocation.occupied_start_time, allocation.occupied_end_time, '[)') AS claim_range
            FROM manleai_calendar_appointment_resource_allocations allocation
            WHERE allocation.salon_id = target_salon_id
              AND allocation.released_at IS NULL
              AND EXISTS (
                  SELECT 1
                  FROM candidate_windows candidate
                  WHERE candidate.resource_pool_id = allocation.resource_pool_id
                    AND candidate.claim_range && tstzrange(
                        allocation.occupied_start_time,
                        allocation.occupied_end_time,
                        '[)'
                    )
              )
        ),
        relevant_overrides AS (
            SELECT exception.resource_pool_id,
                   exception.capacity_override,
                   tstzrange(exception.starts_at, exception.ends_at, '[)') AS override_range
            FROM manleai_calendar_exceptions exception
            WHERE exception.salon_id = target_salon_id
              AND exception.scope_type = 'resource'
              AND exception.effect = 'capacity_override'
              AND exception.cancelled_at IS NULL
              AND EXISTS (
                  SELECT 1
                  FROM candidate_windows candidate
                  WHERE candidate.resource_pool_id = exception.resource_pool_id
                    AND candidate.claim_range && tstzrange(exception.starts_at, exception.ends_at, '[)')
              )
        ),
        boundaries AS (
            SELECT resource_pool_id, lower(claim_range) AS boundary FROM candidate_windows
            UNION
            SELECT resource_pool_id, upper(claim_range) FROM candidate_windows
            UNION
            SELECT resource_pool_id, lower(claim_range) FROM relevant_allocations
            UNION
            SELECT resource_pool_id, upper(claim_range) FROM relevant_allocations
            UNION
            SELECT resource_pool_id, lower(override_range) FROM relevant_overrides
            UNION
            SELECT resource_pool_id, upper(override_range) FROM relevant_overrides
        ),
        probe_points AS (
            SELECT DISTINCT boundary.resource_pool_id, boundary.boundary
            FROM boundaries boundary
            WHERE EXISTS (
                SELECT 1
                FROM candidate_windows candidate
                WHERE candidate.resource_pool_id = boundary.resource_pool_id
                  AND candidate.claim_range @> boundary.boundary
            )
        ),
        usage_at_boundary AS (
            SELECT probe.resource_pool_id,
                   probe.boundary,
                   COALESCE(sum(allocation.units_allocated), 0)::BIGINT AS units_used
            FROM probe_points probe
            LEFT JOIN relevant_allocations allocation
              ON allocation.resource_pool_id = probe.resource_pool_id
             AND allocation.claim_range @> probe.boundary
            GROUP BY probe.resource_pool_id, probe.boundary
        )
        SELECT 1
        FROM usage_at_boundary usage
        JOIN manleai_calendar_resource_pools pool
          ON pool.salon_id = target_salon_id
         AND pool.id = usage.resource_pool_id
        LEFT JOIN relevant_overrides override_item
          ON override_item.resource_pool_id = usage.resource_pool_id
         AND override_item.override_range @> usage.boundary
        WHERE usage.units_used > COALESCE(override_item.capacity_override, pool.capacity)
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ManleAI Calendar resource capacity is exceeded',
            CONSTRAINT = 'manleai_calendar_appointment_resource_capacity_guard';
    END IF;
END;
$$ LANGUAGE plpgsql VOLATILE;

CREATE FUNCTION enforce_manleai_calendar_resource_claim_lock()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.released_at IS NULL THEN
        PERFORM lock_manleai_calendar_resource_pools(
            NEW.salon_id,
            ARRAY[NEW.resource_pool_id]::UUID[]
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manleai_calendar_appointment_resource_claim_lock
BEFORE INSERT ON manleai_calendar_appointment_resource_allocations
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_resource_claim_lock();

CREATE FUNCTION enforce_manleai_calendar_resource_capacity()
RETURNS TRIGGER AS $$
DECLARE
    appointment_id_to_check UUID;
BEGIN
    SELECT segment.appointment_id
    INTO appointment_id_to_check
    FROM appointment_services segment
    WHERE segment.salon_id = NEW.salon_id
      AND segment.id = NEW.appointment_service_id;

    IF appointment_id_to_check IS NOT NULL THEN
        PERFORM validate_manleai_calendar_resource_capacity(
            NEW.salon_id,
            appointment_id_to_check
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER manleai_calendar_appointment_resource_capacity_guard
AFTER INSERT ON manleai_calendar_appointment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_resource_capacity();

CREATE FUNCTION validate_manleai_calendar_execution_graph(target_attempt_id UUID)
RETURNS VOID AS $$
DECLARE
    attempt_item RECORD;
    quote_item RECORD;
    slot_id_to_check UUID;
    appointment_id_to_check UUID;
    appointment_party_size SMALLINT;
BEGIN
    SELECT attempt.*
    INTO attempt_item
    FROM booking_attempts attempt
    WHERE attempt.id = target_attempt_id;

    IF NOT FOUND
       OR attempt_item.scheduling_authority <> 'manleai_calendar'
       OR attempt_item.operation_type <> 'book' THEN
        RETURN;
    END IF;

    IF attempt_item.status <> 'confirmed'
       OR attempt_item.authority_appointment_version <> 1
       OR NOT EXISTS (
           SELECT 1
           FROM salon_settings settings
           JOIN manleai_calendar_configs config ON config.salon_id = settings.salon_id
           WHERE settings.salon_id = attempt_item.salon_id
             AND settings.scheduling_authority = 'manleai_calendar'
             AND settings.scheduling_authority_version = attempt_item.scheduling_authority_version
             AND config.version = attempt_item.authority_config_version
             AND config.activated_version = config.version
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'new ManleAI Calendar books require the current activated authority fence',
            CONSTRAINT = 'booking_attempts_manleai_calendar_book_graph_guard';
    END IF;

    SELECT quote.*
    INTO quote_item
    FROM availability_quotes quote
    JOIN availability_quote_slots slot
      ON slot.salon_id = quote.salon_id
     AND slot.quote_id = quote.id
     AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint
    WHERE quote.id = attempt_item.availability_quote_id
      AND quote.salon_id = attempt_item.salon_id;

    IF NOT FOUND
       OR quote_item.scheduling_authority <> 'manleai_calendar'
       OR quote_item.operation_type <> 'book'
       OR quote_item.scheduling_authority_version <> attempt_item.scheduling_authority_version
       OR quote_item.authority_config_version <> attempt_item.authority_config_version
       OR quote_item.consumed_at IS NULL
       OR quote_item.consumed_at >= quote_item.expires_at
       OR quote_item.consumed_by_attempt_id IS DISTINCT FROM attempt_item.id
       OR quote_item.party_size <> attempt_item.party_size THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'committed ManleAI Calendar book requires its exact consumed quote',
            CONSTRAINT = 'booking_attempts_manleai_calendar_book_graph_guard';
    END IF;

    SELECT slot.id
    INTO slot_id_to_check
    FROM availability_quote_slots slot
    WHERE slot.salon_id = quote_item.salon_id
      AND slot.quote_id = quote_item.id
      AND slot.slot_fingerprint = attempt_item.availability_slot_fingerprint;

    PERFORM validate_manleai_calendar_quote_resource_integrity(quote_item.id);

    SELECT event.appointment_id, appointment.party_size
    INTO appointment_id_to_check, appointment_party_size
    FROM manleai_calendar_execution_events event
    JOIN appointments appointment
      ON appointment.salon_id = event.salon_id
     AND appointment.id = event.appointment_id
    WHERE event.salon_id = attempt_item.salon_id
      AND event.booking_attempt_id = attempt_item.id
      AND event.event_type = 'appointment_confirmed'
      AND appointment.booking_attempt_id = attempt_item.id
      AND appointment.scheduling_authority = 'manleai_calendar'
      AND appointment.status = 'confirmed'
      AND appointment.authority_appointment_version = 1
      AND appointment.scheduling_authority_version = attempt_item.scheduling_authority_version
      AND appointment.authority_config_version = attempt_item.authority_config_version;

    IF NOT FOUND
       OR appointment_id_to_check::TEXT IS DISTINCT FROM attempt_item.authority_appointment_id
       OR appointment_party_size <> attempt_item.party_size THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'committed ManleAI Calendar book requires one matching appointment root',
            CONSTRAINT = 'booking_attempts_manleai_calendar_book_graph_guard';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM availability_quote_slot_segments segment
                WHERE segment.quote_slot_id = slot_id_to_check
            )
            EXCEPT
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM booking_attempt_segments segment
                WHERE segment.booking_attempt_id = attempt_item.id
                  AND segment.scheduling_authority = 'manleai_calendar'
            )
        ) missing_attempt_segment
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM booking_attempt_segments segment
                WHERE segment.booking_attempt_id = attempt_item.id
                  AND segment.scheduling_authority = 'manleai_calendar'
            )
            EXCEPT
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM availability_quote_slot_segments segment
                WHERE segment.quote_slot_id = slot_id_to_check
            )
        ) extra_attempt_segment
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM booking_attempt_segments segment
                WHERE segment.booking_attempt_id = attempt_item.id
                  AND segment.scheduling_authority = 'manleai_calendar'
            )
            EXCEPT
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM appointment_services segment
                WHERE segment.appointment_id = appointment_id_to_check
                  AND segment.scheduling_authority = 'manleai_calendar'
                  AND segment.plan_version = 1
                  AND segment.released_at IS NULL
            )
        ) missing_appointment_segment
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM appointment_services segment
                WHERE segment.appointment_id = appointment_id_to_check
                  AND segment.scheduling_authority = 'manleai_calendar'
                  AND segment.plan_version = 1
                  AND segment.released_at IS NULL
            )
            EXCEPT
            (
                SELECT segment.sort_order, segment.service_id, segment.staff_id,
                       segment.staff_selection_mode, segment.guest_reference,
                       segment.duration_minutes, segment.scheduled_start_time,
                       segment.scheduled_end_time, segment.buffer_before_minutes,
                       segment.buffer_after_minutes, segment.occupied_start_time,
                       segment.occupied_end_time
                FROM booking_attempt_segments segment
                WHERE segment.booking_attempt_id = attempt_item.id
                  AND segment.scheduling_authority = 'manleai_calendar'
            )
        ) extra_appointment_segment
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'quote, attempt, and appointment segments must match exactly',
            CONSTRAINT = 'booking_attempts_manleai_calendar_book_graph_guard';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM availability_quote_slot_segments segment
                JOIN availability_quote_slot_resource_allocations allocation
                  ON allocation.quote_slot_segment_id = segment.id
                WHERE segment.quote_slot_id = slot_id_to_check
            )
            EXCEPT
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM booking_attempt_segments segment
                JOIN booking_attempt_segment_resource_allocations allocation
                  ON allocation.booking_attempt_segment_id = segment.id
                WHERE segment.booking_attempt_id = attempt_item.id
            )
        ) missing_attempt_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM booking_attempt_segments segment
                JOIN booking_attempt_segment_resource_allocations allocation
                  ON allocation.booking_attempt_segment_id = segment.id
                WHERE segment.booking_attempt_id = attempt_item.id
            )
            EXCEPT
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM availability_quote_slot_segments segment
                JOIN availability_quote_slot_resource_allocations allocation
                  ON allocation.quote_slot_segment_id = segment.id
                WHERE segment.quote_slot_id = slot_id_to_check
            )
        ) extra_attempt_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM booking_attempt_segments segment
                JOIN booking_attempt_segment_resource_allocations allocation
                  ON allocation.booking_attempt_segment_id = segment.id
                WHERE segment.booking_attempt_id = attempt_item.id
            )
            EXCEPT
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM appointment_services segment
                JOIN manleai_calendar_appointment_resource_allocations allocation
                  ON allocation.appointment_service_id = segment.id
                 AND allocation.released_at IS NULL
                WHERE segment.appointment_id = appointment_id_to_check
                  AND segment.plan_version = 1
                  AND segment.released_at IS NULL
            )
        ) missing_appointment_resource
    ) OR EXISTS (
        SELECT 1
        FROM (
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM appointment_services segment
                JOIN manleai_calendar_appointment_resource_allocations allocation
                  ON allocation.appointment_service_id = segment.id
                 AND allocation.released_at IS NULL
                WHERE segment.appointment_id = appointment_id_to_check
                  AND segment.plan_version = 1
                  AND segment.released_at IS NULL
            )
            EXCEPT
            (
                SELECT segment.sort_order, allocation.resource_pool_id, allocation.units_allocated
                FROM booking_attempt_segments segment
                JOIN booking_attempt_segment_resource_allocations allocation
                  ON allocation.booking_attempt_segment_id = segment.id
                WHERE segment.booking_attempt_id = attempt_item.id
            )
        ) extra_appointment_resource
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'quote, attempt, and appointment resources must match exactly',
            CONSTRAINT = 'booking_attempts_manleai_calendar_book_graph_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION enforce_manleai_calendar_execution_graph()
RETURNS TRIGGER AS $$
DECLARE
    attempt_id_to_check UUID;
BEGIN
    IF TG_TABLE_NAME = 'booking_attempts' THEN
        attempt_id_to_check := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'booking_attempt_segments' THEN
        attempt_id_to_check := COALESCE(NEW.booking_attempt_id, OLD.booking_attempt_id);
    ELSIF TG_TABLE_NAME = 'booking_attempt_segment_resource_allocations' THEN
        SELECT segment.booking_attempt_id
        INTO attempt_id_to_check
        FROM booking_attempt_segments segment
        WHERE segment.id = COALESCE(NEW.booking_attempt_segment_id, OLD.booking_attempt_segment_id);
    ELSIF TG_TABLE_NAME = 'appointments' THEN
        attempt_id_to_check := COALESCE(NEW.booking_attempt_id, OLD.booking_attempt_id);
    ELSIF TG_TABLE_NAME = 'appointment_services' THEN
        SELECT appointment.booking_attempt_id
        INTO attempt_id_to_check
        FROM appointments appointment
        WHERE appointment.id = COALESCE(NEW.appointment_id, OLD.appointment_id);
    ELSIF TG_TABLE_NAME = 'manleai_calendar_appointment_resource_allocations' THEN
        SELECT appointment.booking_attempt_id
        INTO attempt_id_to_check
        FROM appointment_services segment
        JOIN appointments appointment ON appointment.id = segment.appointment_id
        WHERE segment.id = COALESCE(NEW.appointment_service_id, OLD.appointment_service_id);
    ELSE
        attempt_id_to_check := COALESCE(NEW.booking_attempt_id, OLD.booking_attempt_id);
    END IF;

    IF attempt_id_to_check IS NOT NULL THEN
        PERFORM validate_manleai_calendar_execution_graph(attempt_id_to_check);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER booking_attempts_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE ON booking_attempts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER booking_attempt_segments_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON booking_attempt_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER booking_attempt_segment_resources_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON booking_attempt_segment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER appointments_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE ON appointments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER appointment_services_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON appointment_services
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER appointment_resources_manleai_calendar_book_graph_guard
AFTER INSERT OR UPDATE OR DELETE ON manleai_calendar_appointment_resource_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE CONSTRAINT TRIGGER execution_events_manleai_calendar_book_graph_guard
AFTER INSERT ON manleai_calendar_execution_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_execution_graph();

CREATE FUNCTION enforce_manleai_calendar_consumed_quote_immutable()
RETURNS TRIGGER AS $$
DECLARE
    quote_id_to_check UUID;
    quote_consumed_at TIMESTAMPTZ;
BEGIN
    IF TG_TABLE_NAME = 'availability_quotes' THEN
        IF OLD.scheduling_authority <> 'manleai_calendar' THEN
            IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
            RETURN NEW;
        END IF;
        IF OLD.consumed_at IS NOT NULL THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'consumed ManleAI Calendar quotes are immutable',
                CONSTRAINT = 'availability_quotes_manleai_calendar_immutable_guard';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        IF NEW.consumed_at IS NOT NULL
           AND (to_jsonb(NEW) - ARRAY['consumed_at', 'consumed_by_attempt_id'])
               IS DISTINCT FROM
               (to_jsonb(OLD) - ARRAY['consumed_at', 'consumed_by_attempt_id']) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'quote consumption cannot rewrite offered evidence',
                CONSTRAINT = 'availability_quotes_manleai_calendar_immutable_guard';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'availability_quote_slots' THEN
        quote_id_to_check := COALESCE(NEW.quote_id, OLD.quote_id);
    ELSIF TG_TABLE_NAME = 'availability_quote_slot_segments' THEN
        SELECT slot.quote_id
        INTO quote_id_to_check
        FROM availability_quote_slots slot
        WHERE slot.id = COALESCE(NEW.quote_slot_id, OLD.quote_slot_id);
    ELSE
        SELECT slot.quote_id
        INTO quote_id_to_check
        FROM availability_quote_slot_segments segment
        JOIN availability_quote_slots slot ON slot.id = segment.quote_slot_id
        WHERE segment.id = COALESCE(NEW.quote_slot_segment_id, OLD.quote_slot_segment_id);
    END IF;

    SELECT quote.consumed_at
    INTO quote_consumed_at
    FROM availability_quotes quote
    WHERE quote.id = quote_id_to_check
      AND quote.scheduling_authority = 'manleai_calendar';

    IF quote_consumed_at IS NOT NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'consumed ManleAI Calendar quote evidence is immutable',
            CONSTRAINT = 'availability_quotes_manleai_calendar_immutable_guard';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER availability_quotes_manleai_calendar_immutable_guard
BEFORE UPDATE OR DELETE ON availability_quotes
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_consumed_quote_immutable();

CREATE TRIGGER availability_quote_slots_manleai_calendar_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON availability_quote_slots
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_consumed_quote_immutable();

CREATE TRIGGER availability_quote_slot_segments_manleai_calendar_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON availability_quote_slot_segments
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_consumed_quote_immutable();

CREATE TRIGGER availability_quote_slot_resources_manleai_calendar_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON availability_quote_slot_resource_allocations
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_consumed_quote_immutable();

CREATE FUNCTION enforce_manleai_calendar_committed_attempt_immutable()
RETURNS TRIGGER AS $$
DECLARE
    attempt_id_to_check UUID;
    attempt_authority TEXT;
BEGIN
    IF TG_TABLE_NAME = 'booking_attempts' THEN
        IF OLD.scheduling_authority = 'manleai_calendar' THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'committed ManleAI Calendar attempts are immutable',
                CONSTRAINT = 'booking_attempts_manleai_calendar_immutable_guard';
        END IF;
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'booking_attempt_segments' THEN
        attempt_id_to_check := COALESCE(NEW.booking_attempt_id, OLD.booking_attempt_id);
    ELSE
        SELECT segment.booking_attempt_id
        INTO attempt_id_to_check
        FROM booking_attempt_segments segment
        WHERE segment.id = COALESCE(NEW.booking_attempt_segment_id, OLD.booking_attempt_segment_id);
    END IF;

    SELECT attempt.scheduling_authority
    INTO attempt_authority
    FROM booking_attempts attempt
    WHERE attempt.id = attempt_id_to_check;

    IF attempt_authority = 'manleai_calendar' THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'committed ManleAI Calendar attempt evidence is immutable',
            CONSTRAINT = 'booking_attempts_manleai_calendar_immutable_guard';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER booking_attempts_manleai_calendar_immutable_guard
BEFORE UPDATE OR DELETE ON booking_attempts
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_committed_attempt_immutable();

CREATE TRIGGER booking_attempt_segments_manleai_calendar_immutable_guard
BEFORE UPDATE OR DELETE ON booking_attempt_segments
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_committed_attempt_immutable();

CREATE TRIGGER booking_attempt_resources_manleai_calendar_immutable_guard
BEFORE UPDATE OR DELETE ON booking_attempt_segment_resource_allocations
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_committed_attempt_immutable();

CREATE FUNCTION enforce_manleai_calendar_appointment_history_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME = 'appointment_services' THEN
        IF OLD.scheduling_authority <> 'manleai_calendar' THEN
            IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
            RETURN NEW;
        END IF;
        IF TG_OP = 'DELETE'
           OR OLD.released_at IS NOT NULL
           OR NEW.released_at IS NULL
           OR (to_jsonb(NEW) - 'released_at') IS DISTINCT FROM (to_jsonb(OLD) - 'released_at') THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'ManleAI Calendar appointment plan history is immutable except for release',
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

CREATE TRIGGER appointment_services_manleai_calendar_history_immutable_guard
BEFORE UPDATE OR DELETE ON appointment_services
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_appointment_history_immutable();

CREATE TRIGGER appointment_resources_manleai_calendar_history_immutable_guard
BEFORE UPDATE OR DELETE ON manleai_calendar_appointment_resource_allocations
FOR EACH ROW EXECUTE FUNCTION enforce_manleai_calendar_appointment_history_immutable();
