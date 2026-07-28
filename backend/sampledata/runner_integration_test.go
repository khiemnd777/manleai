package sampledata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/modules/auth"
)

func TestApplyCreatesLotusSampleProfileAndReplaysExactly(t *testing.T) {
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

	bootstrapAvailable, err := auth.NewRepository(db).BootstrapAvailable(ctx)
	if err != nil || !bootstrapAvailable {
		t.Fatalf("normal migrated database bootstrap available=%t err=%v", bootstrapAvailable, err)
	}
	var normalUserCount, normalSalonCount int
	var sampleLedgerAbsent bool
	if err := db.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM users),
		  (SELECT count(*) FROM salons),
		  to_regclass('public.sample_data_migrations') IS NULL
	`).Scan(&normalUserCount, &normalSalonCount, &sampleLedgerAbsent); err != nil {
		t.Fatalf("inspect normal migrated database: %v", err)
	}
	if normalUserCount != 0 || normalSalonCount != 0 || !sampleLedgerAbsent {
		t.Fatalf("normal migration inserted fixture state users=%d salons=%d sample_ledger_absent=%t", normalUserCount, normalSalonCount, sampleLedgerAbsent)
	}

	req := ApplyRequest{
		Profile:             ProfileSampleTest,
		Confirmation:        ConfirmationToken,
		AdminEmail:          "sample.admin@example.test",
		AdminName:           "Sample Platform Admin",
		AdminPassword:       "sample-admin-password",
		OpsEmail:            "sample.ops@example.test",
		OpsName:             "Sample Platform Ops",
		OpsPassword:         "sample-ops-password",
		TenantOwnerPassword: "sample-owner-password",
	}
	result, err := Apply(ctx, db, req)
	if err != nil {
		t.Fatalf("apply sample data: %v", err)
	}
	if result.SampleMigrationReplayed || result.UserCount != 3 || result.SalonCount != 1 || result.CategoryCount != 5 || result.CategoryAliasCount != 20 || result.ServiceCount != 7 || result.ServiceAliasCount != 7 || result.ConsultationProfileCount != 7 || result.StaffCount != 4 || result.StaffServiceLinkCount != 28 || result.LocalHoursCount != 6 {
		t.Fatalf("first result=%#v", result)
	}
	if result.PIIGrantCount != 0 || result.ProviderConfigCount != 0 {
		t.Fatalf("unsafe sample grants/configs result=%#v", result)
	}

	assertSampleAccessShape(t, ctx, db, req)

	replayed, err := Apply(ctx, db, req)
	if err != nil {
		t.Fatalf("replay sample data: %v", err)
	}
	if !replayed.SampleMigrationReplayed || replayed.SalonID != result.SalonID || replayed.UserCount != result.UserCount {
		t.Fatalf("replay result=%#v first=%#v", replayed, result)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(email,password_hash,full_name,status)
		VALUES('live-collision@example.test','integration-test-only','Live Collision','active')
	`); err != nil {
		t.Fatalf("insert live collision: %v", err)
	}
	if _, err := Apply(ctx, db, req); !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("apply with live identity error=%v, want ErrUnsafeTarget", err)
	}
}

