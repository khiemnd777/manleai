-- Phase 2 SaaS access-control foundation.
--
-- This is an expand-only migration. Existing owner_user_id, user_roles,
-- role_permissions, and owner-scoped application queries remain valid during
-- the rollback window. New code can resolve tenant membership and platform
-- delegation without reinterpreting the legacy authorization paths.

ALTER TABLE roles
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'legacy';

ALTER TABLE permissions
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'legacy';

ALTER TABLE permissions
    ADD COLUMN IF NOT EXISTS delegation_scope TEXT NOT NULL DEFAULT 'none';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'roles_scope_check'
          AND conrelid = 'roles'::regclass
    ) THEN
        ALTER TABLE roles
            ADD CONSTRAINT roles_scope_check
            CHECK (scope IN ('legacy', 'tenant', 'platform'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'permissions_scope_check'
          AND conrelid = 'permissions'::regclass
    ) THEN
        ALTER TABLE permissions
            ADD CONSTRAINT permissions_scope_check
            CHECK (scope IN ('legacy', 'tenant', 'platform'));
    END IF;

	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'permissions_delegation_scope_check'
		  AND conrelid = 'permissions'::regclass
	) THEN
		ALTER TABLE permissions
			ADD CONSTRAINT permissions_delegation_scope_check
			CHECK (delegation_scope IN ('none', 'salon'));
	END IF;
END
$$;

INSERT INTO roles (name, display_name, scope)
VALUES
    ('tenant_owner', 'Tenant Owner', 'tenant'),
    ('tenant_business_manager', 'Tenant Business Manager', 'tenant'),
    ('platform_admin', 'Platform Administrator', 'platform'),
    ('platform_ops', 'Platform Operations', 'platform')
ON CONFLICT (name) DO UPDATE
SET display_name = EXCLUDED.display_name,
    scope = EXCLUDED.scope,
    updated_at = now();

INSERT INTO permissions (name, display_name, scope, delegation_scope)
VALUES
    ('platform.tenants.read', 'Read platform tenant directory', 'platform', 'none'),
    ('platform.access.manage', 'Manage platform and tenant access', 'platform', 'none'),
    ('business.read', 'Read tenant business data', 'tenant', 'salon'),
    ('business.write', 'Manage tenant business data', 'tenant', 'salon'),
    ('technical.read', 'Read tenant technical configuration', 'platform', 'salon'),
    ('technical.write', 'Manage tenant technical configuration', 'platform', 'salon'),
    ('operations.read', 'Read tenant operational health', 'platform', 'salon'),
    ('operations.write', 'Manage tenant operational recovery actions', 'platform', 'salon'),
    ('audit.read', 'Read tenant access and operational audit', 'platform', 'salon')
