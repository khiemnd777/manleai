package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	appdatabase "github.com/manleai/ai-receptionist/internal/database"
)

func TestRunAgainstFreshIsolatedPostgres(t *testing.T) {
	databaseURL := os.Getenv("SCHEDULING_LOAD_FRESH_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SCHEDULING_LOAD_FRESH_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if os.Getenv("SCHEDULING_LOAD_FRESH_DATABASE_MIGRATE") == RequiredAttestation {
		if err := appdatabase.Migrate(context.Background(), db); err != nil {
			t.Fatalf("migrate fresh isolated database: %v", err)
		}
	}
	var databaseName string
	var databaseUser string
	if err := db.QueryRow(`SELECT current_database(), current_user`).Scan(&databaseName, &databaseUser); err != nil {
		t.Fatalf("read database identity: %v", err)
	}
	if !stringsHasDedicatedPrefix(databaseName) {
		t.Fatalf("database %q does not use required %q prefix", databaseName, DefaultDatabasePrefix)
	}
	baseConfig := Config{
		DatabaseURL: databaseURL, ExpectedDatabaseName: databaseName, ExpectedDatabaseUser: databaseUser, DatabasePrefix: DefaultDatabasePrefix,
		Attestation: RequiredAttestation, Release: "fresh-pg-integration", Seed: 17,
		Concurrency: 2, OperationsPerWorkload: 2, Duration: 90 * time.Second,
	}
	runIDs := []string{uuid.NewString(), uuid.NewString()}
	for _, runID := range runIDs {
		config := baseConfig
		config.RunID = runID
		report, err := Run(context.Background(), config)
		if err != nil {
			t.Fatalf("Run(%s) error = %v; report = %#v", runID, err, report)
		}
		if !report.Passed || violationCount(report.InvariantViolations) != 0 || report.Database.MigrationCount == 0 || report.Totals.ExpectedConflicts == 0 || report.Totals.Replayed == 0 {
			t.Fatalf("Run(%s) report did not prove expected gates: %#v", runID, report)
		}
	}
	collision := baseConfig
	collision.RunID = runIDs[0]
	if _, err := Run(context.Background(), collision); !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("same run ID error = %v, want ErrRunAlreadyExists", err)
	}
}

func stringsHasDedicatedPrefix(databaseName string) bool {
	return len(databaseName) > len(DefaultDatabasePrefix) && databaseName[:len(DefaultDatabasePrefix)] == DefaultDatabasePrefix
}
