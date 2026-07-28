package migrations

import (
	"strings"
	"testing"
)

func TestV73AddsClassificationWithoutSampleFixtures(t *testing.T) {
	raw, err := Files.ReadFile("V73__sample_data_classification.sql")
	if err != nil {
		t.Fatalf("read V73: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"ALTER TABLE users",
		"ALTER TABLE salons",
		"data_classification IN ('live', 'sample_test')",
		"DEFAULT 'live'",
		"salons_owner_data_classification_fk",
		"REFERENCES users(id, data_classification)",
		"users_data_classification_immutable_guard",
		"salons_data_classification_immutable_guard",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V73 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"owner@lotusnails.example",
		"Lotus Nails Studio",
		"INSERT INTO users",
		"INSERT INTO salons",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V73 must stay schema-only; found %q", forbidden)
		}
	}
}
