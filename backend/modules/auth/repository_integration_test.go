package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func TestRepositoryPostgresIssuedAccessTokenStopsAuthorizingAfterUserIsDisabled(t *testing.T) {
	db := openAuthIntegrationDatabase(t)
	ctx := context.Background()
	userID := seedAuthIntegrationUser(t, ctx, db, "active")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Auth Access Principal Test', $1, $2)
		RETURNING id::text
	`, "+1"+uuid.NewString()[:10], userID).Scan(&salonID); err != nil {
		t.Fatalf("insert auth salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE name = 'salon_owner'
	`, userID); err != nil {
		t.Fatalf("assign salon owner role: %v", err)
	}

	repository := NewRepository(db)
	service := NewService(repository, testAuthConfig())
	user, err := repository.FindUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("load active user: %v", err)
	}
	tokens, err := service.issueTokens(ctx, *user)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}

	app := fiber.New()
	api := app.Group("/api", middleware.WithAccessPrincipalResolver(repository))
	api.Get("/protected", middleware.RequireAuth(testAuthConfig().JWTSecret), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"user_id":  middleware.UserID(c),
			"salon_id": middleware.SalonID(c),
			"roles":    c.Locals(middleware.LocalRoles),
		})
	})

	response := executeProtectedAuthRequest(t, app, tokens.AccessToken)
	if response.StatusCode != fiber.StatusOK {
		response.Body.Close()
		t.Fatalf("active user status=%d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var activePrincipal struct {
		UserID  string   `json:"user_id"`
		SalonID string   `json:"salon_id"`
		Roles   []string `json:"roles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&activePrincipal); err != nil {
		response.Body.Close()
		t.Fatalf("decode active principal: %v", err)
	}
	response.Body.Close()
	if activePrincipal.UserID != userID || activePrincipal.SalonID != salonID || len(activePrincipal.Roles) != 1 || activePrincipal.Roles[0] != "salon_owner" {
		t.Fatalf("active principal=%#v, want current owner tenant and role", activePrincipal)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE user_roles
		SET role_id = (SELECT id FROM roles WHERE name = 'staff')
		WHERE user_id = $1
	`, userID); err != nil {
		t.Fatalf("replace current role: %v", err)
	}
	response = executeProtectedAuthRequest(t, app, tokens.AccessToken)
	if response.StatusCode != fiber.StatusOK {
		response.Body.Close()
		t.Fatalf("role-updated user status=%d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var roleUpdatedPrincipal struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&roleUpdatedPrincipal); err != nil {
		response.Body.Close()
		t.Fatalf("decode role-updated principal: %v", err)
	}
	response.Body.Close()
	if len(roleUpdatedPrincipal.Roles) != 1 || roleUpdatedPrincipal.Roles[0] != "staff" {
		t.Fatalf("roles=%v, want current database-owned [staff] rather than token claims", roleUpdatedPrincipal.Roles)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (
			user_id, role_id, status, created_by_user_id, updated_by_user_id
		)
		SELECT $1, id, 'active', $1, $1
		FROM roles
		WHERE name = 'platform_ops'
	`, userID); err != nil {
		t.Fatalf("assign current platform role: %v", err)
	}
	response = executeProtectedAuthRequest(t, app, tokens.AccessToken)
	if response.StatusCode != fiber.StatusOK {
		response.Body.Close()
		t.Fatalf("platform-role-updated user status=%d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var platformRolePrincipal struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&platformRolePrincipal); err != nil {
		response.Body.Close()
		t.Fatalf("decode platform-role-updated principal: %v", err)
	}
	response.Body.Close()
	if len(platformRolePrincipal.Roles) != 2 || platformRolePrincipal.Roles[0] != "platform_ops" || platformRolePrincipal.Roles[1] != "staff" {
		t.Fatalf("roles=%v, want current database-owned [platform_ops staff]", platformRolePrincipal.Roles)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE platform_role_assignments
		SET status = 'revoked', version = version + 1, updated_at = now()
		WHERE user_id = $1
	`, userID); err != nil {
		t.Fatalf("revoke current platform role: %v", err)
	}
	response = executeProtectedAuthRequest(t, app, tokens.AccessToken)
	if response.StatusCode != fiber.StatusOK {
		response.Body.Close()
		t.Fatalf("platform-role-revoked user status=%d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var platformRoleRevokedPrincipal struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&platformRoleRevokedPrincipal); err != nil {
		response.Body.Close()
		t.Fatalf("decode platform-role-revoked principal: %v", err)
	}
	response.Body.Close()
	if len(platformRoleRevokedPrincipal.Roles) != 1 || platformRoleRevokedPrincipal.Roles[0] != "staff" {
		t.Fatalf("roles=%v, want revoked platform role omitted without token refresh", platformRoleRevokedPrincipal.Roles)
	}

	if _, err := db.ExecContext(ctx, `UPDATE users SET status = 'disabled', updated_at = now() WHERE id = $1`, userID); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	response = executeProtectedAuthRequest(t, app, tokens.AccessToken)
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("disabled user status=%d, want %d", response.StatusCode, fiber.StatusUnauthorized)
	}
	if _, err := service.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user refresh error=%v, want ErrInvalidCredentials", err)
	}
	var refreshConsumed bool
	if err := db.QueryRowContext(ctx, `
		SELECT revoked_at IS NOT NULL
		FROM refresh_tokens
		WHERE token_hash = $1
	`, hashToken(tokens.RefreshToken)).Scan(&refreshConsumed); err != nil {
		t.Fatalf("load disabled refresh token: %v", err)
	}
	if !refreshConsumed {
		t.Fatal("disabled user's refresh token was not consumed")
	}
}

