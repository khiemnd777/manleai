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

func TestMigrationFilesOrderV75AfterV74(t *testing.T) {
	files, err := loadMigrationFiles()
	if err != nil {
		t.Fatalf("load migration files: %v", err)
	}
	indexByVersion := make(map[string]int, len(files))
	for i, file := range files {
		indexByVersion[file.Version] = i
	}
	v63Index, hasV63 := indexByVersion["63"]
	v64Index, hasV64 := indexByVersion["64"]
	v65Index, hasV65 := indexByVersion["65"]
	v66Index, hasV66 := indexByVersion["66"]
	v67Index, hasV67 := indexByVersion["67"]
	v68Index, hasV68 := indexByVersion["68"]
	v69Index, hasV69 := indexByVersion["69"]
	v70Index, hasV70 := indexByVersion["70"]
	v71Index, hasV71 := indexByVersion["71"]
	v72Index, hasV72 := indexByVersion["72"]
	v73Index, hasV73 := indexByVersion["73"]
	v74Index, hasV74 := indexByVersion["74"]
	v75Index, hasV75 := indexByVersion["75"]
	if !hasV63 || !hasV64 || !hasV65 || !hasV66 || !hasV67 || !hasV68 || !hasV69 || !hasV70 || !hasV71 || !hasV72 || !hasV73 || !hasV74 || !hasV75 {
		t.Fatalf("migration versions include V63=%t V64=%t V65=%t V66=%t V67=%t V68=%t V69=%t V70=%t V71=%t V72=%t V73=%t V74=%t V75=%t", hasV63, hasV64, hasV65, hasV66, hasV67, hasV68, hasV69, hasV70, hasV71, hasV72, hasV73, hasV74, hasV75)
	}
	if v64Index != v63Index+1 {
		t.Fatalf("V64 index=%d, want immediately after V63 index=%d", v64Index, v63Index)
	}
	if v65Index != v64Index+1 {
		t.Fatalf("V65 index=%d, want immediately after V64 index=%d", v65Index, v64Index)
	}
	if v66Index != v65Index+1 {
		t.Fatalf("V66 index=%d, want immediately after V65 index=%d", v66Index, v65Index)
	}
	if v67Index != v66Index+1 {
		t.Fatalf("V67 index=%d, want immediately after V66 index=%d", v67Index, v66Index)
	}
	if v68Index != v67Index+1 {
		t.Fatalf("V68 index=%d, want immediately after V67 index=%d", v68Index, v67Index)
	}
	if v69Index != v68Index+1 {
		t.Fatalf("V69 index=%d, want immediately after V68 index=%d", v69Index, v68Index)
	}
	if v70Index != v69Index+1 {
		t.Fatalf("V70 index=%d, want immediately after V69 index=%d", v70Index, v69Index)
	}
	if v71Index != v70Index+1 {
		t.Fatalf("V71 index=%d, want immediately after V70 index=%d", v71Index, v70Index)
	}
	if v72Index != v71Index+1 {
		t.Fatalf("V72 index=%d, want immediately after V71 index=%d", v72Index, v71Index)
	}
	if v73Index != v72Index+1 {
		t.Fatalf("V73 index=%d, want immediately after V72 index=%d", v73Index, v72Index)
	}
	if v74Index != v73Index+1 {
		t.Fatalf("V74 index=%d, want immediately after V73 index=%d", v74Index, v73Index)
	}
	if v75Index != v74Index+1 {
		t.Fatalf("V75 index=%d, want immediately after V74 index=%d", v75Index, v74Index)
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
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '64'`).Scan(&count); err != nil {
		t.Fatalf("load V64 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V64 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '65'`).Scan(&count); err != nil {
		t.Fatalf("load V65 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V65 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '66'`).Scan(&count); err != nil {
		t.Fatalf("load V66 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V66 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '67'`).Scan(&count); err != nil {
		t.Fatalf("load V67 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V67 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '68'`).Scan(&count); err != nil {
		t.Fatalf("load V68 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V68 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '69'`).Scan(&count); err != nil {
		t.Fatalf("load V69 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V69 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '70'`).Scan(&count); err != nil {
		t.Fatalf("load V70 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V70 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '71'`).Scan(&count); err != nil {
		t.Fatalf("load V71 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V71 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '72'`).Scan(&count); err != nil {
		t.Fatalf("load V72 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V72 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '73'`).Scan(&count); err != nil {
		t.Fatalf("load V73 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V73 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '74'`).Scan(&count); err != nil {
		t.Fatalf("load V74 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V74 migration records=%d, want 1", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_schema_migrations WHERE version = '75'`).Scan(&count); err != nil {
		t.Fatalf("load V75 record: %v", err)
	}
	if count != 1 {
		t.Fatalf("V75 migration records=%d, want 1", count)
	}
}
