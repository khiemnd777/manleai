package access

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func TestAccessRepositoryTenantPlatformAndPIIBoundaries(t *testing.T) {
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
	adminID := insertAccessTestUser(t, db, "admin-"+suffix+"@example.test")
	opsID := insertAccessTestUser(t, db, "ops-"+suffix+"@example.test")
	ownerAID := insertAccessTestUser(t, db, "owner-a-"+suffix+"@example.test")
	ownerBID := insertAccessTestUser(t, db, "owner-b-"+suffix+"@example.test")
	managerID := insertAccessTestUser(t, db, "manager-"+suffix+"@example.test")
	salonAID := insertAccessTestSalon(t, db, ownerAID, "Access A "+suffix)
	salonBID := insertAccessTestSalon(t, db, ownerBID, "Access B "+suffix)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (
			user_id, role_id, status, created_by_user_id, updated_by_user_id
		)
		SELECT $1, role.id, 'active', $1, $1
		FROM roles AS role
		WHERE role.name = 'platform_admin'
	`, adminID); err != nil {
		t.Fatalf("seed platform admin: %v", err)
	}

	repository := NewRepository(db)
	service := NewService(repository)
	adminActor := middleware.ActorContext{UserID: adminID, Roles: []string{RolePlatformAdmin}}
	users, err := service.ListUsers(ctx, adminActor, "owner-a-"+suffix, 25)
	if err != nil || len(users.Users) != 1 || users.Users[0].ID != ownerAID || users.Users[0].Email == "" {
		t.Fatalf("list access users=%#v err=%v", users, err)
	}

	assertAccessDecision(t, repository, ownerAID, AccessCheck{Surface: SurfaceTenant, SalonID: salonAID, Capability: CapabilityBusinessRead}, true)
	assertAccessDecision(t, repository, ownerAID, AccessCheck{Surface: SurfaceTenant, SalonID: salonBID, Capability: CapabilityBusinessRead}, false)
	assertAccessDecision(t, repository, adminID, AccessCheck{Surface: SurfacePlatform, SalonID: salonBID, Capability: CapabilityBusinessRead}, true)
	assertAccessDecision(t, repository, adminID, AccessCheck{Surface: SurfacePlatform, SalonID: salonBID, Capability: CapabilityBusinessRead, PIIScope: PIIScopeCustomers}, false)

	roleRequest := PlatformRoleMutationRequest{
		ActionKey:       "access-test-role-" + suffix,
		Role:            RolePlatformOps,
		Status:          "active",
		ExpectedVersion: 0,
	}
	roleAssignment, replayed, err := service.MutatePlatformRole(ctx, adminActor, opsID, roleRequest)
	if err != nil || replayed || roleAssignment.Role != RolePlatformOps {
		t.Fatalf("create ops role assignment=%#v replayed=%t err=%v", roleAssignment, replayed, err)
	}
	roleReplay, replayed, err := service.MutatePlatformRole(ctx, adminActor, opsID, roleRequest)
	if err != nil || !replayed || roleReplay.ID != roleAssignment.ID || roleReplay.Version != roleAssignment.Version {
		t.Fatalf("replay ops role assignment=%#v replayed=%t err=%v", roleReplay, replayed, err)
	}
	changedRoleRequest := roleRequest
	changedRoleRequest.Role = RolePlatformAdmin
	if _, _, err := service.MutatePlatformRole(ctx, adminActor, opsID, changedRoleRequest); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed action-key reuse error=%v, want action conflict", err)
	}

	assignmentRequest := SalonAssignmentMutationRequest{
		ActionKey:       "access-test-assignment-" + suffix,
		Status:          "active",
		ExpectedVersion: 0,
		Permissions:     []string{string(CapabilityBusinessRead), string(CapabilityBusinessWrite)},
	}
	assignment, replayed, err := service.MutateSalonAssignment(ctx, adminActor, salonAID, opsID, assignmentRequest)
	if err != nil || replayed || assignment.SalonID != salonAID {
		t.Fatalf("create salon assignment=%#v replayed=%t err=%v", assignment, replayed, err)
	}
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonAID, Capability: CapabilityBusinessRead}, true)
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonBID, Capability: CapabilityBusinessRead}, false)
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfaceTenant, SalonID: salonAID, Capability: CapabilityBusinessRead}, false)

	membershipRequest := MembershipMutationRequest{
		ActionKey:       "access-test-membership-" + suffix,
		Role:            RoleTenantBusinessManager,
		Status:          "active",
		ExpectedVersion: 0,
	}
	membership, replayed, err := service.MutateMembership(ctx, adminActor, salonAID, managerID, membershipRequest)
	if err != nil || replayed || membership.IsOwner {
		t.Fatalf("create membership=%#v replayed=%t err=%v", membership, replayed, err)
	}
	assertAccessDecision(t, repository, managerID, AccessCheck{Surface: SurfaceTenant, SalonID: salonAID, Capability: CapabilityBusinessWrite}, true)
	assertAccessDecision(t, repository, managerID, AccessCheck{Surface: SurfaceTenant, SalonID: salonBID, Capability: CapabilityBusinessWrite}, false)
	staleMembershipRequest := membershipRequest
	staleMembershipRequest.ActionKey = "access-test-membership-stale-" + suffix
	staleMembershipRequest.Status = "revoked"
	staleMembershipRequest.ExpectedVersion = membership.Version - 1
	if _, _, err := service.MutateMembership(ctx, adminActor, salonAID, managerID, staleMembershipRequest); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale membership error=%v, want version conflict", err)
	}

	ownerMemberships, err := repository.ListMemberships(ctx, adminID, salonAID)
	if err != nil {
		t.Fatalf("list memberships: %v", err)
	}
	var ownerMembership Membership
	for _, item := range ownerMemberships {
		if item.UserID == ownerAID {
			ownerMembership = item
			break
		}
	}
	if !ownerMembership.IsOwner || ownerMembership.Role != RoleTenantOwner {
		t.Fatalf("owner membership=%#v, want trigger-backed active tenant owner", ownerMembership)
	}
	if _, _, err := service.MutateMembership(ctx, adminActor, salonAID, ownerAID, MembershipMutationRequest{
		ActionKey:       "access-test-owner-revoke-" + suffix,
		Role:            RoleTenantOwner,
		Status:          "revoked",
		ExpectedVersion: ownerMembership.Version,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner revoke error=%v, want forbidden", err)
	}

	service.now = func() time.Time { return time.Now().UTC() }
	grantRequest := PIIGrantRequest{
		ActionKey: "access-test-pii-" + suffix,
		UserID:    opsID,
		Scope:     string(PIIScopeCustomers),
		Reason:    "integration-test-change-reference",
		ExpiresAt: service.now().Add(time.Hour),
	}
	grant, replayed, err := service.GrantPIIAccess(ctx, adminActor, salonAID, grantRequest)
	if err != nil || replayed {
		t.Fatalf("grant PII=%#v replayed=%t err=%v", grant, replayed, err)
	}
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonAID, Capability: CapabilityBusinessRead, PIIScope: PIIScopeCustomers}, true)
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonBID, Capability: CapabilityBusinessRead, PIIScope: PIIScopeCustomers}, false)
	revoked, replayed, err := service.RevokePIIAccess(ctx, adminActor, salonAID, grant.ID, PIIGrantRevokeRequest{
		ActionKey:       "access-test-pii-revoke-" + suffix,
		ExpectedVersion: grant.Version,
	})
	if err != nil || replayed || revoked.RevokedAt == nil {
		t.Fatalf("revoke PII=%#v replayed=%t err=%v", revoked, replayed, err)
	}
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonAID, Capability: CapabilityBusinessRead, PIIScope: PIIScopeCustomers}, false)

	roleBoundGrantRequest := grantRequest
	roleBoundGrantRequest.ActionKey = "access-test-pii-role-bound-" + suffix
	roleBoundGrant, replayed, err := service.GrantPIIAccess(ctx, adminActor, salonAID, roleBoundGrantRequest)
	if err != nil || replayed {
		t.Fatalf("grant role-bound PII=%#v replayed=%t err=%v", roleBoundGrant, replayed, err)
	}
	revokedRole, replayed, err := service.MutatePlatformRole(ctx, adminActor, opsID, PlatformRoleMutationRequest{
		ActionKey:       "access-test-role-revoke-" + suffix,
		Role:            RolePlatformOps,
		Status:          "revoked",
		ExpectedVersion: roleAssignment.Version,
	})
	if err != nil || replayed || revokedRole.Status != "revoked" {
		t.Fatalf("revoke ops role=%#v replayed=%t err=%v", revokedRole, replayed, err)
	}
	autoRevokedGrant, err := scanPIIGrant(db.QueryRowContext(ctx, piiGrantSelect+` WHERE grant_record.id = $1`, roleBoundGrant.ID))
	if err != nil || autoRevokedGrant.RevokedAt == nil {
		t.Fatalf("role-bound PII grant after role revoke=%#v err=%v, want revoked", autoRevokedGrant, err)
	}
	assignments, err := repository.ListSalonAssignments(ctx, adminID, salonAID)
	if err != nil {
		t.Fatalf("list assignments after role revoke: %v", err)
	}
	var revokedAssignment *SalonAssignment
	for i := range assignments {
		if assignments[i].ID == assignment.ID {
			revokedAssignment = &assignments[i]
			break
		}
	}
	if revokedAssignment == nil || revokedAssignment.Status != "revoked" || revokedAssignment.Version != assignment.Version+1 {
		t.Fatalf("assignment after role revoke=%#v, want revoked version %d", revokedAssignment, assignment.Version+1)
	}
	var relatedRevocationEvents int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM access_control_events AS event
		JOIN access_control_actions AS action ON action.id = event.action_id
		WHERE action.actor_user_id = $1
		  AND action.action_key = $2
		  AND event.event_type IN (
		      'platform.salon_assignment.revoked_by_role_transition',
		      'platform.pii_grant.revoked_by_role_transition'
		  )
	`, adminID, "access-test-role-revoke-"+suffix).Scan(&relatedRevocationEvents); err != nil {
		t.Fatalf("count role-transition child audit: %v", err)
	}
	if relatedRevocationEvents != 2 {
		t.Fatalf("role-transition child audit events=%d, want 2", relatedRevocationEvents)
	}
	reactivatedRole, replayed, err := service.MutatePlatformRole(ctx, adminActor, opsID, PlatformRoleMutationRequest{
		ActionKey:       "access-test-role-reactivate-" + suffix,
		Role:            RolePlatformOps,
		Status:          "active",
		ExpectedVersion: revokedRole.Version,
	})
	if err != nil || replayed || reactivatedRole.Status != "active" {
		t.Fatalf("reactivate ops role=%#v replayed=%t err=%v", reactivatedRole, replayed, err)
	}
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonAID, Capability: CapabilityBusinessRead}, false)
	assertAccessDecision(t, repository, opsID, AccessCheck{Surface: SurfacePlatform, SalonID: salonAID, Capability: CapabilityBusinessRead, PIIScope: PIIScopeCustomers}, false)

	var auditReasonLeak bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM access_control_events
			WHERE actor_user_id = $1
			  AND details::text LIKE '%integration-test-change-reference%'
		)
	`, adminID).Scan(&auditReasonLeak); err != nil {
		t.Fatalf("inspect access audit: %v", err)
	}
	if auditReasonLeak {
		t.Fatal("access audit details leaked PII grant change reference")
	}
}

func TestConcurrentPlatformAdminDemotionsPreserveOneActiveAdmin(t *testing.T) {
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
	adminAID := insertAccessTestUser(t, db, "concurrent-admin-a-"+suffix+"@example.test")
	adminBID := insertAccessTestUser(t, db, "concurrent-admin-b-"+suffix+"@example.test")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (
			user_id, role_id, status, created_by_user_id, updated_by_user_id
		)
		SELECT account.id, role.id, 'active', account.id, account.id
		FROM users AS account
		CROSS JOIN roles AS role
		WHERE account.id = ANY($1)
		  AND role.name = 'platform_admin'
	`, pq.Array([]string{adminAID, adminBID})); err != nil {
		t.Fatalf("seed concurrent admins: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE platform_role_assignments AS assignment
		SET status = 'revoked',
		    version = version + 1,
		    updated_at = now()
		FROM roles AS role
		WHERE role.id = assignment.role_id
		  AND role.name = 'platform_admin'
		  AND assignment.status = 'active'
		  AND assignment.user_id <> ALL($1)
	`, pq.Array([]string{adminAID, adminBID})); err != nil {
		t.Fatalf("isolate concurrent admin fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION phase2_test_delay_platform_role_update()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			PERFORM pg_sleep(0.15);
			RETURN NEW;
		END
		$$;
		DROP TRIGGER IF EXISTS phase2_test_delay_platform_role_update
			ON platform_role_assignments;
		CREATE TRIGGER phase2_test_delay_platform_role_update
		BEFORE UPDATE ON platform_role_assignments
		FOR EACH ROW EXECUTE FUNCTION phase2_test_delay_platform_role_update();
	`); err != nil {
		t.Fatalf("install concurrent admin test trigger: %v", err)
	}
	defer func() {
		_, _ = db.Exec(`DROP TRIGGER IF EXISTS phase2_test_delay_platform_role_update ON platform_role_assignments`)
		_, _ = db.Exec(`DROP FUNCTION IF EXISTS phase2_test_delay_platform_role_update()`)
	}()

	repository := NewRepository(db)
	service := NewService(repository)
	type mutationResult struct {
		err error
	}
	start := make(chan struct{})
	results := make(chan mutationResult, 2)
	var workers sync.WaitGroup
	for _, mutation := range []struct {
		actorID  string
		targetID string
		key      string
	}{
		{actorID: adminAID, targetID: adminBID, key: "concurrent-demote-b-" + suffix},
		{actorID: adminBID, targetID: adminAID, key: "concurrent-demote-a-" + suffix},
	} {
		mutation := mutation
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, _, mutationErr := service.MutatePlatformRole(ctx, middleware.ActorContext{UserID: mutation.actorID}, mutation.targetID, PlatformRoleMutationRequest{
				ActionKey:       mutation.key,
				Role:            RolePlatformOps,
				Status:          "active",
				ExpectedVersion: 1,
			})
			results <- mutationResult{err: mutationErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	guarded := 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
		case errors.Is(result.err, ErrLastAdmin), errors.Is(result.err, ErrForbidden):
			guarded++
		default:
			t.Fatalf("concurrent admin demotion returned unexpected error: %v", result.err)
		}
	}
	if successes != 1 || guarded != 1 {
		t.Fatalf("concurrent admin demotions successes=%d guarded=%d, want 1/1", successes, guarded)
	}
	var activeAdmins int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM platform_role_assignments AS assignment
		JOIN roles AS role ON role.id = assignment.role_id
		WHERE assignment.user_id = ANY($1)
		  AND assignment.status = 'active'
		  AND role.name = 'platform_admin'
	`, pq.Array([]string{adminAID, adminBID})).Scan(&activeAdmins); err != nil {
		t.Fatalf("count concurrent admins: %v", err)
	}
	if activeAdmins != 1 {
		t.Fatalf("active concurrent admins=%d, want 1", activeAdmins)
	}
}

func insertAccessTestUser(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (email, password_hash, full_name, status)
		VALUES ($1, 'integration-test-only', 'Access Test', 'active')
		RETURNING id::text
	`, email).Scan(&id); err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	return id
}

func insertAccessTestSalon(t *testing.T, db *sql.DB, ownerUserID, name string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ($1, '+13125550199', $2)
		RETURNING id::text
	`, name, ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert salon %s: %v", name, err)
	}
	return id
}

func assertAccessDecision(t *testing.T, repository *Repository, actorUserID string, check AccessCheck, want bool) {
	t.Helper()
	allowed, err := repository.Evaluate(context.Background(), actorUserID, check)
	if err != nil {
		t.Fatalf("evaluate actor=%s check=%#v: %v", actorUserID, check, err)
	}
	if allowed != want {
		t.Fatalf("evaluate actor=%s check=%#v allowed=%t, want %t", actorUserID, check, allowed, want)
	}
}
