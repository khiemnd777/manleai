package sampledata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	"golang.org/x/crypto/bcrypt"
)

const (
	ProfileSampleTest = "sample_test"
	ConfirmationToken = "APPLY_SAMPLE_TEST_DATA"
	LotusOwnerEmail   = "owner@lotusnails.example"
	LotusSalonID      = "10000000-0000-4000-8000-000000000001"
	LotusSalonName    = "Lotus Nails Studio"

	sampleMigrationLockID int64 = 73903191241173
)

var ErrUnsafeTarget = errors.New("sample data target is not an empty or sample-only database")

type ApplyRequest struct {
	Profile             string
	Confirmation        string
	AdminEmail          string
	AdminName           string
	AdminPassword       string
	OpsEmail            string
	OpsName             string
	OpsPassword         string
	TenantOwnerPassword string
}

type ApplyResult struct {
	Profile                  string `json:"profile"`
	AdminEmail               string `json:"admin_email"`
	OpsEmail                 string `json:"ops_email"`
	TenantOwnerEmail         string `json:"tenant_owner_email"`
	SalonID                  string `json:"salon_id"`
	SalonName                string `json:"salon_name"`
	SampleMigrationReplayed  bool   `json:"sample_migration_replayed"`
	UserCount                int    `json:"user_count"`
	SalonCount               int    `json:"salon_count"`
	CategoryCount            int    `json:"category_count"`
	CategoryAliasCount       int    `json:"category_alias_count"`
	ServiceCount             int    `json:"service_count"`
	ServiceAliasCount        int    `json:"service_alias_count"`
	ConsultationProfileCount int    `json:"consultation_profile_count"`
	StaffCount               int    `json:"staff_count"`
	StaffServiceLinkCount    int    `json:"staff_service_link_count"`
	LocalHoursCount          int    `json:"local_hours_count"`
	PIIGrantCount            int    `json:"pii_grant_count"`
	ProviderConfigCount      int    `json:"provider_config_count"`
	SchedulingAuthority      string `json:"scheduling_authority"`
	BookingMode              string `json:"booking_mode"`
}

type fixtureMigration struct {
	Version  string
	Order    int
	Name     string
	Path     string
	SQL      string
	Checksum string
}

func Apply(ctx context.Context, db *sql.DB, req ApplyRequest) (*ApplyResult, error) {
	normalized, err := normalizeRequest(req)
	if err != nil {
		return nil, err
	}

	lockConn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open sample migration lock connection: %w", err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, sampleMigrationLockID); err != nil {
		return nil, fmt.Errorf("lock sample data migration: %w", err)
	}
	defer lockConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, sampleMigrationLockID)

	if err := requireSampleSchema(ctx, db); err != nil {
		return nil, err
	}
	if err := requireSampleOnlyTarget(ctx, db, normalized); err != nil {
		return nil, err
	}

	adminID, err := ensureSampleUser(ctx, db, normalized.AdminEmail, normalized.AdminName, normalized.AdminPassword, access.PrincipalScopePlatform, "")
	if err != nil {
		return nil, fmt.Errorf("provision sample Platform Admin identity: %w", err)
	}
	opsID, err := ensureSampleUser(ctx, db, normalized.OpsEmail, normalized.OpsName, normalized.OpsPassword, access.PrincipalScopePlatform, "")
	if err != nil {
		return nil, fmt.Errorf("provision sample Platform Ops identity: %w", err)
	}
	if _, err := ensureSampleUser(ctx, db, LotusOwnerEmail, "Linh Nguyen", normalized.TenantOwnerPassword, access.PrincipalScopeTenant, "salon_owner"); err != nil {
		return nil, fmt.Errorf("provision sample Tenant Owner identity: %w", err)
	}

	accessRepository := access.NewRepository(db)
	if err := ensureAdminRole(ctx, db, accessRepository, normalized.AdminEmail, adminID); err != nil {
		return nil, err
	}
	accessService := access.NewService(accessRepository)
	if err := ensureOpsRole(ctx, db, accessService, adminID, opsID); err != nil {
		return nil, err
	}

	replayed, err := migrateFixtures(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := ensureOpsSalonAssignment(ctx, db, accessService, adminID, opsID); err != nil {
		return nil, err
	}

	return loadResult(ctx, db, normalized, replayed)
}

