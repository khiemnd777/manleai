package access

import "time"

type Surface string

type PrincipalScope string

const (
	SurfaceTenant          Surface        = "tenant"
	SurfacePlatform        Surface        = "platform"
	PrincipalScopeTenant   PrincipalScope = "tenant"
	PrincipalScopePlatform PrincipalScope = "platform"
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
	CapabilityRegistrationRead    Capability = "platform.registration_requests.read"
	CapabilityRegistrationManage  Capability = "platform.registration_requests.manage"
	CapabilityTenantProvision     Capability = "platform.tenants.provision"
	CapabilityBusinessRead        Capability = "business.read"
	CapabilityBusinessWrite       Capability = "business.write"
	CapabilityTechnicalRead       Capability = "technical.read"
	CapabilityTechnicalWrite      Capability = "technical.write"
	CapabilityOperationsRead      Capability = "operations.read"
	CapabilityOperationsWrite     Capability = "operations.write"
	CapabilityAuditRead           Capability = "audit.read"
	CapabilityServicesRead        Capability = "services.read"
	CapabilityServicesWrite       Capability = "services.write"
	CapabilityTrainingRead        Capability = "training.read"
	CapabilityTrainingWrite       Capability = "training.write"
	CapabilityCallsRead           Capability = "calls.read"
	CapabilityCallsManage         Capability = "calls.manage"
	CapabilityCallsSimulate       Capability = "calls.simulate"
	CapabilityCallsRedact         Capability = "calls.redact"
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
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	Scope           string   `json:"scope"`
	DelegationScope string   `json:"delegation_scope"`
	Requires        []string `json:"requires"`
}

type AccessUser struct {
	ID                 string         `json:"id"`
	Email              string         `json:"email"`
	FullName           string         `json:"full_name"`
	Status             string         `json:"status"`
	PrincipalScope     PrincipalScope `json:"principal_scope"`
	DataClassification string         `json:"data_classification"`
}

type ListUsersResponse struct {
	Users []AccessUser `json:"users"`
}

type Membership struct {
	ID        string     `json:"id"`
	SalonID   string     `json:"salon_id"`
	UserID    string     `json:"user_id"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	IsOwner   bool       `json:"is_owner"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	User      AccessUser `json:"user"`
}

type MembershipMutationRequest struct {
	ActionKey       string `json:"action_key"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type PlatformRoleAssignment struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	User      AccessUser `json:"user"`
}

type PlatformRoleMutationRequest struct {
	ActionKey       string `json:"action_key"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type PlatformUserCreateRequest struct {
	ActionKey    string `json:"action_key"`
	Email        string `json:"email"`
	FullName     string `json:"full_name"`
	Password     string `json:"password"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Status       string `json:"status"`
}

type PlatformUserUpdateRequest struct {
	ActionKey       string `json:"action_key"`
	Email           string `json:"email"`
	FullName        string `json:"full_name"`
	Password        string `json:"password,omitempty"`
	PasswordHash    string `json:"-"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type SalonAssignment struct {
	ID          string     `json:"id"`
	SalonID     string     `json:"salon_id"`
	UserID      string     `json:"user_id"`
	Status      string     `json:"status"`
	Permissions []string   `json:"permissions"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	User        AccessUser `json:"user"`
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
	User        AccessUser `json:"user"`
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

type SupportAccessRequest struct {
	ID                 string     `json:"id"`
	SalonID            string     `json:"salon_id"`
	PlatformUserID     string     `json:"platform_user_id"`
	RequestedByUserID  string     `json:"requested_by_user_id"`
	Status             string     `json:"status"`
	EffectiveStatus    string     `json:"effective_status"`
	Reason             string     `json:"reason"`
	Capabilities       []string   `json:"capabilities"`
	PIIScopes          []string   `json:"pii_scopes"`
	RequestedExpiresAt time.Time  `json:"requested_expires_at"`
	ApprovedExpiresAt  *time.Time `json:"approved_expires_at,omitempty"`
	DecisionByUserID   string     `json:"decision_by_user_id,omitempty"`
	DecisionAt         *time.Time `json:"decision_at,omitempty"`
	RevokedByUserID    string     `json:"revoked_by_user_id,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	Version            int64      `json:"version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	User               AccessUser `json:"user"`
}

type EffectiveSupportAccess struct {
	Capabilities []string   `json:"capabilities"`
	PIIScopes    []string   `json:"pii_scopes"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type SupportAccessRequestCreate struct {
	ActionKey    string    `json:"action_key"`
	UserID       string    `json:"user_id"`
	Capabilities []string  `json:"capabilities"`
	PIIScopes    []string  `json:"pii_scopes"`
	Reason       string    `json:"reason"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type SupportAccessDecisionRequest struct {
	ActionKey       string     `json:"action_key"`
	ExpectedVersion int64      `json:"expected_version"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type ListSupportAccessRequestsResponse struct {
	Requests []SupportAccessRequest `json:"requests"`
}

type BootstrapPlatformAdminResult struct {
	Assignment PlatformRoleAssignment `json:"assignment"`
	Replayed   bool                   `json:"replayed"`
}

type BootstrapPlatformAdminRequest struct {
	Email        string
	FullName     string
	PasswordHash string
	ActionKey    string
	Reason       string
}
