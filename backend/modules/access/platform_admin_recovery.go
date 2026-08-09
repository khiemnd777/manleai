package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
)

const rotateSinglePlatformAdminPasswordActionType = "platform.user.recovery_password_rotate"

var passwordFingerprintPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type RotateSinglePlatformAdminPasswordRequest struct {
	PasswordHash        string
	PasswordFingerprint string
	ActionKey           string
	Reason              string
}

type RotateSinglePlatformAdminPasswordResult struct {
	UserID               string `json:"user_id"`
	Email                string `json:"email"`
	AssignmentID         string `json:"assignment_id"`
	AssignmentVersion    int64  `json:"assignment_version"`
	RevokedRefreshTokens int64  `json:"revoked_refresh_tokens"`
	Replayed             bool   `json:"replayed"`
}

type rotatePlatformAdminPasswordAuditResult struct {
	UserID               string `json:"user_id"`
	AssignmentID         string `json:"assignment_id"`
	AssignmentVersion    int64  `json:"assignment_version"`
	RevokedRefreshTokens int64  `json:"revoked_refresh_tokens"`
}

// RotateSinglePlatformAdminPassword is a protected operator recovery path. It
// fails closed unless production has exactly one active Platform Admin and
// changes only that identity's password plus its session/version fences.
func (r *Repository) RotateSinglePlatformAdminPassword(ctx context.Context, req RotateSinglePlatformAdminPasswordRequest) (*RotateSinglePlatformAdminPasswordResult, error) {
	req.PasswordHash = strings.TrimSpace(req.PasswordHash)
	req.PasswordFingerprint = strings.TrimSpace(req.PasswordFingerprint)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.PasswordHash == "" || !passwordFingerprintPattern.MatchString(req.PasswordFingerprint) || !validActionKey(req.ActionKey) || !validChangeReference(req.Reason) {
		return nil, ErrValidation
	}

	fingerprint, err := requestFingerprint(struct {
		PasswordFingerprint string `json:"password_fingerprint"`
		Reason              string `json:"reason"`
	}{req.PasswordFingerprint, req.Reason})
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockPlatformRoleGovernanceTx(ctx, tx); err != nil {
		return nil, err
	}

	type recoveryTarget struct {
		UserID            string
		Email             string
		AssignmentID      string
		AssignmentVersion int64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT account.id::text, account.email, assignment.id::text, assignment.version
		FROM users AS account
		JOIN platform_role_assignments AS assignment
		  ON assignment.user_id = account.id
		 AND assignment.status = 'active'
		JOIN roles AS role
		  ON role.id = assignment.role_id
		 AND role.scope = 'platform'
		 AND role.name = 'platform_admin'
		WHERE account.status = 'active'
		  AND account.principal_scope = 'platform'
		ORDER BY account.id
		LIMIT 2
		FOR UPDATE OF account, assignment
	`)
	if err != nil {
		return nil, err
	}
	targets := make([]recoveryTarget, 0, 2)
	for rows.Next() {
		var target recoveryTarget
		if err := rows.Scan(&target.UserID, &target.Email, &target.AssignmentID, &target.AssignmentVersion); err != nil {
			rows.Close()
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(targets) != 1 {
		return nil, ErrRecoveryInvariant
	}
	target := targets[0]
	if err := lockPlatformPrincipalTx(ctx, tx, target.UserID); err != nil {
		return nil, err
	}

	replay, err := beginAccessMutation(ctx, tx, target.UserID, req.ActionKey, rotateSinglePlatformAdminPasswordActionType, fingerprint)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		var recorded rotatePlatformAdminPasswordAuditResult
		if err := json.Unmarshal(replay, &recorded); err != nil {
			return nil, err
		}
		if recorded.UserID != target.UserID || recorded.AssignmentID != target.AssignmentID {
			return nil, ErrRecoveryInvariant
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &RotateSinglePlatformAdminPasswordResult{
			UserID:               recorded.UserID,
			Email:                target.Email,
			AssignmentID:         recorded.AssignmentID,
			AssignmentVersion:    recorded.AssignmentVersion,
			RevokedRefreshTokens: recorded.RevokedRefreshTokens,
			Replayed:             true,
		}, nil
	}

	passwordUpdate, err := tx.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $1, updated_at = now()
		WHERE id = $2
		  AND status = 'active'
		  AND principal_scope = 'platform'
	`, req.PasswordHash, target.UserID)
	if err != nil {
		return nil, err
	}
	if affected, err := passwordUpdate.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, err
		}
		return nil, ErrRecoveryInvariant
	}

	assignmentUpdate, err := tx.ExecContext(ctx, `
		UPDATE platform_role_assignments
		SET version = version + 1,
		    updated_by_user_id = $1,
		    updated_at = now()
		WHERE id = $2
		  AND user_id = $1
		  AND status = 'active'
		  AND version = $3
	`, target.UserID, target.AssignmentID, target.AssignmentVersion)
	if err != nil {
		return nil, err
	}
	if affected, err := assignmentUpdate.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, err
		}
		return nil, ErrRecoveryInvariant
	}

	refreshDelete, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, target.UserID)
	if err != nil {
		return nil, err
	}
	revokedRefreshTokens, err := refreshDelete.RowsAffected()
	if err != nil {
		return nil, err
	}

	recorded := rotatePlatformAdminPasswordAuditResult{
		UserID:               target.UserID,
		AssignmentID:         target.AssignmentID,
		AssignmentVersion:    target.AssignmentVersion + 1,
		RevokedRefreshTokens: revokedRefreshTokens,
	}
	details := map[string]any{
		"principal_scope":        PrincipalScopePlatform,
		"role":                   RolePlatformAdmin,
		"status":                 "active",
		"password_changed":       true,
		"assignment_version":     recorded.AssignmentVersion,
		"revoked_refresh_tokens": revokedRefreshTokens,
		"reason_supplied":        true,
	}
	if err := recordAccessMutation(ctx, tx, target.UserID, "", target.UserID, req.ActionKey, rotateSinglePlatformAdminPasswordActionType, fingerprint, "platform.user.recovery_password_rotated", "platform_role_assignment", target.AssignmentID, details, recorded); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RotateSinglePlatformAdminPasswordResult{
		UserID:               target.UserID,
		Email:                target.Email,
		AssignmentID:         target.AssignmentID,
		AssignmentVersion:    recorded.AssignmentVersion,
		RevokedRefreshTokens: revokedRefreshTokens,
	}, nil
}
