-- Owner-first onboarding keeps legacy salons unchanged, but it must refuse to
-- install the new invariant over an already incompatible row. The migrator
-- applies this file transactionally, so a failed preflight leaves no V54 DDL.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM salon_settings
        WHERE scheduling_authority = 'owner_manual'
          AND booking_mode = 'confirmed_booking'
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'owner_manual scheduling authority cannot use confirmed_booking mode',
            CONSTRAINT = 'salon_settings_owner_manual_booking_mode_guard';
    END IF;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE salons
    ADD COLUMN creation_operation_key TEXT,
    ADD COLUMN creation_payload_fingerprint TEXT,
    ADD CONSTRAINT salons_creation_proof_pair_check
        CHECK (
            (creation_operation_key IS NULL AND creation_payload_fingerprint IS NULL)
            OR
            (creation_operation_key IS NOT NULL AND creation_payload_fingerprint IS NOT NULL)
        ),
    ADD CONSTRAINT salons_creation_operation_key_check
        CHECK (
            creation_operation_key IS NULL
            OR (
                creation_operation_key = btrim(creation_operation_key)
                AND length(creation_operation_key) BETWEEN 1 AND 256
            )
        ),
    ADD CONSTRAINT salons_creation_payload_fingerprint_check
        CHECK (
            creation_payload_fingerprint IS NULL
            OR creation_payload_fingerprint ~ '^[0-9a-f]{64}$'
        );

CREATE UNIQUE INDEX salons_owner_creation_operation_key
    ON salons (owner_user_id, creation_operation_key)
    WHERE creation_operation_key IS NOT NULL;

ALTER TABLE salon_settings
    ADD CONSTRAINT salon_settings_owner_manual_booking_mode_guard
        CHECK (
            scheduling_authority <> 'owner_manual'
            OR booking_mode <> 'confirmed_booking'
        );
