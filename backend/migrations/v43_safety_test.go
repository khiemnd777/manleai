package migrations

import (
	"strings"
	"testing"
)

func TestV43InvalidatesOnlyPreviouslyRunnableSquareCatalogReadiness(t *testing.T) {
	source, err := Files.ReadFile("V43__pos_snapshot_generation.sql")
	if err != nil {
		t.Fatalf("read V43 migration: %v", err)
	}
	sql := string(source)
	start := strings.Index(sql, "UPDATE pos_connections")
	if start < 0 {
		t.Fatal("V43 is missing Square readiness invalidation update")
	}
	endOffset := strings.Index(sql[start:], ";")
	if endOffset < 0 {
		t.Fatal("V43 readiness invalidation update is not terminated")
	}
	statement := sql[start : start+endOffset]
	for _, fragment := range []string{
		"snapshot_generation = snapshot_generation + 1",
		"status = 'connected'",
		"last_sync_at = NULL",
		"error_message = NULL",
		"provider = 'square'",
		"status IN ('connected', 'active', 'syncing', 'error')",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("V43 invalidation is missing %q", fragment)
		}
	}
	for _, preservedStatus := range []string{"'disabled'", "'expired_token'", "'not_connected'"} {
		if strings.Contains(statement, preservedStatus) {
			t.Fatalf("V43 invalidation must preserve status %s", preservedStatus)
		}
	}
	for _, credentialColumn := range []string{"access_token_encrypted", "refresh_token_encrypted"} {
		if strings.Contains(statement, credentialColumn) {
			t.Fatalf("V43 invalidation must not mutate %s", credentialColumn)
		}
	}
}
