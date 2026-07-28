-- Platform Admin is the control-plane authority. Platform Ops remains
-- salon-scoped and receives explicit baseline and temporary capabilities from
-- an Admin. Tenant Owner membership can be suspended without changing salon
-- ownership.

CREATE OR REPLACE FUNCTION public.app_platform_admin_capability(
    target_actor_id UUID,
    required_capability TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments assignment
          ON assignment.user_id = account.id
         AND assignment.status = 'active'
        JOIN public.roles role
          ON role.id = assignment.role_id
         AND role.name = 'platform_admin'
        JOIN public.role_permissions role_permission
          ON role_permission.role_id = role.id
        JOIN public.permissions permission
          ON permission.id = role_permission.permission_id
         AND permission.name = required_capability
        WHERE account.id = target_actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'platform'
    )
$$;

CREATE OR REPLACE FUNCTION public.app_active_support_authorization(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments role_assignment
          ON role_assignment.user_id = account.id
         AND role_assignment.status = 'active'
        JOIN public.roles role
          ON role.id = role_assignment.role_id
         AND role.name = 'platform_ops'
        JOIN public.platform_salon_assignments salon_assignment
          ON salon_assignment.user_id = account.id
         AND salon_assignment.salon_id = target_salon_id
         AND salon_assignment.status = 'active'
        JOIN public.platform_salon_assignment_permissions assignment_permission
          ON assignment_permission.assignment_id = salon_assignment.id
        JOIN public.permissions base_permission
          ON base_permission.id = assignment_permission.permission_id
         AND base_permission.name = required_capability
        JOIN public.platform_support_access_requests request
          ON request.platform_user_id = account.id
         AND request.salon_id = target_salon_id
         AND request.status = 'approved'
         AND request.approved_expires_at > now()
        JOIN public.platform_support_access_request_permissions child
          ON child.request_id = request.id
        JOIN public.permissions granted_permission
          ON granted_permission.id = child.permission_id
         AND granted_permission.name = required_capability
        WHERE account.id = target_actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'platform'
    )
$$;

CREATE OR REPLACE FUNCTION public.app_active_support_pii_grant(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT,
    required_pii_scope TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT public.app_active_support_authorization(
               target_actor_id, target_salon_id, required_capability
           )
       AND EXISTS (
            SELECT 1
            FROM public.platform_pii_access_grants grant_record
            JOIN public.platform_support_access_requests request
              ON request.id = grant_record.support_access_request_id
             AND request.status = 'approved'
             AND request.approved_expires_at > now()
            JOIN public.platform_support_access_request_permissions child
              ON child.request_id = request.id
             AND child.pii_scope = required_pii_scope
            WHERE grant_record.user_id = target_actor_id
              AND grant_record.salon_id = target_salon_id
              AND grant_record.scope = required_pii_scope
              AND grant_record.revoked_at IS NULL
              AND grant_record.expires_at > now()
       )
$$;

CREATE OR REPLACE FUNCTION public.app_actor_feature_access(
    target_actor_id UUID,
    target_salon_id UUID,
    required_capability TEXT,
    required_pii_scope TEXT DEFAULT NULL
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
BEGIN
    IF target_actor_id IS NULL OR target_salon_id IS NULL THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.salon_memberships membership
          ON membership.user_id = account.id
         AND membership.salon_id = target_salon_id
         AND membership.status = 'active'
        JOIN public.role_permissions role_permission
          ON role_permission.role_id = membership.role_id
        JOIN public.permissions permission
          ON permission.id = role_permission.permission_id
         AND permission.name = required_capability
        WHERE account.id = target_actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'tenant'
    ) THEN
        RETURN true;
    END IF;

    IF public.app_platform_admin_capability(target_actor_id, required_capability) THEN
        RETURN true;
    END IF;

    IF NOT public.app_active_support_authorization(
        target_actor_id, target_salon_id, required_capability
    ) THEN
        RETURN false;
    END IF;
    IF required_pii_scope IS NULL OR required_pii_scope = '' THEN
        RETURN true;
    END IF;
    RETURN public.app_active_support_pii_grant(
        target_actor_id, target_salon_id, required_capability, required_pii_scope
    );
END
$$;

CREATE OR REPLACE FUNCTION public.app_rls_actor_salon_access(
    target_salon_id UUID,
    required_pii_scope TEXT DEFAULT NULL
)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    actor_id UUID := public.app_request_actor_user_id();
    ops_allowed BOOLEAN := false;
BEGIN
    IF actor_id IS NULL OR target_salon_id IS NULL THEN
        RETURN false;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.salon_memberships membership
          ON membership.user_id = account.id
         AND membership.salon_id = target_salon_id
         AND membership.status = 'active'
        WHERE account.id = actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'tenant'
    ) THEN
        RETURN true;
    END IF;

    IF public.app_platform_admin_capability(actor_id, 'platform.tenants.read') THEN
        RETURN true;
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments role_assignment
          ON role_assignment.user_id = account.id
         AND role_assignment.status = 'active'
        JOIN public.roles role
          ON role.id = role_assignment.role_id
         AND role.name = 'platform_ops'
        JOIN public.platform_salon_assignments salon_assignment
          ON salon_assignment.user_id = account.id
         AND salon_assignment.salon_id = target_salon_id
         AND salon_assignment.status = 'active'
        WHERE account.id = actor_id
          AND account.status = 'active'
          AND account.principal_scope = 'platform'
    ) INTO ops_allowed;

    IF NOT ops_allowed THEN
        RETURN false;
    END IF;
    IF required_pii_scope IS NULL OR required_pii_scope = '' THEN
        RETURN true;
    END IF;
    IF required_pii_scope NOT IN ('customers', 'calls', 'appointments', 'notifications') THEN
        RETURN false;
    END IF;
    RETURN EXISTS (
        SELECT 1
        FROM public.platform_pii_access_grants grant_record
        WHERE grant_record.salon_id = target_salon_id
          AND grant_record.user_id = actor_id
          AND grant_record.scope = required_pii_scope
          AND grant_record.revoked_at IS NULL
          AND grant_record.expires_at > now()
    );
