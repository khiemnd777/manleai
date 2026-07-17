package migrations

import (
	"strings"
	"testing"
)

func TestV45ForwardFillsOwnerCategoryWithoutOverwritingOwnerData(t *testing.T) {
	source, err := Files.ReadFile("V45__taxonomy_owner_category_forward_fill.sql")
	if err != nil {
		t.Fatalf("read V45 migration: %v", err)
	}
	sql := string(source)
	for _, fragment := range []string{
		"ON category.slug = taxonomy.slug AND category.status = 'active'",
		"WHERE service_category_aliases.source = 'system'",
		"service.service_category_source IN ('unassigned', 'suggested')",
		"service.archived_at IS NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V45 is missing forward-safe contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"INSERT INTO services",
		"INSERT INTO service_categories",
		"INSERT INTO pos_entity_links",
		"category.source = 'system'",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V45 must not create operational owners or restrict the target to system categories: found %q", forbidden)
		}
	}
}
