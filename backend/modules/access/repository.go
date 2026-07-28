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

func (r *Repository) ListPlatformUsers(ctx context.Context, actorUserID, query string, limit int) ([]AccessUser, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	return r.listUsersByPrincipalScope(ctx, query, limit, PrincipalScopePlatform)
}

func (r *Repository) ListTenantUsers(ctx context.Context, actorUserID, salonID, query string, limit int) ([]AccessUser, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := r.ensureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	return r.listUsersByPrincipalScope(ctx, query, limit, PrincipalScopeTenant)
}

func (r *Repository) listUsersByPrincipalScope(ctx context.Context, query string, limit int, scope PrincipalScope) ([]AccessUser, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text,email,full_name,status,principal_scope,data_classification
		FROM users
		WHERE principal_scope = $3
		  AND ($1='' OR position(lower($1) in lower(email))>0 OR position(lower($1) in lower(full_name))>0)
		ORDER BY status,full_name,email,id
		LIMIT $2
	`, query, limit, string(scope))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccessUser, 0)
	for rows.Next() {
		var item AccessUser
		if err := rows.Scan(&item.ID, &item.Email, &item.FullName, &item.Status, &item.PrincipalScope, &item.DataClassification); err != nil {
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
				  AND account.principal_scope = 'tenant'
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
					  AND account.principal_scope = 'platform'
					  AND permission.name = $2
				)
			`, actorUserID, string(check.Capability)).Scan(&allowed)
			return allowed, err
		}
		// Platform Administrators are the control-plane authority. Their active
		// role permission is sufficient for every salon-scoped Platform action;
		// salon assignments, temporary Ops grants, and PII grants do not apply.
		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users account
				JOIN platform_role_assignments assignment ON assignment.user_id=account.id AND assignment.status='active'
				JOIN roles role ON role.id=assignment.role_id AND role.name='platform_admin'
				JOIN role_permissions role_permission ON role_permission.role_id=role.id
				JOIN permissions permission ON permission.id=role_permission.permission_id
				WHERE account.id=$1 AND account.status='active' AND account.principal_scope='platform'
				  AND permission.name=$2
			)
		`, actorUserID, string(check.Capability)).Scan(&allowed)
		if err != nil || allowed {
			return allowed, err
		}

		err = r.db.QueryRowContext(ctx, `
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
		`, actorUserID, check.SalonID, string(check.Capability)).Scan(&allowed)
		if err != nil || !allowed {
			return allowed, err
		}
		if temporaryOpsAuthorizationCapability(check.Capability) {
			err = r.db.QueryRowContext(ctx, `SELECT public.app_active_support_authorization($1::uuid, $2::uuid, $3)`, actorUserID, check.SalonID, string(check.Capability)).Scan(&allowed)
			if err != nil || !allowed {
				return allowed, err
			}
		}
		if check.PIIScope == "" {
			return true, nil
		}

		if temporaryOpsAuthorizationCapability(check.Capability) {
			err = r.db.QueryRowContext(ctx, `SELECT public.app_active_support_pii_grant($1::uuid, $2::uuid, $3, $4)`, actorUserID, check.SalonID, string(check.Capability), string(check.PIIScope)).Scan(&allowed)
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

func temporaryOpsAuthorizationCapability(capability Capability) bool {
	switch capability {
	case CapabilityServicesRead, CapabilityServicesWrite, CapabilityTrainingRead, CapabilityTrainingWrite, CapabilityCallsRead, CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
		return true
	default:
		return false
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

func (r *Repository) ListPlatformSupportAccessRequests(ctx context.Context, actorUserID, salonID string) ([]SupportAccessRequest, error) {
	if err := r.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	return r.listSupportAccessRequests(ctx, salonID)
}

func (r *Repository) GetEffectiveSupportAccess(ctx context.Context, actorUserID, salonID string) (*EffectiveSupportAccess, error) {
	item := &EffectiveSupportAccess{Capabilities: []string{}, PIIScopes: []string{}}
	var isAdmin bool
	if err := r.db.QueryRowContext(ctx, platformAdminExistsQuery, actorUserID).Scan(&isAdmin); err != nil {
		return nil, err
	}
	if isAdmin {
		rows, err := r.db.QueryContext(ctx, `
			SELECT permission.name
			FROM platform_role_assignments assignment
			JOIN roles role ON role.id=assignment.role_id
			JOIN role_permissions role_permission ON role_permission.role_id=role.id
			JOIN permissions permission ON permission.id=role_permission.permission_id
			WHERE assignment.user_id=$1 AND assignment.status='active' AND role.name='platform_admin'
			  AND permission.name IN ('services.read','services.write','training.read','training.write','calls.read','calls.manage','calls.simulate','calls.redact')
			ORDER BY permission.name
		`, actorUserID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var capability string
			if err := rows.Scan(&capability); err != nil {
				return nil, err
			}
			item.Capabilities = append(item.Capabilities, capability)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		item.PIIScopes = []string{"appointments", "calls", "customers", "notifications"}
		return item, nil
	}
	err := r.db.QueryRowContext(ctx, `
		SELECT ARRAY(
		         SELECT DISTINCT permission.name
		         FROM platform_support_access_requests request
		         JOIN platform_support_access_request_permissions child ON child.request_id = request.id
		         JOIN permissions permission ON permission.id = child.permission_id
		         WHERE request.salon_id = $1
		           AND request.platform_user_id = $2
		           AND request.status = 'approved'
		           AND request.approved_expires_at > now()
		         ORDER BY permission.name
		       ),
		       ARRAY(
		         SELECT DISTINCT child.pii_scope
		         FROM platform_support_access_requests request
		         JOIN platform_support_access_request_permissions child ON child.request_id = request.id
		         JOIN platform_pii_access_grants grant_record
		           ON grant_record.support_access_request_id = request.id
		          AND grant_record.scope = child.pii_scope
		          AND grant_record.user_id = request.platform_user_id
		          AND grant_record.salon_id = request.salon_id
		          AND grant_record.revoked_at IS NULL
		          AND grant_record.expires_at > now()
		         WHERE request.salon_id = $1
		           AND request.platform_user_id = $2
		           AND request.status = 'approved'
		           AND request.approved_expires_at > now()
		           AND child.pii_scope IS NOT NULL
		         ORDER BY child.pii_scope
		       ),
		       MAX(request.approved_expires_at) FILTER (
		         WHERE request.status = 'approved' AND request.approved_expires_at > now()
		       )
		FROM platform_support_access_requests request
		WHERE request.salon_id = $1
		  AND request.platform_user_id = $2
	`, salonID, actorUserID).Scan(pq.Array(&item.Capabilities), pq.Array(&item.PIIScopes), &item.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) RecordPlatformSupportAction(ctx context.Context, actorUserID, salonID string, capability Capability, piiScope PIIScope, method, route string) error {
	details, err := json.Marshal(map[string]any{
		"capability": capability,
		"pii_scope":  piiScope,
		"method":     method,
		"route":      route,
		"outcome":    "authorized",
	})
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO access_control_events (
			actor_user_id, salon_id, event_type, object_type, object_id, details
		)
		VALUES ($1, $2, 'platform.support_action.authorized', 'platform_support_action', $3, $4)
	`, actorUserID, salonID, string(capability), string(details))
	return err
}