func assertSampleAccessShape(t *testing.T, ctx context.Context, db *sql.DB, req ApplyRequest) {
	t.Helper()
	var sampleUsers, sampleSalons, ownerClassificationMatches, platformPrincipals, tenantPrincipals int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE data_classification='sample_test'`).Scan(&sampleUsers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM salons WHERE data_classification='sample_test'`).Scan(&sampleSalons); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM salons salon
		JOIN users owner_account ON owner_account.id=salon.owner_user_id
		WHERE salon.id=$1 AND owner_account.data_classification=salon.data_classification
	`, LotusSalonID).Scan(&ownerClassificationMatches); err != nil {
		t.Fatal(err)
	}
	if sampleUsers != 3 || sampleSalons != 1 || ownerClassificationMatches != 1 {
		t.Fatalf("classification users=%d salons=%d owner_matches=%d", sampleUsers, sampleSalons, ownerClassificationMatches)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE data_classification='sample_test' AND principal_scope='platform'`).Scan(&platformPrincipals); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE data_classification='sample_test' AND principal_scope='tenant'`).Scan(&tenantPrincipals); err != nil {
		t.Fatal(err)
	}
	if platformPrincipals != 2 || tenantPrincipals != 1 {
		t.Fatalf("sample principal scopes platform=%d tenant=%d, want 2/1", platformPrincipals, tenantPrincipals)
	}

	var adminRole, opsRole string
	if err := db.QueryRowContext(ctx, `
		SELECT role.name
		FROM platform_role_assignments assignment
		JOIN roles role ON role.id=assignment.role_id
		JOIN users account ON account.id=assignment.user_id
		WHERE lower(account.email)=lower($1) AND assignment.status='active'
	`, req.AdminEmail).Scan(&adminRole); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT role.name
		FROM platform_role_assignments assignment
		JOIN roles role ON role.id=assignment.role_id
		JOIN users account ON account.id=assignment.user_id
		WHERE lower(account.email)=lower($1) AND assignment.status='active'
	`, req.OpsEmail).Scan(&opsRole); err != nil {
		t.Fatal(err)
	}
	if adminRole != "platform_admin" || opsRole != "platform_ops" {
		t.Fatalf("platform roles admin=%q ops=%q", adminRole, opsRole)
	}

	var opsPermissions []string
	if err := db.QueryRowContext(ctx, `
		SELECT ARRAY(
		  SELECT permission.name
		  FROM platform_salon_assignments assignment
		  JOIN users account ON account.id=assignment.user_id
		  JOIN platform_salon_assignment_permissions assigned ON assigned.assignment_id=assignment.id
		  JOIN permissions permission ON permission.id=assigned.permission_id
		  WHERE assignment.salon_id=$1 AND lower(account.email)=lower($2) AND assignment.status='active'
		  ORDER BY permission.name
		)
	`, LotusSalonID, req.OpsEmail).Scan(pq.Array(&opsPermissions)); err != nil {
		t.Fatal(err)
	}
	wantPermissions := []string{"audit.read", "business.read", "business.write", "operations.read", "operations.write", "technical.read", "technical.write"}
	if !equalStrings(opsPermissions, wantPermissions) {
		t.Fatalf("ops permissions=%v want=%v", opsPermissions, wantPermissions)
	}

	var exactServices, exactStaff, exactHours int
	if err := db.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM services
		   WHERE salon_id=$1 AND (name,duration_minutes,price_from) IN (
		     ('Classic Manicure',30,25.00),('Gel Manicure',45,38.00),
		     ('Classic Pedicure',45,40.00),('Spa Pedicure',60,55.00),
		     ('Dip Powder Manicure',60,50.00),('Acrylic Full Set',75,65.00),
		     ('Gel Removal',20,15.00)
		   )),
		  (SELECT count(*) FROM staff
		   WHERE salon_id=$1 AND name IN ('Linh Nguyen','Mai Tran','Vy Pham','Hannah Le')),
		  (SELECT count(*) FROM salon_business_hour_periods
		   WHERE salon_id=$1 AND source='local_override'
		     AND day_of_week BETWEEN 1 AND 6
		     AND start_local_time=TIME '09:30' AND end_local_time=TIME '19:00')
	`, LotusSalonID).Scan(&exactServices, &exactStaff, &exactHours); err != nil {
		t.Fatal(err)
	}
	if exactServices != 7 || exactStaff != 4 || exactHours != 6 {
		t.Fatalf("exact Lotus fixture services=%d staff=%d hours=%d", exactServices, exactStaff, exactHours)
	}

	if _, err := db.ExecContext(ctx, `UPDATE users SET data_classification='live' WHERE lower(email)=lower($1)`, req.OpsEmail); err == nil {
		t.Fatal("expected immutable user classification guard")
	} else {
		var postgresError *pq.Error
		if !errors.As(err, &postgresError) || postgresError.Code != "23514" || postgresError.Constraint != "users_data_classification_immutable_guard" {
			t.Fatalf("classification mutation error=%v", err)
		}
	}
}
