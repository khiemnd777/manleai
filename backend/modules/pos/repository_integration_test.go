package pos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestRepositoryProviderSnapshotRejectsLocationSwitchAndOutOfOrderCompletion(t *testing.T) {
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
		VALUES ($1, 'integration-test', 'POS Snapshot Fence Test')
		RETURNING id::text
	`, "pos-snapshot-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('POS Snapshot Fence Test Salon', '+13125550200', $1)
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
		INSERT INTO pos_connections (salon_id, provider, status, location_id, last_sync_at)
		VALUES ($1, 'square', 'active', 'location-a', now())
	`, salonID); err != nil {
		t.Fatalf("insert POS connection: %v", err)
	}

	repo := NewRepository(db)
	locationAGeneration, err := repo.BeginProviderSnapshot(ctx, salonID, ProviderSquare, "location-a")
	if err != nil {
		t.Fatalf("begin location A snapshot: %v", err)
	}
	startedConnection, err := repo.GetConnection(ctx, salonID, ProviderSquare)
	if err != nil {
		t.Fatalf("load started location A snapshot: %v", err)
	}
	if startedConnection.Status != StatusSyncing || startedConnection.LastSyncAt != nil {
		t.Fatalf("connection after snapshot start = %#v, want syncing with no completed sync", startedConnection)
	}
	connection, err := repo.UpdateLocation(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("switch selected location: %v", err)
	}
	if connection.SnapshotGeneration <= locationAGeneration {
		t.Fatalf("location switch generation = %d, want greater than %d", connection.SnapshotGeneration, locationAGeneration)
	}
	if connection.LastSyncAt != nil || connection.Status != StatusConnected {
		t.Fatalf("connection after location switch = %#v, want connected with no completed sync", connection)
	}
	sameLocationConnection, err := repo.UpdateLocation(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("reselect current location: %v", err)
	}
	if sameLocationConnection.SnapshotGeneration != connection.SnapshotGeneration {
		t.Fatalf("same-location selection generation = %d, want unchanged %d", sameLocationConnection.SnapshotGeneration, connection.SnapshotGeneration)
	}

	staleLocationSnapshot := ProviderSnapshot{
		Provider:   ProviderSquare,
		LocationID: "location-a",
		Generation: locationAGeneration,
		Services: []Service{{
			POSProvider:       ProviderSquare,
			POSServiceID:      "service-" + suffix,
			POSServiceVersion: 1,
			Name:              "Stale Location Service",
			DurationMinutes:   30,
			AIBookable:        true,
			Active:            true,
		}},
	}
	if _, err := repo.ApplyProviderSnapshot(ctx, salonID, staleLocationSnapshot); !errors.Is(err, ErrStaleProviderSnapshot) {
		t.Fatalf("apply stale location snapshot error = %v, want ErrStaleProviderSnapshot", err)
	}
	var staleServiceCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM services WHERE salon_id = $1 AND pos_service_id = $2
	`, salonID, "service-"+suffix).Scan(&staleServiceCount); err != nil {
		t.Fatalf("count stale location services: %v", err)
	}
	if staleServiceCount != 0 {
		t.Fatalf("stale location service count = %d, want 0", staleServiceCount)
	}

	olderGeneration, err := repo.BeginProviderSnapshot(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin older location B snapshot: %v", err)
	}
	newerGeneration, err := repo.BeginProviderSnapshot(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin newer location B snapshot: %v", err)
	}
	newerSnapshot := ProviderSnapshot{
		Provider:   ProviderSquare,
		LocationID: "location-b",
		Generation: newerGeneration,
		Services: []Service{{
			POSProvider:       ProviderSquare,
			POSServiceID:      "service-" + suffix,
			POSServiceVersion: 2,
			Name:              "Current Location Service",
			DurationMinutes:   45,
			AIBookable:        true,
			Active:            true,
		}},
	}
	if _, err := repo.ApplyProviderSnapshot(ctx, salonID, newerSnapshot); err != nil {
		t.Fatalf("apply newer snapshot: %v", err)
	}
	olderSnapshot := newerSnapshot
	olderSnapshot.Generation = olderGeneration
	olderSnapshot.Services = append([]Service(nil), newerSnapshot.Services...)
	olderSnapshot.Services[0].POSServiceVersion = 1
	olderSnapshot.Services[0].Name = "Out Of Order Old Service"
	if _, err := repo.ApplyProviderSnapshot(ctx, salonID, olderSnapshot); !errors.Is(err, ErrStaleProviderSnapshot) {
		t.Fatalf("apply out-of-order snapshot error = %v, want ErrStaleProviderSnapshot", err)
	}
	if err := repo.MarkSyncCompleteForGeneration(ctx, salonID, ProviderSquare, olderGeneration, StatusError, "old sync failed"); !errors.Is(err, ErrStaleProviderSnapshot) {
		t.Fatalf("complete old generation error = %v, want ErrStaleProviderSnapshot", err)
	}
	if err := repo.MarkSyncCompleteForGeneration(ctx, salonID, ProviderSquare, newerGeneration, StatusActive, ""); err != nil {
		t.Fatalf("complete newer generation: %v", err)
	}
	activeConnection, err := repo.GetConnection(ctx, salonID, ProviderSquare)
	if err != nil {
		t.Fatalf("load completed location B snapshot: %v", err)
	}
	if activeConnection.Status != StatusActive || activeConnection.LastSyncAt == nil {
		t.Fatalf("connection after successful sync = %#v, want active with completed sync timestamp", activeConnection)
	}

	failedGeneration, err := repo.BeginProviderSnapshot(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin failed location B snapshot: %v", err)
	}
	if err := repo.MarkSyncCompleteForGeneration(ctx, salonID, ProviderSquare, failedGeneration, StatusError, "provider unavailable"); err != nil {
		t.Fatalf("complete failed location B snapshot: %v", err)
	}
	failedConnection, err := repo.GetConnection(ctx, salonID, ProviderSquare)
	if err != nil {
		t.Fatalf("load failed location B snapshot: %v", err)
	}
	if failedConnection.Status != StatusError || failedConnection.LastSyncAt != nil {
		t.Fatalf("connection after failed sync = %#v, want error with no completed sync timestamp", failedConnection)
	}

	recoveryGeneration, err := repo.BeginProviderSnapshot(ctx, salonID, ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin recovery location B snapshot: %v", err)
	}
	if err := repo.MarkSyncCompleteForGeneration(ctx, salonID, ProviderSquare, recoveryGeneration, StatusActive, ""); err != nil {
		t.Fatalf("complete recovery location B snapshot: %v", err)
	}
	recoveredConnection, err := repo.GetConnection(ctx, salonID, ProviderSquare)
	if err != nil {
		t.Fatalf("load recovered location B snapshot: %v", err)
	}
	if recoveredConnection.Status != StatusActive || recoveredConnection.LastSyncAt == nil {
		t.Fatalf("connection after recovery sync = %#v, want active with completed sync timestamp", recoveredConnection)
	}

	var serviceName string
	var serviceVersion int64
	if err := db.QueryRowContext(ctx, `
		SELECT name, pos_service_version
		FROM services
		WHERE salon_id = $1 AND pos_provider = 'square' AND pos_service_id = $2
	`, salonID, "service-"+suffix).Scan(&serviceName, &serviceVersion); err != nil {
		t.Fatalf("load current provider service: %v", err)
	}
	if serviceName != "Current Location Service" || serviceVersion != 2 {
		t.Fatalf("current provider service = %q v%d, want current v2", serviceName, serviceVersion)
	}
}

func TestRepositoryProviderEligibilityRevokesAIBookableWithoutReenablingOwnerFlag(t *testing.T) {
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
		VALUES ($1, 'integration-test', 'POS Eligibility Test')
		RETURNING id::text
	`, "pos-eligibility-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('POS Eligibility Test Salon', '+13125550199', $1)
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

	repo := NewRepository(db)
	service := Service{
		POSProvider:       ProviderSquare,
		POSServiceID:      "provider-service-" + suffix,
		POSServiceVersion: 1,
		Name:              "Eligibility Service",
		DurationMinutes:   30,
		AIBookable:        true,
		Active:            true,
	}
	if err := repo.UpsertServices(ctx, salonID, []Service{service}); err != nil {
		t.Fatalf("initial eligible import: %v", err)
	}
	assertImportedServiceAIBookable(t, ctx, db, salonID, service.POSServiceID, true)

	service.AIBookable = false
	service.POSServiceVersion++
	if err := repo.UpsertServices(ctx, salonID, []Service{service}); err != nil {
		t.Fatalf("provider eligibility revocation import: %v", err)
	}
	assertImportedServiceAIBookable(t, ctx, db, salonID, service.POSServiceID, false)

	service.AIBookable = true
	service.POSServiceVersion++
	if err := repo.UpsertServices(ctx, salonID, []Service{service}); err != nil {
		t.Fatalf("provider eligibility restore import: %v", err)
	}
	assertImportedServiceAIBookable(t, ctx, db, salonID, service.POSServiceID, false)

	member := StaffMember{
		POSProvider: ProviderSquare,
		POSStaffID:  "provider-staff-" + suffix,
		Name:        "Eligibility Technician",
		AIBookable:  true,
		Active:      true,
	}
	if err := repo.UpsertStaff(ctx, salonID, []StaffMember{member}); err != nil {
		t.Fatalf("initial eligible staff import: %v", err)
	}
	assertImportedStaffAIBookable(t, ctx, db, salonID, member.POSStaffID, true)

	member.AIBookable = false
	if err := repo.UpsertStaff(ctx, salonID, []StaffMember{member}); err != nil {
		t.Fatalf("staff provider eligibility revocation import: %v", err)
	}
	assertImportedStaffAIBookable(t, ctx, db, salonID, member.POSStaffID, false)

	member.AIBookable = true
	if err := repo.UpsertStaff(ctx, salonID, []StaffMember{member}); err != nil {
		t.Fatalf("staff provider eligibility restore import: %v", err)
	}
	assertImportedStaffAIBookable(t, ctx, db, salonID, member.POSStaffID, false)
}

func assertImportedServiceAIBookable(t *testing.T, ctx context.Context, db *sql.DB, salonID string, providerServiceID string, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRowContext(ctx, `
		SELECT ai_bookable
		FROM services
		WHERE salon_id = $1
		  AND pos_provider = 'square'
		  AND pos_service_id = $2
	`, salonID, providerServiceID).Scan(&got); err != nil {
		t.Fatalf("load imported service eligibility: %v", err)
	}
	if got != want {
		t.Fatalf("ai_bookable = %t, want %t", got, want)
	}
}

func assertImportedStaffAIBookable(t *testing.T, ctx context.Context, db *sql.DB, salonID string, providerStaffID string, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRowContext(ctx, `
		SELECT ai_bookable
		FROM staff
		WHERE salon_id = $1
		  AND pos_provider = 'square'
		  AND pos_staff_id = $2
	`, salonID, providerStaffID).Scan(&got); err != nil {
		t.Fatalf("load imported staff eligibility: %v", err)
	}
	if got != want {
		t.Fatalf("staff ai_bookable = %t, want %t", got, want)
	}
}
