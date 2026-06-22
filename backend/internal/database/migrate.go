package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/manleai/ai-receptionist/migrations"
)

const migrationLockID int64 = 30919910842811

type migrationFile struct {
	Version  string
	Order    int
	Name     string
	Path     string
	SQL      string
	Checksum string
}

func Migrate(ctx context.Context, db *sql.DB) error {
	files, err := loadMigrationFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS app_schema_migrations (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}

	for _, migration := range files {
		applied, err := migrationApplied(ctx, tx, migration)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		legacyApplied, err := legacyMigrationApplied(ctx, tx, migration)
		if err != nil {
			return err
		}
		if legacyApplied {
			if err := markMigrationApplied(ctx, tx, migration); err != nil {
				return err
			}
			continue
		}

		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Path, err)
		}
		if err := markMigrationApplied(ctx, tx, migration); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func loadMigrationFiles() ([]migrationFile, error) {
	paths, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("find migrations: %w", err)
	}

	files := make([]migrationFile, 0, len(paths))
	for _, filePath := range paths {
		raw, err := migrations.Files.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", filePath, err)
		}
		migration, err := parseMigrationFile(filePath, string(raw))
		if err != nil {
			return nil, err
		}
		files = append(files, migration)
	}
	sortMigrationFiles(files)
	return files, nil
}

func sortMigrationFiles(files []migrationFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Order == files[j].Order {
			return files[i].Path < files[j].Path
		}
		return files[i].Order < files[j].Order
	})
}

func parseMigrationFile(filePath string, sqlText string) (migrationFile, error) {
	base := path.Base(filePath)
	name := strings.TrimSuffix(base, ".sql")
	parts := strings.SplitN(name, "__", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "V") || strings.TrimPrefix(parts[0], "V") == "" {
		return migrationFile{}, fmt.Errorf("invalid migration filename %s", filePath)
	}
	version := strings.TrimPrefix(parts[0], "V")
	order, err := strconv.Atoi(version)
	if err != nil {
		return migrationFile{}, fmt.Errorf("invalid migration version %s in %s", version, filePath)
	}

	sum := sha256.Sum256([]byte(sqlText))
	return migrationFile{
		Version:  version,
		Order:    order,
		Name:     strings.ReplaceAll(parts[1], "_", " "),
		Path:     filePath,
		SQL:      sqlText,
		Checksum: hex.EncodeToString(sum[:]),
	}, nil
}

func migrationApplied(ctx context.Context, tx *sql.Tx, migration migrationFile) (bool, error) {
	var checksum string
	err := tx.QueryRowContext(ctx, `
		SELECT checksum
		FROM app_schema_migrations
		WHERE version = $1
	`, migration.Version).Scan(&checksum)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", migration.Path, err)
	}
	if checksum != migration.Checksum {
		return false, fmt.Errorf("migration %s checksum changed after it was applied", migration.Path)
	}
	return true, nil
}

func legacyMigrationApplied(ctx context.Context, tx *sql.Tx, migration migrationFile) (bool, error) {
	flywayApplied, err := flywayMigrationApplied(ctx, tx, migration.Version)
	if err != nil {
		return false, err
	}
	if flywayApplied {
		return true, nil
	}

	if migration.Version != "1" {
		return false, nil
	}
	return foundationSchemaExists(ctx, tx)
}

func flywayMigrationApplied(ctx context.Context, tx *sql.Tx, version string) (bool, error) {
	var tableExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'flyway_schema_history'
		)
	`).Scan(&tableExists); err != nil {
		return false, fmt.Errorf("check flyway history table: %w", err)
	}
	if !tableExists {
		return false, nil
	}

	var applied bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM flyway_schema_history
			WHERE version = $1
			  AND success = TRUE
		)
	`, version).Scan(&applied); err != nil {
		return false, fmt.Errorf("check flyway migration %s: %w", version, err)
	}
	return applied, nil
}

func foundationSchemaExists(ctx context.Context, tx *sql.Tx) (bool, error) {
	const expectedTables = 14

	var tableCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN (
			'users',
			'roles',
			'permissions',
			'user_roles',
			'role_permissions',
			'refresh_tokens',
			'salons',
			'salon_business_hours',
			'salon_settings',
			'services',
			'staff',
			'pos_connections',
			'pos_sync_logs',
			'pos_errors'
		  )
	`).Scan(&tableCount); err != nil {
		return false, fmt.Errorf("check foundation schema: %w", err)
	}

	if tableCount == 0 {
		return false, nil
	}
	if tableCount != expectedTables {
		return false, fmt.Errorf("partial foundation schema exists without a migration record: found %d of %d expected tables", tableCount, expectedTables)
	}
	return true, nil
}

func markMigrationApplied(ctx context.Context, tx *sql.Tx, migration migrationFile) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_schema_migrations (version, name, checksum)
		VALUES ($1, $2, $3)
		ON CONFLICT (version) DO NOTHING
	`, migration.Version, migration.Name, migration.Checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Path, err)
	}
	return nil
}
