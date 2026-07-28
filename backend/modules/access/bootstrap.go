package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// BootstrapPlatformAdmin creates one dedicated Platform identity, or exactly
// replays that bootstrap, only while no active Platform Administrator exists.
// It never promotes a Tenant identity into the Platform realm.
func (r *Repository) BootstrapPlatformAdmin(ctx context.Context, req BootstrapPlatformAdminRequest) (*BootstrapPlatformAdminResult, error) {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Email == "" || !validActionKey(req.ActionKey) || !validChangeReference(req.Reason) {
		return nil, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockPlatformRoleGovernanceTx(ctx, tx); err != nil {
		return nil, err
	}
	var targetUserID string
	var principalScope string
	var targetStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, principal_scope, status
		FROM users
		WHERE lower(email) = $1
		FOR UPDATE
	`, req.Email).Scan(&targetUserID, &principalScope, &targetStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	fingerprint, err := requestFingerprint(struct {
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		Reason   string `json:"reason"`
	}{req.Email, req.FullName, req.Reason})
	if err != nil {
		return nil, err
	}
	if targetUserID != "" {
		if principalScope != string(PrincipalScopePlatform) || targetStatus != "active" {
			return nil, ErrNotFound
		}
		replay, err := beginAccessMutation(ctx, tx, targetUserID, req.ActionKey, "platform.role.bootstrap_admin", fingerprint)
		if err != nil {
			return nil, err
		}
		if replay != nil {
			var assignment PlatformRoleAssignment
			if err := json.Unmarshal(replay, &assignment); err != nil {
				return nil, err
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &BootstrapPlatformAdminResult{Assignment: assignment, Replayed: true}, nil
		}
	}
	var activeAdmins int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM platform_role_assignments AS assignment
		JOIN roles AS role ON role.id = assignment.role_id
		WHERE assignment.status = 'active'
		  AND role.name = 'platform_admin'
	`).Scan(&activeAdmins); err != nil {
		return nil, err
	}
	if activeAdmins > 0 {
		return nil, ErrBootstrapClosed
	}
	if targetUserID == "" {
		if req.FullName == "" || strings.TrimSpace(req.PasswordHash) == "" {
			return nil, ErrValidation
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash, full_name, status, principal_scope)
			VALUES ($1, $2, $3, 'active', 'platform')
			RETURNING id::text
		`, req.Email, req.PasswordHash, req.FullName).Scan(&targetUserID); err != nil {
			return nil, classifyAccessConstraint(err)
		}
		if _, err := beginAccessMutation(ctx, tx, targetUserID, req.ActionKey, "platform.role.bootstrap_admin", fingerprint); err != nil {
			return nil, err
		}
	}
	if err := lockPlatformPrincipalTx(ctx, tx, targetUserID); err != nil {
		return nil, err
	}
	relatedEvents, revokedAssignments, revokedPIIGrants, err := revokePlatformPrincipalDelegationsTx(ctx, tx, targetUserID, targetUserID)
	if err != nil {
		return nil, err
	}
	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name = 'platform_admin' AND scope = 'platform'`).Scan(&roleID); err != nil {
		return nil, err
	}
	var existingID string
	var existingVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, version
		FROM platform_role_assignments
		WHERE user_id = $1
		FOR UPDATE
	`, targetUserID).Scan(&existingID, &existingVersion)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO platform_role_assignments (
				user_id, role_id, status, created_by_user_id, updated_by_user_id
			)
			VALUES ($1, $2, 'active', $1, $1)
			RETURNING id::text
		`, targetUserID, roleID).Scan(&existingID); err != nil {
			return nil, classifyAccessConstraint(err)
		}
	} else if err != nil {
		return nil, err
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE platform_role_assignments
			SET role_id = $1,
			    status = 'active',
			    version = $2 + 1,
			    updated_by_user_id = $3,
			    updated_at = now()
			WHERE id = $4
		`, roleID, existingVersion, targetUserID, existingID); err != nil {
			return nil, classifyAccessConstraint(err)
		}
	}
	assignment, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+` WHERE assignment.id = $1`, existingID))
	if err != nil {
		return nil, err
	}
	details := map[string]any{
		"role":                      RolePlatformAdmin,
		"status":                    "active",
		"version":                   assignment.Version,
		"bootstrap":                 true,
		"reason_supplied":           true,
		"revoked_salon_assignments": revokedAssignments,
		"revoked_pii_grants":        revokedPIIGrants,
	}
	if err := recordAccessMutation(ctx, tx, targetUserID, "", targetUserID, req.ActionKey, "platform.role.bootstrap_admin", fingerprint, "platform.role.bootstrapped", "platform_role_assignment", assignment.ID, details, assignment, relatedEvents...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &BootstrapPlatformAdminResult{Assignment: *assignment}, nil
}
