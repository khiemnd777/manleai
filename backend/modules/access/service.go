package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrValidation       = errors.New("access validation failed")
	ErrForbidden        = errors.New("access forbidden")
	ErrNotFound         = errors.New("access record not found")
	ErrVersionConflict  = errors.New("access version conflict")
	ErrActionConflict   = errors.New("access action conflict")
	ErrBootstrapClosed  = errors.New("platform administrator bootstrap is closed")
	ErrLastAdmin        = errors.New("last platform administrator cannot be revoked")
	ErrIdentityConflict = errors.New("access identity conflict")
)

const (
	maxActionKeyLength = 256
	maxReasonLength    = 128
	maxPIIGrantTTL     = 24 * time.Hour
	maxSupportGrantTTL = 30 * 24 * time.Hour
)

var changeReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type Store interface {
	Evaluate(ctx context.Context, actorUserID string, check AccessCheck) (bool, error)
	ListPlatformUsers(ctx context.Context, actorUserID, query string, limit int) ([]AccessUser, error)
	ListTenantUsers(ctx context.Context, actorUserID, salonID, query string, limit int) ([]AccessUser, error)
	ListCapabilities(ctx context.Context, actorUserID string) ([]CapabilityDefinition, error)
	ListMemberships(ctx context.Context, actorUserID, salonID string) ([]Membership, error)
	MutateMembership(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req MembershipMutationRequest) (*Membership, bool, error)
	ListPlatformRoles(ctx context.Context, actorUserID string) ([]PlatformRoleAssignment, error)
	CreatePlatformUser(ctx context.Context, actorUserID, fingerprint string, req PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error)
	UpdatePlatformUser(ctx context.Context, actorUserID, targetUserID, fingerprint string, req PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error)
	MutatePlatformRole(ctx context.Context, actorUserID, targetUserID, fingerprint string, req PlatformRoleMutationRequest) (*PlatformRoleAssignment, bool, error)
	ListSalonAssignments(ctx context.Context, actorUserID, salonID string) ([]SalonAssignment, error)
	MutateSalonAssignment(ctx context.Context, actorUserID, salonID, targetUserID, fingerprint string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error)
	ListPIIGrants(ctx context.Context, actorUserID, salonID string) ([]PIIGrant, error)
	GrantPIIAccess(ctx context.Context, actorUserID, salonID, fingerprint string, req PIIGrantRequest) (*PIIGrant, bool, error)
	RevokePIIAccess(ctx context.Context, actorUserID, salonID, grantID, fingerprint string, req PIIGrantRevokeRequest) (*PIIGrant, bool, error)
	ListAuditEvents(ctx context.Context, actorUserID, salonID string, limit, offset int) ([]AuditEvent, bool, error)
	ListPlatformSupportAccessRequests(ctx context.Context, actorUserID, salonID string) ([]SupportAccessRequest, error)
	CreateSupportAccessRequest(ctx context.Context, actorUserID, salonID, fingerprint string, req SupportAccessRequestCreate) (*SupportAccessRequest, bool, error)
	CancelSupportAccessRequest(ctx context.Context, actorUserID, salonID, requestID, fingerprint string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error)
	RevokeSupportAccessRequest(ctx context.Context, actorUserID, salonID, requestID, fingerprint string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error)
}

type effectiveSupportAccessStore interface {
	GetEffectiveSupportAccess(ctx context.Context, actorUserID, salonID string) (*EffectiveSupportAccess, error)
}

type platformSupportActionStore interface {
	RecordPlatformSupportAction(ctx context.Context, actorUserID, salonID string, capability Capability, piiScope PIIScope, method, route string) error
}

func normalizeUserDirectoryQuery(query string, limit int) (string, int, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 25
	}
	if limit > 50 || len([]rune(query)) > 120 {
		return "", 0, ErrValidation
	}
	return query, limit, nil
}

