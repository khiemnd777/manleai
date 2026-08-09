package migrations

import (
	"strings"
	"testing"
)

func TestV91ManleAICalendarPreservesAuthorizedActualActor(t *testing.T) {
	raw, err := Files.ReadFile("V91__manleai_calendar_actual_actor.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"CREATE OR REPLACE FUNCTION public.app_manleai_calendar_write_access",
		"public.has_active_tenant_membership(target_salon_id, target_actor_id)",
		"public.has_platform_salon_capability(",
		"'technical.write'",
		"CREATE OR REPLACE FUNCTION enforce_manleai_calendar_config_write",
		"CREATE OR REPLACE FUNCTION enforce_manleai_calendar_exception_write",
		"CREATE OR REPLACE FUNCTION enforce_manleai_calendar_config_event_actor",
		"manleai_calendar_configs_activation_actor_guard",
		"manleai_calendar_exceptions_creator_actor_guard",
		"manleai_calendar_exceptions_cancellation_actor_guard",
		"manleai_calendar_config_events_actor_guard",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V91 missing actual-actor calendar contract %q", token)
		}
	}
	for _, forbidden := range []string{
		"UPDATE manleai_calendar_config_events",
		"DELETE FROM manleai_calendar_config_events",
		"UPDATE salons SET owner_user_id",
		"DROP TABLE",
		"TRUNCATE",
	} {
		if strings.Contains(strings.ToUpper(source), strings.ToUpper(forbidden)) {
			t.Fatalf("V91 must not rewrite owner or immutable calendar history: %q", forbidden)
		}
	}
}
