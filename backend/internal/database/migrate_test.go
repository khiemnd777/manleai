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

func TestMigrationFilesOrderV58AfterV57(t *testing.T) {
	files, err := loadMigrationFiles()
	if err != nil {
		t.Fatalf("load migration files: %v", err)
	}

	indexByVersion := make(map[string]int, len(files))
	for i, file := range files {
		indexByVersion[file.Version] = i
	}
	v47Index, hasV47 := indexByVersion["47"]
	v48Index, hasV48 := indexByVersion["48"]
	v49Index, hasV49 := indexByVersion["49"]
	v50Index, hasV50 := indexByVersion["50"]
	v51Index, hasV51 := indexByVersion["51"]
	v52Index, hasV52 := indexByVersion["52"]
	v53Index, hasV53 := indexByVersion["53"]
	v54Index, hasV54 := indexByVersion["54"]
	v55Index, hasV55 := indexByVersion["55"]
	v56Index, hasV56 := indexByVersion["56"]
	v57Index, hasV57 := indexByVersion["57"]
	v58Index, hasV58 := indexByVersion["58"]
	if !hasV47 || !hasV48 || !hasV49 || !hasV50 || !hasV51 || !hasV52 || !hasV53 || !hasV54 || !hasV55 || !hasV56 || !hasV57 || !hasV58 {
		t.Fatalf("migration versions include V47=%t V48=%t V49=%t V50=%t V51=%t V52=%t V53=%t V54=%t V55=%t V56=%t V57=%t V58=%t", hasV47, hasV48, hasV49, hasV50, hasV51, hasV52, hasV53, hasV54, hasV55, hasV56, hasV57, hasV58)
	}
	if v48Index != v47Index+1 {
		t.Fatalf("V48 index=%d, want immediately after V47 index=%d", v48Index, v47Index)
	}
	if v49Index != v48Index+1 {
		t.Fatalf("V49 index=%d, want immediately after V48 index=%d", v49Index, v48Index)
	}
	if v50Index != v49Index+1 {
		t.Fatalf("V50 index=%d, want immediately after V49 index=%d", v50Index, v49Index)
	}
	if v51Index != v50Index+1 {
		t.Fatalf("V51 index=%d, want immediately after V50 index=%d", v51Index, v50Index)
	}
	if v52Index != v51Index+1 {
		t.Fatalf("V52 index=%d, want immediately after V51 index=%d", v52Index, v51Index)
	}
	if v53Index != v52Index+1 {
		t.Fatalf("V53 index=%d, want immediately after V52 index=%d", v53Index, v52Index)
	}
	if v54Index != v53Index+1 {
		t.Fatalf("V54 index=%d, want immediately after V53 index=%d", v54Index, v53Index)
	}
	if v55Index != v54Index+1 {
		t.Fatalf("V55 index=%d, want immediately after V54 index=%d", v55Index, v54Index)
	}
	if v56Index != v55Index+1 {
		t.Fatalf("V56 index=%d, want immediately after V55 index=%d", v56Index, v55Index)
	}
	if v57Index != v56Index+1 {
		t.Fatalf("V57 index=%d, want immediately after V56 index=%d", v57Index, v56Index)
	}
	if v58Index != v57Index+1 {
		t.Fatalf("V58 index=%d, want immediately after V57 index=%d", v58Index, v57Index)
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
		SELECT COUNT(*) FROM app_schema_migrations WHERE version = '52'
	`).Scan(&count); err != nil {
		t.Fatalf("load V52 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V52 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '53'`).Scan(&count); err != nil {
		t.Fatalf("load V53 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V53 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '54'`).Scan(&count); err != nil {
		t.Fatalf("load V54 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V54 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '55'`).Scan(&count); err != nil {
		t.Fatalf("load V55 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V55 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '56'`).Scan(&count); err != nil {
		t.Fatalf("load V56 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V56 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '57'`).Scan(&count); err != nil {
		t.Fatalf("load V57 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V57 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '58'`).Scan(&count); err != nil {
		t.Fatalf("load V58 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V58 migration records=%d, want 1", count)
	}
}
