package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestBootstrapOwnerCreatesSalonOwnerAndIssuesTokens(t *testing.T) {
	store := &fakeAuthStore{bootstrapAvailable: true}
	service := NewService(store, testAuthConfig())

	res, err := service.BootstrapOwner(context.Background(), BootstrapOwnerRequest{
		Email:    " Owner@Example.COM ",
		FullName: " Linh Nguyen ",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("BootstrapOwner returned error: %v", err)
	}
	if !store.createCalled {
		t.Fatal("expected CreateFirstOwner to be called")
	}
	if store.createdParams.Email != "owner@example.com" {
		t.Fatalf("email = %q, want owner@example.com", store.createdParams.Email)
	}
	if store.createdParams.FullName != "Linh Nguyen" {
		t.Fatalf("full name = %q, want Linh Nguyen", store.createdParams.FullName)
	}
	if store.createdParams.PasswordHash == "password123" {
		t.Fatal("password was stored without hashing")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.createdParams.PasswordHash), []byte("password123")); err != nil {
		t.Fatalf("password hash does not match password: %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}
	if len(res.Roles) != 1 || res.Roles[0] != "salon_owner" {
		t.Fatalf("roles = %v, want [salon_owner]", res.Roles)
	}
	if store.refreshToken == "" {
		t.Fatal("expected refresh token to be stored")
	}
	if store.refreshExpiresAt.Before(time.Now().Add(55 * time.Minute)) {
		t.Fatalf("refresh token expiry = %s, want about an hour from now", store.refreshExpiresAt)
	}
}

func TestBootstrapOwnerRejectsWhenSetupIsClosed(t *testing.T) {
	store := &fakeAuthStore{bootstrapAvailable: false}
	service := NewService(store, testAuthConfig())

	_, err := service.BootstrapOwner(context.Background(), BootstrapOwnerRequest{
		Email:    "owner@example.com",
		FullName: "Linh Nguyen",
		Password: "password123",
	})
	if !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("error = %v, want ErrBootstrapClosed", err)
	}
	if store.createCalled {
		t.Fatal("CreateFirstOwner should not be called when bootstrap is closed")
	}
}

func TestBootstrapOwnerValidatesInput(t *testing.T) {
	tests := []BootstrapOwnerRequest{
		{Email: "", FullName: "Linh Nguyen", Password: "password123"},
		{Email: "owner", FullName: "Linh Nguyen", Password: "password123"},
		{Email: "owner@example.com", FullName: "", Password: "password123"},
		{Email: "owner@example.com", FullName: "Linh Nguyen", Password: "short"},
	}

	for _, req := range tests {
		store := &fakeAuthStore{bootstrapAvailable: true}
		service := NewService(store, testAuthConfig())
		_, err := service.BootstrapOwner(context.Background(), req)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("BootstrapOwner(%+v) error = %v, want ErrValidation", req, err)
		}
		if store.createCalled {
			t.Fatalf("CreateFirstOwner should not be called for invalid request %+v", req)
		}
	}
}

func TestBootstrapStatusReturnsAvailability(t *testing.T) {
	store := &fakeAuthStore{bootstrapAvailable: true}
	service := NewService(store, testAuthConfig())

	res, err := service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("BootstrapStatus returned error: %v", err)
	}
	if !res.Available {
		t.Fatal("available = false, want true")
	}
}

func testAuthConfig() config.Config {
	return config.Config{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	}
}

type fakeAuthStore struct {
	bootstrapAvailable bool
	bootstrapErr       error
	createCalled       bool
	createdParams      CreateFirstOwnerParams
	createErr          error
	user               *User
	roles              []string
	salonID            string
	refreshToken       string
	refreshExpiresAt   time.Time
}

func (f *fakeAuthStore) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	return nil, ErrNotFound
}

func (f *fakeAuthStore) FindUserByID(ctx context.Context, id string) (*User, error) {
	if f.user == nil {
		return nil, ErrNotFound
	}
	return f.user, nil
}

func (f *fakeAuthStore) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	if f.roles != nil {
		return f.roles, nil
	}
	return []string{"salon_owner"}, nil
}

func (f *fakeAuthStore) PrimarySalonIDForUser(ctx context.Context, userID string) (string, error) {
	return f.salonID, nil
}

func (f *fakeAuthStore) StoreRefreshToken(ctx context.Context, userID string, token string, expiresAt time.Time) error {
	f.refreshToken = token
	f.refreshExpiresAt = expiresAt
	return nil
}

func (f *fakeAuthStore) FindRefreshTokenUser(ctx context.Context, token string) (string, error) {
	return "", ErrNotFound
}

func (f *fakeAuthStore) RevokeRefreshToken(ctx context.Context, token string) error {
	return nil
}

func (f *fakeAuthStore) BootstrapAvailable(ctx context.Context) (bool, error) {
	if f.bootstrapErr != nil {
		return false, f.bootstrapErr
	}
	return f.bootstrapAvailable, nil
}

func (f *fakeAuthStore) CreateFirstOwner(ctx context.Context, params CreateFirstOwnerParams) (*User, error) {
	f.createCalled = true
	f.createdParams = params
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.user != nil {
		return f.user, nil
	}
	now := time.Now().UTC()
	return &User{
		ID:           "user-1",
		Email:        params.Email,
		PasswordHash: params.PasswordHash,
		FullName:     params.FullName,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}
