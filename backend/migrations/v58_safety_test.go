package migrations

import (
	"strings"
	"testing"
)

func TestV58CrossTableServiceAliasOwnershipSafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V58__cross_table_service_alias_ownership.sql")
	if err != nil {
		t.Fatalf("read V58: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"LOCK TABLE public.service_aliases, public.service_category_aliases",
		"conflicting_alias_count BIGINT",
		"service_alias_cross_table_active_unique",
		"CREATE OR REPLACE FUNCTION public.lock_service_alias_ownership",
		"pg_catalog.pg_advisory_xact_lock",
		"pg_catalog.hashtextextended",
		"SET search_path = pg_catalog, public",
		"service_alias_ownership_key_immutable",
		"TG_TABLE_SCHEMA = 'public' AND TG_TABLE_NAME = 'service_aliases'",
		"TG_TABLE_SCHEMA = 'public' AND TG_TABLE_NAME = 'service_category_aliases'",
		"CREATE TRIGGER service_aliases_ownership_guard",
		"CREATE TRIGGER service_category_aliases_ownership_guard",
		"BEFORE INSERT OR UPDATE ON public.service_aliases",
		"BEFORE INSERT OR UPDATE ON public.service_category_aliases",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V58 missing %q", fragment)
		}
	}
	if strings.Contains(source, "SET status = 'archived'") || strings.Contains(source, "DELETE FROM public.service_") {
		t.Fatal("V58 must fail preflight instead of repairing or deleting legacy alias conflicts")
	}
}
