package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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
)

type Service struct {
	repo *Repository
	cfg  config.Config
}

func NewService(repo *Repository, cfg config.Config) *Service {
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
