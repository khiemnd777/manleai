package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const renameTenantEmailActionType = "tenant.identity.rename_email"

type RenameTenantEmailRequest struct {
	CurrentEmail string
	NewEmail     string
	ActionKey    string
	Reason       string
}

type RenameTenantEmailResult struct {
	UserID               string         `json:"user_id"`
	PrincipalScope       PrincipalScope `json:"principal_scope"`
	Status               string         `json:"status"`
	OwnedSalonCount      int64          `json:"owned_salon_count"`
	RevokedRefreshTokens int64          `json:"revoked_refresh_tokens"`
	Replayed             bool           `json:"replayed"`
}

// RenameTenantEmail changes only the login email of one active Tenant owner.
// It preserves the immutable user ID and every salon-owned relationship,
// revokes existing refresh sessions, and records an idempotent access event.
func (r *Repository) RenameTenantEmail(ctx context.Context, req RenameTenantEmailRequest) (*RenameTenantEmailResult, error) {
	req.CurrentEmail = strings.ToLower(strings.TrimSpace(req.CurrentEmail))
	req.NewEmail = strings.ToLower(strings.TrimSpace(req.NewEmail))
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Reason = strings.TrimSpace(req.Reason)
	if !validPlatformEmail(req.CurrentEmail) || !validPlatformEmail(req.NewEmail) || req.CurrentEmail == req.NewEmail || !validActionKey(req.ActionKey) || !validChangeReference(req.Reason) {
		return nil, ErrValidation
	}

	fingerprint, err := requestFingerprint(struct {
		CurrentEmail string `json:"current_email"`
		NewEmail     string `json:"new_email"`
		Reason       string `json:"reason"`
	}{req.CurrentEmail, req.NewEmail, req.Reason})
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	userID, principalScope, status, err := tenantIdentityByEmailForUpdate(ctx, tx, req.CurrentEmail)
	if errors.Is(err, sql.ErrNoRows) {
		userID, principalScope, status, err = tenantIdentityByEmailForUpdate(ctx, tx, req.NewEmail)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
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
			return nil, ErrNotFound
		}
		replay, err := beginAccessMutation(ctx, tx, userID, req.ActionKey, renameTenantEmailActionType, fingerprint)
		if err != nil {
			return nil, err
		}
		if replay == nil {
			return nil, ErrNotFound
		}
		var result RenameTenantEmailResult
		if err := json.Unmarshal(replay, &result); err != nil {
			return nil, err
		}
		result.Replayed = true
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if err != nil {
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
		return nil, ErrNotFound
	}

	replay, err := beginAccessMutation(ctx, tx, userID, req.ActionKey, renameTenantEmailActionType, fingerprint)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		var result RenameTenantEmailResult
		if err := json.Unmarshal(replay, &result); err != nil {
			return nil, err
		}
		result.Replayed = true
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &result, nil
	}

	var newEmailOccupied bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, req.NewEmail).Scan(&newEmailOccupied); err != nil {
		return nil, err
	}
	if newEmailOccupied {
		return nil, ErrIdentityConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email=$1,updated_at=now() WHERE id=$2 AND principal_scope='tenant' AND status='active'`, req.NewEmail, userID); err != nil {
		return nil, classifyTenantEmailConstraint(err)
	}
	refreshResult, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	revokedRefreshTokens, err := refreshResult.RowsAffected()
	if err != nil {
		return nil, err
	}

	result := &RenameTenantEmailResult{
		UserID:               userID,
		PrincipalScope:       PrincipalScopeTenant,
		Status:               "active",
		OwnedSalonCount:      ownedSalonCount,
		RevokedRefreshTokens: revokedRefreshTokens,
	}
	details := map[string]any{
		"principal_scope":        PrincipalScopeTenant,
		"status":                 "active",
		"email_changed":          true,
		"owned_salon_count":      ownedSalonCount,
		"revoked_refresh_tokens": revokedRefreshTokens,
		"reason_supplied":        true,
	}
	if err := recordAccessMutation(ctx, tx, userID, "", userID, req.ActionKey, renameTenantEmailActionType, fingerprint, "tenant.identity.email_changed", "user_login_identity", userID, details, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func tenantIdentityByEmailForUpdate(ctx context.Context, tx *sql.Tx, email string) (string, PrincipalScope, string, error) {
	var userID string
	var principalScope PrincipalScope
	var status string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text,principal_scope,status
		FROM users
		WHERE lower(email)=lower($1)
		FOR UPDATE
	`, email).Scan(&userID, &principalScope, &status)
	return userID, principalScope, status, err
}

func tenantOwnedSalonCount(ctx context.Context, tx *sql.Tx, userID string) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM salons WHERE owner_user_id=$1`, userID).Scan(&count)
	return count, err
}

func classifyTenantEmailConstraint(err error) error {
	classified := classifyAccessConstraint(err)
	if errors.Is(classified, ErrVersionConflict) {
		return ErrIdentityConflict
	}
	return classified
}
