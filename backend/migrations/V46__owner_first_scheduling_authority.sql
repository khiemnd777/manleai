ALTER TABLE salon_settings
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD CONSTRAINT salon_settings_scheduling_authority_check
    CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider'));

ALTER TABLE booking_attempts
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD COLUMN authority_provider TEXT,
    ADD COLUMN authority_appointment_id TEXT,
    ADD COLUMN authority_appointment_version INTEGER,
    ADD COLUMN target_authority_appointment_version INTEGER,
    ADD COLUMN authority_idempotency_key TEXT,
    ADD COLUMN authority_location_id TEXT,
    ADD COLUMN authority_snapshot_generation BIGINT,
    ADD CONSTRAINT booking_attempts_scheduling_authority_check
        CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    ADD CONSTRAINT booking_attempts_authority_provider_nonempty_check
        CHECK (authority_provider IS NULL OR length(trim(authority_provider)) > 0),
    ADD CONSTRAINT booking_attempts_authority_appointment_id_nonempty_check
        CHECK (authority_appointment_id IS NULL OR length(trim(authority_appointment_id)) > 0),
    ADD CONSTRAINT booking_attempts_authority_location_id_nonempty_check
        CHECK (authority_location_id IS NULL OR length(trim(authority_location_id)) > 0),
    ADD CONSTRAINT booking_attempts_authority_appointment_version_check
        CHECK (authority_appointment_version IS NULL OR authority_appointment_version >= 0),
    ADD CONSTRAINT booking_attempts_target_authority_appointment_version_check
        CHECK (target_authority_appointment_version IS NULL OR target_authority_appointment_version >= 0),
    ADD CONSTRAINT booking_attempts_authority_snapshot_generation_check
        CHECK (authority_snapshot_generation IS NULL OR authority_snapshot_generation >= 0),
    ADD CONSTRAINT booking_attempts_authority_appointment_pair_check
        CHECK (authority_appointment_version IS NULL OR authority_appointment_id IS NOT NULL),
    ADD CONSTRAINT booking_attempts_authority_fence_pair_check
        CHECK (
            (authority_location_id IS NULL AND authority_snapshot_generation IS NULL)
            OR (
                authority_location_id IS NOT NULL
                AND authority_snapshot_generation IS NOT NULL
            )
        );

UPDATE booking_attempts
SET scheduling_authority = 'external_provider',
    authority_provider = pos_provider,
    authority_appointment_id = pos_booking_id,
    authority_appointment_version = pos_booking_version,
    target_authority_appointment_version = target_pos_booking_version,
    authority_idempotency_key = pos_idempotency_key,
    authority_location_id = provider_location_id,
    authority_snapshot_generation = provider_snapshot_generation;

ALTER TABLE booking_attempt_segments
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD COLUMN authority_provider TEXT,
    ADD COLUMN authority_service_id TEXT,
    ADD COLUMN authority_service_version BIGINT,
    ADD COLUMN authority_staff_id TEXT,
    ADD CONSTRAINT booking_attempt_segments_scheduling_authority_check
        CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    ADD CONSTRAINT booking_attempt_segments_authority_provider_nonempty_check
        CHECK (authority_provider IS NULL OR length(trim(authority_provider)) > 0),
    ADD CONSTRAINT booking_attempt_segments_authority_service_id_nonempty_check
        CHECK (authority_service_id IS NULL OR length(trim(authority_service_id)) > 0),
    ADD CONSTRAINT booking_attempt_segments_authority_staff_id_nonempty_check
        CHECK (authority_staff_id IS NULL OR length(trim(authority_staff_id)) > 0),
    ADD CONSTRAINT booking_attempt_segments_authority_service_version_check
        CHECK (authority_service_version IS NULL OR authority_service_version >= 0),
    ADD CONSTRAINT booking_attempt_segments_authority_service_pair_check
        CHECK (authority_service_version IS NULL OR authority_service_id IS NOT NULL);

UPDATE booking_attempt_segments segment
SET scheduling_authority = attempt.scheduling_authority,
    authority_provider = attempt.authority_provider,
    authority_service_id = segment.pos_service_id,
    authority_service_version = segment.pos_service_version,
    authority_staff_id = segment.pos_staff_id
FROM booking_attempts attempt
WHERE attempt.id = segment.booking_attempt_id;

ALTER TABLE appointments
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD COLUMN authority_provider TEXT,
    ADD COLUMN authority_appointment_id TEXT,
    ADD COLUMN authority_appointment_version INTEGER,
    ADD COLUMN authority_customer_id TEXT,
    ADD COLUMN confirmed_at TIMESTAMPTZ,
    ADD COLUMN confirmed_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN confirmation_source TEXT,
    ADD CONSTRAINT appointments_scheduling_authority_check
        CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    ADD CONSTRAINT appointments_authority_provider_nonempty_check
        CHECK (authority_provider IS NULL OR length(trim(authority_provider)) > 0),
    ADD CONSTRAINT appointments_authority_appointment_id_nonempty_check
        CHECK (authority_appointment_id IS NULL OR length(trim(authority_appointment_id)) > 0),
    ADD CONSTRAINT appointments_authority_customer_id_nonempty_check
        CHECK (authority_customer_id IS NULL OR length(trim(authority_customer_id)) > 0),
    ADD CONSTRAINT appointments_authority_appointment_version_check
        CHECK (authority_appointment_version IS NULL OR authority_appointment_version >= 0),
    ADD CONSTRAINT appointments_authority_appointment_pair_check
        CHECK (authority_appointment_version IS NULL OR authority_appointment_id IS NOT NULL),
    ADD CONSTRAINT appointments_confirmation_source_check
        CHECK (
            confirmation_source IS NULL
            OR confirmation_source IN ('owner_manual', 'manleai_calendar', 'external_provider')
        ),
    ADD CONSTRAINT appointments_confirmation_source_authority_check
        CHECK (
            confirmation_source IS NULL
            OR confirmation_source = scheduling_authority
        ),
    ADD CONSTRAINT appointments_confirmation_provenance_pair_check
        CHECK (
            (confirmed_at IS NULL AND confirmation_source IS NULL)
            OR (confirmed_at IS NOT NULL AND confirmation_source IS NOT NULL)
        ),
    ADD CONSTRAINT appointments_confirmation_actor_mode_check
        CHECK (
            (confirmation_source IS NULL AND confirmed_by_user_id IS NULL)
            OR (
                confirmation_source = 'owner_manual'
                AND confirmed_by_user_id IS NOT NULL
            )
            OR (
                confirmation_source IN ('external_provider', 'manleai_calendar')
                AND confirmed_by_user_id IS NULL
            )
        );

