package pos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

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
	var canonicalServiceID string
	if err := db.QueryRowContext(ctx, `
		SELECT id::text
		FROM services
		WHERE salon_id = $1 AND pos_provider = 'square' AND pos_service_id = $2
	`, salonID, service.POSServiceID).Scan(&canonicalServiceID); err != nil {
		t.Fatalf("load canonical service ID: %v", err)
	}
	draftProfile := &ServiceConsultationProfileMutation{
		Status:                   ConsultationProfileStatusDraft,
		RecommendedOutcomes:      []string{},
		CompatibleCurrentSystems: []string{},
		LengthCapabilities:       []string{},
		PriorityTags:             []string{},
		FinishOptions:            []string{},
	}
	if _, err := repo.UpdateServiceOwnerControls(ctx, salonID, ownerID, canonicalServiceID, ServiceOwnerControlsMutation{
		AIDescription:       "Owner-approved comparison guidance.",
		ConsultationProfile: draftProfile,
	}); err != nil {
		t.Fatalf("update provider-managed owner controls: %v", err)
	}
	if _, err := repo.UpdateServiceOwnerControls(ctx, salonID, ownerID, canonicalServiceID, ServiceOwnerControlsMutation{
		AIDescription:       "Owner-approved comparison guidance.",
		ConsultationProfile: draftProfile,
	}); err != nil {
		t.Fatalf("repeat idempotent owner controls update: %v", err)
	}
	var profileCount int
	var profileRevision int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), max(revision)
		FROM service_consultation_profiles
		WHERE salon_id = $1 AND service_id = $2
	`, salonID, canonicalServiceID).Scan(&profileCount, &profileRevision); err != nil {
		t.Fatalf("load owner controls profile: %v", err)
	}
	if profileCount != 1 || profileRevision != 1 {
		t.Fatalf("profile count/revision = %d/%d, want stable 1/1", profileCount, profileRevision)
	}

	service.AIBookable = false
	service.Name = "Eligibility Service From Provider"
	service.POSServiceVersion++
	if err := repo.UpsertServices(ctx, salonID, []Service{service}); err != nil {
		t.Fatalf("provider eligibility revocation import: %v", err)
	}
	assertImportedServiceAIBookable(t, ctx, db, salonID, service.POSServiceID, false)
	var importedName string
	var retainedAIDescription string
	if err := db.QueryRowContext(ctx, `
		SELECT name, COALESCE(ai_description, '')
		FROM services
		WHERE salon_id = $1 AND id = $2
	`, salonID, canonicalServiceID).Scan(&importedName, &retainedAIDescription); err != nil {
		t.Fatalf("load provider fields and owner controls after sync: %v", err)
	}
	if importedName != "Eligibility Service From Provider" || retainedAIDescription != "Owner-approved comparison guidance." {
		t.Fatalf("service after sync = %q / %q, want provider name plus retained owner guidance", importedName, retainedAIDescription)
	}

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

type aiBookablePGFixture struct {
	db           *sql.DB
	ownerID      string
	otherOwnerID string
	salonID      string
}

type aiBookableServiceSpec struct {
	provider        string
	providerID      string
	providerVersion int64
	syncStatus      string
	active          bool
	archived        bool
	durationMinutes int
}

type aiBookableStaffSpec struct {
	provider   string
	providerID string
	syncStatus string
	active     bool
	archived   bool
}

func TestRepositoryAIBookableInternalAuthoritiesAllowLocalCanonicalEntities(t *testing.T) {
	for _, authority := range []string{schedulingAuthorityOwnerManual, schedulingAuthorityManleAICalendar} {
		t.Run(authority, func(t *testing.T) {
			fixture := newAIBookablePGFixture(t, authority)
			repo := NewRepository(fixture.db)
			serviceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{
				provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, durationMinutes: 45,
			})
			staffID := insertAIBookableStaff(t, fixture, aiBookableStaffSpec{
				provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true,
			})

			for attempt := 0; attempt < 2; attempt++ {
				service, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true)
				if err != nil || service == nil || !service.AIBookable {
					t.Fatalf("enable local service attempt %d = %#v/%v", attempt+1, service, err)
				}
				staff, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, true)
				if err != nil || staff == nil || !staff.AIBookable {
					t.Fatalf("enable local staff attempt %d = %#v/%v", attempt+1, staff, err)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				service, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, false)
				if err != nil || service == nil || service.AIBookable {
					t.Fatalf("disable local service attempt %d = %#v/%v", attempt+1, service, err)
				}
				staff, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, false)
				if err != nil || staff == nil || staff.AIBookable {
					t.Fatalf("disable local staff attempt %d = %#v/%v", attempt+1, staff, err)
				}
			}
		})
	}
}

func TestRepositoryAIBookableInternalAuthoritiesRejectIneligibleCanonicalEntities(t *testing.T) {
	serviceCases := []struct {
		name string
		spec aiBookableServiceSpec
	}{
		{name: "inactive", spec: aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, durationMinutes: 30}},
		{name: "archived", spec: aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, archived: true, durationMinutes: 30}},
		{name: "zero duration", spec: aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true}},
	}
	staffCases := []struct {
		name string
		spec aiBookableStaffSpec
	}{
		{name: "inactive", spec: aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly}},
		{name: "archived", spec: aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, archived: true}},
	}

	for _, authority := range []string{schedulingAuthorityOwnerManual, schedulingAuthorityManleAICalendar} {
		for _, test := range serviceCases {
			t.Run(authority+" service "+test.name, func(t *testing.T) {
				fixture := newAIBookablePGFixture(t, authority)
				serviceID := insertAIBookableService(t, fixture, test.spec)
				if _, err := NewRepository(fixture.db).UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true); !errors.Is(err, ErrValidation) {
					t.Fatalf("enable error = %v, want ErrValidation", err)
				}
				assertCanonicalAIBookable(t, fixture.db, "services", serviceID, false)
			})
		}
		for _, test := range staffCases {
			t.Run(authority+" staff "+test.name, func(t *testing.T) {
				fixture := newAIBookablePGFixture(t, authority)
				staffID := insertAIBookableStaff(t, fixture, test.spec)
				if _, err := NewRepository(fixture.db).UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, true); !errors.Is(err, ErrValidation) {
					t.Fatalf("enable error = %v, want ErrValidation", err)
				}
				assertCanonicalAIBookable(t, fixture.db, "staff", staffID, false)
			})
		}
	}
}

func TestRepositoryAIBookableExternalAuthorityRequiresActiveProviderEvidence(t *testing.T) {
	fixture := newAIBookablePGFixture(t, schedulingAuthorityExternalProvider)
	repo := NewRepository(fixture.db)
	serviceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{
		provider: "provider-a", providerID: "service-valid", providerVersion: 7,
		syncStatus: SyncStatusSynced, active: true, durationMinutes: 60,
	})
	staffID := insertAIBookableStaff(t, fixture, aiBookableStaffSpec{
		provider: "provider-a", providerID: "staff-valid", syncStatus: SyncStatusSynced, active: true,
	})
	insertAIBookableLink(t, fixture, EntityTypeService, serviceID, "provider-a", "service-active-link", SyncStatusSynced)
	insertAIBookableLink(t, fixture, EntityTypeStaff, staffID, "provider-a", "staff-active-link", SyncStatusSynced)

	for attempt := 0; attempt < 2; attempt++ {
		service, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true)
		if err != nil || service == nil || !service.AIBookable {
			t.Fatalf("enable external service attempt %d = %#v/%v", attempt+1, service, err)
		}
		staff, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, true)
		if err != nil || staff == nil || !staff.AIBookable {
			t.Fatalf("enable external staff attempt %d = %#v/%v", attempt+1, staff, err)
		}
	}
	linkVersionServiceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{
		provider: "provider-a", providerID: "service-legacy-without-version", syncStatus: SyncStatusSynced, active: true, durationMinutes: 30,
	})
	insertAIBookableLink(t, fixture, EntityTypeService, linkVersionServiceID, "provider-a", "service-versioned-link", SyncStatusSynced, 11)
	service, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, linkVersionServiceID, true)
	if err != nil || service == nil || !service.AIBookable {
		t.Fatalf("enable service from authoritative link version = %#v/%v", service, err)
	}
}

func TestRepositoryAIBookableExternalAuthorityRejectsIncompleteEvidence(t *testing.T) {
	serviceCases := []struct {
		name         string
		spec         aiBookableServiceSpec
		link         bool
		linkID       string
		linkStatus   string
		linkVersions []int64
	}{
		{name: "old provider", spec: aiBookableServiceSpec{provider: "provider-b", providerID: "service-old", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30}, link: true, linkID: "service-old", linkStatus: SyncStatusSynced},
		{name: "local only", spec: aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, durationMinutes: 30}},
		{name: "unmapped", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-unmapped", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30}},
		{name: "sync failed", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-failed", providerVersion: 1, syncStatus: SyncStatusSyncFailed, active: true, durationMinutes: 30}, link: true, linkID: "service-failed", linkStatus: SyncStatusSyncFailed},
		{name: "empty provider id", spec: aiBookableServiceSpec{provider: "provider-a", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30}, link: true, linkStatus: SyncStatusSynced},
		{name: "zero provider version", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-zero-version", syncStatus: SyncStatusSynced, active: true, durationMinutes: 30}, link: true, linkID: "service-zero-version", linkStatus: SyncStatusSynced},
		{name: "explicit zero link version", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-positive-legacy-version", providerVersion: 9, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30}, link: true, linkID: "service-zero-link-version", linkStatus: SyncStatusSynced, linkVersions: []int64{0}},
		{name: "inactive", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-inactive", providerVersion: 1, syncStatus: SyncStatusSynced, durationMinutes: 30}, link: true, linkID: "service-inactive", linkStatus: SyncStatusSynced},
		{name: "archived", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-archived", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, archived: true, durationMinutes: 30}, link: true, linkID: "service-archived", linkStatus: SyncStatusSynced},
		{name: "zero duration", spec: aiBookableServiceSpec{provider: "provider-a", providerID: "service-zero-duration", providerVersion: 1, syncStatus: SyncStatusSynced, active: true}, link: true, linkID: "service-zero-duration", linkStatus: SyncStatusSynced},
	}
	for _, test := range serviceCases {
		t.Run("service "+test.name, func(t *testing.T) {
			fixture := newAIBookablePGFixture(t, schedulingAuthorityExternalProvider)
			serviceID := insertAIBookableService(t, fixture, test.spec)
			if test.link {
				insertAIBookableLink(t, fixture, EntityTypeService, serviceID, test.spec.provider, test.linkID, test.linkStatus, test.linkVersions...)
			}
			if _, err := NewRepository(fixture.db).UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true); !errors.Is(err, ErrValidation) {
				t.Fatalf("enable error = %v, want ErrValidation", err)
			}
			assertCanonicalAIBookable(t, fixture.db, "services", serviceID, false)
		})
	}

	staffCases := []struct {
		name       string
		spec       aiBookableStaffSpec
		link       bool
		linkID     string
		linkStatus string
	}{
		{name: "old provider", spec: aiBookableStaffSpec{provider: "provider-b", providerID: "staff-old", syncStatus: SyncStatusSynced, active: true}, link: true, linkID: "staff-old", linkStatus: SyncStatusSynced},
		{name: "local only", spec: aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true}},
		{name: "unmapped", spec: aiBookableStaffSpec{provider: "provider-a", providerID: "staff-unmapped", syncStatus: SyncStatusSynced, active: true}},
		{name: "sync failed", spec: aiBookableStaffSpec{provider: "provider-a", providerID: "staff-failed", syncStatus: SyncStatusSyncFailed, active: true}, link: true, linkID: "staff-failed", linkStatus: SyncStatusSyncFailed},
		{name: "empty provider id", spec: aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusSynced, active: true}, link: true, linkStatus: SyncStatusSynced},
		{name: "inactive", spec: aiBookableStaffSpec{provider: "provider-a", providerID: "staff-inactive", syncStatus: SyncStatusSynced}, link: true, linkID: "staff-inactive", linkStatus: SyncStatusSynced},
		{name: "archived", spec: aiBookableStaffSpec{provider: "provider-a", providerID: "staff-archived", syncStatus: SyncStatusSynced, active: true, archived: true}, link: true, linkID: "staff-archived", linkStatus: SyncStatusSynced},
	}
	for _, test := range staffCases {
		t.Run("staff "+test.name, func(t *testing.T) {
			fixture := newAIBookablePGFixture(t, schedulingAuthorityExternalProvider)
			staffID := insertAIBookableStaff(t, fixture, test.spec)
			if test.link {
				insertAIBookableLink(t, fixture, EntityTypeStaff, staffID, test.spec.provider, test.linkID, test.linkStatus)
			}
			if _, err := NewRepository(fixture.db).UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, true); !errors.Is(err, ErrValidation) {
				t.Fatalf("enable error = %v, want ErrValidation", err)
			}
			assertCanonicalAIBookable(t, fixture.db, "staff", staffID, false)
		})
	}
}

func TestRepositoryAIBookableMutationsAreTenantScoped(t *testing.T) {
	fixture := newAIBookablePGFixture(t, schedulingAuthorityOwnerManual)
	serviceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, durationMinutes: 30})
	staffID := insertAIBookableStaff(t, fixture, aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true})
	repo := NewRepository(fixture.db)
	if _, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.otherOwnerID, serviceID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant service error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.otherOwnerID, staffID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant staff error = %v, want ErrNotFound", err)
	}
	assertCanonicalAIBookable(t, fixture.db, "services", serviceID, false)
	assertCanonicalAIBookable(t, fixture.db, "staff", staffID, false)
}

func TestRepositoryAIBookableMissingSettingsFailsEnableButAllowsDisable(t *testing.T) {
	fixture := newAIBookablePGFixture(t, schedulingAuthorityOwnerManual)
	serviceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, durationMinutes: 30})
	staffID := insertAIBookableStaff(t, fixture, aiBookableStaffSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true})
	if _, err := fixture.db.Exec(`UPDATE services SET ai_bookable = true WHERE id = $1`, serviceID); err != nil {
		t.Fatalf("seed enabled service: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE staff SET ai_bookable = true WHERE id = $1`, staffID); err != nil {
		t.Fatalf("seed enabled staff: %v", err)
	}
	if _, err := fixture.db.Exec(`DELETE FROM salon_settings WHERE salon_id = $1`, fixture.salonID); err != nil {
		t.Fatalf("delete scheduling settings: %v", err)
	}
	repo := NewRepository(fixture.db)
	if _, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("service enable without settings error = %v, want ErrValidation", err)
	}
	if _, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("staff enable without settings error = %v, want ErrValidation", err)
	}
	if _, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, false); err != nil {
		t.Fatalf("service disable without settings: %v", err)
	}
	if _, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, false); err != nil {
		t.Fatalf("staff disable without settings: %v", err)
	}
}

