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

func TestLoginDisabledUserAlwaysReturnsInvalidCredentials(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	store := &fakeAuthStore{user: &User{
		ID:             "disabled-user",
		Email:          "disabled@example.com",
		PasswordHash:   string(passwordHash),
		FullName:       "Disabled User",
		Status:         "disabled",
		PrincipalScope: "tenant",
	}}
	service := NewService(store, testAuthConfig())

	for _, password := range []string{"wrong-password", "correct-password"} {
		_, err := service.Login(context.Background(), LoginRequest{
			Email:    store.user.Email,
			Password: password,
		})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login password %q error = %v, want ErrInvalidCredentials", password, err)
		}
	}
	if store.storeRefreshCalls != 0 {
		t.Fatalf("stored refresh tokens = %d, want 0", store.storeRefreshCalls)
	}
}

func TestRefreshUsesAtomicRotationAndReturnsItsReplacement(t *testing.T) {
	store := &fakeAuthStore{
		user: &User{
			ID:             "active-user",
			Email:          "active@example.com",
			FullName:       "Active User",
			Status:         "active",
			PrincipalScope: "tenant",
		},
		roles:   []string{"salon_owner"},
		salonID: "salon-1",
	}
	service := NewService(store, testAuthConfig())

	res, err := service.Refresh(context.Background(), "current-refresh-token")
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if store.rotateCurrentToken != "current-refresh-token" {
		t.Fatalf("rotation current token = %q", store.rotateCurrentToken)
	}
	if store.rotateReplacementToken == "" || store.rotateReplacementToken == store.rotateCurrentToken {
		t.Fatalf("rotation replacement token = %q", store.rotateReplacementToken)
	}
	if res.RefreshToken != store.rotateReplacementToken {
		t.Fatalf("response refresh token = %q, want atomic replacement", res.RefreshToken)
	}
	if store.rotateExpiresAt.Before(time.Now().Add(55 * time.Minute)) {
		t.Fatalf("replacement expiry = %s, want about an hour from now", store.rotateExpiresAt)
	}
	if store.storeRefreshCalls != 0 {
		t.Fatalf("non-atomic refresh stores = %d, want 0", store.storeRefreshCalls)
	}
}

func TestRefreshDerivesOneExactSuccessorForConcurrentReplay(t *testing.T) {
	store := &fakeAuthStore{
		user: &User{ID: "active-user", Email: "active@example.com", FullName: "Active User", Status: "active", PrincipalScope: "tenant"},
	}
	service := NewService(store, testAuthConfig())

	first, err := service.Refresh(context.Background(), "shared-current-token")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	firstReplacement := first.RefreshToken
	second, err := service.Refresh(context.Background(), "shared-current-token")
	if err != nil {
		t.Fatalf("replayed refresh: %v", err)
	}
	if firstReplacement == "" || second.RefreshToken != firstReplacement {
		t.Fatalf("successors first=%q second=%q, want one exact successor", firstReplacement, second.RefreshToken)
	}
	if firstReplacement == "shared-current-token" {
		t.Fatal("refresh rotation reused the current token")
	}
}

func TestRefreshMapsConsumedDisabledTokenToInvalidCredentials(t *testing.T) {
	store := &fakeAuthStore{rotateErr: ErrDisabledUser}
	service := NewService(store, testAuthConfig())

	_, err := service.Refresh(context.Background(), "disabled-user-token")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh error = %v, want ErrInvalidCredentials", err)
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
	bootstrapAvailable     bool
	bootstrapErr           error
	createCalled           bool
	createdParams          CreateFirstOwnerParams
	createErr              error
	user                   *User
	roles                  []string
	salonID                string
	refreshToken           string
	refreshExpiresAt       time.Time
	storeRefreshCalls      int
	rotateCurrentToken     string
	rotateReplacementToken string
	rotateExpiresAt        time.Time
	rotateErr              error
	revokedToken           string
	revokeErr              error
}

func (f *fakeAuthStore) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	if f.user == nil {
		return nil, ErrNotFound
	}
	return f.user, nil
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
	f.storeRefreshCalls++
	f.refreshToken = token
	f.refreshExpiresAt = expiresAt
	return nil
}

func (f *fakeAuthStore) RotateRefreshToken(ctx context.Context, currentToken string, replacementToken string, replacementExpiresAt time.Time) (*User, error) {
	f.rotateCurrentToken = currentToken
	f.rotateReplacementToken = replacementToken
	f.rotateExpiresAt = replacementExpiresAt
	if f.rotateErr != nil {
		return nil, f.rotateErr
	}
	if f.user == nil {
		return nil, ErrNotFound
	}
	return f.user, nil
}

func (f *fakeAuthStore) RevokeRefreshToken(ctx context.Context, token string) error {
	f.revokedToken = token
	return f.revokeErr
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
		ID:             "user-1",
		Email:          params.Email,
		PasswordHash:   params.PasswordHash,
		FullName:       params.FullName,
		Status:         "active",
		PrincipalScope: "tenant",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}