func normalizeRequest(req ApplyRequest) (ApplyRequest, error) {
	req.Profile = strings.TrimSpace(req.Profile)
	req.Confirmation = strings.TrimSpace(req.Confirmation)
	req.AdminEmail = strings.ToLower(strings.TrimSpace(req.AdminEmail))
	req.AdminName = strings.TrimSpace(req.AdminName)
	req.OpsEmail = strings.ToLower(strings.TrimSpace(req.OpsEmail))
	req.OpsName = strings.TrimSpace(req.OpsName)
	if req.Profile != ProfileSampleTest || req.Confirmation != ConfirmationToken {
		return ApplyRequest{}, fmt.Errorf("profile must be %q and confirmation must be %q", ProfileSampleTest, ConfirmationToken)
	}
	if !validExactEmail(req.AdminEmail) || !validExactEmail(req.OpsEmail) || req.AdminEmail == req.OpsEmail || req.AdminEmail == LotusOwnerEmail || req.OpsEmail == LotusOwnerEmail {
		return ApplyRequest{}, errors.New("admin, ops, and sample owner emails must be valid and distinct")
	}
	if req.AdminName == "" || req.OpsName == "" {
		return ApplyRequest{}, errors.New("admin and ops names are required")
	}
	for label, password := range map[string]string{
		"admin":        req.AdminPassword,
		"ops":          req.OpsPassword,
		"tenant owner": req.TenantOwnerPassword,
	} {
		if len(password) < 12 || strings.TrimSpace(password) == "" {
			return ApplyRequest{}, fmt.Errorf("%s password must contain at least 12 characters", label)
		}
	}
	return req, nil
}

func validExactEmail(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value
}

