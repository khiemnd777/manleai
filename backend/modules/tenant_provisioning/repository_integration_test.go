package tenant_provisioning

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	"github.com/manleai/ai-receptionist/modules/salon"
	registration "github.com/manleai/ai-receptionist/modules/tenant_registration"
)

type allowRegistrationProvisioning struct{}

func (allowRegistrationProvisioning) Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error {
	return nil
}

func TestRepositoryAtomicProvisioningInvitationAndProviderIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	adminDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	runtimeDB, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open context database: %v", err)
	}
	t.Cleanup(func() { _ = runtimeDB.Close() })

	actorID, actorEmail := insertProvisioningPlatformActor(t, adminDB)
	actor := middleware.ActorContext{UserID: actorID, PrincipalScope: middleware.PrincipalScopePlatform}
	platformCtx := databasecontext.WithActor(context.Background(), actorID)
	publicCtx := databasecontext.WithScope(context.Background(), databasecontext.ScopePublic)
	registrationRepository := registration.NewRepository(runtimeDB)
	registrationService := registration.NewService(registrationRepository, allowRegistrationProvisioning{})
	service := NewService(NewRepository(runtimeDB, salon.NewService(salon.NewRepository(runtimeDB))), allowRegistrationProvisioning{})

	requestID, version := createQualifiedRegistration(t, registrationService, registrationRepository, publicCtx, platformCtx, actor)
	input := validProvisionRequest()
	input.ActionKey = "provision-concurrent-" + uuid.NewString()
	input.ExpectedVersion = version
	input.Owner.Email = "invited-owner-" + uuid.NewString() + "@example.test"
	input.Owner.FullName = "Invited Integration Owner"
	input.Salon.Name = "Atomic Provisioning " + uuid.NewString()

	results := make([]*ProvisionResult, 2)
	errorsByCall := make([]error, 2)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for index := range results {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			results[i], errorsByCall[i] = service.Provision(platformCtx, actor, requestID, input)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, callErr := range errorsByCall {
		if callErr != nil {
			t.Fatalf("concurrent provision %d error=%v", index, callErr)
		}
	}
	if results[0].SalonID == "" || results[0].SalonID != results[1].SalonID || results[0].OwnerUserID != results[1].OwnerUserID || results[0].Replayed == results[1].Replayed {
		t.Fatalf("concurrent results=%#v", results)
	}
	created := results[0]
	if created.Replayed {
		created = results[1]
	}
	if created.SchedulingAuthority != "owner_manual" {
		t.Fatalf("initial scheduling authority=%q", created.SchedulingAuthority)
	}
	changed := input
	changed.Salon.Name += " changed"
	if _, err := service.Provision(platformCtx, actor, requestID, changed); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed provision action error=%v", err)
	}

	var authority, bookingMode, activeProvider, ownerScope, ownerStatus string
	var aiEnabled bool
	if err := adminDB.QueryRow(`SELECT settings.scheduling_authority,settings.booking_mode,salon.ai_enabled,COALESCE(salon.active_pos_provider,''),owner.principal_scope,owner.status FROM salons salon JOIN salon_settings settings ON settings.salon_id=salon.id JOIN users owner ON owner.id=salon.owner_user_id WHERE salon.id=$1`, created.SalonID).Scan(&authority, &bookingMode, &aiEnabled, &activeProvider, &ownerScope, &ownerStatus); err != nil {
		t.Fatalf("read provisioned salon: %v", err)
	}
	if authority != "owner_manual" || bookingMode != "pending_approval" || aiEnabled || activeProvider != "" || ownerScope != "tenant" || ownerStatus != "invited" {
		t.Fatalf("unsafe provisioned defaults authority=%q booking=%q ai=%t provider=%q scope=%q status=%q", authority, bookingMode, aiEnabled, activeProvider, ownerScope, ownerStatus)
	}
	var integrationConfigs, createdSalons, ownerMemberships int
	if err := adminDB.QueryRow(`SELECT count(*) FROM salon_integration_configs WHERE salon_id=$1`, created.SalonID).Scan(&integrationConfigs); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM salons WHERE creation_operation_key=$1`, "registration-provision:"+requestID).Scan(&createdSalons); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM salon_memberships membership JOIN roles role ON role.id=membership.role_id WHERE membership.salon_id=$1 AND membership.user_id=$2 AND membership.is_owner AND membership.status='active' AND role.name='tenant_owner'`, created.SalonID, created.OwnerUserID).Scan(&ownerMemberships); err != nil {
		t.Fatal(err)
	}
	if integrationConfigs != 0 || createdSalons != 1 || ownerMemberships != 1 {
		t.Fatalf("provision side effects configs=%d salons=%d owner memberships=%d", integrationConfigs, createdSalons, ownerMemberships)
	}

	failedRequestID, failedVersion := createQualifiedRegistration(t, registrationService, registrationRepository, publicCtx, platformCtx, actor)
	failed := validProvisionRequest()
	failed.ActionKey = "platform-owner-rejected-" + uuid.NewString()
	failed.ExpectedVersion = failedVersion
	failed.Owner = OwnerIdentityInput{Mode: OwnerModeUseExisting, UserID: actorID, Email: actorEmail, FullName: "Platform Actor"}
	failed.Salon.Name = "Must Roll Back " + uuid.NewString()
	if _, err := service.Provision(platformCtx, actor, failedRequestID, failed); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("platform identity provision error=%v", err)
	}
	var failedStatus string
	var failedCurrentVersion int64
	var failedSalons int
	if err := adminDB.QueryRow(`SELECT status,version FROM tenant_registration_requests WHERE id=$1`, failedRequestID).Scan(&failedStatus, &failedCurrentVersion); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM salons WHERE creation_operation_key=$1`, "registration-provision:"+failedRequestID).Scan(&failedSalons); err != nil {
		t.Fatal(err)
	}
	if failedStatus != "qualified" || failedCurrentVersion != failedVersion || failedSalons != 0 {
		t.Fatalf("failed provision did not roll back status=%q version=%d salons=%d", failedStatus, failedCurrentVersion, failedSalons)
	}

	invitationInput := InvitationRequest{ActionKey: "invite-" + uuid.NewString(), ExpectedVersion: created.RequestVersion}
	invitation, err := service.CreateInvitation(platformCtx, actor, requestID, invitationInput)
	if err != nil || !invitation.TokenAvailable || invitation.RawToken == "" {
		t.Fatalf("invitation=%#v error=%v", invitation, err)
	}
	invitationReplay, err := service.CreateInvitation(platformCtx, actor, requestID, invitationInput)
	if err != nil || !invitationReplay.Replayed || invitationReplay.TokenAvailable || invitationReplay.RawToken != "" || invitationReplay.InvitationID != invitation.InvitationID {
		t.Fatalf("invitation replay=%#v error=%v", invitationReplay, err)
	}
	var persistedHash, firstStatus string
	if err := adminDB.QueryRow(`SELECT token_hash,status FROM tenant_owner_invitations WHERE id=$1`, invitation.InvitationID).Scan(&persistedHash, &firstStatus); err != nil {
		t.Fatal(err)
	}
	firstSum := sha256.Sum256([]byte(invitation.RawToken))
	if persistedHash != hex.EncodeToString(firstSum[:]) || persistedHash == invitation.RawToken || firstStatus != "active" {
		t.Fatalf("persisted invitation hash/status=%q/%q", persistedHash, firstStatus)
	}

	rotated, err := service.CreateInvitation(platformCtx, actor, requestID, InvitationRequest{ActionKey: "rotate-expired-" + uuid.NewString(), ExpectedVersion: invitation.RequestVersion, Rotate: true})
	if err != nil || !rotated.TokenAvailable || rotated.RawToken == invitation.RawToken {
		t.Fatalf("rotated invitation=%#v error=%v", rotated, err)
	}
	if err := adminDB.QueryRow(`SELECT status FROM tenant_owner_invitations WHERE id=$1`, invitation.InvitationID).Scan(&firstStatus); err != nil || firstStatus != "revoked" {
		t.Fatalf("prior invitation status=%q error=%v", firstStatus, err)
	}
	if _, err := adminDB.Exec(`UPDATE tenant_owner_invitations SET expires_at=now()-interval '1 second' WHERE id=$1`, rotated.InvitationID); err != nil {
		t.Fatalf("expire invitation fixture: %v", err)
	}
	if _, err := service.AcceptInvitation(publicCtx, AcceptInvitationRequest{Token: rotated.RawToken, Password: "integration password 123"}); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("expired invitation error=%v", err)
	}
	fresh, err := service.CreateInvitation(platformCtx, actor, requestID, InvitationRequest{ActionKey: "rotate-fresh-" + uuid.NewString(), ExpectedVersion: rotated.RequestVersion, Rotate: true})
	if err != nil || !fresh.TokenAvailable {
		t.Fatalf("fresh invitation=%#v error=%v", fresh, err)
	}
	if _, err := adminDB.Exec(`INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, created.OwnerUserID, "refresh-"+uuid.NewString()); err != nil {
		t.Fatalf("insert refresh token: %v", err)
	}
	if result, err := service.AcceptInvitation(publicCtx, AcceptInvitationRequest{Token: fresh.RawToken, Password: "integration password 123"}); err != nil || result.Status != "accepted" {
		t.Fatalf("accept invitation=%#v error=%v", result, err)
	}
	if _, err := service.AcceptInvitation(publicCtx, AcceptInvitationRequest{Token: fresh.RawToken, Password: "integration password 123"}); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("reused invitation error=%v", err)
	}
	var activatedStatus, passwordHash string
	var liveRefreshTokens int
	if err := adminDB.QueryRow(`SELECT status,password_hash FROM users WHERE id=$1`, created.OwnerUserID).Scan(&activatedStatus, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.QueryRow(`SELECT count(*) FROM refresh_tokens WHERE user_id=$1 AND revoked_at IS NULL`, created.OwnerUserID).Scan(&liveRefreshTokens); err != nil {
		t.Fatal(err)
	}
	if activatedStatus != "active" || passwordHash == "!manleai-invited" || liveRefreshTokens != 0 {
		t.Fatalf("activation status=%q password=%q live refresh=%d", activatedStatus, passwordHash, liveRefreshTokens)
	}

	existingUserID, existingEmail := insertActiveTenantIdentity(t, adminDB)
	existingRequestID, existingVersion := createQualifiedRegistration(t, registrationService, registrationRepository, publicCtx, platformCtx, actor)
	existing := validProvisionRequest()
	existing.ActionKey = "existing-owner-" + uuid.NewString()
	existing.ExpectedVersion = existingVersion
	existing.Owner = OwnerIdentityInput{Mode: OwnerModeUseExisting, UserID: existingUserID, Email: existingEmail, FullName: "Existing Owner"}
	existing.Salon.Name = "Existing Owner Salon " + uuid.NewString()
	existingResult, err := service.Provision(platformCtx, actor, existingRequestID, existing)
	if err != nil || existingResult.OwnerUserID != existingUserID {
		t.Fatalf("existing tenant identity provision=%#v error=%v", existingResult, err)
	}
	if _, err := service.CreateInvitation(platformCtx, actor, existingRequestID, InvitationRequest{ActionKey: "active-owner-invite-" + uuid.NewString(), ExpectedVersion: existingResult.RequestVersion}); !errors.Is(err, ErrInvitationUnavailable) {
		t.Fatalf("active existing owner invitation error=%v", err)
	}
}

