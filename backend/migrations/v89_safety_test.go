package migrations

import (
	"strings"
	"testing"
)

func TestV89AuthoritySwitchStoresAuthorizedActualActor(t *testing.T) {
	raw, err := Files.ReadFile("V89__platform_authority_actual_actor.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"app_actor_feature_access(NEW.actor_user_id, NEW.salon_id, 'technical.write')",
		"salon.owner_user_id = NEW.actor_user_id",
		"scheduling_authority_switch_runs_actor_guard",
		"scheduling_authority_switch_events_actor_guard",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V89 missing actual-actor authority contract %q", token)
		}
	}
	if strings.Contains(source, "switch_run.actor_user_id = NEW.actor_user_id") {
		t.Fatal("V89 must preserve the actual actor of each event instead of forcing the preview actor")
	}
	if strings.Contains(source, "CREATE OR REPLACE FUNCTION public.app_actor_feature_access") {
		t.Fatal("V89 must preserve the existing interactive RBAC contract instead of redefining it")
	}
	for _, forbidden := range []string{"UPDATE scheduling_authority_switch_runs", "UPDATE scheduling_authority_switch_events", "DROP TABLE", "TRUNCATE"} {
		if strings.Contains(strings.ToUpper(source), strings.ToUpper(forbidden)) {
			t.Fatalf("V89 must not rewrite immutable history or use destructive SQL: %q", forbidden)
		}
	}
}
