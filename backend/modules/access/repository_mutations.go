package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/lib/pq"
)

const (
	accessActionLockPrefix        = "saas-access-action:"
	platformPrincipalLockPrefix   = "saas-access-platform-principal:"
	platformRoleGovernanceLockKey = "saas-access-platform-role-governance"
)

func (r *Repository) MutateMembership(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req MembershipMutationRequest) (*Membership, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "tenant.membership.set", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item Membership
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}

	var ownerUserID string
	if err := tx.QueryRowContext(ctx, `SELECT owner_user_id::text FROM salons WHERE id = $1 FOR UPDATE`, salonID).Scan(&ownerUserID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}
	if err := ensureActiveUserScopeTx(ctx, tx, targetUserID, PrincipalScopeTenant); err != nil {
		return nil, false, err
	}
	if targetUserID == ownerUserID {
		if req.Role != RoleTenantOwner {
			return nil, false, ErrValidation
		}
	} else if req.Role != RoleTenantBusinessManager {
		return nil, false, ErrValidation
	}

	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name = $1 AND scope = 'tenant'`, req.Role).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrValidation
	} else if err != nil {
		return nil, false, err
	}

	current, err := scanMembership(tx.QueryRowContext(ctx, membershipSelect+`
		WHERE membership.salon_id = $1 AND membership.user_id = $2
		FOR UPDATE OF membership
	`, salonID, targetUserID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	if errors.Is(err, ErrNotFound) {
		if req.ExpectedVersion != 0 {
			return nil, false, ErrVersionConflict
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO salon_memberships (
				salon_id, user_id, role_id, status, is_owner,
				created_by_user_id, updated_by_user_id
			)
			VALUES ($1, $2, $3, $4, false, $5, $5)
		`, salonID, targetUserID, roleID, req.Status, actorUserID)
		if err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
	} else {
		if current.Version != req.ExpectedVersion {
			return nil, false, ErrVersionConflict
		}
		if current.IsOwner && current.Role != req.Role {
			return nil, false, ErrForbidden
		}
		if current.Role != req.Role || current.Status != req.Status {
			_, err = tx.ExecContext(ctx, `
				UPDATE salon_memberships
				SET role_id = $1,
				    status = $2,
				    version = version + 1,
				    updated_by_user_id = $3,
				    updated_at = now()
				WHERE id = $4
			`, roleID, req.Status, actorUserID, current.ID)
			if err != nil {
				return nil, false, classifyAccessConstraint(err)
			}
		}
	}

	item, err := scanMembership(tx.QueryRowContext(ctx, membershipSelect+`
		WHERE membership.salon_id = $1 AND membership.user_id = $2
	`, salonID, targetUserID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"role": item.Role, "status": item.Status, "is_owner": item.IsOwner, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, targetUserID, req.ActionKey, "tenant.membership.set", fingerprint, "tenant.membership.set", "salon_membership", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) CreatePlatformUser(ctx context.Context, actorUserID, fingerprint string, req PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.user.create", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item PlatformRoleAssignment
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := lockPlatformRoleGovernanceTx(ctx, tx); err != nil {
		return nil, false, err
	}
	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name=$1 AND scope='platform'`, req.Role).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrValidation
	} else if err != nil {
		return nil, false, err
	}
	var targetUserID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,full_name,status,principal_scope)
		VALUES ($1,$2,$3,'active','platform')
		RETURNING id::text
	`, req.Email, req.PasswordHash, req.FullName).Scan(&targetUserID); err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (user_id,role_id,status,created_by_user_id,updated_by_user_id)
		VALUES ($1,$2,$3,$4,$4)
	`, targetUserID, roleID, req.Status, actorUserID); err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	item, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+` WHERE assignment.user_id=$1`, targetUserID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"email": item.User.Email, "full_name": item.User.FullName, "role": item.Role, "status": item.Status, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, "", targetUserID, req.ActionKey, "platform.user.create", fingerprint, "platform.user.created", "platform_role_assignment", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) UpdatePlatformUser(ctx context.Context, actorUserID, targetUserID, fingerprint string, req PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.user.update", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item PlatformRoleAssignment
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := lockPlatformRoleGovernanceTx(ctx, tx); err != nil {
		return nil, false, err
	}
	if err := lockPlatformPrincipalTx(ctx, tx, targetUserID); err != nil {
		return nil, false, err
	}
	current, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+` WHERE assignment.user_id=$1 FOR UPDATE OF assignment, account`, targetUserID))
	if err != nil {
		return nil, false, err
	}
	if current.Version != req.ExpectedVersion {
		return nil, false, ErrVersionConflict
	}
	removesAdmin := current.Role == RolePlatformAdmin && current.Status == "active" && (req.Role != RolePlatformAdmin || req.Status != "active")
	if removesAdmin {
		var otherAdmins int
		if err := tx.QueryRowContext(ctx, `
			SELECT count(*) FROM platform_role_assignments assignment
			JOIN roles role ON role.id=assignment.role_id
			WHERE assignment.status='active' AND role.name='platform_admin' AND assignment.user_id<>$1
		`, targetUserID).Scan(&otherAdmins); err != nil {
			return nil, false, err
		}
		if otherAdmins == 0 {
			return nil, false, ErrLastAdmin
		}
	}
	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name=$1 AND scope='platform'`, req.Role).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrValidation
	} else if err != nil {
		return nil, false, err
	}
	roleChanged := current.Role != req.Role || current.Status != req.Status
	profileChanged := current.User.Email != req.Email || current.User.FullName != req.FullName
	passwordChanged := req.PasswordHash != ""
	relatedEvents := []relatedAccessEvent{}
	var revokedAssignments, revokedPIIGrants int64
	if roleChanged {
		relatedEvents, revokedAssignments, revokedPIIGrants, err = revokePlatformPrincipalDelegationsTx(ctx, tx, targetUserID, actorUserID)
		if err != nil {
			return nil, false, err
		}
	}
	if profileChanged || passwordChanged {
		if passwordChanged {
			_, err = tx.ExecContext(ctx, `UPDATE users SET email=$1,full_name=$2,password_hash=$3,updated_at=now() WHERE id=$4 AND principal_scope='platform'`, req.Email, req.FullName, req.PasswordHash, targetUserID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE users SET email=$1,full_name=$2,updated_at=now() WHERE id=$3 AND principal_scope='platform'`, req.Email, req.FullName, targetUserID)
		}
		if err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
	}
	if roleChanged || profileChanged || passwordChanged {
		if _, err := tx.ExecContext(ctx, `UPDATE platform_role_assignments SET role_id=$1,status=$2,version=version+1,updated_by_user_id=$3,updated_at=now() WHERE id=$4`, roleID, req.Status, actorUserID, current.ID); err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
	}
	if passwordChanged || roleChanged {
		if _, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id=$1`, targetUserID); err != nil {
			return nil, false, err
		}
	}
	item, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+` WHERE assignment.user_id=$1`, targetUserID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"email": item.User.Email, "full_name": item.User.FullName, "password_changed": passwordChanged, "role": item.Role, "status": item.Status, "version": item.Version, "revoked_salon_assignments": revokedAssignments, "revoked_pii_grants": revokedPIIGrants}
	if err := recordAccessMutation(ctx, tx, actorUserID, "", targetUserID, req.ActionKey, "platform.user.update", fingerprint, "platform.user.updated", "platform_role_assignment", item.ID, details, item, relatedEvents...); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) MutatePlatformRole(ctx context.Context, actorUserID, targetUserID, fingerprint string, req PlatformRoleMutationRequest) (*PlatformRoleAssignment, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.role.set", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item PlatformRoleAssignment
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := lockPlatformRoleGovernanceTx(ctx, tx); err != nil {
		return nil, false, err
	}
	if err := lockPlatformPrincipalTx(ctx, tx, targetUserID); err != nil {
		return nil, false, err
	}
	if err := ensureActiveUserScopeTx(ctx, tx, targetUserID, PrincipalScopePlatform); err != nil {
		return nil, false, err
	}
	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name = $1 AND scope = 'platform'`, req.Role).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrValidation
	} else if err != nil {
		return nil, false, err
	}

	current, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+`
		WHERE assignment.user_id = $1
		FOR UPDATE OF assignment
	`, targetUserID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	var revokedAssignments int64
	var revokedPIIGrants int64
	var relatedEvents []relatedAccessEvent
	roleChanged := false
	if errors.Is(err, ErrNotFound) {
		if req.ExpectedVersion != 0 {
			return nil, false, ErrVersionConflict
		}
		relatedEvents, revokedAssignments, revokedPIIGrants, err = revokePlatformPrincipalDelegationsTx(ctx, tx, targetUserID, actorUserID)
		if err != nil {
			return nil, false, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO platform_role_assignments (
				user_id, role_id, status, created_by_user_id, updated_by_user_id
			)
			VALUES ($1, $2, $3, $4, $4)
		`, targetUserID, roleID, req.Status, actorUserID)
		if err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
		roleChanged = true
	} else {
		if current.Version != req.ExpectedVersion {
			return nil, false, ErrVersionConflict
		}
		removesAdmin := current.Role == RolePlatformAdmin && current.Status == "active" && (req.Role != RolePlatformAdmin || req.Status != "active")
		if removesAdmin {
			var otherAdmins int
			if err := tx.QueryRowContext(ctx, `
				SELECT count(*)
				FROM platform_role_assignments AS assignment
				JOIN roles AS role ON role.id = assignment.role_id
				WHERE assignment.status = 'active'
				  AND role.name = 'platform_admin'
				  AND assignment.user_id <> $1
			`, targetUserID).Scan(&otherAdmins); err != nil {
				return nil, false, err
			}
			if otherAdmins == 0 {
				return nil, false, ErrLastAdmin
			}
		}
		if current.Role != req.Role || current.Status != req.Status {
			relatedEvents, revokedAssignments, revokedPIIGrants, err = revokePlatformPrincipalDelegationsTx(ctx, tx, targetUserID, actorUserID)
			if err != nil {
				return nil, false, err
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE platform_role_assignments
				SET role_id = $1,
				    status = $2,
				    version = version + 1,
				    updated_by_user_id = $3,
				    updated_at = now()
				WHERE id = $4
			`, roleID, req.Status, actorUserID, current.ID)
			if err != nil {
				return nil, false, classifyAccessConstraint(err)
			}
			roleChanged = true
		}
	}
	if roleChanged {
		if _, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id=$1`, targetUserID); err != nil {
			return nil, false, err
		}
	}
	item, err := scanPlatformRole(tx.QueryRowContext(ctx, platformRoleSelect+` WHERE assignment.user_id = $1`, targetUserID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{
		"role":                      item.Role,
		"status":                    item.Status,
		"version":                   item.Version,
		"revoked_salon_assignments": revokedAssignments,
		"revoked_pii_grants":        revokedPIIGrants,
	}
	if err := recordAccessMutation(ctx, tx, actorUserID, "", targetUserID, req.ActionKey, "platform.role.set", fingerprint, "platform.role.set", "platform_role_assignment", item.ID, details, item, relatedEvents...); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) MutateSalonAssignment(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.salon_assignment.set", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item SalonAssignment
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := ensureSalonTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if err := lockPlatformPrincipalTx(ctx, tx, targetUserID); err != nil {
		return nil, false, err
	}
	var targetIsOps bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users AS account
			JOIN platform_role_assignments AS role_assignment
			  ON role_assignment.user_id = account.id
			 AND role_assignment.status = 'active'
			JOIN roles AS role ON role.id = role_assignment.role_id
			WHERE account.id = $1
			  AND account.status = 'active'
			  AND account.principal_scope = 'platform'
			  AND role.name = 'platform_ops'
		)
	`, targetUserID).Scan(&targetIsOps); err != nil {
		return nil, false, err
	}
	if !targetIsOps {
		return nil, false, ErrValidation
	}

	current, err := scanSalonAssignment(tx.QueryRowContext(ctx, salonAssignmentSelect+`
		WHERE assignment.salon_id = $1 AND assignment.user_id = $2
		FOR UPDATE OF assignment
	`, salonID, targetUserID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	var assignmentID string
	changed := false
	if errors.Is(err, ErrNotFound) {
		if req.ExpectedVersion != 0 {
			return nil, false, ErrVersionConflict
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO platform_salon_assignments (
				salon_id, user_id, status, created_by_user_id, updated_by_user_id
			)
			VALUES ($1, $2, $3, $4, $4)
			RETURNING id::text
		`, salonID, targetUserID, req.Status, actorUserID).Scan(&assignmentID); err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
		changed = true
	} else {
		if current.Version != req.ExpectedVersion {
			return nil, false, ErrVersionConflict
		}
		assignmentID = current.ID
		changed = current.Status != req.Status || !reflect.DeepEqual(current.Permissions, req.Permissions)
		if changed {
			if _, err := tx.ExecContext(ctx, `
				UPDATE platform_salon_assignments
				SET status = $1,
				    version = version + 1,
				    updated_by_user_id = $2,
				    updated_at = now()
				WHERE id = $3
			`, req.Status, actorUserID, assignmentID); err != nil {
				return nil, false, err
			}
		}
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM platform_salon_assignment_permissions WHERE assignment_id = $1`, assignmentID); err != nil {
			return nil, false, err
		}
		if len(req.Permissions) > 0 {
			result, err := tx.ExecContext(ctx, `
				INSERT INTO platform_salon_assignment_permissions (assignment_id, permission_id)
				SELECT $1, permission.id
				FROM permissions AS permission
				WHERE permission.name = ANY($2)
			`, assignmentID, pq.Array(req.Permissions))
			if err != nil {
				return nil, false, classifyAccessConstraint(err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return nil, false, err
			}
			if affected != int64(len(req.Permissions)) {
				return nil, false, ErrValidation
			}
		}
	}
	relatedEvents := []relatedAccessEvent{}
	if changed {
		relatedEvents, err = revokeSalonSupportRequestsTx(ctx, tx, targetUserID, salonID, actorUserID)
		if err != nil {
			return nil, false, err
		}
	}
	item, err := scanSalonAssignment(tx.QueryRowContext(ctx, salonAssignmentSelect+` WHERE assignment.id = $1`, assignmentID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"status": item.Status, "permissions": item.Permissions, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, targetUserID, req.ActionKey, "platform.salon_assignment.set", fingerprint, "platform.salon_assignment.set", "platform_salon_assignment", item.ID, details, item, relatedEvents...); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) GrantPIIAccess(ctx context.Context, actorUserID, salonID, fingerprint string, req PIIGrantRequest) (*PIIGrant, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.pii_grant.create", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item PIIGrant
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := ensureSalonTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if err := lockPlatformPrincipalTx(ctx, tx, req.UserID); err != nil {
		return nil, false, err
	}
	eligible, err := targetEligibleForPIIGrantTx(ctx, tx, req.UserID, salonID, PIIScope(req.Scope))
	if err != nil {
		return nil, false, err
	}
	if !eligible {
		return nil, false, ErrValidation
	}
	var grantID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO platform_pii_access_grants (
			salon_id, user_id, scope, reason, expires_at, created_by_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, salonID, req.UserID, req.Scope, req.Reason, req.ExpiresAt, actorUserID).Scan(&grantID); err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	item, err := scanPIIGrant(tx.QueryRowContext(ctx, piiGrantSelect+` WHERE grant_record.id = $1`, grantID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"scope": item.Scope, "expires_at": item.ExpiresAt, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, req.UserID, req.ActionKey, "platform.pii_grant.create", fingerprint, "platform.pii_grant.created", "platform_pii_access_grant", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) RevokePIIAccess(ctx context.Context, actorUserID, salonID, grantID, fingerprint string, req PIIGrantRevokeRequest) (*PIIGrant, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.pii_grant.revoke", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item PIIGrant
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	item, err := scanPIIGrant(tx.QueryRowContext(ctx, piiGrantSelect+`
		WHERE grant_record.id = $1 AND grant_record.salon_id = $2
		FOR UPDATE OF grant_record
	`, grantID, salonID))
	if err != nil {
		return nil, false, err
	}
	if item.Version != req.ExpectedVersion {
		return nil, false, ErrVersionConflict
	}
	if item.RevokedAt == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE platform_pii_access_grants
			SET revoked_at = now(),
			    revoked_by_user_id = $1,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $2
		`, actorUserID, grantID); err != nil {
			return nil, false, err
		}
		item, err = scanPIIGrant(tx.QueryRowContext(ctx, piiGrantSelect+` WHERE grant_record.id = $1`, grantID))
		if err != nil {
			return nil, false, err
		}
	}
	details := map[string]any{"scope": item.Scope, "revoked": item.RevokedAt != nil, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, item.UserID, req.ActionKey, "platform.pii_grant.revoke", fingerprint, "platform.pii_grant.revoked", "platform_pii_access_grant", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) CreateSupportAccessRequest(ctx context.Context, actorUserID, salonID, fingerprint string, req SupportAccessRequestCreate) (*SupportAccessRequest, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, "platform.support_access.grant", fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item SupportAccessRequest
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	if err := ensureSalonTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if err := ensureActiveUserScopeTx(ctx, tx, req.UserID, PrincipalScopePlatform); err != nil {
		return nil, false, err
	}
	if err := lockPlatformPrincipalTx(ctx, tx, req.UserID); err != nil {
		return nil, false, err
	}
	eligible, err := targetEligibleForSupportTx(ctx, tx, req.UserID, salonID, req.Capabilities)
	if err != nil {
		return nil, false, err
	}
	if !eligible {
		return nil, false, ErrValidation
	}
	var requestID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO platform_support_access_requests (
			salon_id, platform_user_id, requested_by_user_id, reason, requested_expires_at,
			status, approved_expires_at, decision_by_user_id, decision_at
		) VALUES ($1, $2, $3, $4, $5, 'approved', $5, $3, now())
		RETURNING id::text
	`, salonID, req.UserID, actorUserID, req.Reason, req.ExpiresAt).Scan(&requestID); err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO platform_support_access_request_permissions (request_id, permission_id)
		SELECT $1, permission.id FROM permissions permission WHERE permission.name = ANY($2)
	`, requestID, pq.Array(req.Capabilities))
	if err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if affected != int64(len(req.Capabilities)) {
		return nil, false, ErrValidation
	}
	for _, scope := range req.PIIScopes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_support_access_request_permissions (request_id, pii_scope) VALUES ($1, $2)`, requestID, scope); err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO platform_pii_access_grants (salon_id,user_id,scope,reason,expires_at,created_by_user_id,support_access_request_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
		`, salonID, req.UserID, scope, req.Reason, req.ExpiresAt, actorUserID, requestID); err != nil {
			return nil, false, classifyAccessConstraint(err)
		}
	}
	item, err := scanSupportAccessRequest(tx.QueryRowContext(ctx, supportAccessRequestSelect+` WHERE request.id = $1`, requestID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"status": item.Status, "capabilities": item.Capabilities, "pii_scopes": item.PIIScopes, "requested_expires_at": item.RequestedExpiresAt, "version": item.Version}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, req.UserID, req.ActionKey, "platform.support_access.grant", fingerprint, "platform.support_access.granted", "platform_support_access_request", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func (r *Repository) CancelSupportAccessRequest(ctx context.Context, actorUserID, salonID, requestID, fingerprint string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	return r.mutateSupportAccessRequest(ctx, actorUserID, salonID, requestID, "cancel", fingerprint, req)
}

func (r *Repository) RevokeSupportAccessRequest(ctx context.Context, actorUserID, salonID, requestID, fingerprint string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	return r.mutateSupportAccessRequest(ctx, actorUserID, salonID, requestID, "revoke", fingerprint, req)
}

func (r *Repository) mutateSupportAccessRequest(ctx context.Context, actorUserID, salonID, requestID, decision, fingerprint string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	actionType := "platform.support_access." + decision
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	replay, err := beginAccessMutation(ctx, tx, actorUserID, req.ActionKey, actionType, fingerprint)
	if err != nil {
		return nil, false, err
	}
	if err := ensurePlatformAdminTx(ctx, tx, actorUserID); err != nil {
		return nil, false, err
	}
	if replay != nil {
		var item SupportAccessRequest
		if err := json.Unmarshal(replay, &item); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return &item, true, nil
	}
	item, err := scanSupportAccessRequest(tx.QueryRowContext(ctx, supportAccessRequestSelect+` WHERE request.id = $1 AND request.salon_id = $2 FOR UPDATE OF request`, requestID, salonID))
	if err != nil {
		return nil, false, err
	}
	if item.Version != req.ExpectedVersion {
		return nil, false, ErrVersionConflict
	}
	nextStatus := ""
	switch decision {
	case "cancel":
		if item.Status != "pending_owner_review" {
			return nil, false, ErrVersionConflict
		}
		nextStatus = "cancelled"
	case "revoke":
		if item.Status != "approved" {
			return nil, false, ErrVersionConflict
		}
		nextStatus = "revoked"
	default:
		return nil, false, ErrValidation
	}
	if decision == "revoke" {
		_, err = tx.ExecContext(ctx, `UPDATE platform_support_access_requests SET status='revoked',approved_expires_at=NULL,revoked_by_user_id=$1,revoked_at=now(),version=version+1,updated_at=now() WHERE id=$2`, actorUserID, requestID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE platform_support_access_requests SET status='cancelled',version=version+1,updated_at=now() WHERE id=$1`, requestID)
	}
	if err != nil {
		return nil, false, classifyAccessConstraint(err)
	}
	if decision == "revoke" {
		if _, err := tx.ExecContext(ctx, `UPDATE platform_pii_access_grants SET revoked_at=COALESCE(revoked_at,now()),revoked_by_user_id=COALESCE(revoked_by_user_id,$1),version=CASE WHEN revoked_at IS NULL THEN version+1 ELSE version END,updated_at=now() WHERE support_access_request_id=$2`, actorUserID, requestID); err != nil {
			return nil, false, err
		}
	}
	item, err = scanSupportAccessRequest(tx.QueryRowContext(ctx, supportAccessRequestSelect+` WHERE request.id = $1`, requestID))
	if err != nil {
		return nil, false, err
	}
	details := map[string]any{"status": nextStatus, "effective_status": item.EffectiveStatus, "capabilities": item.Capabilities, "pii_scopes": item.PIIScopes, "version": item.Version}
	eventType := actionType + "d"
	if decision == "revoke" {
		eventType = "platform.support_access.revoked"
	}
	if decision == "cancel" {
		eventType = "platform.support_access.cancelled"
	}
	if err := recordAccessMutation(ctx, tx, actorUserID, salonID, item.PlatformUserID, req.ActionKey, actionType, fingerprint, eventType, "platform_support_access_request", item.ID, details, item); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

func beginAccessMutation(ctx context.Context, tx *sql.Tx, actorUserID, actionKey, actionType, fingerprint string) ([]byte, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, accessActionLockPrefix+actorUserID+":"+actionKey); err != nil {
		return nil, err
	}
	var storedType string
	var storedFingerprint string
	var payload []byte
	err := tx.QueryRowContext(ctx, `
		SELECT action_type, request_fingerprint, response_payload
		FROM access_control_actions
		WHERE actor_user_id = $1 AND action_key = $2
	`, actorUserID, actionKey).Scan(&storedType, &storedFingerprint, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedType != actionType || storedFingerprint != fingerprint {
		return nil, ErrActionConflict
	}
	return payload, nil
}

type relatedAccessEvent struct {
	SalonID      string
	TargetUserID string
	EventType    string
	ObjectType   string
	ObjectID     string
	Details      map[string]any
}

func recordAccessMutation(ctx context.Context, tx *sql.Tx, actorUserID, salonID, targetUserID, actionKey, actionType, fingerprint, eventType, objectType, objectID string, details map[string]any, response any, relatedEvents ...relatedAccessEvent) error {
	responsePayload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	detailsPayload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var actionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO access_control_actions (
			actor_user_id, salon_id, target_user_id, action_key,
			action_type, request_fingerprint, response_payload
		)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7)
		RETURNING id::text
	`, actorUserID, salonID, targetUserID, actionKey, actionType, fingerprint, string(responsePayload)).Scan(&actionID); err != nil {
		return classifyAccessConstraint(err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO access_control_events (
			action_id, actor_user_id, salon_id, target_user_id,
			event_type, object_type, object_id, details
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6, $7, $8)
	`, actionID, actorUserID, salonID, targetUserID, eventType, objectType, objectID, string(detailsPayload)); err != nil {
		return err
	}
	for _, related := range relatedEvents {
		relatedDetails, err := json.Marshal(related.Details)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO access_control_events (
				action_id, actor_user_id, salon_id, target_user_id,
				event_type, object_type, object_id, details
			)
			VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6, $7, $8)
		`, actionID, actorUserID, related.SalonID, related.TargetUserID, related.EventType, related.ObjectType, related.ObjectID, string(relatedDetails)); err != nil {
			return err
		}
	}
	return nil
}

func lockPlatformRoleGovernanceTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, platformRoleGovernanceLockKey)
	return err
}