func requireSampleSchema(ctx context.Context, db *sql.DB) error {
	var applied int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM app_schema_migrations WHERE version IN ('73', '74')
	`).Scan(&applied)
	if err != nil {
		return fmt.Errorf("check normal migrations V73-V74: %w", err)
	}
	if applied != 2 {
		return errors.New("normal migrations V73 and V74 must be applied before sample data")
	}
	return nil
}

func requireSampleOnlyTarget(ctx context.Context, db *sql.DB, req ApplyRequest) error {
	var liveUsers, liveSalons, unexpectedSampleUsers, unexpectedSampleSalons int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE data_classification <> 'sample_test'`).Scan(&liveUsers); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM salons WHERE data_classification <> 'sample_test'`).Scan(&liveSalons); err != nil {
		return err
	}
	if liveUsers > 0 || liveSalons > 0 {
		return fmt.Errorf("%w: found %d live users and %d live salons; reset the pre-live database before applying fixtures", ErrUnsafeTarget, liveUsers, liveSalons)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM users
		WHERE data_classification='sample_test'
		  AND lower(email) NOT IN (lower($1),lower($2),lower($3))
	`, req.AdminEmail, req.OpsEmail, LotusOwnerEmail).Scan(&unexpectedSampleUsers); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM salons
		WHERE data_classification='sample_test' AND id::text <> $1
	`, LotusSalonID).Scan(&unexpectedSampleSalons); err != nil {
		return err
	}
	if unexpectedSampleUsers > 0 || unexpectedSampleSalons > 0 {
		return fmt.Errorf("%w: found %d incompatible sample users and %d incompatible sample salons", ErrUnsafeTarget, unexpectedSampleUsers, unexpectedSampleSalons)
	}
	return nil
}

func ensureSampleUser(ctx context.Context, db *sql.DB, email, fullName, password string, principalScope access.PrincipalScope, legacyRole string) (string, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var userID, classification, status, existingScope, existingName, passwordHash string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, data_classification, status, principal_scope, full_name, password_hash
		FROM users
		WHERE lower(email)=lower($1)
		FOR UPDATE
	`, email).Scan(&userID, &classification, &status, &existingScope, &existingName, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return "", hashErr
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO users (email,password_hash,full_name,status,principal_scope,data_classification)
			VALUES ($1,$2,$3,'active',$4,'sample_test')
			RETURNING id::text
		`, email, string(hash), fullName, string(principalScope)).Scan(&userID); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if classification != ProfileSampleTest || status != "active" || existingScope != string(principalScope) {
		return "", fmt.Errorf("existing user %s is not an active sample_test identity", email)
	} else if existingName != fullName || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return "", fmt.Errorf("existing sample identity %s does not match the requested fixture", email)
	}

	if legacyRole != "" {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO user_roles (user_id,role_id)
			SELECT $1,id FROM roles WHERE name=$2 AND scope='legacy'
			ON CONFLICT DO NOTHING
		`, userID, legacyRole)
		if err != nil {
			return "", err
		}
		if affected, err := result.RowsAffected(); err != nil {
			return "", err
		} else if affected == 0 {
			var assigned bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM user_roles assignment
					JOIN roles role ON role.id=assignment.role_id
					WHERE assignment.user_id=$1 AND role.name=$2 AND role.scope='legacy'
				)
			`, userID, legacyRole).Scan(&assigned); err != nil {
				return "", err
			}
			if !assigned {
				return "", fmt.Errorf("legacy role %s is unavailable", legacyRole)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

func ensureAdminRole(ctx context.Context, db *sql.DB, repository *access.Repository, email, userID string) error {
	role, status, exists, err := currentPlatformRole(ctx, db, userID)
	if err != nil {
		return err
	}
	if exists {
		if role == access.RolePlatformAdmin && status == "active" {
			return nil
		}
		return fmt.Errorf("sample admin has incompatible Platform role %s:%s", role, status)
	}
	if _, err := repository.BootstrapPlatformAdmin(ctx, access.BootstrapPlatformAdminRequest{Email: email, ActionKey: "sample-data:platform-admin:v1", Reason: "sample-data-v1"}); err != nil {
		return fmt.Errorf("bootstrap sample Platform Admin: %w", err)
	}
	return nil
}

func ensureOpsRole(ctx context.Context, db *sql.DB, service *access.Service, adminID, opsID string) error {
	role, status, exists, err := currentPlatformRole(ctx, db, opsID)
	if err != nil {
		return err
	}
	if exists {
		if role == access.RolePlatformOps && status == "active" {
			return nil
		}
		return fmt.Errorf("sample ops has incompatible Platform role %s:%s", role, status)
	}
	_, _, err = service.MutatePlatformRole(ctx, middleware.ActorContext{UserID: adminID}, opsID, access.PlatformRoleMutationRequest{
		ActionKey:       "sample-data:platform-ops:v1",
		Role:            access.RolePlatformOps,
		Status:          "active",
		ExpectedVersion: 0,
	})
	if err != nil {
		return fmt.Errorf("assign sample Platform Ops role: %w", err)
	}
	return nil
}

func currentPlatformRole(ctx context.Context, db *sql.DB, userID string) (string, string, bool, error) {
	var role, status string
	err := db.QueryRowContext(ctx, `
		SELECT role.name,assignment.status
		FROM platform_role_assignments assignment
		JOIN roles role ON role.id=assignment.role_id
		WHERE assignment.user_id=$1
	`, userID).Scan(&role, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return role, status, err == nil, err
}

func ensureOpsSalonAssignment(ctx context.Context, db *sql.DB, service *access.Service, adminID, opsID string) error {
	want := []string{
		string(access.CapabilityAuditRead),
		string(access.CapabilityBusinessRead),
		string(access.CapabilityBusinessWrite),
		string(access.CapabilityOperationsRead),
		string(access.CapabilityOperationsWrite),
		string(access.CapabilityTechnicalRead),
		string(access.CapabilityTechnicalWrite),
	}
	var status string
	var version int64
	var permissions []string
	err := db.QueryRowContext(ctx, `
		SELECT assignment.status,assignment.version,
		       ARRAY(
		         SELECT permission.name
		         FROM platform_salon_assignment_permissions assigned
		         JOIN permissions permission ON permission.id=assigned.permission_id
		         WHERE assigned.assignment_id=assignment.id
		         ORDER BY permission.name
		       )
		FROM platform_salon_assignments assignment
		WHERE assignment.salon_id=$1 AND assignment.user_id=$2
	`, LotusSalonID, opsID).Scan(&status, &version, pq.Array(&permissions))
	if err == nil {
		if status == "active" && equalStrings(permissions, want) {
			return nil
		}
		return fmt.Errorf("sample Ops salon assignment is incompatible: status=%s version=%d permissions=%v", status, version, permissions)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, _, err = service.MutateSalonAssignment(ctx, middleware.ActorContext{UserID: adminID}, LotusSalonID, opsID, access.SalonAssignmentMutationRequest{
		ActionKey:       "sample-data:ops-lotus-assignment:v1",
		Status:          "active",
		Permissions:     want,
		ExpectedVersion: 0,
	})
	if err != nil {
		return fmt.Errorf("assign sample Ops to Lotus salon: %w", err)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func migrateFixtures(ctx context.Context, db *sql.DB) (bool, error) {
	migrations, err := loadFixtureMigrations()
	if err != nil {
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS sample_data_migrations (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			profile TEXT NOT NULL CHECK (profile='sample_test'),
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return false, fmt.Errorf("ensure sample migration ledger: %w", err)
	}
	replayed := true
	for _, migration := range migrations {
		var checksum string
		err := tx.QueryRowContext(ctx, `SELECT checksum FROM sample_data_migrations WHERE version=$1`, migration.Version).Scan(&checksum)
		if err == nil {
			if checksum != migration.Checksum {
				return false, fmt.Errorf("sample migration %s checksum changed after application", migration.Path)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		replayed = false
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return false, fmt.Errorf("apply sample migration %s: %w", migration.Path, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sample_data_migrations(version,name,checksum,profile)
			VALUES($1,$2,$3,'sample_test')
		`, migration.Version, migration.Name, migration.Checksum); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return replayed, nil
}

