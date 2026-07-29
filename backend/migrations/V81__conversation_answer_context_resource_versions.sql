-- Persisted collection revisions for the structured Conversation AI answer
-- context. Per-record Business versions remain the optimistic-write owner;
-- these collection rows let every runtime replica reject stale cached catalog
-- projections without scanning every service or staff record on each turn.

INSERT INTO business_resource_versions (
    salon_id, resource_type, resource_id, version
)
SELECT salon.id, resource.resource_type, 'collection', 1
FROM salons salon
CROSS JOIN (VALUES ('service'), ('staff')) AS resource(resource_type)
ON CONFLICT DO NOTHING;

-- V77 owns the salon-wide transfer collection seeds. Extend the same function
-- so a newly created salon receives every collection fence in one trigger.
CREATE OR REPLACE FUNCTION phase12_seed_transfer_resource_versions()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO business_resource_versions (salon_id, resource_type, resource_id, version)
    VALUES
        (NEW.id, 'service', 'collection', 1),
        (NEW.id, 'staff', 'collection', 1),
        (NEW.id, 'service_aliases', 'collection', 1),
        (NEW.id, 'consultation_profiles', 'collection', 1),
        (NEW.id, 'knowledge_base', 'collection', 1),
        (NEW.id, 'service_categories', 'collection', 1)
    ON CONFLICT DO NOTHING;

    INSERT INTO technical_resource_versions (salon_id, resource_type, resource_id, version)
    VALUES
        (NEW.id, 'ai_receptionist', 'policy', 1),
        (NEW.id, 'integration_config', 'square', 0),
        (NEW.id, 'integration_config', 'twilio', 0),
        (NEW.id, 'integration_config', 'openai', 0)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION phase13_bump_answer_context_collection_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    old_salon_id UUID;
    new_salon_id UUID;
    target_resource_type TEXT := TG_ARGV[0];
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_salon_id := OLD.salon_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        new_salon_id := NEW.salon_id;
    END IF;

    IF old_salon_id IS NOT NULL THEN
        INSERT INTO business_resource_versions (
            salon_id, resource_type, resource_id, version
        ) VALUES (
            old_salon_id, target_resource_type, 'collection', 1
        )
        ON CONFLICT (salon_id, resource_type, resource_id)
        DO UPDATE SET
            version = business_resource_versions.version + 1,
            updated_at = now();
    END IF;

    IF new_salon_id IS NOT NULL AND new_salon_id IS DISTINCT FROM old_salon_id THEN
        INSERT INTO business_resource_versions (
            salon_id, resource_type, resource_id, version
        ) VALUES (
            new_salon_id, target_resource_type, 'collection', 1
        )
        ON CONFLICT (salon_id, resource_type, resource_id)
        DO UPDATE SET
            version = business_resource_versions.version + 1,
            updated_at = now();
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER services_bump_answer_context_collection_insert_delete
AFTER INSERT OR DELETE ON services
FOR EACH ROW
EXECUTE FUNCTION phase13_bump_answer_context_collection_version('service');

CREATE TRIGGER services_bump_answer_context_collection_update
AFTER UPDATE OF
    name, description, ai_description, duration_minutes, price_from,
    price_display, service_category_id, ai_bookable, active, archived_at,
    pos_provider, pos_service_version, sync_status
ON services
FOR EACH ROW
WHEN (
    ROW(
        OLD.name, OLD.description, OLD.ai_description, OLD.duration_minutes,
        OLD.price_from, OLD.price_display, OLD.service_category_id,
        OLD.ai_bookable, OLD.active, OLD.archived_at, OLD.pos_provider,
        OLD.pos_service_version, OLD.sync_status
    ) IS DISTINCT FROM ROW(
        NEW.name, NEW.description, NEW.ai_description, NEW.duration_minutes,
        NEW.price_from, NEW.price_display, NEW.service_category_id,
        NEW.ai_bookable, NEW.active, NEW.archived_at, NEW.pos_provider,
        NEW.pos_service_version, NEW.sync_status
    )
)
EXECUTE FUNCTION phase13_bump_answer_context_collection_version('service');

CREATE TRIGGER staff_bump_answer_context_collection_insert_delete
AFTER INSERT OR DELETE ON staff
FOR EACH ROW
EXECUTE FUNCTION phase13_bump_answer_context_collection_version('staff');

CREATE TRIGGER staff_bump_answer_context_collection_update
AFTER UPDATE OF name, ai_bookable, active, archived_at, pos_provider, sync_status
ON staff
FOR EACH ROW
WHEN (
    ROW(
        OLD.name, OLD.ai_bookable, OLD.active, OLD.archived_at,
        OLD.pos_provider, OLD.sync_status
    ) IS DISTINCT FROM ROW(
        NEW.name, NEW.ai_bookable, NEW.active, NEW.archived_at,
        NEW.pos_provider, NEW.sync_status
    )
)
EXECUTE FUNCTION phase13_bump_answer_context_collection_version('staff');