func lockPlatformPrincipalTx(ctx context.Context, tx *sql.Tx, userID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, platformPrincipalLockPrefix+userID)
	return err
}

func revokePlatformPrincipalDelegationsTx(ctx context.Context, tx *sql.Tx, targetUserID, actorUserID string) ([]relatedAccessEvent, int64, int64, error) {
	assignmentRows, err := tx.QueryContext(ctx, `
		UPDATE platform_salon_assignments
		SET status = 'revoked',
		    version = version + 1,
		    updated_by_user_id = $2,
		    updated_at = now()
		WHERE user_id = $1
		  AND status = 'active'
		RETURNING id::text, salon_id::text, version
	`, targetUserID, actorUserID)
	if err != nil {
		return nil, 0, 0, err
	}
	relatedEvents := make([]relatedAccessEvent, 0)
	var revokedAssignments int64
	for assignmentRows.Next() {
		var assignmentID string
		var salonID string
		var version int64
		if err := assignmentRows.Scan(&assignmentID, &salonID, &version); err != nil {
			assignmentRows.Close()
			return nil, 0, 0, err
		}
		revokedAssignments++
		relatedEvents = append(relatedEvents, relatedAccessEvent{
			SalonID:      salonID,
			TargetUserID: targetUserID,
			EventType:    "platform.salon_assignment.revoked_by_role_transition",
			ObjectType:   "platform_salon_assignment",
			ObjectID:     assignmentID,
			Details:      map[string]any{"status": "revoked", "version": version},
		})
	}
	if err := assignmentRows.Err(); err != nil {
		assignmentRows.Close()
		return nil, 0, 0, err
	}
	assignmentRows.Close()
	supportRows, err := tx.QueryContext(ctx, `
		UPDATE platform_support_access_requests
		SET status='revoked',
		    approved_expires_at=NULL,
		    revoked_by_user_id=$2,
		    revoked_at=now(),
		    version=version+1,
		    updated_at=now()
		WHERE platform_user_id=$1 AND status='approved'
		RETURNING id::text, salon_id::text, version
	`, targetUserID, actorUserID)
	if err != nil {
		return nil, 0, 0, err
	}
	for supportRows.Next() {
		var requestID, salonID string
		var version int64
		if err := supportRows.Scan(&requestID, &salonID, &version); err != nil {
			supportRows.Close()
			return nil, 0, 0, err
		}
		relatedEvents = append(relatedEvents, relatedAccessEvent{SalonID: salonID, TargetUserID: targetUserID, EventType: "platform.support_access.revoked_by_role_transition", ObjectType: "platform_support_access_request", ObjectID: requestID, Details: map[string]any{"status": "revoked", "version": version}})
	}
	if err := supportRows.Err(); err != nil {
		supportRows.Close()
		return nil, 0, 0, err
	}
	supportRows.Close()
	grantRows, err := tx.QueryContext(ctx, `
		UPDATE platform_pii_access_grants
		SET revoked_at = now(),
		    revoked_by_user_id = $2,
		    version = version + 1,
		    updated_at = now()
		WHERE user_id = $1
		  AND revoked_at IS NULL
		RETURNING id::text, salon_id::text, scope, version
	`, targetUserID, actorUserID)
	if err != nil {
		return nil, 0, 0, err
	}
	var revokedPIIGrants int64
	for grantRows.Next() {
		var grantID string
		var salonID string
		var scope string
		var version int64
		if err := grantRows.Scan(&grantID, &salonID, &scope, &version); err != nil {
			grantRows.Close()
			return nil, 0, 0, err
		}
		revokedPIIGrants++
		relatedEvents = append(relatedEvents, relatedAccessEvent{
			SalonID:      salonID,
			TargetUserID: targetUserID,
			EventType:    "platform.pii_grant.revoked_by_role_transition",
			ObjectType:   "platform_pii_access_grant",
			ObjectID:     grantID,
			Details:      map[string]any{"scope": scope, "revoked": true, "version": version},
		})
	}
	if err := grantRows.Err(); err != nil {
		grantRows.Close()
		return nil, 0, 0, err
	}
	grantRows.Close()
	return relatedEvents, revokedAssignments, revokedPIIGrants, nil
}

