package migrations

import (
	"strings"
	"testing"
)

func TestV63ProviderDiagnosticRedactionContract(t *testing.T) {
	raw, err := Files.ReadFile("V63__provider_diagnostic_redaction.sql")
	if err != nil {
		t.Fatalf("read V63: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"UPDATE pos_errors",
		"payload = NULL",
		"UPDATE booking_attempts",
		"UPDATE pos_connections",
		"UPDATE pos_sync_logs",
		"UPDATE pos_sync_jobs",
		"UPDATE pos_entity_links",
		"UPDATE services",
		"UPDATE staff",
		"UPDATE customers",
		"UPDATE appointments",
		"UPDATE voice_webhook_events",
		"^[A-Za-z0-9._:-]{1,128}$",
		"payload - 'error' - 'StreamError'",
		"Realtime operation failed.",
		"DROP FUNCTION phase1_safe_pos_error_message(TEXT)",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V63 missing %q", fragment)
		}
	}
}
