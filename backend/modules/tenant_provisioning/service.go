package tenant_provisioning

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
	registration "github.com/manleai/ai-receptionist/modules/tenant_registration"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrValidation            = errors.New("tenant provisioning validation failed")
	ErrForbidden             = errors.New("tenant provisioning forbidden")
	ErrNotFound              = errors.New("tenant registration not found")
	ErrVersionConflict       = errors.New("tenant provisioning version conflict")
	ErrActionConflict        = errors.New("tenant provisioning action conflict")
	ErrStatusConflict        = errors.New("tenant registration is not ready for provisioning")
	ErrIdentityConflict      = errors.New("tenant owner identity conflicts with current data")
	ErrInvitationUnavailable = errors.New("owner invitation is unavailable")
	ErrInvitationInvalid     = errors.New("owner invitation is invalid")
)

type Store interface {
	Provision(context.Context, string, string, string, ProvisionRequest) (*ProvisionResult, error)
	CreateInvitation(context.Context, string, string, string, string, string, InvitationRequest) (*InvitationResult, error)
	AcceptInvitation(context.Context, string, string) error
	SearchTenantIdentities(context.Context, string, int) ([]TenantIdentity, error)
}
type authorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}
type Service struct {
	repo   Store
	access authorizer
}

func NewService(repo Store, accessService authorizer) *Service {
	return &Service{repo: repo, access: accessService}
}

func (s *Service) Provision(ctx context.Context, actor middleware.ActorContext, requestID string, req ProvisionRequest) (*ProvisionResult, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return nil, err
	}
	requestID = strings.TrimSpace(requestID)
	req = normalizeProvision(req)
	if _, err := uuid.Parse(requestID); err != nil || !validActionKey(req.ActionKey) || req.ExpectedVersion < 1 || !validOwner(req.Owner) || !validSalon(req.Salon) {
		return nil, ErrValidation
	}
	fp, err := hashJSON(struct {
		RequestID       string             `json:"request_id"`
		ExpectedVersion int64              `json:"expected_version"`
		Owner           OwnerIdentityInput `json:"owner"`
		Salon           SalonProfileInput  `json:"salon"`
	}{requestID, req.ExpectedVersion, req.Owner, req.Salon})
	if err != nil {
		return nil, err
	}
	return s.repo.Provision(ctx, actor.UserID, requestID, fp, req)
}
func (s *Service) CreateInvitation(ctx context.Context, actor middleware.ActorContext, requestID string, req InvitationRequest) (*InvitationResult, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return nil, err
	}
	requestID, req.ActionKey = strings.TrimSpace(requestID), strings.TrimSpace(req.ActionKey)
	if _, err := uuid.Parse(requestID); err != nil || !validActionKey(req.ActionKey) || req.ExpectedVersion < 1 {
		return nil, ErrValidation
	}
	fp, err := hashJSON(struct {
		RequestID       string `json:"request_id"`
		ExpectedVersion int64  `json:"expected_version"`
		Rotate          bool   `json:"rotate"`
	}{requestID, req.ExpectedVersion, req.Rotate})
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])
	return s.repo.CreateInvitation(ctx, actor.UserID, requestID, fp, tokenHash, token, req)
}
func (s *Service) AcceptInvitation(ctx context.Context, req AcceptInvitationRequest) (*AcceptInvitationResult, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" || len(req.Password) < 12 || utf8.RuneCountInString(req.Password) > 128 {
		return nil, ErrInvitationInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(token))
	if err := s.repo.AcceptInvitation(ctx, hex.EncodeToString(sum[:]), string(hash)); err != nil {
		if errors.Is(err, ErrInvitationInvalid) {
			return nil, ErrInvitationInvalid
		}
		return nil, err
	}
	return &AcceptInvitationResult{Status: "accepted"}, nil
}
func (s *Service) SearchTenantIdentities(ctx context.Context, actor middleware.ActorContext, query string) (*TenantIdentityList, error) {
	if err := s.authorize(ctx, actor); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 160 {
		return nil, ErrValidation
	}
	users, err := s.repo.SearchTenantIdentities(ctx, query, 20)
	if err != nil {
		return nil, err
	}
	return &TenantIdentityList{Users: users}, nil
}
func (s *Service) authorize(ctx context.Context, actor middleware.ActorContext) error {
	if s == nil || s.access == nil || actor.PrincipalScope != middleware.PrincipalScopePlatform {
		return ErrForbidden
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, Capability: access.CapabilityTenantProvision}); err != nil {
		return ErrForbidden
	}
	return nil
}

func normalizeProvision(req ProvisionRequest) ProvisionRequest {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.Owner.Mode = strings.TrimSpace(req.Owner.Mode)
	req.Owner.UserID = strings.TrimSpace(req.Owner.UserID)
	req.Owner.Email = strings.ToLower(strings.TrimSpace(req.Owner.Email))
	req.Owner.FullName = strings.TrimSpace(req.Owner.FullName)
	req.Owner.Phone = strings.TrimSpace(req.Owner.Phone)
	req.Salon.Name = strings.TrimSpace(req.Salon.Name)
	req.Salon.Phone = strings.TrimSpace(req.Salon.Phone)
	req.Salon.Address = strings.TrimSpace(req.Salon.Address)
	req.Salon.City = strings.TrimSpace(req.Salon.City)
	req.Salon.State = strings.ToUpper(strings.TrimSpace(req.Salon.State))
	req.Salon.ZipCode = strings.TrimSpace(req.Salon.ZipCode)
	req.Salon.Timezone = strings.TrimSpace(req.Salon.Timezone)
	req.Salon.PrimaryLanguage = strings.TrimSpace(req.Salon.PrimaryLanguage)
	req.Salon.SecondaryLanguage = strings.TrimSpace(req.Salon.SecondaryLanguage)
	req.Salon.HandoffPhone = strings.TrimSpace(req.Salon.HandoffPhone)
	return req
}
func validOwner(owner OwnerIdentityInput) bool {
	address, err := mail.ParseAddress(owner.Email)
	_, validPhone := registration.NormalizeUSPhone(owner.Phone)
	if owner.Phone == "" {
		validPhone = true
	}
	if err != nil || address.Address != owner.Email || utf8.RuneCountInString(owner.Email) > 320 || owner.FullName == "" || utf8.RuneCountInString(owner.FullName) > 160 || !validPhone {
		return false
	}
	switch owner.Mode {
	case OwnerModeCreateInvited:
		return owner.UserID == ""
	case OwnerModeUseExisting:
		_, err := uuid.Parse(owner.UserID)
		return err == nil
	default:
		return false
	}
}
func validSalon(item SalonProfileInput) bool {
	_, validPhone := registration.NormalizeUSPhone(item.Phone)
	_, validHandoff := registration.NormalizeUSPhone(item.HandoffPhone)
	if item.HandoffPhone == "" {
		validHandoff = true
	}
	_, timezoneErr := time.LoadLocation(item.Timezone)
	return item.Name != "" && utf8.RuneCountInString(item.Name) <= 200 && validPhone && validHandoff && item.City != "" && utf8.RuneCountInString(item.City) <= 120 && registration.ValidUSState(item.State) && registration.ValidUSZIP(item.ZipCode) && timezoneErr == nil && (item.PrimaryLanguage == "en" || item.PrimaryLanguage == "vi") && (item.SecondaryLanguage == "en" || item.SecondaryLanguage == "vi")
}
func validActionKey(value string) bool {
	n := utf8.RuneCountInString(value)
	return n >= 1 && n <= 256 && value == strings.TrimSpace(value)
}
func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