func revokeSalonSupportRequestsTx(ctx context.Context, tx *sql.Tx, targetUserID, salonID, actorUserID string) ([]relatedAccessEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		UPDATE platform_support_access_requests
		SET status='revoked',approved_expires_at=NULL,revoked_by_user_id=$3,revoked_at=now(),version=version+1,updated_at=now()
		WHERE platform_user_id=$1 AND salon_id=$2 AND status='approved'
		RETURNING id::text,version
	`, targetUserID, salonID, actorUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []relatedAccessEvent{}
	for rows.Next() {
		var requestID string
		var version int64
		if err := rows.Scan(&requestID, &version); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE platform_pii_access_grants SET revoked_at=COALESCE(revoked_at,now()),revoked_by_user_id=COALESCE(revoked_by_user_id,$1),version=CASE WHEN revoked_at IS NULL THEN version+1 ELSE version END,updated_at=now() WHERE support_access_request_id=$2`, actorUserID, requestID); err != nil {
			return nil, err
		}
		events = append(events, relatedAccessEvent{SalonID: salonID, TargetUserID: targetUserID, EventType: "platform.support_access.revoked_by_assignment_transition", ObjectType: "platform_support_access_request", ObjectID: requestID, Details: map[string]any{"status": "revoked", "version": version}})
	}
	return events, rows.Err()
}

