package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const rotateTenantOwnerPasswordActionType = "tenant.identity.recovery_password_rotate"

var errTenantOwnerRecoveryInvariant = errors.New("tenant owner recovery invariant failed")

type RotateTenantOwnerPasswordRequest struct {
	SalonID             string
	Email               string
	PasswordHash        string
	PasswordFingerprint string
	ActionKey           string
	Reason              string
}

type RotateTenantOwnerPasswordResult struct {
	UserID               string         `json:"user_id"`
	SalonID              string         `json:"salon_id"`
	PrincipalScope       PrincipalScope `json:"principal_scope"`
	Status               string         `json:"status"`
	OwnedSalonCount      int64          `json:"owned_salon_count"`
	RevokedRefreshTokens int64          `json:"revoked_refresh_tokens"`
	Replayed             bool           `json:"replayed"`
}

type rotateTenantOwnerPasswordAuditResult struct {
	UserID               string         `json:"user_id"`
	SalonID              string         `json:"salon_id"`
	PrincipalScope       PrincipalScope `json:"principal_scope"`
	Status               string         `json:"status"`
	OwnedSalonCount      int64          `json:"owned_salon_count"`
	RevokedRefreshTokens int64          `json:"revoked_refresh_tokens"`
}

// RotateTenantOwnerPassword is a protected operator recovery path for the
// exact active Tenant owner of one salon. It changes only the bcrypt password
// and refresh-session state while preserving identity, membership, ownership,
// roles, and salon data.
func (r *Repository) RotateTenantOwnerPassword(ctx context.Context, req RotateTenantOwnerPasswordRequest) (*RotateTenantOwnerPasswordResult, error) {
	req.SalonID = strings.TrimSpace(req.SalonID)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.PasswordHash = strings.TrimSpace(req.PasswordHash)
	req.PasswordFingerprint = strings.TrimSpace(req.PasswordFingerprint)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.SalonID == "" || !validPlatformEmail(req.Email) || req.PasswordHash == "" || !passwordFingerprintPattern.MatchString(req.PasswordFingerprint) || !validActionKey(req.ActionKey) || !validChangeReference(req.Reason) {
		return nil, ErrValidation
	}

	fingerprint, err := requestFingerprint(struct {
		SalonID             string `json:"salon_id"`
		Email               string `json:"email"`
		PasswordFingerprint string `json:"password_fingerprint"`
		Reason              string `json:"reason"`
	}{req.SalonID, req.Email, req.PasswordFingerprint, req.Reason})
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var userID string
	var principalScope PrincipalScope
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT account.id::text, account.principal_scope, account.status
		FROM users AS account
		JOIN salons AS salon
		  ON salon.id = $1::uuid
		 AND salon.owner_user_id = account.id
		WHERE lower(account.email) = lower($2)
		FOR UPDATE OF account, salon
	`, req.SalonID, req.Email).Scan(&userID, &principalScope, &status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if principalScope != PrincipalScopeTenant || status != "active" {
		return nil, ErrNotFound
	}

	ownedSalonCount, err := tenantOwnedSalonCount(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if ownedSalonCount < 1 {
		return nil, errTenantOwnerRecoveryInvariant
	}

	replay, err := beginAccessMutation(ctx, tx, userID, req.ActionKey, rotateTenantOwnerPasswordActionType, fingerprint)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		var recorded rotateTenantOwnerPasswordAuditResult
		if err := json.Unmarshal(replay, &recorded); err != nil {
			return nil, err
		}
		if recorded.UserID != userID || recorded.SalonID != req.SalonID || recorded.PrincipalScope != PrincipalScopeTenant || recorded.Status != "active" {
			return nil, errTenantOwnerRecoveryInvariant
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &RotateTenantOwnerPasswordResult{
			UserID:               recorded.UserID,
			SalonID:              recorded.SalonID,
			PrincipalScope:       recorded.PrincipalScope,
			Status:               recorded.Status,
			OwnedSalonCount:      recorded.OwnedSalonCount,
			RevokedRefreshTokens: recorded.RevokedRefreshTokens,
			Replayed:             true,
		}, nil
	}

	passwordUpdate, err := tx.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $1, updated_at = now()
		WHERE id = $2::uuid
		  AND status = 'active'
		  AND principal_scope = 'tenant'
	`, req.PasswordHash, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := passwordUpdate.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, err
		}
		return nil, errTenantOwnerRecoveryInvariant
	}

	refreshDelete, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	revokedRefreshTokens, err := refreshDelete.RowsAffected()
	if err != nil {
		return nil, err
	}

	recorded := rotateTenantOwnerPasswordAuditResult{
		UserID:               userID,
		SalonID:              req.SalonID,
		PrincipalScope:       PrincipalScopeTenant,
		Status:               "active",
		OwnedSalonCount:      ownedSalonCount,
		RevokedRefreshTokens: revokedRefreshTokens,
	}
	details := map[string]any{
		"principal_scope":        PrincipalScopeTenant,
		"status":                 "active",
		"password_changed":       true,
		"owned_salon_count":      ownedSalonCount,
		"revoked_refresh_tokens": revokedRefreshTokens,
		"reason_supplied":        true,
	}
	if err := recordAccessMutation(ctx, tx, userID, req.SalonID, userID, req.ActionKey, rotateTenantOwnerPasswordActionType, fingerprint, "tenant.identity.recovery_password_rotated", "user_login_identity", userID, details, recorded); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RotateTenantOwnerPasswordResult{
		UserID:               recorded.UserID,
		SalonID:              recorded.SalonID,
		PrincipalScope:       recorded.PrincipalScope,
		Status:               recorded.Status,
		OwnedSalonCount:      recorded.OwnedSalonCount,
		RevokedRefreshTokens: recorded.RevokedRefreshTokens,
	}, nil
}