func createQualifiedRegistration(t *testing.T, service *registration.Service, repository *registration.Repository, publicCtx, platformCtx context.Context, actor middleware.ActorContext) (string, int64) {
	t.Helper()
	request := registration.PublicSubmissionRequest{SubmissionKey: uuid.NewString(), ContactFullName: "Registration Applicant", ContactEmail: "applicant-" + uuid.NewString() + "@example.test", ContactPhone: "312-555-0148", SalonName: "Applicant Salon " + uuid.NewString(), SalonPhone: "773-555-0180", City: "Chicago", State: "IL", ZipCode: "60614", LocationCount: 1, PreferredContactLanguage: "en", Locale: "en", SourcePage: "home", ConsentVersion: registration.ConsentVersion, ContactConsent: true}
	created, err := service.Submit(publicCtx, request)
	if err != nil {
		t.Fatalf("submit registration: %v", err)
	}
	items, _, _, err := repository.List(platformCtx, registration.ListFilter{Query: created.RequestReference, Limit: 2})
	if err != nil || len(items) != 1 {
		t.Fatalf("find registration items=%#v error=%v", items, err)
	}
	qualified := registration.StatusQualified
	result, err := service.Mutate(platformCtx, actor, items[0].ID, registration.MutationRequest{ActionKey: "qualify-" + uuid.NewString(), ExpectedVersion: 1, Status: &qualified})
	if err != nil {
		t.Fatalf("qualify registration: %v", err)
	}
	return items[0].ID, result.Version
}

func insertProvisioningPlatformActor(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	email := "provisioning-platform-" + uuid.NewString() + "@example.test"
	var id string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,status,principal_scope) VALUES($1,'integration-test','Provisioning Platform Actor','active','platform') RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("insert platform actor: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO platform_role_assignments(user_id,role_id,created_by_user_id,updated_by_user_id) SELECT $1,id,$1,$1 FROM roles WHERE name='platform_admin'`, id); err != nil {
		t.Fatalf("assign platform admin: %v", err)
	}
	return id, email
}

func insertActiveTenantIdentity(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	email := "existing-tenant-" + uuid.NewString() + "@example.test"
	var id string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,status,principal_scope) VALUES($1,'integration-test','Existing Tenant Owner','active','tenant') RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("insert tenant identity: %v", err)
	}
	return id, email
}
