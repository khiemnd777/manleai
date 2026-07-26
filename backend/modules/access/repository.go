package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListUsers(ctx context.Context, actorUserID, query string, limit int) ([]AccessUser, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text,email,full_name,status
		FROM users
		WHERE $1='' OR position(lower($1) in lower(email))>0 OR position(lower($1) in lower(full_name))>0
		ORDER BY status,full_name,email,id
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccessUser, 0)
	for rows.Next() {
		var item AccessUser
		if err := rows.Scan(&item.ID, &item.Email, &item.FullName, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Evaluate(ctx context.Context, actorUserID string, check AccessCheck) (bool, error) {
	var allowed bool
	switch check.Surface {
	case SurfaceTenant:
		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users AS account
				JOIN salon_memberships AS membership ON membership.user_id = account.id
				JOIN role_permissions AS role_permission ON role_permission.role_id = membership.role_id
				JOIN permissions AS permission ON permission.id = role_permission.permission_id
				WHERE account.id = $1
				  AND account.status = 'active'
				  AND membership.salon_id = $2
				  AND membership.status = 'active'
				  AND permission.name = $3
			)
		`, actorUserID, check.SalonID, string(check.Capability)).Scan(&allowed)
		if err != nil {
			return false, err
		}
		// Tenant members are accessing their own salon workflow. Platform PII
		// grants are intentionally not consulted on this route-derived surface.
		return allowed, nil
	case SurfacePlatform:
		if check.SalonID == "" {
			err := r.db.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM users AS account
					JOIN platform_role_assignments AS assignment
					  ON assignment.user_id = account.id
					 AND assignment.status = 'active'
					JOIN role_permissions AS role_permission
					  ON role_permission.role_id = assignment.role_id
					JOIN permissions AS permission
					  ON permission.id = role_permission.permission_id
					WHERE account.id = $1
					  AND account.status = 'active'
					  AND permission.name = $2
				)
			`, actorUserID, string(check.Capability)).Scan(&allowed)
			return allowed, err
		}

		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users AS account
				JOIN platform_role_assignments AS platform_role
				  ON platform_role.user_id = account.id
				 AND platform_role.status = 'active'
				JOIN roles AS role ON role.id = platform_role.role_id
				WHERE account.id = $1
				  AND account.status = 'active'
				  AND (
				      (
				          role.name = 'platform_admin'
				          AND EXISTS (
				              SELECT 1
				              FROM role_permissions AS role_permission
				              JOIN permissions AS permission
				                ON permission.id = role_permission.permission_id
				              WHERE role_permission.role_id = role.id
				                AND permission.name = $3
				          )
				      )
				      OR (
				          role.name = 'platform_ops'
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
				  )
			)
		`, actorUserID, check.SalonID, string(check.Capability)).Scan(&allowed)
		if err != nil || !allowed || check.PIIScope == "" {
			return allowed, err
		}

		err = r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM platform_pii_access_grants AS grant_record
				WHERE grant_record.user_id = $1
				  AND grant_record.salon_id = $2
				  AND grant_record.scope = $3
				  AND grant_record.revoked_at IS NULL
				  AND grant_record.expires_at > now()
			)
		`, actorUserID, check.SalonID, string(check.PIIScope)).Scan(&allowed)
		return allowed, err
	default:
		return false, ErrForbidden
	}
}

