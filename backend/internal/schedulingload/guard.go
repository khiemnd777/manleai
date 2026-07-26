package schedulingload

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/migrations"
)

func validateDatabaseTarget(ctx context.Context, db *sql.DB, config Config) (DatabaseEvidence, error) {
	var databaseName string
	var databaseUser string
	if err := db.QueryRowContext(ctx, `SELECT current_database(), current_user`).Scan(&databaseName, &databaseUser); err != nil {
		return DatabaseEvidence{}, fmt.Errorf("read database identity: %w", err)
	}
	if config.Attestation != RequiredAttestation || databaseName != config.ExpectedDatabaseName || databaseUser != config.ExpectedDatabaseUser ||
		!strings.HasPrefix(strings.ToLower(databaseName), config.DatabasePrefix) || unsafeDatabaseName(databaseName) {
		return DatabaseEvidence{}, ErrUnsafeTarget
	}

	expected, err := expectedMigrationEvidence()
	if err != nil {
		return DatabaseEvidence{}, err
	}
	applied, err := appliedMigrationEvidence(ctx, db)
	if err != nil {
		return DatabaseEvidence{}, err
	}
	if len(expected) != len(applied) {
		return DatabaseEvidence{}, fmt.Errorf("%w: applied migration count %d does not match release count %d", ErrUnsafeTarget, len(applied), len(expected))
	}
	for i := range expected {
		if expected[i].Version != applied[i].Version || expected[i].Checksum != applied[i].Checksum {
			return DatabaseEvidence{}, fmt.Errorf("%w: migration %s checksum/version drift", ErrUnsafeTarget, expected[i].Version)
		}
	}

	marker := syntheticEmail(config.RunID, "owner")
	var alreadyExists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email=$1)`, marker).Scan(&alreadyExists); err != nil {
		return DatabaseEvidence{}, fmt.Errorf("check run identity: %w", err)
	}
	if alreadyExists {
		return DatabaseEvidence{}, ErrRunAlreadyExists
	}

	return DatabaseEvidence{
		Name: databaseName, User: databaseUser, MigrationCount: len(applied), Migrations: applied,
		MigrationChecksumFingerprint: migrationFingerprint(applied),
	}, nil
}

func expectedMigrationEvidence() ([]MigrationEvidence, error) {
	paths, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	items := make([]MigrationEvidence, 0, len(paths))
	for _, filePath := range paths {
		raw, err := migrations.Files.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", filePath, err)
		}
		base := strings.TrimSuffix(path.Base(filePath), ".sql")
		parts := strings.SplitN(base, "__", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "V") {
			return nil, fmt.Errorf("invalid embedded migration name %s", filePath)
		}
		sum := sha256.Sum256(raw)
		items = append(items, MigrationEvidence{
			Version: strings.TrimPrefix(parts[0], "V"), Name: strings.ReplaceAll(parts[1], "_", " "), Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sortMigrationEvidence(items)
	return items, nil
}

func appliedMigrationEvidence(ctx context.Context, db *sql.DB) ([]MigrationEvidence, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT version,name,checksum FROM app_schema_migrations
		ORDER BY CASE WHEN version ~ '^[0-9]+$' THEN version::bigint ELSE 9223372036854775807 END, version
	`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	items := make([]MigrationEvidence, 0)
	for rows.Next() {
		var item MigrationEvidence
		if err := rows.Scan(&item.Version, &item.Name, &item.Checksum); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func sortMigrationEvidence(items []MigrationEvidence) {
	sort.Slice(items, func(i, j int) bool {
		return numericVersion(items[i].Version) < numericVersion(items[j].Version)
	})
}

func numericVersion(value string) int64 {
	var result int64
	for _, runeValue := range value {
		if runeValue < '0' || runeValue > '9' {
			return 1<<63 - 1
		}
		result = result*10 + int64(runeValue-'0')
	}
	return result
}

func migrationFingerprint(items []MigrationEvidence) string {
	hash := sha256.New()
	for _, item := range items {
		_, _ = hash.Write([]byte(item.Version))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(item.Checksum))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func syntheticEmail(runID string, role string) string {
	return "load-" + runID + "-" + role + "@invalid.example"
}
