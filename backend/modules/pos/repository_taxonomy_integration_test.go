package pos

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestRefreshServiceCategorySuggestionsUsesDatabaseTaxonomyAndIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("POS_TAXONOMY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("POS_TAXONOMY_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	ownerID := uuid.NewString()
	salonID := uuid.NewString()
	gelServiceID := uuid.NewString()
	pedicureServiceID := uuid.NewString()
	manualServiceID := uuid.NewString()
	unknownServiceID := uuid.NewString()
	manualCategoryID := uuid.NewString()
	ownerManicureCategoryID := uuid.NewString()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, full_name)
		VALUES ($1, $2, 'integration-test', 'Taxonomy Test Owner')
	`, ownerID, ownerID+"@example.test"); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salons (id, name, phone, owner_user_id)
		VALUES ($1, 'Taxonomy Integration Salon', '+15550000000', $2)
	`, salonID, ownerID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_categories (id, salon_id, name, slug, source, status)
		VALUES
			($1, $2, 'Owner Hand Care', 'manicure', 'manual', 'active'),
			($3, $2, 'Owner Special', 'owner-special', 'manual', 'active')
	`, ownerManicureCategoryID, salonID, manualCategoryID); err != nil {
		t.Fatalf("insert owner categories: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO services (
			id, salon_id, pos_provider, pos_service_id, name, duration_minutes, active,
			service_category_id, service_category_source, service_category_confidence
		)
		VALUES
			($1, $5, 'square', 'gel', 'Gel Manicure', 45, true, NULL, 'unassigned', NULL),
			($2, $5, 'square', 'pedi', 'Classic Pedicure', 45, true, NULL, 'unassigned', NULL),
			($3, $5, 'square', 'manual', 'Acrylic Full Set', 60, true, $6, 'manual', 1.000),
			($4, $5, 'square', 'unknown', 'Volcano Signature Ritual', 75, true, NULL, 'unassigned', NULL)
	`, gelServiceID, pedicureServiceID, manualServiceID, unknownServiceID, salonID, manualCategoryID); err != nil {
		t.Fatalf("insert services: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (
			salon_id, category_id, alias, normalized_alias, source, status, confidence
		)
		VALUES ($1, $2, 'mani', 'mani', 'owner', 'active', 1.000)
	`, salonID, manualCategoryID); err != nil {
		t.Fatalf("insert owner category alias conflict: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (
			salon_id, service_id, alias, normalized_alias, source, status, confidence
		)
		VALUES ($1, $2, 'gel mani', 'gel mani', 'owner', 'active', 1.000)
	`, salonID, pedicureServiceID); err != nil {
		t.Fatalf("insert owner alias conflict: %v", err)
	}

	repository := NewRepository(db)
	first, err := repository.RefreshServiceCategorySuggestions(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if first.CreatedCategories != 9 {
		t.Fatalf("created categories = %d, want 9 because the owner-owned manicure slug is preserved", first.CreatedCategories)
	}
	if first.SuggestedServices != 2 {
		t.Fatalf("suggested services = %d, want 2", first.SuggestedServices)
	}
	if first.SkippedReviewedServices != 1 {
		t.Fatalf("skipped reviewed services = %d, want 1", first.SkippedReviewedServices)
	}
	if first.UnmatchedUnreviewedServices != 1 {
		t.Fatalf("unmatched unreviewed services = %d, want 1", first.UnmatchedUnreviewedServices)
	}
	if first.SkippedServiceAliasConflicts == 0 {
		t.Fatal("expected the owner-owned gel mani alias conflict to be reported")
	}
	if first.SkippedAliasConflicts == 0 {
		t.Fatal("expected the owner-owned mani category alias conflict to be reported")
	}

	assertServiceCategoryState(t, db, gelServiceID, ownerManicureCategoryID, ServiceCategoryAssignmentSuggested)
	assertServiceCategorySlug(t, db, pedicureServiceID, "pedicure", ServiceCategoryAssignmentSuggested)
	assertServiceCategoryState(t, db, manualServiceID, manualCategoryID, ServiceCategoryAssignmentManual)
	assertServiceCategoryState(t, db, unknownServiceID, "", ServiceCategoryAssignmentUnassigned)

	var ownerCategoryName, ownerCategorySource string
	if err := db.QueryRowContext(ctx, `
		SELECT name, source FROM service_categories WHERE id = $1
	`, ownerManicureCategoryID).Scan(&ownerCategoryName, &ownerCategorySource); err != nil {
		t.Fatalf("load owner category: %v", err)
	}
	if ownerCategoryName != "Owner Hand Care" || ownerCategorySource != ServiceCategorySourceManual {
		t.Fatalf("owner category was overwritten: name=%q source=%q", ownerCategoryName, ownerCategorySource)
	}

	var aliasServiceID, aliasSource string
	if err := db.QueryRowContext(ctx, `
		SELECT service_id::text, source
		FROM service_aliases
		WHERE salon_id = $1 AND normalized_alias = 'gel mani'
	`, salonID).Scan(&aliasServiceID, &aliasSource); err != nil {
		t.Fatalf("load owner alias: %v", err)
	}
	if aliasServiceID != pedicureServiceID || aliasSource != "owner" {
		t.Fatalf("owner alias was overwritten: service_id=%q source=%q", aliasServiceID, aliasSource)
	}
	var categoryAliasID, categoryAliasSource string
	if err := db.QueryRowContext(ctx, `
		SELECT category_id::text, source
		FROM service_category_aliases
		WHERE salon_id = $1 AND normalized_alias = 'mani'
	`, salonID).Scan(&categoryAliasID, &categoryAliasSource); err != nil {
		t.Fatalf("load owner category alias: %v", err)
	}
	if categoryAliasID != manualCategoryID || categoryAliasSource != ServiceCategoryAliasSourceOwner {
		t.Fatalf("owner category alias was overwritten: category_id=%q source=%q", categoryAliasID, categoryAliasSource)
	}

	second, err := repository.RefreshServiceCategorySuggestions(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if second.CreatedCategories != 0 || second.RestoredSystemCategories != 0 ||
		second.CreatedAliases != 0 || second.UpdatedSystemAliases != 0 ||
		second.SuggestedServices != 0 || second.CreatedServiceAliases != 0 ||
		second.UpdatedSystemServiceAliases != 0 {
		t.Fatalf("second refresh was not idempotent: %+v", *second)
	}
}

func assertServiceCategoryState(t *testing.T, db *sql.DB, serviceID string, wantCategoryID string, wantSource string) {
	t.Helper()
	var categoryID, source string
	if err := db.QueryRow(`
		SELECT COALESCE(service_category_id::text, ''), service_category_source
		FROM services WHERE id = $1
	`, serviceID).Scan(&categoryID, &source); err != nil {
		t.Fatalf("load service category state: %v", err)
	}
	if categoryID != wantCategoryID || source != wantSource {
		t.Fatalf("service %s category state = (%q, %q), want (%q, %q)", serviceID, categoryID, source, wantCategoryID, wantSource)
	}
}

func assertServiceCategorySlug(t *testing.T, db *sql.DB, serviceID string, wantSlug string, wantSource string) {
	t.Helper()
	var slug, source string
	if err := db.QueryRow(`
		SELECT COALESCE(category.slug, ''), service.service_category_source
		FROM services service
		LEFT JOIN service_categories category ON category.id = service.service_category_id
		WHERE service.id = $1
	`, serviceID).Scan(&slug, &source); err != nil {
		t.Fatalf("load service category slug: %v", err)
	}
	if slug != wantSlug || source != wantSource {
		t.Fatalf("service %s category state = (%q, %q), want (%q, %q)", serviceID, slug, source, wantSlug, wantSource)
	}
}