func TestRepositoryPostgresConcurrentRefreshRotationCreatesOneSuccessor(t *testing.T) {
	db := openAuthIntegrationDatabase(t)
	ctx := context.Background()
	userID := seedAuthIntegrationUser(t, ctx, db, "active")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	repository := NewRepository(db)
	currentToken := "current-" + uuid.NewString()
	if err := repository.StoreRefreshToken(ctx, userID, currentToken, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("store current token: %v", err)
	}
	replacements := []string{"replacement-a-" + uuid.NewString(), "replacement-b-" + uuid.NewString()}
	type rotationResult struct {
		replacement string
		err         error
	}
	start := make(chan struct{})
	results := make(chan rotationResult, len(replacements))
	var waitGroup sync.WaitGroup
	for _, replacement := range replacements {
		replacement := replacement
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := repository.RotateRefreshToken(ctx, currentToken, replacement, time.Now().UTC().Add(time.Hour))
			results <- rotationResult{replacement: replacement, err: err}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var successfulReplacement string
	notFoundCount := 0
	for result := range results {
		if result.err == nil {
			if successfulReplacement != "" {
				t.Fatalf("more than one concurrent rotation succeeded: %q and %q", successfulReplacement, result.replacement)
			}
			successfulReplacement = result.replacement
			continue
		}
		if errors.Is(result.err, ErrNotFound) {
			notFoundCount++
			continue
		}
		t.Fatalf("concurrent rotation error: %v", result.err)
	}
	if successfulReplacement == "" || notFoundCount != 1 {
		t.Fatalf("successful replacement = %q, invalid contenders = %d, want one each", successfulReplacement, notFoundCount)
	}

	var currentRevoked bool
	if err := db.QueryRowContext(ctx, `
		SELECT revoked_at IS NOT NULL
		FROM refresh_tokens
		WHERE token_hash = $1
	`, hashToken(currentToken)).Scan(&currentRevoked); err != nil {
		t.Fatalf("load current token: %v", err)
	}
	if !currentRevoked {
		t.Fatal("current token remains valid after rotation")
	}
	var liveCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM refresh_tokens
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
	`, userID).Scan(&liveCount); err != nil {
		t.Fatalf("count live successors: %v", err)
	}
	if liveCount != 1 {
		t.Fatalf("live successor count = %d, want 1", liveCount)
	}
	var winnerCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM refresh_tokens
		WHERE user_id = $1 AND token_hash = $2 AND revoked_at IS NULL
	`, userID, hashToken(successfulReplacement)).Scan(&winnerCount); err != nil {
		t.Fatalf("load successful successor: %v", err)
	}
	if winnerCount != 1 {
		t.Fatalf("successful successor count = %d, want 1", winnerCount)
	}
}