func ensureActiveUserScopeTx(ctx context.Context, tx *sql.Tx, userID string, scope PrincipalScope) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND status = 'active' AND principal_scope = $2)`, userID, string(scope)).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func ensureSalonTx(ctx context.Context, tx *sql.Tx, salonID string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id = $1)`, salonID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func targetEligibleForSupportTx(ctx context.Context, tx *sql.Tx, userID, salonID string, capabilities []string) (bool, error) {
	var eligible bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users account
			JOIN platform_role_assignments role_assignment
			  ON role_assignment.user_id=account.id AND role_assignment.status='active'
			JOIN roles role ON role.id=role_assignment.role_id
			WHERE account.id=$1 AND account.status='active' AND account.principal_scope='platform'
			AND role.name='platform_ops'
			AND EXISTS (
					SELECT 1 FROM platform_salon_assignments salon_assignment
					WHERE salon_assignment.user_id=account.id AND salon_assignment.salon_id=$2 AND salon_assignment.status='active'
					AND (
						SELECT count(DISTINCT permission.name)
						FROM platform_salon_assignment_permissions child
						JOIN permissions permission ON permission.id=child.permission_id
						WHERE child.assignment_id=salon_assignment.id AND permission.name=ANY($3)
					) = cardinality($3::text[])
				)
		)
	`, userID, salonID, pq.Array(capabilities)).Scan(&eligible)
	return eligible, err
}

func targetEligibleForPIIGrantTx(ctx context.Context, tx *sql.Tx, userID, salonID string, scope PIIScope) (bool, error) {
	requiredCapability := CapabilityBusinessRead
	if scope == PIIScopeCalls || scope == PIIScopeNotifications {
		requiredCapability = CapabilityOperationsRead
	}
	var eligible bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users AS account
			JOIN platform_role_assignments AS platform_role
			  ON platform_role.user_id = account.id
			 AND platform_role.status = 'active'
			JOIN roles AS role ON role.id = platform_role.role_id
			WHERE account.id = $1
			  AND account.status = 'active'
			  AND account.principal_scope = 'platform'
			  AND role.name = 'platform_ops'
			  AND EXISTS (
			              SELECT 1
			              FROM platform_salon_assignments AS salon_assignment
			              JOIN platform_salon_assignment_permissions AS assignment_permission
			                ON assignment_permission.assignment_id = salon_assignment.id
			              JOIN permissions AS permission
			                ON permission.id = assignment_permission.permission_id
			              WHERE salon_assignment.user_id = account.id
			                AND salon_assignment.salon_id = $2
			                AND salon_assignment.status = 'active'
			                AND permission.name = $3
			  )
		)
	`, userID, salonID, string(requiredCapability)).Scan(&eligible)
	return eligible, err
}

func classifyAccessConstraint(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23503":
			return ErrNotFound
		case "23505":
			return ErrVersionConflict
		case "23514", "P0001":
			return ErrValidation
		}
	}
	return err
}