func TestRepositoryAIBookableDisableIsIdempotentForInactiveArchivedEntities(t *testing.T) {
	fixture := newAIBookablePGFixture(t, schedulingAuthorityExternalProvider)
	serviceID := insertAIBookableService(t, fixture, aiBookableServiceSpec{
		provider: "provider-b", syncStatus: SyncStatusSyncFailed, archived: true, durationMinutes: 0,
	})
	staffID := insertAIBookableStaff(t, fixture, aiBookableStaffSpec{
		provider: "provider-b", syncStatus: SyncStatusSyncFailed, archived: true,
	})
	if _, err := fixture.db.Exec(`UPDATE services SET ai_bookable = true WHERE id = $1`, serviceID); err != nil {
		t.Fatalf("seed archived service enabled: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE staff SET ai_bookable = true WHERE id = $1`, staffID); err != nil {
		t.Fatalf("seed archived staff enabled: %v", err)
	}
	repo := NewRepository(fixture.db)
	for attempt := 0; attempt < 2; attempt++ {
		service, err := repo.UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, false)
		if err != nil || service == nil || service.AIBookable {
			t.Fatalf("disable archived service attempt %d = %#v/%v", attempt+1, service, err)
		}
		staff, err := repo.UpdateStaffAIBookable(context.Background(), fixture.salonID, fixture.ownerID, staffID, false)
		if err != nil || staff == nil || staff.AIBookable {
			t.Fatalf("disable archived staff attempt %d = %#v/%v", attempt+1, staff, err)
		}
	}
}

