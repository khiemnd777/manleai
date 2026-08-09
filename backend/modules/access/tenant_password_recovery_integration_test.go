package access

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/database"
	"golang.org/x/crypto/bcrypt"
)

func TestRotateTenantOwnerPasswordPreservesIdentityAndFailsClosed(t *testing.T) {
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
	email := "tenant-recovery-" + suffix + "@example.test"
	ownerID := insertAccessTestUser(t, db, email, PrincipalScopeTenant)
	salonID := insertAccessTestSalon(t, db, ownerID, "Tenant recovery "+suffix)
	otherOwnerID := insertAccessTestUser(t, db, "tenant-recovery-other-"+suffix+"@example.test", PrincipalScopeTenant)
	otherSalonID := insertAccessTestSalon(t, db, otherOwnerID, "Other tenant recovery "+suffix)
	platformID := insertAccessTestUser(t, db, "tenant-recovery-platform-"+suffix+"@example.test", PrincipalScopePlatform)

	oldPassword := "old-tenant-password-" + suffix
	newPassword := "new-tenant-password-" + suffix
	oldHash, err := bcrypt.GenerateFromPassword([]byte(oldPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash new password: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, string(oldHash), ownerID); err != nil {
		t.Fatalf("seed old password: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, ownerID, "tenant-recovery-token-"+suffix, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}

	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(newPassword)))
	request := RotateTenantOwnerPasswordRequest{
		SalonID:             salonID,
		Email:               email,
		PasswordHash:        string(newHash),
		PasswordFingerprint: fingerprint,
		ActionKey:           "tenant-owner-recovery-" + suffix,
		Reason:              "approved-tenant-recovery-" + suffix,
	}
	repository := NewRepository(db)
	result, err := repository.RotateTenantOwnerPassword(ctx, request)
	if err != nil {
		t.Fatalf("rotate Tenant owner password: %v", err)
	}
	if result.Replayed || result.UserID != ownerID || result.SalonID != salonID || result.PrincipalScope != PrincipalScopeTenant || result.Status != "active" || result.OwnedSalonCount != 1 || result.RevokedRefreshTokens != 1 {
		t.Fatalf("unexpected recovery result: %#v", result)
	}

	var persistedHash, persistedEmail, persistedScope, persistedStatus, persistedOwnerID string
	if err := db.QueryRowContext(ctx, `
		SELECT account.password_hash,account.email,account.principal_scope,account.status,salon.owner_user_id::text
		FROM users AS account
		JOIN salons AS salon ON salon.id=$1
		WHERE account.id=$2
	`, salonID, ownerID).Scan(&persistedHash, &persistedEmail, &persistedScope, &persistedStatus, &persistedOwnerID); err != nil {
		t.Fatalf("load recovered identity: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(persistedHash), []byte(newPassword)) != nil {
		t.Fatal("replacement password was not persisted")
	}
	if bcrypt.CompareHashAndPassword([]byte(persistedHash), []byte(oldPassword)) == nil {
		t.Fatal("old password still matches after recovery")
	}
	if persistedEmail != email || persistedScope != string(PrincipalScopeTenant) || persistedStatus != "active" || persistedOwnerID != ownerID {
		t.Fatalf("identity drifted: email=%s scope=%s status=%s owner=%s", persistedEmail, persistedScope, persistedStatus, persistedOwnerID)
	}
	var refreshCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM refresh_tokens WHERE user_id=$1`, ownerID).Scan(&refreshCount); err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	if refreshCount != 0 {
		t.Fatalf("refresh token count=%d, want 0", refreshCount)
	}

	replayed, err := repository.RotateTenantOwnerPassword(ctx, request)
	if err != nil {
		t.Fatalf("replay Tenant owner recovery: %v", err)
	}
	if !replayed.Replayed || replayed.UserID != result.UserID || replayed.SalonID != result.SalonID || replayed.RevokedRefreshTokens != result.RevokedRefreshTokens {
		t.Fatalf("unexpected replay result: %#v", replayed)
	}

	changedPassword := "changed-tenant-password-" + suffix
	changedHash, err := bcrypt.GenerateFromPassword([]byte(changedPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash changed password: %v", err)
	}
	changed := request
	changed.PasswordHash = string(changedHash)
	changed.PasswordFingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(changedPassword)))
	if _, err := repository.RotateTenantOwnerPassword(ctx, changed); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed action replay error=%v, want action conflict", err)
	}

	wrongSalon := request
	wrongSalon.ActionKey = "tenant-owner-recovery-wrong-salon-" + suffix
	wrongSalon.SalonID = otherSalonID
	if _, err := repository.RotateTenantOwnerPassword(ctx, wrongSalon); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-salon recovery error=%v, want not found", err)
	}
	platformRequest := request
	platformRequest.ActionKey = "tenant-owner-recovery-platform-" + suffix
	platformRequest.Email = "tenant-recovery-platform-" + suffix + "@example.test"
	platformRequest.SalonID = salonID
	if _, err := repository.RotateTenantOwnerPassword(ctx, platformRequest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Platform identity recovery error=%v, want not found", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, otherOwnerID); err != nil {
		t.Fatalf("disable other owner: %v", err)
	}
	disabledRequest := request
	disabledRequest.ActionKey = "tenant-owner-recovery-disabled-" + suffix
	disabledRequest.Email = "tenant-recovery-other-" + suffix + "@example.test"
	disabledRequest.SalonID = otherSalonID
	if _, err := repository.RotateTenantOwnerPassword(ctx, disabledRequest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled Tenant recovery error=%v, want not found", err)
	}

	var actionType, responsePayload, eventType, details string
	if err := db.QueryRowContext(ctx, `
		SELECT action.action_type,action.response_payload::text,event.event_type,event.details::text
		FROM access_control_actions AS action
		JOIN access_control_events AS event ON event.action_id=action.id
		WHERE action.actor_user_id=$1 AND action.action_key=$2
	`, ownerID, request.ActionKey).Scan(&actionType, &responsePayload, &eventType, &details); err != nil {
		t.Fatalf("load recovery audit: %v", err)
	}
	if actionType != rotateTenantOwnerPasswordActionType || eventType != "tenant.identity.recovery_password_rotated" {
		t.Fatalf("unexpected audit type: action=%s event=%s", actionType, eventType)
	}
	for _, secret := range []string{email, oldPassword, newPassword, string(oldHash), string(newHash), fingerprint} {
		if strings.Contains(responsePayload, secret) || strings.Contains(details, secret) {
			t.Fatalf("audit payload exposed credential material")
		}
	}
	if !strings.Contains(details, `"password_changed": true`) && !strings.Contains(details, `"password_changed":true`) {
		t.Fatalf("audit details omitted password-change evidence: %s", details)
	}
	_ = platformID
}
