-- One login identity belongs to exactly one authorization realm. A person who
-- works in both realms must use separate tenant and platform identities.
--
-- This migration deliberately fails when historical data mixes the two
-- realms. Automatically choosing one side would silently remove or expand
-- access, so an operator must remediate those identities before retrying.
ALTER TABLE users
    ADD COLUMN principal_scope TEXT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users AS account
        WHERE (
            EXISTS (SELECT 1 FROM salons AS salon WHERE salon.owner_user_id = account.id)
            OR EXISTS (SELECT 1 FROM salon_memberships AS membership WHERE membership.user_id = account.id)
            OR EXISTS (SELECT 1 FROM user_roles AS assignment WHERE assignment.user_id = account.id)
        )
        AND (
            EXISTS (SELECT 1 FROM platform_role_assignments AS assignment WHERE assignment.user_id = account.id)
            OR EXISTS (SELECT 1 FROM platform_salon_assignments AS assignment WHERE assignment.user_id = account.id)
            OR EXISTS (SELECT 1 FROM platform_pii_access_grants AS grant_record WHERE grant_record.user_id = account.id)
        )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'existing users mix tenant and platform principals; split each human into separate login identities before applying V74';
    END IF;
END
$$;

UPDATE users AS account
SET principal_scope = 'platform'
WHERE EXISTS (
    SELECT 1 FROM platform_role_assignments AS assignment
    WHERE assignment.user_id = account.id
)
OR EXISTS (
    SELECT 1 FROM platform_salon_assignments AS assignment
    WHERE assignment.user_id = account.id
)
OR EXISTS (
    SELECT 1 FROM platform_pii_access_grants AS grant_record
    WHERE grant_record.user_id = account.id
);

UPDATE users
SET principal_scope = 'tenant'
WHERE principal_scope IS NULL;

ALTER TABLE users
    ALTER COLUMN principal_scope SET DEFAULT 'tenant',
    ALTER COLUMN principal_scope SET NOT NULL,
    ADD CONSTRAINT users_principal_scope_check
        CHECK (principal_scope IN ('tenant', 'platform')),
    ADD CONSTRAINT users_id_principal_scope_key
        UNIQUE (id, principal_scope);

CREATE INDEX idx_users_principal_scope
    ON users (principal_scope, status, full_name, email, id);

ALTER TABLE salons
    ADD COLUMN owner_principal_scope TEXT NOT NULL DEFAULT 'tenant'
        CHECK (owner_principal_scope = 'tenant'),
    ADD CONSTRAINT salons_owner_principal_scope_fk
        FOREIGN KEY (owner_user_id, owner_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE RESTRICT;

ALTER TABLE salon_memberships
    ADD COLUMN user_principal_scope TEXT NOT NULL DEFAULT 'tenant'
        CHECK (user_principal_scope = 'tenant'),
    ADD CONSTRAINT salon_memberships_user_principal_scope_fk
        FOREIGN KEY (user_id, user_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE CASCADE;

ALTER TABLE user_roles
    ADD COLUMN user_principal_scope TEXT NOT NULL DEFAULT 'tenant'
        CHECK (user_principal_scope = 'tenant'),
    ADD CONSTRAINT user_roles_user_principal_scope_fk
        FOREIGN KEY (user_id, user_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE CASCADE;

ALTER TABLE platform_role_assignments
    ADD COLUMN user_principal_scope TEXT NOT NULL DEFAULT 'platform'
        CHECK (user_principal_scope = 'platform'),
    ADD CONSTRAINT platform_role_assignments_user_principal_scope_fk
        FOREIGN KEY (user_id, user_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE CASCADE;

ALTER TABLE platform_salon_assignments
    ADD COLUMN user_principal_scope TEXT NOT NULL DEFAULT 'platform'
        CHECK (user_principal_scope = 'platform'),
    ADD CONSTRAINT platform_salon_assignments_user_principal_scope_fk
        FOREIGN KEY (user_id, user_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE CASCADE;

ALTER TABLE platform_pii_access_grants
    ADD COLUMN user_principal_scope TEXT NOT NULL DEFAULT 'platform'
        CHECK (user_principal_scope = 'platform'),
    ADD CONSTRAINT platform_pii_access_grants_user_principal_scope_fk
        FOREIGN KEY (user_id, user_principal_scope)
        REFERENCES users(id, principal_scope)
        ON DELETE CASCADE;

CREATE FUNCTION enforce_principal_scope_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.principal_scope IS DISTINCT FROM NEW.principal_scope THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'principal scope is immutable',
            CONSTRAINT = 'users_principal_scope_immutable_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_principal_scope_immutable_guard
BEFORE UPDATE OF principal_scope ON users
FOR EACH ROW
EXECUTE FUNCTION enforce_principal_scope_immutable();

COMMENT ON COLUMN users.principal_scope IS
'Immutable authorization realm for one login identity. A person requiring tenant and platform access must use separate identities.';
