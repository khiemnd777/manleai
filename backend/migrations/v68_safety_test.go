package migrations

import (
	"strings"
	"testing"
)

func TestV68SaaSRowLevelSecurityContract(t *testing.T) {
	raw, err := Files.ReadFile("V68__saas_row_level_security.sql")
	if err != nil {
		t.Fatalf("read V68: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"app.actor_user_id",
		"app.database_scope",
		"app_rls_actor_salon_access",
		"app_rls_salon_select_allowed",
		"app_rls_salon_write_allowed",
		"ENABLE ROW LEVEL SECURITY",
		"FOR SELECT USING",
		"FOR INSERT WITH CHECK",
		"FOR UPDATE USING",
		"FOR DELETE USING",
		"platform_pii_access_grants",
		"call_transcript_messages",
		"owner_user_id = public.app_request_actor_user_id()",
		"non-owner runtime role",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V68 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"ALTER TABLE public.salons FORCE ROW LEVEL SECURITY", "BYPASSRLS TO", "owner_user_id = actor_user_id"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V68 contains unsafe shortcut %q", forbidden)
		}
	}
}
