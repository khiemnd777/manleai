-- SaaS Phase 10 Platform-owned AI runtime control.
--
-- `salons.ai_enabled` remains the runtime source of truth. This migration
-- extends the existing technical mutation ledger so Platform Admin/Ops can
-- change that flag with the same optimistic-version, idempotency, actual-actor,
-- and immutable-audit guarantees used by provider configuration.

ALTER TABLE technical_resource_versions
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_type_check,
    DROP CONSTRAINT IF EXISTS technical_resource_versions_resource_id_check;

ALTER TABLE technical_resource_versions
    ADD CONSTRAINT technical_resource_versions_resource_type_check
        CHECK (resource_type IN ('integration_config', 'ai_runtime')),
    ADD CONSTRAINT technical_resource_versions_resource_identity_check
        CHECK (
            (resource_type = 'integration_config' AND resource_id IN ('square', 'twilio', 'openai'))
            OR (resource_type = 'ai_runtime' AND resource_id = 'ai_booking')
        );

INSERT INTO technical_resource_versions (
    salon_id, resource_type, resource_id, version
)
SELECT id, 'ai_runtime', 'ai_booking', 0
FROM salons
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION phase10_seed_ai_runtime_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO technical_resource_versions (
        salon_id, resource_type, resource_id, version
    ) VALUES (NEW.id, 'ai_runtime', 'ai_booking', 0)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END
$$;

CREATE TRIGGER salons_seed_ai_runtime_version
AFTER INSERT ON salons
FOR EACH ROW EXECUTE FUNCTION phase10_seed_ai_runtime_version();

COMMENT ON FUNCTION phase10_seed_ai_runtime_version() IS
'Creates only the version fence for Platform-owned AI runtime control; it does not enable AI or mutate provider configuration.';