func TestRepositoryAIBookableEnableUsesSharedFenceAgainstConcurrentChanges(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		spec      aiBookableServiceSpec
		link      bool
		mutate    func(*testing.T, *sql.Tx, *aiBookablePGFixture, string)
	}{
		{
			name:      "authority switches from internal to external",
			authority: schedulingAuthorityOwnerManual,
			spec:      aiBookableServiceSpec{provider: "provider-a", syncStatus: SyncStatusLocalOnly, active: true, durationMinutes: 30},
			mutate: func(t *testing.T, tx *sql.Tx, fixture *aiBookablePGFixture, _ string) {
				if _, err := tx.Exec(`UPDATE salon_settings SET scheduling_authority = $2 WHERE salon_id = $1`, fixture.salonID, schedulingAuthorityExternalProvider); err != nil {
					t.Fatalf("switch authority: %v", err)
				}
			},
		},
		{
			name:      "active provider changes",
			authority: schedulingAuthorityExternalProvider,
			spec:      aiBookableServiceSpec{provider: "provider-a", providerID: "service-provider", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30},
			link:      true,
			mutate: func(t *testing.T, tx *sql.Tx, fixture *aiBookablePGFixture, _ string) {
				if _, err := tx.Exec(`UPDATE salons SET active_pos_provider = 'provider-b' WHERE id = $1`, fixture.salonID); err != nil {
					t.Fatalf("switch active provider: %v", err)
				}
			},
		},
		{
			name:      "sync becomes failed",
			authority: schedulingAuthorityExternalProvider,
			spec:      aiBookableServiceSpec{provider: "provider-a", providerID: "service-sync", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30},
			link:      true,
			mutate: func(t *testing.T, tx *sql.Tx, fixture *aiBookablePGFixture, serviceID string) {
				if _, err := tx.Exec(`UPDATE services SET sync_status = 'sync_failed' WHERE salon_id = $1 AND id = $2`, fixture.salonID, serviceID); err != nil {
					t.Fatalf("fail service sync: %v", err)
				}
			},
		},
		{
			name:      "service becomes archived",
			authority: schedulingAuthorityExternalProvider,
			spec:      aiBookableServiceSpec{provider: "provider-a", providerID: "service-archive", providerVersion: 1, syncStatus: SyncStatusSynced, active: true, durationMinutes: 30},
			link:      true,
			mutate: func(t *testing.T, tx *sql.Tx, fixture *aiBookablePGFixture, serviceID string) {
				if _, err := tx.Exec(`UPDATE services SET active = false, archived_at = now() WHERE salon_id = $1 AND id = $2`, fixture.salonID, serviceID); err != nil {
					t.Fatalf("archive service: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAIBookablePGFixture(t, test.authority)
			serviceID := insertAIBookableService(t, fixture, test.spec)
			if test.link {
				insertAIBookableLink(t, fixture, EntityTypeService, serviceID, test.spec.provider, test.spec.providerID, SyncStatusSynced)
			}
			tx, err := fixture.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("begin concurrent fence tx: %v", err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, bookingCalendarReconciliationLockPrefix+fixture.salonID); err != nil {
				t.Fatalf("lock shared reconciliation key: %v", err)
			}
			test.mutate(t, tx, fixture, serviceID)
			result := make(chan error, 1)
			go func() {
				_, callErr := NewRepository(fixture.db).UpdateServiceAIBookable(context.Background(), fixture.salonID, fixture.ownerID, serviceID, true)
				result <- callErr
			}()
			select {
			case early := <-result:
				t.Fatalf("enable returned before concurrent fence committed: %v", early)
			case <-time.After(75 * time.Millisecond):
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit concurrent mutation: %v", err)
			}
			select {
			case callErr := <-result:
				if !errors.Is(callErr, ErrValidation) {
					t.Fatalf("post-fence enable error = %v, want ErrValidation", callErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("enable did not resume after concurrent fence committed")
			}
			assertCanonicalAIBookable(t, fixture.db, "services", serviceID, false)
		})
	}
}

func TestRepositoryReadinessMutationsSerializeOnGlobalSchedulingFence(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context, *Repository, *aiBookablePGFixture) error
	}{
		{
			name: "selected location",
			invoke: func(ctx context.Context, repo *Repository, fixture *aiBookablePGFixture) error {
				_, err := repo.UpdateLocation(ctx, fixture.salonID, "provider-a", "location-b")
				return err
			},
		},
		{
			name: "snapshot generation",
			invoke: func(ctx context.Context, repo *Repository, fixture *aiBookablePGFixture) error {
				_, err := repo.BeginProviderSnapshot(ctx, fixture.salonID, "provider-a", "location-a")
				return err
			},
		},
		{
			name: "sync state",
			invoke: func(ctx context.Context, repo *Repository, fixture *aiBookablePGFixture) error {
				return repo.MarkSyncing(ctx, fixture.salonID, "provider-a")
			},
		},
		{
			name: "booking write permission evidence",
			invoke: func(ctx context.Context, repo *Repository, fixture *aiBookablePGFixture) error {
				return repo.LogError(ctx, POSError{SalonID: fixture.salonID, Provider: "provider-a", Operation: "create_booking", ErrorCode: ErrorPermissionDenied, ErrorMessage: "permission denied"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAIBookablePGFixture(t, schedulingAuthorityOwnerManual)
			if _, err := fixture.db.Exec(`
				INSERT INTO pos_connections (salon_id,provider,status,location_id,snapshot_generation,last_sync_at)
				VALUES ($1,'provider-a','active','location-a',1,now())
			`, fixture.salonID); err != nil {
				t.Fatalf("insert mutation connection: %v", err)
			}
			fenceTx, err := fixture.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("begin global fence transaction: %v", err)
			}
			defer fenceTx.Rollback()
			if _, err := fenceTx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, bookingCalendarReconciliationLockPrefix+fixture.salonID); err != nil {
				t.Fatalf("acquire global scheduling fence: %v", err)
			}
			result := make(chan error, 1)
			go func() {
				result <- test.invoke(context.Background(), NewRepository(fixture.db), fixture)
			}()
			select {
			case early := <-result:
				t.Fatalf("readiness mutation returned before global fence release: %v", early)
			case <-time.After(75 * time.Millisecond):
			}
			if err := fenceTx.Commit(); err != nil {
				t.Fatalf("release global scheduling fence: %v", err)
			}
			select {
			case mutationErr := <-result:
				if mutationErr != nil {
					t.Fatalf("readiness mutation after fence release: %v", mutationErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("readiness mutation did not resume after global fence release")
			}
		})
	}
}

func newAIBookablePGFixture(t *testing.T, authority string) *aiBookablePGFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping database: %v", err)
	}
	fixture := &aiBookablePGFixture{db: db}
	for target, name := range map[*string]string{&fixture.ownerID: "AI Bookable Owner", &fixture.otherOwnerID: "Other AI Bookable Owner"} {
		if err := db.QueryRow(`
			INSERT INTO users (email, password_hash, full_name)
			VALUES ($1, 'integration-test', $2) RETURNING id::text
		`, "ai-bookable-"+uuid.NewString()+"@example.com", name).Scan(target); err != nil {
			db.Close()
			t.Fatalf("insert owner: %v", err)
		}
	}
	if err := db.QueryRow(`
		INSERT INTO salons (name, phone, owner_user_id, active_pos_provider)
		VALUES ('AI Bookable Test Salon', '+13125550666', $1, 'provider-a') RETURNING id::text
	`, fixture.ownerID).Scan(&fixture.salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO salon_settings (salon_id, scheduling_authority) VALUES ($1,$2)`, fixture.salonID, authority); err != nil {
		t.Fatalf("insert salon settings: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM salons WHERE id = $1`, fixture.salonID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, fixture.ownerID, fixture.otherOwnerID)
		db.Close()
	})
	return fixture
}

