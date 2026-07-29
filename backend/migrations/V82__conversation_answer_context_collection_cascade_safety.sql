-- Keep V81 answer-context collection revisions compatible with salon
-- deletion. Explicit service/staff mutations still bump the owning salon's
-- collection revision, while an ON DELETE CASCADE from salons must not try to
-- recreate a child version row after its parent salon has gone away.

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

    IF old_salon_id IS NOT NULL
       AND EXISTS (SELECT 1 FROM salons WHERE id = old_salon_id) THEN
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

    IF new_salon_id IS NOT NULL
       AND new_salon_id IS DISTINCT FROM old_salon_id
       AND EXISTS (SELECT 1 FROM salons WHERE id = new_salon_id) THEN
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