func (s *Service) ListPlatformUsers(ctx context.Context, actor middleware.ActorContext, query string, limit int) (*ListUsersResponse, error) {
	query, limit, err := normalizeUserDirectoryQuery(query, limit)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListPlatformUsers(ctx, actor.UserID, query, limit)
	if err != nil {
		return nil, err
	}
	return &ListUsersResponse{Users: items}, nil
}

func (s *Service) ListTenantUsers(ctx context.Context, actor middleware.ActorContext, salonID, query string, limit int) (*ListUsersResponse, error) {
	salonID = strings.TrimSpace(salonID)
	query, limit, err := normalizeUserDirectoryQuery(query, limit)
	if err != nil || salonID == "" {
		return nil, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListTenantUsers(ctx, actor.UserID, salonID, query, limit)
	if err != nil {
		return nil, err
	}
	return &ListUsersResponse{Users: items}, nil
}

type Service struct {
	repo Store
	now  func() time.Time
}

func NewService(repo Store) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Authorize(ctx context.Context, actor middleware.ActorContext, check AccessCheck) error {
	if strings.TrimSpace(actor.UserID) == "" || !validAccessCheck(check) {
		return ErrForbidden
	}
	allowed, err := s.repo.Evaluate(ctx, actor.UserID, check)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) ListMemberships(ctx context.Context, actor middleware.ActorContext, salonID string) (*ListMembershipsResponse, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListMemberships(ctx, actor.UserID, strings.TrimSpace(salonID))
	if err != nil {
		return nil, err
	}
	return &ListMembershipsResponse{Memberships: items}, nil
}

func (s *Service) MutateMembership(ctx context.Context, actor middleware.ActorContext, salonID, targetUserID string, req MembershipMutationRequest) (*Membership, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Role = strings.TrimSpace(req.Role)
	req.Status = normalizeStatus(req.Status)
	salonID = strings.TrimSpace(salonID)
	targetUserID = strings.TrimSpace(targetUserID)
	if !validActionKey(req.ActionKey) || salonID == "" || targetUserID == "" || req.ExpectedVersion < 0 || !validTenantRole(req.Role) || !validStatus(req.Status) {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		SalonID         string `json:"salon_id"`
		TargetUserID    string `json:"target_user_id"`
		Role            string `json:"role"`
		Status          string `json:"status"`
		ExpectedVersion int64  `json:"expected_version"`
	}{salonID, targetUserID, req.Role, req.Status, req.ExpectedVersion})
	if err != nil {
		return nil, false, err
	}
	return s.repo.MutateMembership(ctx, actor.UserID, salonID, targetUserID, fingerprint, req)
}

func (s *Service) ListPlatformRoles(ctx context.Context, actor middleware.ActorContext) (*ListPlatformRolesResponse, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListPlatformRoles(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	return &ListPlatformRolesResponse{Assignments: items}, nil
}

func (s *Service) ListCapabilities(ctx context.Context, actor middleware.ActorContext) (*ListCapabilitiesResponse, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListCapabilities(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Requires = capabilityRequirements(Capability(items[index].Name))
	}
	return &ListCapabilitiesResponse{Capabilities: items}, nil
}

func (s *Service) MutatePlatformRole(ctx context.Context, actor middleware.ActorContext, targetUserID string, req PlatformRoleMutationRequest) (*PlatformRoleAssignment, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Role = strings.TrimSpace(req.Role)
	req.Status = normalizeStatus(req.Status)
	targetUserID = strings.TrimSpace(targetUserID)
	if !validActionKey(req.ActionKey) || targetUserID == "" || req.ExpectedVersion < 0 || !validPlatformRole(req.Role) || !validStatus(req.Status) {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		TargetUserID    string `json:"target_user_id"`
		Role            string `json:"role"`
		Status          string `json:"status"`
		ExpectedVersion int64  `json:"expected_version"`
	}{targetUserID, req.Role, req.Status, req.ExpectedVersion})
	if err != nil {
		return nil, false, err
	}
	return s.repo.MutatePlatformRole(ctx, actor.UserID, targetUserID, fingerprint, req)
}