func (r *Repository) ListMemberships(ctx context.Context, actorUserID, salonID string) ([]Membership, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := r.ensureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, membershipSelect+`
		WHERE membership.salon_id = $1
		ORDER BY membership.is_owner DESC, membership.created_at, membership.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Membership, 0)
	for rows.Next() {
		item, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListPlatformRoles(ctx context.Context, actorUserID string) ([]PlatformRoleAssignment, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, platformRoleSelect+`
		ORDER BY assignment.created_at, assignment.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlatformRoleAssignment, 0)
	for rows.Next() {
		item, err := scanPlatformRole(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCapabilities(ctx context.Context, actorUserID string) ([]CapabilityDefinition, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, display_name, scope, delegation_scope
		FROM permissions
		WHERE scope IN ('tenant', 'platform')
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CapabilityDefinition, 0)
	for rows.Next() {
		var item CapabilityDefinition
		if err := rows.Scan(&item.Name, &item.DisplayName, &item.Scope, &item.DelegationScope); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListSalonAssignments(ctx context.Context, actorUserID, salonID string) ([]SalonAssignment, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := r.ensureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, salonAssignmentSelect+`
		WHERE assignment.salon_id = $1
		ORDER BY assignment.created_at, assignment.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SalonAssignment, 0)
	for rows.Next() {
		item, err := scanSalonAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListPIIGrants(ctx context.Context, actorUserID, salonID string) ([]PIIGrant, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := r.ensureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, piiGrantSelect+`
		WHERE grant_record.salon_id = $1
		ORDER BY grant_record.created_at DESC, grant_record.id DESC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PIIGrant, 0)
	for rows.Next() {
		item, err := scanPIIGrant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAuditEvents(ctx context.Context, actorUserID, salonID string, limit, offset int) ([]AuditEvent, bool, error) {
	if salonID == "" {
		if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
			return nil, false, err
		}
	}
	args := []any{limit + 1, offset}
	filter := ""
	if salonID != "" {
		if err := r.ensureSalonExists(ctx, salonID); err != nil {
			return nil, false, err
		}
		filter = "WHERE event.salon_id = $3"
		args = append(args, salonID)
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT event.id::text,
		       COALESCE(event.actor_user_id::text, ''),
		       COALESCE(event.salon_id::text, ''),
		       COALESCE(event.target_user_id::text, ''),
		       event.event_type,
		       event.object_type,
		       event.object_id,
		       event.details,
		       event.created_at
		FROM (
			SELECT id,actor_user_id,salon_id,target_user_id,event_type,object_type,object_id,details,created_at
			FROM access_control_events
			UNION ALL
			SELECT id,actor_user_id,salon_id,NULL::uuid,event_type,resource_type,resource_id,details,created_at
			FROM business_events
			UNION ALL
			SELECT id,actor_user_id,salon_id,NULL::uuid,event_type,resource_type,resource_id,details,created_at
			FROM technical_events
		) AS event
		%s
		ORDER BY event.created_at DESC, event.id DESC
		LIMIT $1 OFFSET $2
	`, filter), args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var item AuditEvent
		var details []byte
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.SalonID, &item.TargetUserID, &item.EventType, &item.ObjectType, &item.ObjectID, &details, &item.CreatedAt); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(details, &item.Details); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func (r *Repository) ensurePlatformAdmin(ctx context.Context, actorUserID string) error {
	var allowed bool
	if err := r.db.QueryRowContext(ctx, platformAdminExistsQuery, actorUserID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func ensurePlatformAdminTx(ctx context.Context, tx *sql.Tx, actorUserID string) error {
	var allowed bool
	if err := tx.QueryRowContext(ctx, platformAdminExistsQuery, actorUserID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (r *Repository) ensureSalonExists(ctx context.Context, salonID string) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id = $1)`, salonID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

const platformAdminExistsQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM users AS account
		JOIN platform_role_assignments AS assignment
		  ON assignment.user_id = account.id
		 AND assignment.status = 'active'
		JOIN roles AS role ON role.id = assignment.role_id
		WHERE account.id = $1
		  AND account.status = 'active'
		  AND role.name = 'platform_admin'
	)
`

const membershipSelect = `
	SELECT membership.id::text,
	       membership.salon_id::text,
	       membership.user_id::text,
	       role.name,
	       membership.status,
	       membership.is_owner,
	       membership.version,
	       membership.created_at,
	       membership.updated_at
	FROM salon_memberships AS membership
	JOIN roles AS role ON role.id = membership.role_id
`

const platformRoleSelect = `
	SELECT assignment.id::text,
	       assignment.user_id::text,
	       role.name,
	       assignment.status,
	       assignment.version,
	       assignment.created_at,
	       assignment.updated_at
	FROM platform_role_assignments AS assignment
	JOIN roles AS role ON role.id = assignment.role_id
`

const salonAssignmentSelect = `
	SELECT assignment.id::text,
	       assignment.salon_id::text,
	       assignment.user_id::text,
	       assignment.status,
	       ARRAY(
	           SELECT permission.name
	           FROM platform_salon_assignment_permissions AS assignment_permission
	           JOIN permissions AS permission ON permission.id = assignment_permission.permission_id
	           WHERE assignment_permission.assignment_id = assignment.id
	           ORDER BY permission.name
	       ),
	       assignment.version,
	       assignment.created_at,
	       assignment.updated_at
	FROM platform_salon_assignments AS assignment
`

const piiGrantSelect = `
	SELECT grant_record.id::text,
	       grant_record.salon_id::text,
	       grant_record.user_id::text,
	       grant_record.scope,
	       grant_record.reason,
	       grant_record.expires_at,
	       grant_record.revoked_at,
	       grant_record.version,
	       grant_record.created_by_user_id::text,
	       grant_record.created_at,
	       grant_record.updated_at
	FROM platform_pii_access_grants AS grant_record
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMembership(row rowScanner) (*Membership, error) {
	var item Membership
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.Role, &item.Status, &item.IsOwner, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func scanPlatformRole(row rowScanner) (*PlatformRoleAssignment, error) {
	var item PlatformRoleAssignment
	if err := row.Scan(&item.ID, &item.UserID, &item.Role, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func scanSalonAssignment(row rowScanner) (*SalonAssignment, error) {
	var item SalonAssignment
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.Status, pq.Array(&item.Permissions), &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Permissions == nil {
		item.Permissions = []string{}
	}
	return &item, nil
}

func scanPIIGrant(row rowScanner) (*PIIGrant, error) {
	var item PIIGrant
	var revokedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.Scope, &item.Reason, &item.ExpiresAt, &revokedAt, &item.Version, &item.CreatedByID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		item.RevokedAt = &value
	}
	return &item, nil
}
