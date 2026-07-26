package migrations

import (
	"strings"
	"testing"
)

func TestV71PublicCatalogProjectionBoundary(t *testing.T) {
	raw, err := Files.ReadFile("V71__public_catalog_projection_boundary.sql")
	if err != nil {
		t.Fatalf("read V71: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"SECURITY DEFINER",
		"SET row_security = off",
		"read_public_catalog(target_slug TEXT)",
		"WHEN 'public' THEN false",
		"'services', service_rows",
		"'staff', staff_rows",
		"'hours', hour_rows",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V71 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"access_token_encrypted", "refresh_token_encrypted", "owner_user_id", "staff.phone", "staff.email", "provider_entity_id',"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V71 public JSON projection contains forbidden field %q", forbidden)
		}
	}
}
