-- SaaS Phase 7 runtime membership boundary.
--
-- Tenant Business users keep their own actor identity. Runtime repositories
-- use this predicate instead of substituting the salon owner's user ID.

CREATE OR REPLACE FUNCTION public.has_active_tenant_membership(
    target_salon_id UUID,
    actor_user_id UUID
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM salon_memberships membership
        JOIN roles role ON role.id = membership.role_id
        WHERE membership.salon_id = target_salon_id
          AND membership.user_id = actor_user_id
          AND membership.status = 'active'
          AND role.scope = 'tenant'
          AND role.name IN ('tenant_owner', 'tenant_business_manager')
    )
$$;

REVOKE ALL ON FUNCTION public.has_active_tenant_membership(UUID, UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.has_active_tenant_membership(UUID, UUID) TO PUBLIC;

COMMENT ON FUNCTION public.has_active_tenant_membership(UUID, UUID) IS
'Authorization predicate only. It preserves the actual actor and never returns or substitutes the salon owner identity.';

CREATE OR REPLACE FUNCTION public.has_platform_salon_capability(
    target_salon_id UUID,
    actor_user_id UUID,
    required_capability TEXT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM users account
        JOIN platform_role_assignments platform_role
          ON platform_role.user_id = account.id
         AND platform_role.status = 'active'
        JOIN roles role ON role.id = platform_role.role_id
        WHERE account.id = actor_user_id
          AND account.status = 'active'
          AND (
              (
                  role.name = 'platform_admin'
                  AND EXISTS (
                      SELECT 1
                      FROM role_permissions role_permission
                      JOIN permissions permission
                        ON permission.id = role_permission.permission_id
                      WHERE role_permission.role_id = role.id
                        AND permission.name = required_capability
                  )
              )
              OR (
                  role.name = 'platform_ops'
                  AND EXISTS (
                      SELECT 1
                      FROM platform_salon_assignments salon_assignment
                      JOIN platform_salon_assignment_permissions assignment_permission
                        ON assignment_permission.assignment_id = salon_assignment.id
                      JOIN permissions permission
                        ON permission.id = assignment_permission.permission_id
                      WHERE salon_assignment.user_id = account.id
                        AND salon_assignment.salon_id = target_salon_id
                        AND salon_assignment.status = 'active'
                        AND permission.name = required_capability
                  )
              )
          )
    )
$$;

REVOKE ALL ON FUNCTION public.has_platform_salon_capability(UUID, UUID, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.has_platform_salon_capability(UUID, UUID, TEXT) TO PUBLIC;

COMMENT ON FUNCTION public.has_platform_salon_capability(UUID, UUID, TEXT) IS
'Database-side defense for fixed Platform routes. It validates the actual actor, salon assignment, and delegated capability without impersonating a tenant owner.';
