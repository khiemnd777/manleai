package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrDisabledUser       = errors.New("user is disabled")
	ErrValidation         = errors.New("validation failed")
	ErrBootstrapClosed    = errors.New("bootstrap owner setup is closed")
)

type Store interface {
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, id string) (*User, error)
	RolesForUser(ctx context.Context, userID string) ([]string, error)
	PrimarySalonIDForUser(ctx context.Context, userID string) (string, error)
	StoreRefreshToken(ctx context.Context, userID string, token string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, currentToken string, replacementToken string, replacementExpiresAt time.Time) (*User, error)
	RevokeRefreshToken(ctx context.Context, token string) error
	BootstrapAvailable(ctx context.Context) (bool, error)
	CreateFirstOwner(ctx context.Context, params CreateFirstOwnerParams) (*User, error)
}

type Service struct {
	repo Store
	cfg  config.Config
}

func NewService(repo Store, cfg config.Config) *Service {
	return &Service{repo: repo, cfg: cfg}
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	user, err := s.repo.FindUserByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != "active" {
		return nil, ErrInvalidCredentials
	}
	if user.PrincipalScope != string(middleware.PrincipalScopeTenant) && user.PrincipalScope != string(middleware.PrincipalScopePlatform) {
		return nil, ErrInvalidCredentials
	}
	return s.issueTokens(ctx, *user)
}

func (s *Service) BootstrapStatus(ctx context.Context) (*BootstrapStatusResponse, error) {
	available, err := s.repo.BootstrapAvailable(ctx)
	if err != nil {
		return nil, err
	}
	return &BootstrapStatusResponse{Available: available}, nil
}

func (s *Service) BootstrapOwner(ctx context.Context, req BootstrapOwnerRequest) (*LoginResponse, error) {
	email, ok := normalizeEmail(req.Email)
	if !ok {
		return nil, ErrValidation
	}
	fullName := strings.TrimSpace(req.FullName)
	if fullName == "" || len(req.Password) < 8 || strings.TrimSpace(req.Password) == "" {
		return nil, ErrValidation
	}

	available, err := s.repo.BootstrapAvailable(ctx)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, ErrBootstrapClosed
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.CreateFirstOwner(ctx, CreateFirstOwnerParams{
		Email:        email,
		PasswordHash: string(passwordHash),
		FullName:     fullName,
	})
	if err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, *user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	replacementToken := s.rotatedRefreshToken(refreshToken)
	if replacementToken == "" {
		return nil, ErrInvalidCredentials
	}
	now := time.Now().UTC()
	user, err := s.repo.RotateRefreshToken(ctx, refreshToken, replacementToken, now.Add(s.cfg.RefreshTokenTTL))
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDisabledUser) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	return s.buildLoginResponse(ctx, *user, replacementToken, now)
}

// rotatedRefreshToken derives one successor for one refresh token. Exact
// concurrent rotations therefore ask the repository to persist or replay the
// same successor instead of creating multiple live sessions. The domain label
// separates this HMAC from JWT signing while reusing the required high-entropy
// deployment secret.
func (s *Service) rotatedRefreshToken(currentToken string) string {
	currentToken = strings.TrimSpace(currentToken)
	if currentToken == "" || strings.TrimSpace(s.cfg.JWTSecret) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	_, _ = mac.Write([]byte("manleai-refresh-rotation-v1\x00"))
	_, _ = mac.Write([]byte(currentToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeRefreshToken(ctx, refreshToken)
}

func (s *Service) Me(ctx context.Context, userID string) (*MeResponse, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := s.repo.RolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	salonID, err := s.repo.PrimarySalonIDForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &MeResponse{User: *user, Roles: roles, SalonID: salonID, PrincipalScope: user.PrincipalScope}, nil
}

func (s *Service) issueTokens(ctx context.Context, user User) (*LoginResponse, error) {
	now := time.Now().UTC()
	refreshToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	response, err := s.buildLoginResponse(ctx, user, refreshToken, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.StoreRefreshToken(ctx, user.ID, refreshToken, now.Add(s.cfg.RefreshTokenTTL)); err != nil {
		return nil, err
	}
	return response, nil
}

func (s *Service) buildLoginResponse(ctx context.Context, user User, refreshToken string, now time.Time) (*LoginResponse, error) {
	if user.PrincipalScope != string(middleware.PrincipalScopeTenant) && user.PrincipalScope != string(middleware.PrincipalScopePlatform) {
		return nil, ErrInvalidCredentials
	}
	roles, err := s.repo.RolesForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	salonID, err := s.repo.PrimarySalonIDForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	expiresAt := now.Add(s.cfg.AccessTokenTTL)
	claims := middleware.Claims{
		UserID:         user.ID,
		SalonID:        salonID,
		PrincipalScope: middleware.PrincipalScope(user.PrincipalScope),
		Roles:          roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      expiresAt,
		User:           user,
		Roles:          roles,
		SalonID:        salonID,
		PrincipalScope: user.PrincipalScope,
	}, nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func normalizeEmail(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", false
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", false
	}
	return email, true
}
