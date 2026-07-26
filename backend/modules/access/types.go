package access

import "time"

type Surface string

const (
	SurfaceTenant   Surface = "tenant"
	SurfacePlatform Surface = "platform"
)

const (
	RoleTenantOwner           = "tenant_owner"
	RoleTenantBusinessManager = "tenant_business_manager"
	RolePlatformAdmin         = "platform_admin"
	RolePlatformOps           = "platform_ops"
)

type Capability string

const (
	CapabilityPlatformTenantsRead Capability = "platform.tenants.read"
	CapabilityPlatformAccess      Capability = "platform.access.manage"
	CapabilityBusinessRead        Capability = "business.read"
	CapabilityBusinessWrite       Capability = "business.write"
	CapabilityTechnicalRead       Capability = "technical.read"
	CapabilityTechnicalWrite      Capability = "technical.write"
	CapabilityOperationsRead      Capability = "operations.read"
	CapabilityOperationsWrite     Capability = "operations.write"
	CapabilityAuditRead           Capability = "audit.read"
)

type PIIScope string

const (
	PIIScopeCustomers     PIIScope = "customers"
	PIIScopeCalls         PIIScope = "calls"
	PIIScopeAppointments  PIIScope = "appointments"
	PIIScopeNotifications PIIScope = "notifications"
)

type AccessCheck struct {
	Surface    Surface
	SalonID    string
	Capability Capability
	PIIScope   PIIScope
}

type CapabilityDefinition struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Scope           string `json:"scope"`
	DelegationScope string `json:"delegation_scope"`
}

type AccessUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Status   string `json:"status"`
}

type ListUsersResponse struct {
	Users []AccessUser `json:"users"`
}

type Membership struct {
	ID        string    `json:"id"`
	SalonID   string    `json:"salon_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	IsOwner   bool      `json:"is_owner"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MembershipMutationRequest struct {
	ActionKey       string `json:"action_key"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type PlatformRoleAssignment struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlatformRoleMutationRequest struct {
	ActionKey       string `json:"action_key"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type SalonAssignment struct {
	ID          string    `json:"id"`
	SalonID     string    `json:"salon_id"`
	UserID      string    `json:"user_id"`
	Status      string    `json:"status"`
	Permissions []string  `json:"permissions"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SalonAssignmentMutationRequest struct {
	ActionKey       string   `json:"action_key"`
	Status          string   `json:"status"`
	Permissions     []string `json:"permissions"`
	ExpectedVersion int64    `json:"expected_version"`
}

type PIIGrant struct {
	ID          string     `json:"id"`
	SalonID     string     `json:"salon_id"`
	UserID      string     `json:"user_id"`
	Scope       string     `json:"scope"`
	Reason      string     `json:"reason"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	Version     int64      `json:"version"`
	CreatedByID string     `json:"created_by_user_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type PIIGrantRequest struct {
	ActionKey string    `json:"action_key"`
	UserID    string    `json:"user_id"`
	Scope     string    `json:"scope"`
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PIIGrantRevokeRequest struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

type AuditEvent struct {
	ID           string         `json:"id"`
	ActorUserID  string         `json:"actor_user_id,omitempty"`
	SalonID      string         `json:"salon_id,omitempty"`
	TargetUserID string         `json:"target_user_id,omitempty"`
	EventType    string         `json:"event_type"`
	ObjectType   string         `json:"object_type"`
	ObjectID     string         `json:"object_id"`
	Details      map[string]any `json:"details"`
	CreatedAt    time.Time      `json:"created_at"`
}

type ListMembershipsResponse struct {
	Memberships []Membership `json:"memberships"`
}

type ListPlatformRolesResponse struct {
	Assignments []PlatformRoleAssignment `json:"assignments"`
}

type ListCapabilitiesResponse struct {
	Capabilities []CapabilityDefinition `json:"capabilities"`
}

type ListSalonAssignmentsResponse struct {
	Assignments []SalonAssignment `json:"assignments"`
}

type ListPIIGrantsResponse struct {
	Grants []PIIGrant `json:"grants"`
}

type ListAuditEventsResponse struct {
	Events  []AuditEvent `json:"events"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"has_more"`
}

type BootstrapPlatformAdminResult struct {
	Assignment PlatformRoleAssignment `json:"assignment"`
	Replayed   bool                   `json:"replayed"`
}
