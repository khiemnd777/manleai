package conversation

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestRepositorySeparatesGuidanceCatalogFromCurrentBookingSnapshot(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Consultant Catalog Readiness Test')
		RETURNING id::text
	`, "consultant-catalog-readiness-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Consultant Catalog Readiness Test Salon', '+13125550302', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert salon: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id)
		VALUES ($1, 'square', 'connected', 'location-a')
	`, salonID); err != nil {
		t.Fatalf("insert POS connection: %v", err)
	}

	posRepo := pos.NewRepository(db)
	conversationRepo := NewRepository(db)
	locationAGeneration, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "location-a")
	if err != nil {
		t.Fatalf("begin location A snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider:   pos.ProviderSquare,
		LocationID: "location-a",
		Generation: locationAGeneration,
		Services: []pos.Service{{
			POSProvider: pos.ProviderSquare, POSServiceID: "consultant-a-service-" + suffix,
			POSServiceVersion: 1, Name: "Location A Gel Manicure", DurationMinutes: 40, AIBookable: true, Active: true,
		}},
		Staff: []pos.StaffMember{{
			POSProvider: pos.ProviderSquare, POSStaffID: "consultant-a-staff-" + suffix,
			Name: "Location A Artist", AIBookable: true, Active: true,
		}},
	}); err != nil {
		t.Fatalf("apply location A snapshot: %v", err)
	}
	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, locationAGeneration, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete location A snapshot: %v", err)
	}

	locationAServiceID := consultantProviderEntityLocalID(t, ctx, db, salonID, "service", "consultant-a-service-"+suffix)
	var categoryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO service_categories (salon_id, name, slug, source)
		VALUES ($1, 'Manicure', $2, 'manual')
		RETURNING id::text
	`, salonID, "manicure-"+suffix).Scan(&categoryID); err != nil {
		t.Fatalf("insert service category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE services SET service_category_id = $1 WHERE id = $2`, categoryID, locationAServiceID); err != nil {
		t.Fatalf("assign location A category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source)
		VALUES ($1, $2, 'A gel mani', $3, 'owner')
	`, salonID, locationAServiceID, "a-gel-mani-"+suffix); err != nil {
		t.Fatalf("insert location A service alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source)
		VALUES ($1, $2, 'Mani', $3, 'owner')
	`, salonID, categoryID, "mani-"+suffix); err != nil {
		t.Fatalf("insert service category alias: %v", err)
	}
	assertConsultantCatalog(t, ctx, conversationRepo, salonID, "Location A Gel Manicure", "Location A Artist", "A gel mani", 1)

	if _, err := posRepo.UpdateLocation(ctx, salonID, pos.ProviderSquare, "location-b"); err != nil {
		t.Fatalf("switch to location B: %v", err)
	}
	assertBookingCatalogNotReady(t, ctx, conversationRepo, salonID)
	assertGuidanceCatalog(t, ctx, conversationRepo, salonID, "Location A Gel Manicure", "A gel mani", 1)

	locationBGeneration, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin location B snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider:   pos.ProviderSquare,
		LocationID: "location-b",
		Generation: locationBGeneration,
		Services: []pos.Service{{
			POSProvider: pos.ProviderSquare, POSServiceID: "consultant-b-service-" + suffix,
			POSServiceVersion: 2, Name: "Location B Spa Pedicure", DurationMinutes: 55, AIBookable: true, Active: true,
		}},
		Staff: []pos.StaffMember{{
			POSProvider: pos.ProviderSquare, POSStaffID: "consultant-b-staff-" + suffix,
			Name: "Location B Artist", AIBookable: true, Active: true,
		}},
	}); err != nil {
		t.Fatalf("apply location B snapshot: %v", err)
	}
	locationBServiceID := consultantProviderEntityLocalID(t, ctx, db, salonID, "service", "consultant-b-service-"+suffix)
	if _, err := db.ExecContext(ctx, `UPDATE services SET service_category_id = $1 WHERE id = $2`, categoryID, locationBServiceID); err != nil {
		t.Fatalf("assign location B category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source)
		VALUES ($1, $2, 'B spa pedi', $3, 'owner')
	`, salonID, locationBServiceID, "b-spa-pedi-"+suffix); err != nil {
		t.Fatalf("insert location B service alias: %v", err)
	}
	assertBookingCatalogNotReady(t, ctx, conversationRepo, salonID)
	assertGuidanceCatalog(t, ctx, conversationRepo, salonID, "Location B Spa Pedicure", "B spa pedi", 1)

	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, locationBGeneration, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete location B snapshot: %v", err)
	}
	assertConsultantCatalog(t, ctx, conversationRepo, salonID, "Location B Spa Pedicure", "Location B Artist", "B spa pedi", 1)
}

func consultantProviderEntityLocalID(t *testing.T, ctx context.Context, db *sql.DB, salonID string, entityType string, providerEntityID string) string {
	t.Helper()
	var entityID string
	if err := db.QueryRowContext(ctx, `
		SELECT entity_id::text
		FROM pos_entity_links
		WHERE salon_id = $1
		  AND entity_type = $2
		  AND provider = 'square'
		  AND provider_entity_id = $3
	`, salonID, entityType, providerEntityID).Scan(&entityID); err != nil {
		t.Fatalf("load %s local ID for %s: %v", entityType, providerEntityID, err)
	}
	return entityID
}

func assertBookingCatalogNotReady(t *testing.T, ctx context.Context, repo *Repository, salonID string) {
	t.Helper()
	fence, err := repo.GetAnswerContextFence(ctx, salonID)
	if err != nil {
		t.Fatalf("load consultant answer-context fence: %v", err)
	}
	if externalProviderAnswerContextReady(fence) {
		t.Fatalf("answer-context fence unexpectedly ready: %#v", fence)
	}
	services, err := repo.ListBookableServices(ctx, salonID)
	if err != nil {
		t.Fatalf("list bookable services: %v", err)
	}
	staff, err := repo.ListBookableStaff(ctx, salonID)
	if err != nil {
		t.Fatalf("list bookable staff: %v", err)
	}
	if len(services) != 0 || len(staff) != 0 {
		t.Fatalf("booking catalog bypassed current snapshot fence: services=%#v staff=%#v", services, staff)
	}
}

func assertConsultantCatalog(t *testing.T, ctx context.Context, repo *Repository, salonID string, serviceName string, staffName string, serviceAlias string, wantCount int) {
	t.Helper()
	fence, err := repo.GetAnswerContextFence(ctx, salonID)
	if err != nil {
		t.Fatalf("load consultant answer-context fence: %v", err)
	}
	if externalProviderAnswerContextReady(fence) != (wantCount > 0) {
		t.Fatalf("answer-context fence readiness = %#v, want ready=%t", fence, wantCount > 0)
	}
	assertGuidanceCatalog(t, ctx, repo, salonID, serviceName, serviceAlias, wantCount)
	services, err := repo.ListBookableServices(ctx, salonID)
	if err != nil {
		t.Fatalf("list consultant services: %v", err)
	}
	if len(services) != wantCount || wantCount == 1 && services[0].Name != serviceName {
		t.Fatalf("consultant services = %#v, want count=%d name=%q", services, wantCount, serviceName)
	}
	bookableStaff, err := repo.ListBookableStaff(ctx, salonID)
	if err != nil {
		t.Fatalf("list consultant bookable staff: %v", err)
	}
	if len(bookableStaff) != wantCount || wantCount == 1 && bookableStaff[0].Name != staffName {
		t.Fatalf("consultant bookable staff = %#v, want count=%d name=%q", bookableStaff, wantCount, staffName)
	}
	activeStaff, err := repo.ListActiveStaff(ctx, salonID)
	if err != nil {
		t.Fatalf("list consultant active staff: %v", err)
	}
	if len(activeStaff) != wantCount || wantCount == 1 && activeStaff[0].Name != staffName {
		t.Fatalf("consultant active staff = %#v, want count=%d name=%q", activeStaff, wantCount, staffName)
	}
	categoryAliases, err := repo.ListActiveServiceCategoryAliases(ctx, salonID)
	if err != nil {
		t.Fatalf("list consultant category aliases: %v", err)
	}
	if len(categoryAliases) != wantCount {
		t.Fatalf("consultant category aliases = %#v, want count=%d", categoryAliases, wantCount)
	}
}

func assertGuidanceCatalog(t *testing.T, ctx context.Context, repo *Repository, salonID string, serviceName string, serviceAlias string, wantCount int) {
	t.Helper()
	services, err := repo.ListGuidanceServices(ctx, salonID)
	if err != nil {
		t.Fatalf("list guidance services: %v", err)
	}
	if len(services) != wantCount || wantCount == 1 && services[0].Name != serviceName {
		t.Fatalf("guidance services = %#v, want count=%d name=%q", services, wantCount, serviceName)
	}
	aliases, err := repo.ListActiveServiceAliases(ctx, salonID)
	if err != nil {
		t.Fatalf("list guidance service aliases: %v", err)
	}
	if len(aliases) != wantCount || wantCount == 1 && aliases[0].Alias != serviceAlias {
		t.Fatalf("guidance service aliases = %#v, want count=%d alias=%q", aliases, wantCount, serviceAlias)
	}
}
