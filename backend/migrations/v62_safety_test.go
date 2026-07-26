package migrations

import (
	"strings"
	"testing"
)

func TestV62PartyBookingRequestTenantIntegrityContract(t *testing.T) {
	raw, err := Files.ReadFile("V62__party_booking_request_tenant_integrity.sql")
	if err != nil {
		t.Fatalf("read V62: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"party booking request tenant preflight failed",
		"WHERE session.salon_id <> request.salon_id",
		"DROP CONSTRAINT party_booking_requests_call_session_id_fkey",
		"FOREIGN KEY (salon_id, call_session_id)",
		"REFERENCES call_sessions(salon_id, id)",
		"ON DELETE CASCADE",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V62 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"UPDATE party_booking_requests", "DELETE FROM party_booking_requests", "ON DELETE SET NULL"} {
		if strings.Contains(strings.ToUpper(source), strings.ToUpper(forbidden)) {
			t.Fatalf("V62 contains unsafe repair behavior %q", forbidden)
		}
	}
}
