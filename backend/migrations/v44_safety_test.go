package migrations

import (
	"strings"
	"testing"
)

func TestV44OwnsTaxonomyInDatabaseAndPreservesSalonOperationalData(t *testing.T) {
	source, err := Files.ReadFile("V44__database_owned_nail_service_taxonomy.sql")
	if err != nil {
		t.Fatalf("read V44 migration: %v", err)
	}
	sql := string(source)
	for _, fragment := range []string{
		"CREATE TABLE service_taxonomy_releases",
		"CREATE TABLE service_taxonomy_categories",
		"CREATE TABLE service_taxonomy_category_aliases",
		"CREATE TABLE service_taxonomy_service_concepts",
		"CREATE TABLE service_taxonomy_service_aliases",
		"WHERE service_categories.source = 'system'",
		"WHERE service_category_aliases.source = 'system'",
		"WHERE service_aliases.source = 'system'",
		"service.service_category_source IN ('unassigned', 'suggested')",
		"HAVING COUNT(DISTINCT service_id) = 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V44 is missing safety contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"INSERT INTO services",
		"INSERT INTO pos_entity_links",
		"INSERT INTO pos_connections",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V44 must not create operational or provider records: found %q", forbidden)
		}
	}
}

func TestV44SeparatesCategoryAliasesFromConcreteServiceAliases(t *testing.T) {
	source, err := Files.ReadFile("V44__database_owned_nail_service_taxonomy.sql")
	if err != nil {
		t.Fatalf("read V44 migration: %v", err)
	}
	sql := string(source)
	for _, concreteAlias := range []string{
		"('gel-manicure', 'gel mani', 'gel mani'",
		"('classic-pedicure', 'classic pedi', 'classic pedi'",
		"('acrylic-fill', 'acrylic refill', 'acrylic refill'",
	} {
		if !strings.Contains(sql, concreteAlias) {
			t.Fatalf("V44 concrete service alias is missing: %q", concreteAlias)
		}
	}
	if strings.Contains(sql, "('manicure', 'gel manicure', 'gel manicure'") {
		t.Fatal("V44 must not duplicate a concrete service name as a category alias")
	}
}
