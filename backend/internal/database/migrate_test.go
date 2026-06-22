package database

import "testing"

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
