ALTER TABLE call_sessions
    ADD CONSTRAINT call_sessions_salon_id_id_key UNIQUE (salon_id, id);

ALTER TABLE appointments
    ADD CONSTRAINT appointments_salon_id_id_key UNIQUE (salon_id, id);

ALTER TABLE services
    ADD CONSTRAINT services_salon_id_id_key UNIQUE (salon_id, id);

ALTER TABLE staff
    ADD CONSTRAINT staff_salon_id_id_key UNIQUE (salon_id, id);

CREATE TABLE scheduling_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    scheduling_authority TEXT NOT NULL DEFAULT 'owner_manual',
    operation_key TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    operation_type TEXT NOT NULL,
    source TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    version INTEGER NOT NULL DEFAULT 1,
    call_session_id UUID,
    target_appointment_id UUID,
    target_scheduling_authority TEXT,
    target_description TEXT,
    customer_name TEXT NOT NULL,
    customer_phone TEXT NOT NULL,
    customer_email TEXT,
    requested_timezone TEXT NOT NULL,
    party_size INTEGER NOT NULL,
    requested_start_time TIMESTAMPTZ,
    requested_end_time TIMESTAMPTZ,
    notes TEXT,
    resolution_reason TEXT,
    contacted_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    dismissed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scheduling_requests_salon_id_id_key UNIQUE (salon_id, id),
    CONSTRAINT scheduling_requests_authority_check
        CHECK (scheduling_authority = 'owner_manual'),
    CONSTRAINT scheduling_requests_operation_key_nonempty_check
        CHECK (length(trim(operation_key)) > 0),
    CONSTRAINT scheduling_requests_request_fingerprint_check
        CHECK (length(request_fingerprint) = 64),
    CONSTRAINT scheduling_requests_operation_type_check
        CHECK (operation_type IN ('book', 'reschedule', 'cancel')),
    CONSTRAINT scheduling_requests_source_nonempty_check
        CHECK (length(trim(source)) > 0),
    CONSTRAINT scheduling_requests_status_check
        CHECK (status IN ('pending', 'contacted', 'resolved', 'dismissed')),
    CONSTRAINT scheduling_requests_version_check
        CHECK (version >= 1),
    CONSTRAINT scheduling_requests_target_authority_check
        CHECK (
            target_scheduling_authority IS NULL
            OR target_scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')
        ),
    CONSTRAINT scheduling_requests_target_description_nonempty_check
        CHECK (target_description IS NULL OR length(trim(target_description)) > 0),
    CONSTRAINT scheduling_requests_operation_target_check
        CHECK (
            (
                operation_type = 'book'
                AND target_appointment_id IS NULL
                AND target_scheduling_authority IS NULL
                AND target_description IS NULL
            )
            OR (
                operation_type IN ('reschedule', 'cancel')
                AND (target_appointment_id IS NOT NULL OR target_description IS NOT NULL)
                AND (target_appointment_id IS NULL OR target_scheduling_authority IS NOT NULL)
            )
        ),
    CONSTRAINT scheduling_requests_customer_name_nonempty_check
        CHECK (length(trim(customer_name)) > 0),
    CONSTRAINT scheduling_requests_customer_phone_nonempty_check
        CHECK (length(trim(customer_phone)) > 0),
    CONSTRAINT scheduling_requests_requested_timezone_nonempty_check
        CHECK (length(trim(requested_timezone)) > 0),
    CONSTRAINT scheduling_requests_party_size_check
        CHECK (party_size >= 1),
    CONSTRAINT scheduling_requests_requested_range_check
        CHECK (
            requested_end_time IS NULL
            OR (
                requested_start_time IS NOT NULL
                AND requested_end_time > requested_start_time
            )
        ),
    CONSTRAINT scheduling_requests_resolution_reason_nonempty_check
        CHECK (resolution_reason IS NULL OR length(trim(resolution_reason)) > 0),
    CONSTRAINT scheduling_requests_lifecycle_check
        CHECK (
            (
                status = 'pending'
                AND contacted_at IS NULL
                AND resolved_at IS NULL
                AND dismissed_at IS NULL
                AND resolution_reason IS NULL
            )
            OR (
                status = 'contacted'
                AND contacted_at IS NOT NULL
                AND resolved_at IS NULL
                AND dismissed_at IS NULL
                AND resolution_reason IS NULL
            )
            OR (
                status = 'resolved'
                AND resolved_at IS NOT NULL
                AND dismissed_at IS NULL
            )
            OR (
                status = 'dismissed'
                AND resolved_at IS NULL
                AND dismissed_at IS NOT NULL
            )
        ),
    CONSTRAINT scheduling_requests_call_session_tenant_fk
        FOREIGN KEY (salon_id, call_session_id)
        REFERENCES call_sessions(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT scheduling_requests_target_appointment_tenant_fk
        FOREIGN KEY (salon_id, target_appointment_id)
        REFERENCES appointments(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED
);

CREATE UNIQUE INDEX idx_scheduling_requests_operation_key
    ON scheduling_requests(salon_id, scheduling_authority, operation_key);

CREATE UNIQUE INDEX idx_scheduling_requests_call_session
    ON scheduling_requests(call_session_id)
    WHERE call_session_id IS NOT NULL;

CREATE INDEX idx_scheduling_requests_salon_status
    ON scheduling_requests(salon_id, status, created_at ASC, id ASC);

CREATE INDEX idx_scheduling_requests_target_appointment
    ON scheduling_requests(salon_id, target_appointment_id)
    WHERE target_appointment_id IS NOT NULL;

CREATE FUNCTION enforce_scheduling_request_immutable_core()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.salon_id IS DISTINCT FROM NEW.salon_id
       OR OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority
       OR OLD.operation_key IS DISTINCT FROM NEW.operation_key
       OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
       OR OLD.operation_type IS DISTINCT FROM NEW.operation_type
       OR OLD.source IS DISTINCT FROM NEW.source
       OR OLD.call_session_id IS DISTINCT FROM NEW.call_session_id
       OR OLD.target_appointment_id IS DISTINCT FROM NEW.target_appointment_id
       OR OLD.target_scheduling_authority IS DISTINCT FROM NEW.target_scheduling_authority
       OR OLD.target_description IS DISTINCT FROM NEW.target_description
       OR OLD.customer_name IS DISTINCT FROM NEW.customer_name
       OR OLD.customer_phone IS DISTINCT FROM NEW.customer_phone
       OR OLD.customer_email IS DISTINCT FROM NEW.customer_email
       OR OLD.requested_timezone IS DISTINCT FROM NEW.requested_timezone
       OR OLD.party_size IS DISTINCT FROM NEW.party_size
       OR OLD.requested_start_time IS DISTINCT FROM NEW.requested_start_time
       OR OLD.requested_end_time IS DISTINCT FROM NEW.requested_end_time
       OR OLD.notes IS DISTINCT FROM NEW.notes
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling request core fields are immutable',
            CONSTRAINT = 'scheduling_requests_immutable_core_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_requests_immutable_core_guard
BEFORE UPDATE ON scheduling_requests
FOR EACH ROW
EXECUTE FUNCTION enforce_scheduling_request_immutable_core();

CREATE TABLE scheduling_request_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    scheduling_request_id UUID NOT NULL,
    service_id UUID NOT NULL,
    service_name TEXT NOT NULL,
    guest_reference TEXT,
    quantity INTEGER NOT NULL DEFAULT 1,
    staff_id UUID,
    staff_name TEXT,
    staff_selection_mode TEXT NOT NULL DEFAULT 'specific',
    duration_minutes INTEGER NOT NULL,
    requested_start_time TIMESTAMPTZ,
    requested_end_time TIMESTAMPTZ,
    sort_order INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scheduling_request_segments_request_tenant_fk
        FOREIGN KEY (salon_id, scheduling_request_id)
        REFERENCES scheduling_requests(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT scheduling_request_segments_service_tenant_fk
        FOREIGN KEY (salon_id, service_id)
        REFERENCES services(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT scheduling_request_segments_staff_tenant_fk
        FOREIGN KEY (salon_id, staff_id)
        REFERENCES staff(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT scheduling_request_segments_service_name_nonempty_check
        CHECK (length(trim(service_name)) > 0),
    CONSTRAINT scheduling_request_segments_guest_reference_nonempty_check
        CHECK (guest_reference IS NULL OR length(trim(guest_reference)) > 0),
    CONSTRAINT scheduling_request_segments_quantity_check
        CHECK (quantity >= 1),
    CONSTRAINT scheduling_request_segments_staff_mode_check
        CHECK (staff_selection_mode IN ('specific', 'anyone')),
    CONSTRAINT scheduling_request_segments_staff_snapshot_check
        CHECK (
            (
                staff_selection_mode = 'specific'
                AND staff_id IS NOT NULL
                AND staff_name IS NOT NULL
                AND length(trim(staff_name)) > 0
            )
            OR (
                staff_selection_mode = 'anyone'
                AND staff_id IS NULL
                AND staff_name IS NULL
            )
        ),
    CONSTRAINT scheduling_request_segments_duration_check
        CHECK (duration_minutes > 0),
    CONSTRAINT scheduling_request_segments_requested_range_check
        CHECK (
            requested_end_time IS NULL
            OR (
                requested_start_time IS NOT NULL
                AND requested_end_time > requested_start_time
            )
        ),
    CONSTRAINT scheduling_request_segments_sort_order_check
        CHECK (sort_order >= 1),
    CONSTRAINT scheduling_request_segments_order_key
        UNIQUE (scheduling_request_id, sort_order)
);

CREATE INDEX idx_scheduling_request_segments_request
    ON scheduling_request_segments(scheduling_request_id, sort_order ASC, id ASC);

CREATE FUNCTION reject_scheduling_request_segment_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling request segments are immutable',
        CONSTRAINT = 'scheduling_request_segments_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_request_segments_immutable_guard
BEFORE UPDATE OR DELETE ON scheduling_request_segments
FOR EACH ROW
EXECUTE FUNCTION reject_scheduling_request_segment_mutation();

CREATE TABLE scheduling_request_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    scheduling_request_id UUID NOT NULL,
    action_key TEXT NOT NULL,
    action_fingerprint TEXT NOT NULL,
    event_type TEXT NOT NULL,
    request_version INTEGER NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scheduling_request_events_request_tenant_fk
        FOREIGN KEY (salon_id, scheduling_request_id)
        REFERENCES scheduling_requests(salon_id, id)
        ON DELETE CASCADE,
    CONSTRAINT scheduling_request_events_action_key_nonempty_check
        CHECK (length(trim(action_key)) > 0),
    CONSTRAINT scheduling_request_events_action_fingerprint_check
        CHECK (length(action_fingerprint) = 64),
    CONSTRAINT scheduling_request_events_event_type_nonempty_check
        CHECK (length(trim(event_type)) > 0),
    CONSTRAINT scheduling_request_events_request_version_check
        CHECK (request_version >= 1),
    CONSTRAINT scheduling_request_events_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT scheduling_request_events_action_key
        UNIQUE (scheduling_request_id, action_key)
);

CREATE INDEX idx_scheduling_request_events_request
    ON scheduling_request_events(scheduling_request_id, created_at ASC, id ASC);

CREATE FUNCTION enforce_scheduling_request_event_actor()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.actor_user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM salons salon
        WHERE salon.id = NEW.salon_id
          AND salon.owner_user_id = NEW.actor_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling request event actor must own the salon',
            CONSTRAINT = 'scheduling_request_events_actor_owner_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_request_events_actor_owner_guard
BEFORE INSERT ON scheduling_request_events
FOR EACH ROW
EXECUTE FUNCTION enforce_scheduling_request_event_actor();

CREATE FUNCTION reject_scheduling_request_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'scheduling request events are immutable',
        CONSTRAINT = 'scheduling_request_events_immutable_guard';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_request_events_immutable_guard
BEFORE UPDATE OR DELETE ON scheduling_request_events
FOR EACH ROW
EXECUTE FUNCTION reject_scheduling_request_event_mutation();

ALTER TABLE owner_notifications
    ADD COLUMN scheduling_request_id UUID,
    ADD CONSTRAINT owner_notifications_scheduling_request_tenant_fk
        FOREIGN KEY (salon_id, scheduling_request_id)
        REFERENCES scheduling_requests(salon_id, id)
        ON DELETE CASCADE,
    ADD CONSTRAINT owner_notifications_owner_manual_request_check
        CHECK (
            (
                type = 'owner_manual_request_pending'
                AND scheduling_request_id IS NOT NULL
                AND dedupe_key = 'owner-manual-request-pending:' || scheduling_request_id::text
            )
            OR (
                type <> 'owner_manual_request_pending'
                AND scheduling_request_id IS NULL
            )
        );

CREATE INDEX idx_owner_notifications_scheduling_request
    ON owner_notifications(scheduling_request_id, created_at ASC)
    WHERE scheduling_request_id IS NOT NULL;

ALTER TABLE call_sessions
    ADD COLUMN scheduling_request_id UUID REFERENCES scheduling_requests(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX idx_call_sessions_scheduling_request
    ON call_sessions(scheduling_request_id)
    WHERE scheduling_request_id IS NOT NULL;

CREATE FUNCTION enforce_call_session_scheduling_request_link()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.scheduling_request_id IS DISTINCT FROM NEW.scheduling_request_id
       AND pg_trigger_depth() <= 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'call session scheduling request link is managed by the request aggregate',
            CONSTRAINT = 'call_sessions_scheduling_request_link_write_guard';
    END IF;

    IF NEW.scheduling_request_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM scheduling_requests request
        WHERE request.id = NEW.scheduling_request_id
          AND request.salon_id = NEW.salon_id
          AND request.call_session_id = NEW.id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'call session scheduling request link must be tenant-consistent and reciprocal',
            CONSTRAINT = 'call_sessions_scheduling_request_link_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER call_sessions_scheduling_request_link_guard
BEFORE INSERT OR UPDATE OF scheduling_request_id, salon_id ON call_sessions
FOR EACH ROW
EXECUTE FUNCTION enforce_call_session_scheduling_request_link();

CREATE FUNCTION sync_scheduling_request_call_session_link()
RETURNS TRIGGER AS $$
DECLARE
    linked_rows INTEGER;
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.call_session_id IS DISTINCT FROM NEW.call_session_id
       AND OLD.call_session_id IS NOT NULL THEN
        UPDATE call_sessions
        SET scheduling_request_id = NULL,
            updated_at = now()
        WHERE id = OLD.call_session_id
          AND salon_id = OLD.salon_id
          AND scheduling_request_id = OLD.id;
    END IF;

    IF NEW.call_session_id IS NULL THEN
        RETURN NEW;
    END IF;

    UPDATE call_sessions
    SET scheduling_request_id = NEW.id,
        updated_at = now()
    WHERE id = NEW.call_session_id
      AND salon_id = NEW.salon_id
      AND (scheduling_request_id IS NULL OR scheduling_request_id = NEW.id);

    GET DIAGNOSTICS linked_rows = ROW_COUNT;
    IF linked_rows <> 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling request call session is already linked or tenant-mismatched',
            CONSTRAINT = 'scheduling_requests_call_session_link_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER scheduling_requests_call_session_link_guard
AFTER INSERT OR UPDATE OF call_session_id, salon_id ON scheduling_requests
FOR EACH ROW
EXECUTE FUNCTION sync_scheduling_request_call_session_link();
