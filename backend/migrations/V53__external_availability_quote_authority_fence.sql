-- Phase 5B fences external availability quotes to the salon authority version.
-- Historical provider/location/snapshot evidence remains unchanged.

ALTER TABLE availability_quotes
    ADD COLUMN authority_fence_provenance TEXT NOT NULL DEFAULT 'known',
    ADD COLUMN retry_of_attempt_id UUID,
    ADD CONSTRAINT availability_quotes_authority_fence_provenance_check
        CHECK (authority_fence_provenance IN ('known', 'legacy_unknown', 'target_origin', 'retry_origin')),
    ADD CONSTRAINT availability_quotes_retry_attempt_tenant_fk
        FOREIGN KEY (salon_id, retry_of_attempt_id)
        REFERENCES booking_attempts(salon_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED;

-- The backfill updates rows covered by existing deferred quote-graph guards.
-- Fire those guards per statement before the later ALTER TABLE in this same
-- migration transaction.
SET CONSTRAINTS ALL IMMEDIATE;

-- V49-V52 external new-book quotes predate the authority-version fence. Their
-- exact originating authority version is unknowable; never infer it from the
-- salon's current (possibly ABA-advanced) setting.
UPDATE availability_quotes
SET authority_fence_provenance = 'legacy_unknown'
WHERE scheduling_authority = 'external_provider'
  AND operation_type IS NULL
  AND scheduling_authority_version IS NULL;

ALTER TABLE availability_quotes
    DROP CONSTRAINT availability_quotes_external_provider_shape_check,
    DROP CONSTRAINT availability_quotes_target_version_check,
    ADD CONSTRAINT availability_quotes_target_version_check
        CHECK (target_authority_appointment_version IS NULL OR target_authority_appointment_version >= 0),
    ADD CONSTRAINT availability_quotes_external_provider_shape_check
        CHECK (
            scheduling_authority <> 'external_provider'
            OR (
                provider IS NOT NULL
                AND provider_location_id IS NOT NULL
                AND provider_snapshot_generation IS NOT NULL
                AND authority_config_version IS NULL
                AND party_size = 1
                AND (
                    (
                        operation_type IS NULL
                        AND target_appointment_id IS NULL
                        AND target_authority_appointment_version IS NULL
                        AND (
                            (
                                authority_fence_provenance = 'known'
                                AND scheduling_authority_version IS NOT NULL
                                AND scheduling_authority_version >= 1
                                AND retry_of_attempt_id IS NULL
                            )
                            OR (
                                authority_fence_provenance = 'legacy_unknown'
                                AND scheduling_authority_version IS NULL
                                AND retry_of_attempt_id IS NULL
                            )
                            OR (
                                authority_fence_provenance = 'retry_origin'
                                AND scheduling_authority_version IS NULL
                                AND retry_of_attempt_id IS NOT NULL
                            )
                        )
                    )
                    OR (
                        operation_type = 'reschedule'
                        AND authority_fence_provenance = 'target_origin'
                        AND scheduling_authority_version IS NULL
                        AND retry_of_attempt_id IS NULL
                        AND target_appointment_id IS NOT NULL
                        AND target_authority_appointment_version IS NOT NULL
                        AND target_authority_appointment_version >= 0
                    )
                )
            )
        ) NOT VALID;

-- The NOT VALID check protects the backfill write itself. This explicit audit
-- gives a direct migration error before PostgreSQL marks the check validated.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM availability_quotes
        WHERE scheduling_authority = 'external_provider'
          AND (
              provider IS NULL
              OR provider_location_id IS NULL
              OR provider_snapshot_generation IS NULL
              OR authority_config_version IS NOT NULL
              OR party_size <> 1
              OR NOT (
                  (
                      operation_type IS NULL
                      AND target_appointment_id IS NULL
                      AND target_authority_appointment_version IS NULL
                      AND (
                          (
                              authority_fence_provenance = 'known'
                              AND scheduling_authority_version IS NOT NULL
                              AND scheduling_authority_version >= 1
                              AND retry_of_attempt_id IS NULL
                          )
                          OR (
                              authority_fence_provenance = 'legacy_unknown'
                              AND scheduling_authority_version IS NULL
                              AND retry_of_attempt_id IS NULL
                          )
                          OR (
                              authority_fence_provenance = 'retry_origin'
                              AND scheduling_authority_version IS NULL
                              AND retry_of_attempt_id IS NOT NULL
                          )
                      )
                  )
                  OR (
                      operation_type = 'reschedule'
                      AND authority_fence_provenance = 'target_origin'
                      AND scheduling_authority_version IS NULL
                      AND retry_of_attempt_id IS NULL
                      AND target_appointment_id IS NOT NULL
                      AND target_authority_appointment_version IS NOT NULL
                      AND target_authority_appointment_version >= 0
                  )
              )
          )
    ) THEN
        RAISE EXCEPTION 'external availability quote authority provenance is incomplete';
    END IF;
END;
$$;

ALTER TABLE availability_quotes
    VALIDATE CONSTRAINT availability_quotes_external_provider_shape_check;

CREATE FUNCTION enforce_availability_quote_authority_fence_provenance()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.authority_fence_provenance = 'legacy_unknown' THEN
        RAISE EXCEPTION 'legacy_unknown quote provenance is migration-owned'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'availability_quotes_authority_fence_provenance_guard';
    END IF;
    IF TG_OP = 'UPDATE'
       AND (
           OLD.authority_fence_provenance IS DISTINCT FROM NEW.authority_fence_provenance
           OR OLD.retry_of_attempt_id IS DISTINCT FROM NEW.retry_of_attempt_id
       ) THEN
        RAISE EXCEPTION 'availability quote authority provenance is immutable'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'availability_quotes_authority_fence_provenance_guard';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER availability_quotes_authority_fence_provenance_guard
BEFORE INSERT OR UPDATE OF authority_fence_provenance, retry_of_attempt_id ON availability_quotes
FOR EACH ROW
EXECUTE FUNCTION enforce_availability_quote_authority_fence_provenance();
