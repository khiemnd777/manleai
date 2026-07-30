package migrations

import (
	"strings"
	"testing"
)

func TestV84OpenAITenantBoundRuntimeSafety(t *testing.T) {
	raw, err := Files.ReadFile("V84__openai_tenant_bound_runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"credential_fingerprint_hmac",
		"idx_openai_unique_credential_identity",
		"destination_profile",
		"openai_runtime_verification_runs",
		"openai_runtime_verification_capabilities",
		"openai_runtime_verification_events",
		"app_worker_claim_openai_runtime_verifications",
		"public.app_worker_discovery_allowed()",
		"FOR UPDATE OF run SKIP LOCKED",
		"public.app_rls_system_salon_allowed(salon_id)",
		"public.app_rls_feature_access(salon_id, ''technical.read'', NULL)",
		"public.app_rls_feature_access(salon_id, ''technical.write'', NULL)",
		"FORCE ROW LEVEL SECURITY",
		"openai-public-v1",
		"openai-voice-v1",
		"manleai.salon_configuration.v10",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V84 missing tenant-bound safety token %q", token)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE",
		"DROP COLUMN",
		"APIKey",
		"api_key TEXT",
		"secrets_encrypted TEXT",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V84 contains forbidden token %q", forbidden)
		}
	}
}
