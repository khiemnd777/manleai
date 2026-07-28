package migrations

import (
	"strings"
	"testing"
)

func TestV76PlatformAdminAuthorityAndOpsDelegationSafety(t *testing.T) {
	raw, err := Files.ReadFile("V76__platform_admin_authority_and_ops_delegation.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	required := []string{
		"app_platform_admin_capability",
		"role.name = 'platform_admin'",
		"role.name = 'platform_ops'",
		"app_active_support_authorization",
		"app_active_support_pii_grant",
		"OLD.owner_user_id IS NOT DISTINCT FROM NEW.owner_user_id",
		"Admin-granted, time-bounded authorization",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("V76 missing safety token %q", token)
		}
	}
}
