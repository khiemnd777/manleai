package migrations

import (
	"strings"
	"testing"
)

func TestV72PlatformPIIScopeEnforcement(t *testing.T) {
	raw, err := Files.ReadFile("V72__platform_pii_scope_enforcement.sql")
	if err != nil {
		t.Fatalf("read V72: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"('customers', 'customers')",
		"('call_sessions', 'calls')",
		"('call_transcript_messages', 'calls')",
		"('appointments', 'appointments')",
		"('booking_attempts', 'appointments')",
		"('scheduling_requests', 'appointments')",
		"('owner_notifications', 'notifications')",
		"('customer_notification_deliveries', 'notifications')",
		"app_rls_salon_select_allowed(salon_id, %L, false)",
		"app_rls_salon_write_allowed(salon_id, %L)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V72 missing %q", fragment)
		}
	}
}
