LOCK TABLE public.service_aliases, public.service_category_aliases
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    conflicting_alias_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO conflicting_alias_count
    FROM public.service_aliases service_alias
    JOIN public.service_category_aliases category_alias
      ON category_alias.salon_id = service_alias.salon_id
     AND category_alias.normalized_alias = service_alias.normalized_alias
     AND category_alias.status = 'active'
    WHERE service_alias.status = 'active';

    IF conflicting_alias_count > 0 THEN
        RAISE EXCEPTION 'cannot install cross-table alias ownership guard: % active salon/normalized-alias conflicts exist', conflicting_alias_count
            USING ERRCODE = '23514',
                  CONSTRAINT = 'service_alias_cross_table_active_unique',
                  HINT = 'Archive one active owner for every conflicting salon and normalized alias, then retry V58. V58 does not repair or discard alias data.';
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION public.lock_service_alias_ownership(
    target_salon_id UUID,
    target_normalized_alias TEXT
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF target_salon_id IS NULL
       OR target_normalized_alias IS NULL
       OR target_normalized_alias = ''
       OR target_normalized_alias <> pg_catalog.btrim(target_normalized_alias) THEN
        RAISE EXCEPTION 'service alias ownership requires a salon and canonical normalized alias'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'service_alias_normalized_key_valid';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'manleai:service-alias-ownership:' || target_salon_id::TEXT || ':' || target_normalized_alias,
            0
        )
    );
END
$$;

CREATE OR REPLACE FUNCTION public.enforce_service_alias_ownership()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, public
AS $$
DECLARE
    conflicting_owner_exists BOOLEAN;
BEGIN
    IF TG_OP = 'UPDATE'
       AND (NEW.salon_id IS DISTINCT FROM OLD.salon_id
            OR NEW.normalized_alias IS DISTINCT FROM OLD.normalized_alias) THEN
        RAISE EXCEPTION 'service alias ownership key is immutable'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'service_alias_ownership_key_immutable';
    END IF;

    PERFORM public.lock_service_alias_ownership(NEW.salon_id, NEW.normalized_alias);

    IF NEW.status <> 'active' THEN
        RETURN NEW;
    END IF;

    IF TG_TABLE_SCHEMA = 'public' AND TG_TABLE_NAME = 'service_aliases' THEN
        SELECT EXISTS (
            SELECT 1
            FROM public.service_category_aliases category_alias
            WHERE category_alias.salon_id = NEW.salon_id
              AND category_alias.normalized_alias = NEW.normalized_alias
              AND category_alias.status = 'active'
        ) INTO conflicting_owner_exists;
    ELSIF TG_TABLE_SCHEMA = 'public' AND TG_TABLE_NAME = 'service_category_aliases' THEN
        SELECT EXISTS (
            SELECT 1
            FROM public.service_aliases service_alias
            WHERE service_alias.salon_id = NEW.salon_id
              AND service_alias.normalized_alias = NEW.normalized_alias
              AND service_alias.status = 'active'
        ) INTO conflicting_owner_exists;
    ELSE
        RAISE EXCEPTION 'service alias ownership trigger is attached to an unsupported table'
            USING ERRCODE = '55000';
    END IF;

    IF conflicting_owner_exists THEN
        RAISE EXCEPTION 'normalized alias already has an active owner in the other alias namespace'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'service_alias_cross_table_active_unique';
    END IF;

    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS service_aliases_ownership_guard ON public.service_aliases;
CREATE TRIGGER service_aliases_ownership_guard
BEFORE INSERT OR UPDATE ON public.service_aliases
FOR EACH ROW
EXECUTE FUNCTION public.enforce_service_alias_ownership();

DROP TRIGGER IF EXISTS service_category_aliases_ownership_guard ON public.service_category_aliases;
CREATE TRIGGER service_category_aliases_ownership_guard
BEFORE INSERT OR UPDATE ON public.service_category_aliases
FOR EACH ROW
EXECUTE FUNCTION public.enforce_service_alias_ownership();

