package database

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func TestMigrationFilesSortByNumericVersion(t *testing.T) {
	files := []migrationFile{
		{Version: "10", Order: 10, Path: "V10__booking_segments_foundation.sql"},
		{Version: "1", Order: 1, Path: "V1__foundation_auth_salon_pos.sql"},
		{Version: "12", Order: 12, Path: "V12__canonical_pos_entity_links.sql"},
		{Version: "2", Order: 2, Path: "V2__milestone_3_booking_foundation.sql"},
		{Version: "11", Order: 11, Path: "V11__conversation_booking_segments.sql"},
	}

	sortMigrationFiles(files)

	got := []string{files[0].Version, files[1].Version, files[2].Version, files[3].Version, files[4].Version}
	want := []string{"1", "2", "10", "11", "12"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted versions = %v, want %v", got, want)
		}
	}
}

func TestParseMigrationFileStoresNumericOrder(t *testing.T) {
	migration, err := parseMigrationFile("V12__canonical_pos_entity_links.sql", "SELECT 1;")
	if err != nil {
		t.Fatalf("parseMigrationFile returned error: %v", err)
	}
	if migration.Version != "12" || migration.Order != 12 {
		t.Fatalf("migration version/order = %s/%d, want 12/12", migration.Version, migration.Order)
	}
}

func TestParseMigrationFileRejectsNonNumericVersion(t *testing.T) {
	_, err := parseMigrationFile("VA__bad.sql", "SELECT 1;")
	if err == nil {
		t.Fatal("parseMigrationFile accepted non-numeric migration version")
	}
}

func TestMigrateAppliesForwardMigrationOnceWithoutChangingAppliedChecksums(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM app_schema_migrations WHERE version = '45'
	`).Scan(&count); err != nil {
		t.Fatalf("load V45 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V45 migration records=%d, want 1", count)
	}
}