func (s *Service) CreatePlatformUser(ctx context.Context, actor middleware.ActorContext, req PlatformUserCreateRequest) (*PlatformRoleAssignment, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Email = normalizePlatformEmail(req.Email)
	req.FullName = strings.TrimSpace(req.FullName)
	req.Role = strings.TrimSpace(req.Role)
	req.Status = normalizeStatus(req.Status)
	if !validActionKey(req.ActionKey) || !validPlatformEmail(req.Email) || req.FullName == "" || len([]rune(req.FullName)) > 160 || len(req.Password) < 8 || !validPlatformRole(req.Role) || !validStatus(req.Status) {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, false, err
	}
	req.PasswordHash = string(hash)
	fingerprint, err := requestFingerprint(struct {
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		Password string `json:"password_fingerprint"`
		Role     string `json:"role"`
		Status   string `json:"status"`
	}{req.Email, req.FullName, passwordFingerprint(req.Password), req.Role, req.Status})
	if err != nil {
		return nil, false, err
	}
	req.Password = ""
	return s.repo.CreatePlatformUser(ctx, actor.UserID, fingerprint, req)
}

func (s *Service) UpdatePlatformUser(ctx context.Context, actor middleware.ActorContext, targetUserID string, req PlatformUserUpdateRequest) (*PlatformRoleAssignment, bool, error) {
	targetUserID = strings.TrimSpace(targetUserID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Email = normalizePlatformEmail(req.Email)
	req.FullName = strings.TrimSpace(req.FullName)
	req.Role = strings.TrimSpace(req.Role)
	req.Status = normalizeStatus(req.Status)
	if targetUserID == "" || !validActionKey(req.ActionKey) || !validPlatformEmail(req.Email) || req.FullName == "" || len([]rune(req.FullName)) > 160 || (req.Password != "" && len(req.Password) < 8) || !validPlatformRole(req.Role) || !validStatus(req.Status) || req.ExpectedVersion < 1 {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, false, err
		}
		req.PasswordHash = string(hash)
	}
	fingerprint, err := requestFingerprint(struct {
		TargetUserID    string `json:"target_user_id"`
		Email           string `json:"email"`
		FullName        string `json:"full_name"`
		Password        string `json:"password_fingerprint,omitempty"`
		Role            string `json:"role"`
		Status          string `json:"status"`
		ExpectedVersion int64  `json:"expected_version"`
	}{targetUserID, req.Email, req.FullName, passwordFingerprint(req.Password), req.Role, req.Status, req.ExpectedVersion})
	if err != nil {
		return nil, false, err
	}
	req.Password = ""
	return s.repo.UpdatePlatformUser(ctx, actor.UserID, targetUserID, fingerprint, req)
}

func normalizePlatformEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validPlatformEmail(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(parsed.Address, value) && len(value) <= 320
}

func passwordFingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ListSalonAssignments(ctx context.Context, actor middleware.ActorContext, salonID string) (*ListSalonAssignmentsResponse, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListSalonAssignments(ctx, actor.UserID, strings.TrimSpace(salonID))
	if err != nil {
		return nil, err
	}
	return &ListSalonAssignmentsResponse{Assignments: items}, nil
}

func (s *Service) MutateSalonAssignment(ctx context.Context, actor middleware.ActorContext, salonID, targetUserID string, req SalonAssignmentMutationRequest) (*SalonAssignment, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Status = normalizeStatus(req.Status)
	req.Permissions = normalizedAssignmentPermissions(req.Permissions)
	salonID = strings.TrimSpace(salonID)
	targetUserID = strings.TrimSpace(targetUserID)
	if !validActionKey(req.ActionKey) || salonID == "" || targetUserID == "" || req.ExpectedVersion < 0 || !validStatus(req.Status) || !validAssignmentPermissions(req.Permissions) {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		SalonID         string   `json:"salon_id"`
		TargetUserID    string   `json:"target_user_id"`
		Status          string   `json:"status"`
		Permissions     []string `json:"permissions"`
		ExpectedVersion int64    `json:"expected_version"`
	}{salonID, targetUserID, req.Status, req.Permissions, req.ExpectedVersion})
	if err != nil {
		return nil, false, err
	}
	return s.repo.MutateSalonAssignment(ctx, actor.UserID, salonID, targetUserID, fingerprint, req)
}