END
$$;

-- Updating an unrelated salon column must not silently reactivate a suspended
-- Owner membership. A real ownership transfer still revokes the old owner and
-- activates the new owner.
CREATE OR REPLACE FUNCTION public.phase2_sync_salon_owner_membership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_role_id UUID;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.owner_user_id IS NOT DISTINCT FROM NEW.owner_user_id THEN
        RETURN NEW;
    END IF;

    SELECT id INTO owner_role_id FROM public.roles
    WHERE name = 'tenant_owner' AND scope = 'tenant';
    IF owner_role_id IS NULL THEN
        RAISE EXCEPTION 'tenant_owner role is unavailable';
    END IF;

    IF TG_OP = 'UPDATE' THEN
        UPDATE public.salon_memberships
        SET status='revoked', is_owner=false, version=version+1,
            updated_by_user_id=NEW.owner_user_id, updated_at=now()
        WHERE salon_id=NEW.id AND user_id=OLD.owner_user_id AND is_owner;
    END IF;

    INSERT INTO public.salon_memberships (
        salon_id,user_id,role_id,status,is_owner,version,
        created_by_user_id,updated_by_user_id
    ) VALUES (
        NEW.id,NEW.owner_user_id,owner_role_id,'active',true,1,
        NEW.owner_user_id,NEW.owner_user_id
    )
    ON CONFLICT (salon_id,user_id) DO UPDATE
    SET role_id=EXCLUDED.role_id,status='active',is_owner=true,
        version=salon_memberships.version+1,
        updated_by_user_id=EXCLUDED.updated_by_user_id,updated_at=now();
    RETURN NEW;
END
$$;

DROP POLICY IF EXISTS platform_support_requests_select ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_select ON public.platform_support_access_requests
FOR SELECT USING (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR platform_user_id = public.app_request_actor_user_id()
    OR public.app_database_scope() IN ('worker', 'provider')
);

DROP POLICY IF EXISTS platform_support_requests_insert ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_insert ON public.platform_support_access_requests
FOR INSERT WITH CHECK (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR public.app_database_scope() = 'worker'
);

DROP POLICY IF EXISTS platform_support_requests_update ON public.platform_support_access_requests;
CREATE POLICY platform_support_requests_update ON public.platform_support_access_requests
FOR UPDATE USING (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR public.app_database_scope() = 'worker'
) WITH CHECK (
    public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
    OR public.app_database_scope() = 'worker'
);

DROP POLICY IF EXISTS platform_support_permissions_all ON public.platform_support_access_request_permissions;
CREATE POLICY platform_support_permissions_all ON public.platform_support_access_request_permissions
FOR ALL USING (
    EXISTS (
        SELECT 1
        FROM public.platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              request.platform_user_id = public.app_request_actor_user_id()
              OR public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
              OR public.app_database_scope() IN ('worker', 'provider')
          )
    )
) WITH CHECK (
    EXISTS (
        SELECT 1
        FROM public.platform_support_access_requests request
        WHERE request.id = platform_support_access_request_permissions.request_id
          AND (
              public.app_platform_admin_capability(public.app_request_actor_user_id(), 'platform.access.manage')
              OR public.app_database_scope() = 'worker'
          )
    )
);

COMMENT ON TABLE public.platform_support_access_requests IS
'Admin-granted, time-bounded authorization for one Platform Ops principal to support one salon. Platform Admin access is direct and never depends on these rows.';
COMMENT ON TABLE public.platform_support_access_request_permissions IS
'Exact temporary capabilities and PII scopes granted by a Platform Admin to one Platform Ops principal.';

REVOKE ALL ON FUNCTION public.app_platform_admin_capability(UUID, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_platform_admin_capability(UUID, TEXT) TO PUBLIC;