func (r *Repository) listSupportAccessRequests(ctx context.Context, salonID string) ([]SupportAccessRequest, error) {
	rows, err := r.db.QueryContext(ctx, supportAccessRequestSelect+`
		WHERE request.salon_id = $1
		ORDER BY request.created_at DESC, request.id DESC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SupportAccessRequest, 0)
	for rows.Next() {
		item, err := scanSupportAccessRequest(rows)
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
			UNION ALL
			SELECT id,actor_user_id,salon_id,NULL::uuid,event_type,'configuration_transfer'::text,run_id::text,details,created_at
			FROM configuration_transfer_events
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
		  AND account.principal_scope = 'platform'
		  AND role.name = 'platform_admin'
	)
`

const membershipSelect = `
	SELECT membership.id::text,
	       membership.salon_id::text,
	       membership.user_id::text,
	       account.email,
	       account.full_name,
	       account.status,
	       account.principal_scope,
	       account.data_classification,
	       role.name,
	       membership.status,
	       membership.is_owner,
	       membership.version,
	       membership.created_at,
	       membership.updated_at
	FROM salon_memberships AS membership
	JOIN users AS account ON account.id = membership.user_id
	JOIN roles AS role ON role.id = membership.role_id
`

const platformRoleSelect = `
	SELECT assignment.id::text,
	       assignment.user_id::text,
	       account.email,
	       account.full_name,
	       account.status,
	       account.principal_scope,
	       account.data_classification,
	       role.name,
	       assignment.status,
	       assignment.version,
	       assignment.created_at,
	       assignment.updated_at
	FROM platform_role_assignments AS assignment
	JOIN users AS account ON account.id = assignment.user_id
	JOIN roles AS role ON role.id = assignment.role_id
`

const salonAssignmentSelect = `
	SELECT assignment.id::text,
	       assignment.salon_id::text,
	       assignment.user_id::text,
	       account.email,
	       account.full_name,
	       account.status,
	       account.principal_scope,
	       account.data_classification,
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
	JOIN users AS account ON account.id = assignment.user_id
`

const piiGrantSelect = `
	SELECT grant_record.id::text,
	       grant_record.salon_id::text,
	       grant_record.user_id::text,
	       account.email,
	       account.full_name,
	       account.status,
	       account.principal_scope,
	       account.data_classification,
	       grant_record.scope,
	       grant_record.reason,
	       grant_record.expires_at,
	       grant_record.revoked_at,
	       grant_record.version,
	       grant_record.created_by_user_id::text,
	       grant_record.created_at,
	       grant_record.updated_at
	FROM platform_pii_access_grants AS grant_record
	JOIN users AS account ON account.id = grant_record.user_id
`

const supportAccessRequestSelect = `
	SELECT request.id::text,
	       request.salon_id::text,
	       request.platform_user_id::text,
	       request.requested_by_user_id::text,
	       request.status,
	       CASE WHEN request.status = 'approved' AND request.approved_expires_at <= now()
	            THEN 'expired' ELSE request.status END,
	       request.reason,
	       ARRAY(
	           SELECT permission.name
	           FROM platform_support_access_request_permissions child
	           JOIN permissions permission ON permission.id = child.permission_id
	           WHERE child.request_id = request.id AND child.permission_id IS NOT NULL
	           ORDER BY permission.name
	       ),
	       ARRAY(
	           SELECT child.pii_scope
	           FROM platform_support_access_request_permissions child
	           WHERE child.request_id = request.id AND child.pii_scope IS NOT NULL
	           ORDER BY child.pii_scope
	       ),
	       request.requested_expires_at,
	       request.approved_expires_at,
	       COALESCE(request.decision_by_user_id::text, ''),
	       request.decision_at,
	       COALESCE(request.revoked_by_user_id::text, ''),
	       request.revoked_at,
	       request.version,
	       request.created_at,
	       request.updated_at,
	       account.email,
	       account.full_name,
	       account.status,
	       account.principal_scope,
	       account.data_classification
	FROM platform_support_access_requests request
	JOIN users account ON account.id = request.platform_user_id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMembership(row rowScanner) (*Membership, error) {
	var item Membership
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.User.Email, &item.User.FullName, &item.User.Status, &item.User.PrincipalScope, &item.User.DataClassification, &item.Role, &item.Status, &item.IsOwner, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.User.ID = item.UserID
	return &item, nil
}

func scanPlatformRole(row rowScanner) (*PlatformRoleAssignment, error) {
	var item PlatformRoleAssignment
	if err := row.Scan(&item.ID, &item.UserID, &item.User.Email, &item.User.FullName, &item.User.Status, &item.User.PrincipalScope, &item.User.DataClassification, &item.Role, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.User.ID = item.UserID
	return &item, nil
}

func scanSalonAssignment(row rowScanner) (*SalonAssignment, error) {
	var item SalonAssignment
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.User.Email, &item.User.FullName, &item.User.Status, &item.User.PrincipalScope, &item.User.DataClassification, &item.Status, pq.Array(&item.Permissions), &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.User.ID = item.UserID
	if item.Permissions == nil {
		item.Permissions = []string{}
	}
	return &item, nil
}

func scanPIIGrant(row rowScanner) (*PIIGrant, error) {
	var item PIIGrant
	var revokedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.SalonID, &item.UserID, &item.User.Email, &item.User.FullName, &item.User.Status, &item.User.PrincipalScope, &item.User.DataClassification, &item.Scope, &item.Reason, &item.ExpiresAt, &revokedAt, &item.Version, &item.CreatedByID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.User.ID = item.UserID
	if revokedAt.Valid {
		value := revokedAt.Time
		item.RevokedAt = &value
	}
	return &item, nil
}

func scanSupportAccessRequest(row rowScanner) (*SupportAccessRequest, error) {
	var item SupportAccessRequest
	var approvedAt, decisionAt, revokedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.SalonID, &item.PlatformUserID, &item.RequestedByUserID,
		&item.Status, &item.EffectiveStatus, &item.Reason,
		pq.Array(&item.Capabilities), pq.Array(&item.PIIScopes),
		&item.RequestedExpiresAt, &approvedAt, &item.DecisionByUserID, &decisionAt,
		&item.RevokedByUserID, &revokedAt, &item.Version, &item.CreatedAt, &item.UpdatedAt,
		&item.User.Email, &item.User.FullName, &item.User.Status,
		&item.User.PrincipalScope, &item.User.DataClassification,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.User.ID = item.PlatformUserID
	if item.Capabilities == nil {
		item.Capabilities = []string{}
	}
	if item.PIIScopes == nil {
		item.PIIScopes = []string{}
	}
	if approvedAt.Valid {
		value := approvedAt.Time
		item.ApprovedExpiresAt = &value
	}
	if decisionAt.Valid {
		value := decisionAt.Time
		item.DecisionAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		item.RevokedAt = &value
	}
	return &item, nil
}
