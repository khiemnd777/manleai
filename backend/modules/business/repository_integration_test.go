package business

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func TestBusinessRepositoryReplayTenantFenceAndProviderOwnership(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	suffix := uuid.NewString()
	ownerAID := insertBusinessTestUser(t, db, "business-owner-a-"+suffix+"@example.test")
	ownerBID := insertBusinessTestUser(t, db, "business-owner-b-"+suffix+"@example.test")
	salonAID := insertBusinessTestSalon(t, db, ownerAID, "Business A "+suffix)
	salonBID := insertBusinessTestSalon(t, db, ownerBID, "Business B "+suffix)

	service := NewService(NewRepository(db), access.NewService(access.NewRepository(db)))
	ownerA := middleware.ActorContext{UserID: ownerAID}
	ownerB := middleware.ActorContext{UserID: ownerBID}
	name := "Structured Gel Manicure " + suffix
	duration := 45
	request := ServiceMutationRequest{
		MutationControl: MutationControl{ActionKey: "create-service-" + suffix, ExpectedVersion: 0},
		Name:            &name,
		DurationMinutes: &duration,
	}
	created, err := service.CreateService(ctx, ownerA, access.SurfaceTenant, salonAID, request)
	if err != nil || created.Replayed || created.Data.ID == "" || created.Data.Version != 1 {
		t.Fatalf("create service=%#v err=%v", created, err)
	}
	replayed, err := service.CreateService(ctx, ownerA, access.SurfaceTenant, salonAID, request)
	if err != nil || !replayed.Replayed || replayed.Data.ID != created.Data.ID || replayed.Data.Version != created.Data.Version {
		t.Fatalf("replay service=%#v err=%v", replayed, err)
	}
	changedName := name + " changed"
	changedRequest := request
	changedRequest.Name = &changedName
	if _, err := service.CreateService(ctx, ownerA, access.SurfaceTenant, salonAID, changedRequest); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed action-key reuse error=%v, want action conflict", err)
	}

	if _, err := service.Services(ctx, ownerB, access.SurfaceTenant, salonAID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant list error=%v, want forbidden", err)
	}
	if _, err := service.UpdateService(ctx, ownerB, access.SurfaceTenant, salonAID, created.Data.ID, ServiceMutationRequest{
		MutationControl: MutationControl{ActionKey: "cross-tenant-update-" + suffix, ExpectedVersion: created.Data.Version},
		Name:            &changedName,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant update error=%v, want forbidden", err)
	}

	providerServiceID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO services (
			id, salon_id, pos_provider, pos_service_id, pos_service_version,
			name, duration_minutes, ai_bookable, active, sync_status, source
		) VALUES ($1,$2,'square',$3,7,$4,30,true,true,'synced','imported')
	`, providerServiceID, salonAID, "provider-service-"+suffix, "Provider Service "+suffix); err != nil {
		t.Fatalf("insert provider service: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id,
			provider_version, sync_status, last_synced_at
		) VALUES ($1,'service',$2,'square',$3,7,'synced',now())
	`, salonAID, providerServiceID, "provider-service-"+suffix); err != nil {
		t.Fatalf("insert provider link: %v", err)
	}
	providerService, err := NewRepository(db).GetService(ctx, salonAID, providerServiceID)
	if err != nil || providerService.Version != 1 || providerService.ManagementMode != ManagementModeProviderReadOnly {
		t.Fatalf("provider service=%#v err=%v", providerService, err)
	}
	providerName := "Unsafe Local Rename"
	if _, err := service.UpdateService(ctx, ownerA, access.SurfaceTenant, salonAID, providerServiceID, ServiceMutationRequest{
		MutationControl: MutationControl{ActionKey: "provider-rename-" + suffix, ExpectedVersion: providerService.Version},
		Name:            &providerName,
	}); !errors.Is(err, ErrProviderReadOnly) {
		t.Fatalf("provider-owned rename error=%v, want read-only", err)
	}

	var actionCount, eventCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM business_actions WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3`, salonAID, ownerAID, request.ActionKey).Scan(&actionCount); err != nil {
		t.Fatalf("count action: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM business_events event JOIN business_actions action ON action.id=event.action_id WHERE action.salon_id=$1 AND action.actor_user_id=$2 AND action.action_key=$3 AND event.actor_user_id=$2 AND event.surface='tenant'`, salonAID, ownerAID, request.ActionKey).Scan(&eventCount); err != nil {
		t.Fatalf("count event: %v", err)
	}
	if actionCount != 1 || eventCount != 1 {
		t.Fatalf("business audit actions=%d events=%d, want one exact actor event", actionCount, eventCount)
	}
	var auditLeak bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM business_actions WHERE salon_id=$1 AND response_payload::text LIKE '%' || $2 || '%')`, salonAID, name).Scan(&auditLeak); err != nil {
		t.Fatalf("inspect safe action payload: %v", err)
	}
	if auditLeak {
		t.Fatal("business action response leaked service business content")
	}

	// Prove the second tenant is a real independent tenant, rather than merely
	// an unknown ID rejected by the repository.
	if response, err := service.ListSalons(ctx, ownerB, access.SurfaceTenant); err != nil || !containsSalon(response.Salons, salonBID) || containsSalon(response.Salons, salonAID) {
		t.Fatalf("tenant B directory=%#v err=%v", response, err)
	}
}

func insertBusinessTestUser(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,status) VALUES($1,'integration-test-only','Business Test','active') RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	return id
}

func insertBusinessTestSalon(t *testing.T, db *sql.DB, ownerUserID, name string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO salons(name,phone,owner_user_id) VALUES($1,'+13125550199',$2) RETURNING id::text`, name, ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert salon %s: %v", name, err)
	}
	if _, err := db.Exec(`INSERT INTO salon_settings(salon_id) VALUES($1)`, id); err != nil {
		t.Fatalf("insert settings for salon %s: %v", name, err)
	}
	return id
}

func containsSalon(items []SalonSummary, salonID string) bool {
	for _, item := range items {
		if item.ID == salonID {
			return true
		}
	}
	return false
}
