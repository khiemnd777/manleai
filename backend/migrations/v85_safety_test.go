package migrations

import (
	"strings"
	"testing"
)

func TestV85TenantRegistrationProvisioningSafety(t *testing.T) {
	raw, err := Files.ReadFile("V85__tenant_registration_and_provisioning.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{"tenant_registration_requests", "tenant_registration_request_events", "tenant_registration_request_actions", "tenant_registration_request_notes", "tenant_owner_invitations", "platform.registration_requests.read", "platform.registration_requests.manage", "platform.tenants.provision", "create_tenant_registration_request", "accept_tenant_owner_invitation", "redact_due_tenant_registration_requests", "FOR UPDATE SKIP LOCKED", "tenant-registration-contact-v1", "tenant-registration-redaction-v1", "token_hash ~ '^[0-9a-f]{64}$'"} {
		if !strings.Contains(source, token) {
			t.Fatalf("V85 missing safety token %q", token)
		}
	}
	for _, forbidden := range []string{"salon_integration_configs", "active_pos_provider", "external_provider", "stripe", "checkout", "raw_token TEXT"} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			t.Fatalf("V85 contains forbidden provisioning inference %q", forbidden)
		}
	}
}