func insertAIBookableService(t *testing.T, fixture *aiBookablePGFixture, spec aiBookableServiceSpec) string {
	t.Helper()
	var serviceID string
	var archivedAt any
	if spec.archived {
		archivedAt = time.Now().UTC()
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, pos_service_version, name,
			duration_minutes, ai_bookable, active, sync_status, archived_at, source
		) VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,false,$7,$8,$9,'local')
		RETURNING id::text
	`, fixture.salonID, spec.provider, spec.providerID, spec.providerVersion, "Service "+uuid.NewString(), spec.durationMinutes, spec.active, spec.syncStatus, archivedAt).Scan(&serviceID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	return serviceID
}

func insertAIBookableStaff(t *testing.T, fixture *aiBookablePGFixture, spec aiBookableStaffSpec) string {
	t.Helper()
	var staffID string
	var archivedAt any
	if spec.archived {
		archivedAt = time.Now().UTC()
	}
	if err := fixture.db.QueryRow(`
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, ai_bookable, active, sync_status, archived_at, source
		) VALUES ($1,$2,NULLIF($3,''),$4,false,$5,$6,$7,'local')
		RETURNING id::text
	`, fixture.salonID, spec.provider, spec.providerID, "Staff "+uuid.NewString(), spec.active, spec.syncStatus, archivedAt).Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	return staffID
}

func insertAIBookableLink(t *testing.T, fixture *aiBookablePGFixture, entityType string, entityID string, provider string, providerEntityID string, syncStatus string, versions ...int64) {
	t.Helper()
	var providerVersion any
	if len(versions) > 0 {
		providerVersion = versions[0]
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id, provider_version, sync_status
		) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7)
	`, fixture.salonID, entityType, entityID, provider, providerEntityID, providerVersion, syncStatus); err != nil {
		t.Fatalf("insert provider link: %v", err)
	}
}

func assertCanonicalAIBookable(t *testing.T, db *sql.DB, table string, entityID string, want bool) {
	t.Helper()
	if table != "services" && table != "staff" {
		t.Fatalf("unsupported canonical table %q", table)
	}
	var got bool
	query := `SELECT ai_bookable FROM ` + table + ` WHERE id = $1`
	if err := db.QueryRow(query, entityID).Scan(&got); err != nil {
		t.Fatalf("load canonical eligibility: %v", err)
	}
	if got != want {
		t.Fatalf("%s ai_bookable = %t, want %t", table, got, want)
	}
}