func (s *Service) ListPIIGrants(ctx context.Context, actor middleware.ActorContext, salonID string) (*ListPIIGrantsResponse, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListPIIGrants(ctx, actor.UserID, strings.TrimSpace(salonID))
	if err != nil {
		return nil, err
	}
	return &ListPIIGrantsResponse{Grants: items}, nil
}

func (s *Service) GrantPIIAccess(ctx context.Context, actor middleware.ActorContext, salonID string, req PIIGrantRequest) (*PIIGrant, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Scope = strings.TrimSpace(req.Scope)
	req.Reason = strings.TrimSpace(req.Reason)
	req.ExpiresAt = req.ExpiresAt.UTC()
	salonID = strings.TrimSpace(salonID)
	now := s.now()
	if !validActionKey(req.ActionKey) || salonID == "" || req.UserID == "" || !validPIIScope(PIIScope(req.Scope)) || !validChangeReference(req.Reason) || !req.ExpiresAt.After(now) || req.ExpiresAt.After(now.Add(maxPIIGrantTTL)) {
		return nil, false, ErrValidation
	}
	if PIIScope(req.Scope) == PIIScopeCalls {
		return nil, false, ErrForbidden
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		SalonID   string    `json:"salon_id"`
		UserID    string    `json:"user_id"`
		Scope     string    `json:"scope"`
		Reason    string    `json:"reason"`
		ExpiresAt time.Time `json:"expires_at"`
	}{salonID, req.UserID, req.Scope, req.Reason, req.ExpiresAt})
	if err != nil {
		return nil, false, err
	}
	return s.repo.GrantPIIAccess(ctx, actor.UserID, salonID, fingerprint, req)
}

func (s *Service) RevokePIIAccess(ctx context.Context, actor middleware.ActorContext, salonID, grantID string, req PIIGrantRevokeRequest) (*PIIGrant, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	salonID = strings.TrimSpace(salonID)
	grantID = strings.TrimSpace(grantID)
	if !validActionKey(req.ActionKey) || salonID == "" || grantID == "" || req.ExpectedVersion < 1 {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		SalonID         string `json:"salon_id"`
		GrantID         string `json:"grant_id"`
		ExpectedVersion int64  `json:"expected_version"`
	}{salonID, grantID, req.ExpectedVersion})
	if err != nil {
		return nil, false, err
	}
	return s.repo.RevokePIIAccess(ctx, actor.UserID, salonID, grantID, fingerprint, req)
}

func (s *Service) ListAuditEvents(ctx context.Context, actor middleware.ActorContext, salonID string, limit, offset int) (*ListAuditEventsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 || offset < 0 {
		return nil, ErrValidation
	}
	if salonID == "" {
		if err := s.requireAccessAdmin(ctx, actor); err != nil {
			return nil, err
		}
	} else if err := s.Authorize(ctx, actor, AccessCheck{Surface: SurfacePlatform, SalonID: salonID, Capability: CapabilityAuditRead}); err != nil {
		return nil, err
	}
	events, hasMore, err := s.repo.ListAuditEvents(ctx, actor.UserID, salonID, limit, offset)
	if err != nil {
		return nil, err
	}
	return &ListAuditEventsResponse{Events: events, Limit: limit, Offset: offset, HasMore: hasMore}, nil
}

func (s *Service) ListPlatformSupportAccessRequests(ctx context.Context, actor middleware.ActorContext, salonID string) (*ListSupportAccessRequestsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return nil, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, err
	}
	items, err := s.repo.ListPlatformSupportAccessRequests(ctx, actor.UserID, salonID)
	return &ListSupportAccessRequestsResponse{Requests: nonNilSupportRequests(items)}, err
}

