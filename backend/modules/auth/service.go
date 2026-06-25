package auth

import (
	"context"
	"crypto/rand"
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
	FindRefreshTokenUser(ctx context.Context, token string) (string, error)
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
	if user.Status != "active" {
		return nil, ErrDisabledUser
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
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
	userID, err := s.repo.FindRefreshTokenUser(ctx, refreshToken)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := s.repo.RevokeRefreshToken(ctx, refreshToken); err != nil {
		return nil, err
	}
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, *user)
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
	return &MeResponse{User: *user, Roles: roles, SalonID: salonID}, nil
}

func (s *Service) issueTokens(ctx context.Context, user User) (*LoginResponse, error) {
	roles, err := s.repo.RolesForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	salonID, err := s.repo.PrimarySalonIDForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.AccessTokenTTL)
	claims := middleware.Claims{
		UserID:  user.ID,
		SalonID: salonID,
		Roles:   roles,
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

	refreshToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	if err := s.repo.StoreRefreshToken(ctx, user.ID, refreshToken, now.Add(s.cfg.RefreshTokenTTL)); err != nil {
		return nil, err
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         user,
		Roles:        roles,
		SalonID:      salonID,
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