func loadFixtureMigrations() ([]fixtureMigration, error) {
	paths, err := fs.Glob(Files, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	items := make([]fixtureMigration, 0, len(paths))
	for _, filePath := range paths {
		raw, err := Files.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		base := strings.TrimSuffix(path.Base(filePath), ".sql")
		parts := strings.SplitN(base, "__", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "V") {
			return nil, fmt.Errorf("invalid sample migration filename %s", filePath)
		}
		order, err := strconv.Atoi(strings.TrimPrefix(parts[0], "V"))
		if err != nil {
			return nil, fmt.Errorf("invalid sample migration version %s", filePath)
		}
		sum := sha256.Sum256(raw)
		items = append(items, fixtureMigration{
			Version:  strings.TrimPrefix(parts[0], "V"),
			Order:    order,
			Name:     strings.ReplaceAll(parts[1], "_", " "),
			Path:     filePath,
			SQL:      string(raw),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	return items, nil
}

func loadResult(ctx context.Context, db *sql.DB, req ApplyRequest, replayed bool) (*ApplyResult, error) {
	result := &ApplyResult{
		Profile:                 ProfileSampleTest,
		AdminEmail:              req.AdminEmail,
		OpsEmail:                req.OpsEmail,
		TenantOwnerEmail:        LotusOwnerEmail,
		SalonID:                 LotusSalonID,
		SalonName:               LotusSalonName,
		SampleMigrationReplayed: replayed,
	}
	err := db.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM users WHERE data_classification='sample_test'),
		  (SELECT count(*) FROM salons WHERE data_classification='sample_test'),
		  (SELECT count(*) FROM service_categories WHERE salon_id=$1),
		  (SELECT count(*) FROM service_category_aliases WHERE salon_id=$1),
		  (SELECT count(*) FROM services WHERE salon_id=$1),
		  (SELECT count(*) FROM service_aliases WHERE salon_id=$1),
		  (SELECT count(*) FROM service_consultation_profiles WHERE salon_id=$1),
		  (SELECT count(*) FROM staff WHERE salon_id=$1),
		  (SELECT count(*) FROM manleai_calendar_service_staff WHERE salon_id=$1),
		  (SELECT count(*) FROM salon_business_hour_periods WHERE salon_id=$1 AND source='local_override'),
		  (SELECT count(*) FROM platform_pii_access_grants WHERE salon_id=$1),
		  (SELECT count(*) FROM salon_integration_configs WHERE salon_id=$1)
		    + (SELECT count(*) FROM pos_connections WHERE salon_id=$1),
		  (SELECT scheduling_authority FROM salon_settings WHERE salon_id=$1),
		  (SELECT booking_mode FROM salon_settings WHERE salon_id=$1)
	`, LotusSalonID).Scan(
		&result.UserCount,
		&result.SalonCount,
		&result.CategoryCount,
		&result.CategoryAliasCount,
		&result.ServiceCount,
		&result.ServiceAliasCount,
		&result.ConsultationProfileCount,
		&result.StaffCount,
		&result.StaffServiceLinkCount,
		&result.LocalHoursCount,
		&result.PIIGrantCount,
		&result.ProviderConfigCount,
		&result.SchedulingAuthority,
		&result.BookingMode,
	)
	if err != nil {
		return nil, err
	}
	if result.UserCount != 3 || result.SalonCount != 1 || result.CategoryCount != 5 || result.CategoryAliasCount != 20 || result.ServiceCount != 7 || result.ServiceAliasCount != 7 || result.ConsultationProfileCount != 7 || result.StaffCount != 4 || result.StaffServiceLinkCount != 28 || result.LocalHoursCount != 6 || result.PIIGrantCount != 0 || result.ProviderConfigCount != 0 || result.SchedulingAuthority != "owner_manual" || result.BookingMode != "pending_approval" {
		return nil, fmt.Errorf("sample data invariant failed: %#v", result)
	}
	var unsafeExecutionFlags int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id=salon.id
		WHERE salon.id=$1
		  AND (salon.ai_enabled OR salon.public_catalog_enabled
		       OR settings.recording_enabled OR settings.sms_confirmation_enabled
		       OR settings.sms_reminder_enabled OR settings.handoff_enabled
		       OR settings.customer_sms_enabled)
	`, LotusSalonID).Scan(&unsafeExecutionFlags); err != nil {
		return nil, err
	}
	if unsafeExecutionFlags != 0 {
		return nil, errors.New("sample data invariant failed: an execution or publication flag is enabled")
	}
	return result, nil
}