ON CONFLICT (name) DO UPDATE
SET display_name = EXCLUDED.display_name,
    scope = EXCLUDED.scope,
	delegation_scope = EXCLUDED.delegation_scope,
    updated_at = now();

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles AS role
JOIN permissions AS permission
  ON (
      role.name = 'platform_admin'
      AND permission.name IN (
          'platform.tenants.read',
          'platform.access.manage',
          'business.read',
          'business.write',
          'technical.read',
          'technical.write',
          'operations.read',
          'operations.write',
          'audit.read'
      )
  )
  OR (
      role.name = 'platform_ops'
      AND permission.name = 'platform.tenants.read'
  )
  OR (
      role.name IN ('tenant_owner', 'tenant_business_manager')
      AND permission.name IN ('business.read', 'business.write')
  )
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS salon_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    is_owner BOOLEAN NOT NULL DEFAULT false,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_salon_memberships_user_active
    ON salon_memberships (user_id, salon_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_salon_memberships_one_owner
    ON salon_memberships (salon_id)
    WHERE is_owner AND status = 'active';

CREATE TABLE IF NOT EXISTS platform_role_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_platform_role_assignments_active_user
    ON platform_role_assignments (user_id, role_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS platform_salon_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salon_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_platform_salon_assignments_active_user
    ON platform_salon_assignments (user_id, salon_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS platform_salon_assignment_permissions (
    assignment_id UUID NOT NULL
        REFERENCES platform_salon_assignments(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL
        REFERENCES permissions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, permission_id)
);

CREATE TABLE IF NOT EXISTS platform_pii_access_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope TEXT NOT NULL
        CHECK (scope IN ('customers', 'calls', 'appointments', 'notifications')),
    reason TEXT NOT NULL
        CHECK (reason ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '24 hours'),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX IF NOT EXISTS idx_platform_pii_access_grants_active
    ON platform_pii_access_grants (user_id, salon_id, scope, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS access_control_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    salon_id UUID REFERENCES salons(id) ON DELETE RESTRICT,
    target_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    action_key TEXT NOT NULL
        CHECK (action_key = btrim(action_key) AND length(action_key) BETWEEN 1 AND 256),
    action_type TEXT NOT NULL
        CHECK (action_type = btrim(action_type) AND length(action_type) BETWEEN 1 AND 128),
    request_fingerprint TEXT NOT NULL
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (actor_user_id, action_key)
);

CREATE TABLE IF NOT EXISTS access_control_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id UUID REFERENCES access_control_actions(id) ON DELETE RESTRICT,
    actor_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    salon_id UUID REFERENCES salons(id) ON DELETE RESTRICT,
    target_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL
        CHECK (event_type = btrim(event_type) AND length(event_type) BETWEEN 1 AND 128),
    object_type TEXT NOT NULL
        CHECK (object_type = btrim(object_type) AND length(object_type) BETWEEN 1 AND 64),
    object_id TEXT NOT NULL
        CHECK (object_id = btrim(object_id) AND length(object_id) BETWEEN 1 AND 256),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_access_control_events_salon_created
    ON access_control_events (salon_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_access_control_events_actor_created
    ON access_control_events (actor_user_id, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION phase2_require_scoped_access_reference()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    referenced_scope TEXT;
BEGIN
    IF TG_TABLE_NAME = 'salon_memberships' THEN
        SELECT scope INTO referenced_scope FROM roles WHERE id = NEW.role_id;
        IF referenced_scope IS DISTINCT FROM 'tenant' THEN
            RAISE EXCEPTION 'salon membership role must be tenant scoped';
        END IF;
    ELSIF TG_TABLE_NAME = 'platform_role_assignments' THEN
        SELECT scope INTO referenced_scope FROM roles WHERE id = NEW.role_id;
        IF referenced_scope IS DISTINCT FROM 'platform' THEN
            RAISE EXCEPTION 'platform role assignment must be platform scoped';
        END IF;
    ELSIF TG_TABLE_NAME = 'platform_salon_assignment_permissions' THEN
        SELECT delegation_scope INTO referenced_scope FROM permissions WHERE id = NEW.permission_id;
        IF referenced_scope IS DISTINCT FROM 'salon' THEN
            RAISE EXCEPTION 'platform salon assignment permission is not delegable';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS salon_memberships_require_tenant_role ON salon_memberships;
CREATE TRIGGER salon_memberships_require_tenant_role
BEFORE INSERT OR UPDATE OF role_id ON salon_memberships
FOR EACH ROW EXECUTE FUNCTION phase2_require_scoped_access_reference();

DROP TRIGGER IF EXISTS platform_role_assignments_require_platform_role ON platform_role_assignments;
CREATE TRIGGER platform_role_assignments_require_platform_role
BEFORE INSERT OR UPDATE OF role_id ON platform_role_assignments
FOR EACH ROW EXECUTE FUNCTION phase2_require_scoped_access_reference();

DROP TRIGGER IF EXISTS platform_assignment_permissions_require_delegable_permission
    ON platform_salon_assignment_permissions;
CREATE TRIGGER platform_assignment_permissions_require_delegable_permission
BEFORE INSERT OR UPDATE OF permission_id ON platform_salon_assignment_permissions
FOR EACH ROW EXECUTE FUNCTION phase2_require_scoped_access_reference();

CREATE OR REPLACE FUNCTION phase2_reject_access_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

DROP TRIGGER IF EXISTS access_control_actions_immutable ON access_control_actions;
CREATE TRIGGER access_control_actions_immutable
BEFORE UPDATE OR DELETE ON access_control_actions
FOR EACH ROW EXECUTE FUNCTION phase2_reject_access_ledger_mutation();

DROP TRIGGER IF EXISTS access_control_events_immutable ON access_control_events;
CREATE TRIGGER access_control_events_immutable
BEFORE UPDATE OR DELETE ON access_control_events
FOR EACH ROW EXECUTE FUNCTION phase2_reject_access_ledger_mutation();

INSERT INTO salon_memberships (
    salon_id,
    user_id,
    role_id,
    status,
    is_owner,
    version,
    created_by_user_id,
    updated_by_user_id
)
SELECT salon.id,
       salon.owner_user_id,
       role.id,
       'active',
       true,
       1,
       salon.owner_user_id,
       salon.owner_user_id
FROM salons AS salon
JOIN roles AS role ON role.name = 'tenant_owner'
ON CONFLICT (salon_id, user_id) DO UPDATE
SET role_id = EXCLUDED.role_id,
    status = 'active',
    is_owner = true,
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = now();

CREATE OR REPLACE FUNCTION phase2_sync_salon_owner_membership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_role_id UUID;
BEGIN
    SELECT id INTO owner_role_id
    FROM roles
    WHERE name = 'tenant_owner' AND scope = 'tenant';

    IF owner_role_id IS NULL THEN
        RAISE EXCEPTION 'tenant_owner role is unavailable';
    END IF;

    IF TG_OP = 'UPDATE' AND OLD.owner_user_id IS DISTINCT FROM NEW.owner_user_id THEN
        UPDATE salon_memberships
        SET status = 'revoked',
            is_owner = false,
            version = version + 1,
            updated_by_user_id = NEW.owner_user_id,
            updated_at = now()
        WHERE salon_id = NEW.id
          AND user_id = OLD.owner_user_id
          AND is_owner;
    END IF;

    INSERT INTO salon_memberships (
        salon_id,
        user_id,
        role_id,
        status,
        is_owner,
        version,
        created_by_user_id,
        updated_by_user_id
    )
    VALUES (
        NEW.id,
        NEW.owner_user_id,
        owner_role_id,
        'active',
        true,
        1,
        NEW.owner_user_id,
        NEW.owner_user_id
    )
    ON CONFLICT (salon_id, user_id) DO UPDATE
    SET role_id = EXCLUDED.role_id,
        status = 'active',
        is_owner = true,
        version = CASE
            WHEN salon_memberships.role_id IS DISTINCT FROM EXCLUDED.role_id
              OR salon_memberships.status IS DISTINCT FROM 'active'
              OR salon_memberships.is_owner IS DISTINCT FROM true
            THEN salon_memberships.version + 1
            ELSE salon_memberships.version
        END,
        updated_by_user_id = EXCLUDED.updated_by_user_id,
        updated_at = CASE
            WHEN salon_memberships.role_id IS DISTINCT FROM EXCLUDED.role_id
              OR salon_memberships.status IS DISTINCT FROM 'active'
              OR salon_memberships.is_owner IS DISTINCT FROM true
            THEN now()
            ELSE salon_memberships.updated_at
        END;

    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS salons_sync_owner_membership ON salons;
CREATE TRIGGER salons_sync_owner_membership
AFTER INSERT OR UPDATE OF owner_user_id ON salons
FOR EACH ROW EXECUTE FUNCTION phase2_sync_salon_owner_membership();

INSERT INTO access_control_events (
    actor_user_id,
    salon_id,
    target_user_id,
    event_type,
    object_type,
    object_id,
    details
)
SELECT membership.user_id,
       membership.salon_id,
       membership.user_id,
       'tenant.owner_membership_backfilled',
       'salon_membership',
       membership.id::text,
       jsonb_build_object('role', 'tenant_owner', 'source', 'salons.owner_user_id')
FROM salon_memberships AS membership
WHERE membership.is_owner
  AND membership.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM access_control_events AS event
      WHERE event.event_type = 'tenant.owner_membership_backfilled'
        AND event.object_type = 'salon_membership'
        AND event.object_id = membership.id::text
  );