func (s *Service) GetEffectiveSupportAccess(ctx context.Context, actor middleware.ActorContext, salonID string) (*EffectiveSupportAccess, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || actor.PrincipalScope != middleware.PrincipalScopePlatform {
		return nil, ErrForbidden
	}
	store, ok := s.repo.(effectiveSupportAccessStore)
	if !ok {
		return nil, ErrNotFound
	}
	return store.GetEffectiveSupportAccess(ctx, actor.UserID, salonID)
}

func (s *Service) RecordPlatformSupportAction(ctx context.Context, actor middleware.ActorContext, salonID string, capability Capability, piiScope PIIScope, method, route string) error {
	salonID = strings.TrimSpace(salonID)
	method = strings.TrimSpace(method)
	route = strings.TrimSpace(route)
	if actor.PrincipalScope != middleware.PrincipalScopePlatform || salonID == "" || method == "" || route == "" || !validCapability(capability) || (piiScope != "" && !validPIIScope(piiScope)) {
		return ErrForbidden
	}
	store, ok := s.repo.(platformSupportActionStore)
	if !ok {
		return ErrNotFound
	}
	return store.RecordPlatformSupportAction(ctx, actor.UserID, salonID, capability, piiScope, method, route)
}

func (s *Service) CreateSupportAccessRequest(ctx context.Context, actor middleware.ActorContext, salonID string, req SupportAccessRequestCreate) (*SupportAccessRequest, bool, error) {
	salonID = strings.TrimSpace(salonID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Capabilities = normalizedAssignmentPermissions(req.Capabilities)
	req.PIIScopes = normalizedSupportPIIScopes(req.PIIScopes)
	req.ExpiresAt = req.ExpiresAt.UTC()
	maxTTL := maxSupportGrantTTL
	if len(req.PIIScopes) > 0 {
		maxTTL = maxPIIGrantTTL
	}
	now := s.now()
	if salonID == "" || req.UserID == "" || !validActionKey(req.ActionKey) || !validChangeReference(req.Reason) || len(req.Capabilities) == 0 || !validSupportCapabilities(req.Capabilities) || !validSupportPIIScopes(req.PIIScopes) || !req.ExpiresAt.After(now) || req.ExpiresAt.After(now.Add(maxTTL)) {
		return nil, false, ErrValidation
	}
	if len(req.PIIScopes) > 0 && !containsString(req.Capabilities, string(CapabilityCallsRead)) && !containsString(req.Capabilities, string(CapabilityTrainingRead)) {
		return nil, false, ErrValidation
	}
	if containsCapabilityPrefix(req.Capabilities, "calls.") && !containsString(req.PIIScopes, string(PIIScopeCalls)) {
		return nil, false, ErrValidation
	}
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	payload := struct {
		SalonID      string    `json:"salon_id"`
		UserID       string    `json:"user_id"`
		Capabilities []string  `json:"capabilities"`
		PIIScopes    []string  `json:"pii_scopes"`
		Reason       string    `json:"reason"`
		ExpiresAt    time.Time `json:"expires_at"`
	}{salonID, req.UserID, req.Capabilities, req.PIIScopes, req.Reason, req.ExpiresAt}
	fingerprint, err := requestFingerprint(payload)
	if err != nil {
		return nil, false, err
	}
	return s.repo.CreateSupportAccessRequest(ctx, actor.UserID, salonID, fingerprint, req)
}

func (s *Service) CancelSupportAccessRequest(ctx context.Context, actor middleware.ActorContext, salonID, requestID string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	return s.supportDecision(ctx, actor, salonID, requestID, "cancel", req)
}

func (s *Service) RevokeSupportAccessRequest(ctx context.Context, actor middleware.ActorContext, salonID, requestID string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	if err := s.requireAccessAdmin(ctx, actor); err != nil {
		return nil, false, err
	}
	return s.supportDecision(ctx, actor, salonID, requestID, "revoke", req)
}

func (s *Service) supportDecision(ctx context.Context, actor middleware.ActorContext, salonID, requestID, decision string, req SupportAccessDecisionRequest) (*SupportAccessRequest, bool, error) {
	salonID = strings.TrimSpace(salonID)
	requestID = strings.TrimSpace(requestID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if salonID == "" || requestID == "" || !validActionKey(req.ActionKey) || req.ExpectedVersion < 1 {
		return nil, false, ErrValidation
	}
	payload := struct {
		SalonID         string     `json:"salon_id"`
		RequestID       string     `json:"request_id"`
		Decision        string     `json:"decision"`
		ExpectedVersion int64      `json:"expected_version"`
		ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	}{salonID, requestID, decision, req.ExpectedVersion, req.ExpiresAt}
	fingerprint, err := requestFingerprint(payload)
	if err != nil {
		return nil, false, err
	}
	if decision == "cancel" {
		if err := s.requireAccessAdmin(ctx, actor); err != nil {
			return nil, false, err
		}
		return s.repo.CancelSupportAccessRequest(ctx, actor.UserID, salonID, requestID, fingerprint, req)
	}
	return s.repo.RevokeSupportAccessRequest(ctx, actor.UserID, salonID, requestID, fingerprint, req)
}

func nonNilSupportRequests(items []SupportAccessRequest) []SupportAccessRequest {
	if items == nil {
		return []SupportAccessRequest{}
	}
	return items
}

func (s *Service) requireAccessAdmin(ctx context.Context, actor middleware.ActorContext) error {
	return s.Authorize(ctx, actor, AccessCheck{Surface: SurfacePlatform, Capability: CapabilityPlatformAccess})
}

func validAccessCheck(check AccessCheck) bool {
	if check.Surface != SurfaceTenant && check.Surface != SurfacePlatform {
		return false
	}
	if !validCapability(check.Capability) {
		return false
	}
	if check.Surface == SurfaceTenant && !validTenantCapability(check.Capability) {
		return false
	}
	if check.SalonID == "" {
		if check.Surface != SurfacePlatform || !validGlobalPlatformCapability(check.Capability) || check.PIIScope != "" {
			return false
		}
	} else if check.Surface == SurfacePlatform && validGlobalPlatformCapability(check.Capability) {
		return false
	}
	if check.PIIScope != "" {
		if !validPIIScope(check.PIIScope) {
			return false
		}
		if check.Surface == SurfacePlatform {
			switch check.PIIScope {
			case PIIScopeCustomers:
				if check.Capability != CapabilityBusinessRead {
					return false
				}
			case PIIScopeCalls:
				if check.Capability != CapabilityOperationsRead && check.Capability != CapabilityCallsRead && check.Capability != CapabilityCallsManage && check.Capability != CapabilityCallsSimulate && check.Capability != CapabilityCallsRedact && check.Capability != CapabilityTrainingRead && check.Capability != CapabilityTrainingWrite {
					return false
				}
			case PIIScopeNotifications:
				if check.Capability != CapabilityOperationsRead {
					return false
				}
			case PIIScopeAppointments:
				if check.Capability != CapabilityBusinessRead && check.Capability != CapabilityOperationsRead {
					return false
				}
			}
		} else if check.Capability != CapabilityBusinessRead && check.Capability != CapabilityCallsRead && check.Capability != CapabilityCallsManage && check.Capability != CapabilityCallsSimulate && check.Capability != CapabilityCallsRedact && check.Capability != CapabilityTrainingRead && check.Capability != CapabilityTrainingWrite {
			return false
		}
	}
	return true
}

func validCapability(value Capability) bool {
	switch value {
	case CapabilityPlatformTenantsRead, CapabilityPlatformAccess, CapabilityRegistrationRead, CapabilityRegistrationManage, CapabilityTenantProvision, CapabilityBusinessRead, CapabilityBusinessWrite, CapabilityTechnicalRead, CapabilityTechnicalWrite, CapabilityOperationsRead, CapabilityOperationsWrite, CapabilityAuditRead, CapabilityServicesRead, CapabilityServicesWrite, CapabilityTrainingRead, CapabilityTrainingWrite, CapabilityCallsRead, CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
		return true
	default:
		return false
	}
}

func validGlobalPlatformCapability(value Capability) bool {
	switch value {
	case CapabilityPlatformTenantsRead, CapabilityPlatformAccess, CapabilityRegistrationRead, CapabilityRegistrationManage, CapabilityTenantProvision:
		return true
	default:
		return false
	}
}

func validTenantCapability(value Capability) bool {
	switch value {
	case CapabilityBusinessRead, CapabilityBusinessWrite, CapabilityServicesRead, CapabilityServicesWrite, CapabilityTrainingRead, CapabilityTrainingWrite, CapabilityCallsRead, CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
		return true
	default:
		return false
	}
}

func validPIIScope(value PIIScope) bool {
	switch value {
	case PIIScopeCustomers, PIIScopeCalls, PIIScopeAppointments, PIIScopeNotifications:
		return true
	default:
		return false
	}
}

func validTenantRole(value string) bool {
	return value == RoleTenantOwner || value == RoleTenantBusinessManager
}

func validPlatformRole(value string) bool {
	return value == RolePlatformAdmin || value == RolePlatformOps
}

func normalizeStatus(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "active"
	}
	return value
}

func validStatus(value string) bool {
	return value == "active" || value == "revoked"
}

func validActionKey(value string) bool {
	return value == strings.TrimSpace(value) && len(value) >= 1 && len(value) <= maxActionKeyLength
}

func validChangeReference(value string) bool {
	return len(value) >= 1 && len(value) <= maxReasonLength && changeReferencePattern.MatchString(value)
}

func normalizedAssignmentPermissions(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsCapabilityPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func validAssignmentPermissions(values []string) bool {
	present := make(map[string]bool, len(values))
	for _, value := range values {
		if !validDelegableCapability(Capability(value)) {
			return false
		}
		present[value] = true
	}
	for _, value := range values {
		for _, required := range capabilityRequirements(Capability(value)) {
			if !present[required] {
				return false
			}
		}
	}
	return true
}

func capabilityRequirements(value Capability) []string {
	switch value {
	case CapabilityBusinessWrite:
		return []string{string(CapabilityBusinessRead)}
	case CapabilityTechnicalWrite:
		return []string{string(CapabilityTechnicalRead)}
	case CapabilityOperationsWrite:
		return []string{string(CapabilityOperationsRead)}
	case CapabilityServicesWrite:
		return []string{string(CapabilityServicesRead)}
	case CapabilityTrainingWrite:
		return []string{string(CapabilityTrainingRead), string(CapabilityServicesRead)}
	case CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
		return []string{string(CapabilityCallsRead)}
	default:
		return []string{}
	}
}

func validDelegableCapability(value Capability) bool {
	switch value {
	case CapabilityBusinessRead, CapabilityBusinessWrite, CapabilityTechnicalRead, CapabilityTechnicalWrite, CapabilityOperationsRead, CapabilityOperationsWrite, CapabilityAuditRead, CapabilityServicesRead, CapabilityServicesWrite, CapabilityTrainingRead, CapabilityTrainingWrite, CapabilityCallsRead, CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
		return true
	default:
		return false
	}
}

func validSupportCapabilities(values []string) bool {
	present := make(map[string]bool, len(values))
	for _, value := range values {
		switch Capability(value) {
		case CapabilityServicesRead, CapabilityServicesWrite, CapabilityTrainingRead, CapabilityTrainingWrite, CapabilityCallsRead, CapabilityCallsManage, CapabilityCallsSimulate, CapabilityCallsRedact:
			present[value] = true
		default:
			return false
		}
	}
	for _, value := range values {
		for _, required := range capabilityRequirements(Capability(value)) {
			if !present[required] {
				return false
			}
		}
	}
	return true
}

func normalizedSupportPIIScopes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validSupportPIIScopes(values []string) bool {
	for _, value := range values {
		if value != string(PIIScopeCalls) {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func requestFingerprint(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