-- Historical appointment rows retain their exact provider mirror evidence, but
-- confirmation audit fields remain NULL because their timestamp, actor, and
-- confirmation source cannot be reconstructed reliably from legacy metadata.
UPDATE appointments
SET scheduling_authority = 'external_provider',
    authority_provider = pos_provider,
    authority_appointment_id = pos_appointment_id,
    authority_appointment_version = pos_appointment_version,
    authority_customer_id = pos_customer_id;

CREATE FUNCTION enforce_appointment_confirmation_provenance()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.confirmed_at IS NOT NULL THEN
        IF OLD.confirmed_at IS DISTINCT FROM NEW.confirmed_at
           OR OLD.confirmation_source IS DISTINCT FROM NEW.confirmation_source
           OR OLD.confirmed_by_user_id IS DISTINCT FROM NEW.confirmed_by_user_id
           OR OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority
           OR OLD.salon_id IS DISTINCT FROM NEW.salon_id THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'appointment confirmation provenance is immutable after confirmation',
                CONSTRAINT = 'appointments_confirmation_provenance_immutable_guard';
        END IF;

        RETURN NEW;
    END IF;

    IF NEW.confirmation_source IS DISTINCT FROM 'owner_manual'
       OR NEW.confirmed_by_user_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM salons salon
        WHERE salon.id = NEW.salon_id
          AND salon.owner_user_id = NEW.confirmed_by_user_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'owner-manual confirmation actor must own the appointment salon',
            CONSTRAINT = 'appointments_owner_confirmation_actor_guard';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER appointments_confirmation_provenance_guard
BEFORE INSERT OR UPDATE OF confirmed_at, confirmation_source, confirmed_by_user_id, scheduling_authority, salon_id
ON appointments
FOR EACH ROW
EXECUTE FUNCTION enforce_appointment_confirmation_provenance();

ALTER TABLE appointment_services
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD COLUMN authority_provider TEXT,
    ADD COLUMN authority_service_id TEXT,
    ADD COLUMN authority_service_version BIGINT,
    ADD COLUMN authority_staff_id TEXT,
    ADD CONSTRAINT appointment_services_scheduling_authority_check
        CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    ADD CONSTRAINT appointment_services_authority_provider_nonempty_check
        CHECK (authority_provider IS NULL OR length(trim(authority_provider)) > 0),
    ADD CONSTRAINT appointment_services_authority_service_id_nonempty_check
        CHECK (authority_service_id IS NULL OR length(trim(authority_service_id)) > 0),
    ADD CONSTRAINT appointment_services_authority_staff_id_nonempty_check
        CHECK (authority_staff_id IS NULL OR length(trim(authority_staff_id)) > 0),
    ADD CONSTRAINT appointment_services_authority_service_version_check
        CHECK (authority_service_version IS NULL OR authority_service_version >= 0),
    ADD CONSTRAINT appointment_services_authority_service_pair_check
        CHECK (authority_service_version IS NULL OR authority_service_id IS NOT NULL);

UPDATE appointment_services segment
SET scheduling_authority = appointment.scheduling_authority,
    authority_provider = appointment.authority_provider,
    authority_service_id = segment.pos_service_id,
    authority_service_version = segment.pos_service_version,
    authority_staff_id = segment.pos_staff_id
FROM appointments appointment
WHERE appointment.id = segment.appointment_id;

ALTER TABLE availability_quotes
    ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider',
    ADD COLUMN authority_provider TEXT,
    ADD COLUMN authority_location_id TEXT,
    ADD COLUMN authority_snapshot_generation BIGINT,
    ADD CONSTRAINT availability_quotes_scheduling_authority_check
        CHECK (scheduling_authority IN ('owner_manual', 'manleai_calendar', 'external_provider')),
    ADD CONSTRAINT availability_quotes_authority_provider_nonempty_check
        CHECK (authority_provider IS NULL OR length(trim(authority_provider)) > 0),
    ADD CONSTRAINT availability_quotes_authority_location_id_nonempty_check
        CHECK (authority_location_id IS NULL OR length(trim(authority_location_id)) > 0),
    ADD CONSTRAINT availability_quotes_authority_snapshot_generation_check
        CHECK (authority_snapshot_generation IS NULL OR authority_snapshot_generation >= 0),
    ADD CONSTRAINT availability_quotes_authority_fence_pair_check
        CHECK (
            (authority_location_id IS NULL AND authority_snapshot_generation IS NULL)
            OR (
                authority_location_id IS NOT NULL
                AND authority_snapshot_generation IS NOT NULL
            )
        );

UPDATE availability_quotes
SET scheduling_authority = 'external_provider',
    authority_provider = provider,
    authority_location_id = provider_location_id,
    authority_snapshot_generation = provider_snapshot_generation;
