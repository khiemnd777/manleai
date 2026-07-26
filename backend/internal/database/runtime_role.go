package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"
)

var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

var (
	ErrRuntimeRoleInvalid   = errors.New("database runtime role is invalid")
	ErrRuntimeRoleUnsafe    = errors.New("database runtime role bypasses row-level security")
	ErrRLSContractNotReady  = errors.New("database row-level security contract is not ready")
	ErrMigrationURLRequired = errors.New("migration database URL is required when RLS enforcement is enabled")
)

func ValidRuntimeRole(role string) bool {
	return runtimeRolePattern.MatchString(strings.TrimSpace(role))
}

func OpenApplication(
	ctx context.Context,
	runtimeDatabaseURL string,
	migrationDatabaseURL string,
	runtimeRole string,
	autoMigrate bool,
	enforceRLS bool,
) (*sql.DB, error) {
	runtimeRole = strings.TrimSpace(runtimeRole)
	migrationDatabaseURL = strings.TrimSpace(migrationDatabaseURL)
	if enforceRLS && !ValidRuntimeRole(runtimeRole) {
		return nil, ErrRuntimeRoleInvalid
	}
	if enforceRLS && autoMigrate && migrationDatabaseURL == "" {
		return nil, ErrMigrationURLRequired
	}
	if autoMigrate {
		migrationURL := migrationDatabaseURL
		if migrationURL == "" {
			migrationURL = runtimeDatabaseURL
		}
		migrationDB, err := Open(ctx, migrationURL)
		if err != nil {
			return nil, err
		}
		if err := Migrate(ctx, migrationDB); err != nil {
			migrationDB.Close()
			return nil, err
		}
		if runtimeRole != "" {
			if err := GrantRuntimePrivileges(ctx, migrationDB, runtimeRole); err != nil {
				migrationDB.Close()
				return nil, err
			}
		}
		if err := migrationDB.Close(); err != nil {
			return nil, err
		}
	}

	runtimeDB, err := Open(ctx, runtimeDatabaseURL)
	if err != nil {
		return nil, err
	}
	if enforceRLS {
		if err := VerifyRuntimeRLS(ctx, runtimeDB, runtimeRole); err != nil {
			runtimeDB.Close()
			return nil, err
		}
	}
	return runtimeDB, nil
}

func GrantRuntimePrivileges(ctx context.Context, migrationDB *sql.DB, role string) error {
	role = strings.TrimSpace(role)
	if migrationDB == nil || !ValidRuntimeRole(role) {
		return ErrRuntimeRoleInvalid
	}
	var exists bool
	if err := migrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrRuntimeRoleInvalid
	}
	quotedRole := pq.QuoteIdentifier(role)
	statements := []string{
		"GRANT USAGE ON SCHEMA public TO " + quotedRole,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + quotedRole,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + quotedRole,
		"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO " + quotedRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + quotedRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + quotedRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + quotedRole,
	}
	for _, statement := range statements {
		if _, err := migrationDB.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("grant runtime database privileges: %w", err)
		}
	}
	return nil
}

func VerifyRuntimeRLS(ctx context.Context, runtimeDB *sql.DB, expectedRole string) error {
	expectedRole = strings.TrimSpace(expectedRole)
	if runtimeDB == nil || !ValidRuntimeRole(expectedRole) {
		return ErrRuntimeRoleInvalid
	}
	var currentRole string
	var superuser, bypassRLS bool
	if err := runtimeDB.QueryRowContext(ctx, `
		SELECT current_user, rolsuper, rolbypassrls
		FROM pg_roles WHERE rolname=current_user
	`).Scan(&currentRole, &superuser, &bypassRLS); err != nil {
		return err
	}
	if currentRole != expectedRole || superuser || bypassRLS {
		return ErrRuntimeRoleUnsafe
	}

	var protectedTableCount, policyCount int
	var ownsOrInheritsProtectedOwner bool
	if err := runtimeDB.QueryRowContext(ctx, `
		SELECT
			count(DISTINCT table_class.oid) FILTER (WHERE table_class.relrowsecurity),
			count(DISTINCT policy.oid),
			COALESCE(bool_or(
				table_class.relrowsecurity
				AND pg_has_role(current_user, table_class.relowner, 'MEMBER')
			), false)
		FROM pg_class table_class
		JOIN pg_namespace table_namespace ON table_namespace.oid=table_class.relnamespace
		LEFT JOIN pg_policy policy ON policy.polrelid=table_class.oid
		WHERE table_namespace.nspname='public'
		  AND (table_class.relname='salons' OR EXISTS (
			SELECT 1 FROM pg_attribute salon_column
			WHERE salon_column.attrelid=table_class.oid
			  AND salon_column.attname='salon_id'
			  AND NOT salon_column.attisdropped
		  ))
	`).Scan(&protectedTableCount, &policyCount, &ownsOrInheritsProtectedOwner); err != nil {
		return err
	}
	if protectedTableCount < 2 || policyCount < protectedTableCount || ownsOrInheritsProtectedOwner {
		return ErrRLSContractNotReady
	}
	return nil
}