func TestRepositoryPostgresConcurrentExactRefreshReplayReturnsOneSuccessor(t *testing.T) {
	db := openAuthIntegrationDatabase(t)
	ctx := context.Background()
	userID := seedAuthIntegrationUser(t, ctx, db, "active")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	repository := NewRepository(db)
	currentToken := "concurrent-current-" + uuid.NewString()
	replacementToken := "concurrent-successor-" + uuid.NewString()
	if err := repository.StoreRefreshToken(ctx, userID, currentToken, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("store current token: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := repository.RotateRefreshToken(ctx, currentToken, replacementToken, time.Now().UTC().Add(time.Hour))
			errs <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("exact concurrent replay: %v", err)
		}
	}

	var liveSuccessors int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM refresh_tokens
		WHERE user_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at > now()
	`, userID, hashToken(replacementToken)).Scan(&liveSuccessors); err != nil {
		t.Fatalf("count successor: %v", err)
	}
	if liveSuccessors != 1 {
		t.Fatalf("live successors=%d, want 1", liveSuccessors)
	}
}

func TestRepositoryPostgresDisabledUserConsumesRefreshWithoutSuccessor(t *testing.T) {
	db := openAuthIntegrationDatabase(t)
	ctx := context.Background()
	userID := seedAuthIntegrationUser(t, ctx, db, "disabled")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	repository := NewRepository(db)
	currentToken := "disabled-current-" + uuid.NewString()
	replacementToken := "disabled-replacement-" + uuid.NewString()
	if err := repository.StoreRefreshToken(ctx, userID, currentToken, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("store disabled current token: %v", err)
	}
	if _, err := repository.RotateRefreshToken(ctx, currentToken, replacementToken, time.Now().UTC().Add(time.Hour)); !errors.Is(err, ErrDisabledUser) {
		t.Fatalf("rotate disabled user token error = %v, want ErrDisabledUser", err)
	}

	var currentRevoked bool
	if err := db.QueryRowContext(ctx, `
		SELECT revoked_at IS NOT NULL
		FROM refresh_tokens
		WHERE token_hash = $1
	`, hashToken(currentToken)).Scan(&currentRevoked); err != nil {
		t.Fatalf("load disabled current token: %v", err)
	}
	if !currentRevoked {
		t.Fatal("disabled user's current token was not consumed")
	}
	var replacementCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM refresh_tokens WHERE token_hash = $1`, hashToken(replacementToken)).Scan(&replacementCount); err != nil {
		t.Fatalf("load disabled replacement token: %v", err)
	}
	if replacementCount != 0 {
		t.Fatalf("disabled replacement count = %d, want 0", replacementCount)
	}
}

func openAuthIntegrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}

func seedAuthIntegrationUser(t *testing.T, ctx context.Context, db *sql.DB, status string) string {
	t.Helper()
	var userID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name, status)
		VALUES ($1, 'integration-test-password', 'Auth Integration User', $2)
		RETURNING id::text
	`, "auth-"+uuid.NewString()+"@example.com", status).Scan(&userID); err != nil {
		t.Fatalf("insert auth user: %v", err)
	}
	return userID
}

func executeProtectedAuthRequest(t *testing.T, app *fiber.App, accessToken string) *http.Response {
	t.Helper()
	request := httptest.NewRequest("GET", "/api/protected", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("execute protected request: %v", err)
	}
	return response
}
