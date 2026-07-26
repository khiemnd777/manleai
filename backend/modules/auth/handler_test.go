package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginHandlerDoesNotRevealDisabledAccountPasswordValidity(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	store := &fakeAuthStore{user: &User{
		ID:           "disabled-user",
		Email:        "disabled@example.com",
		PasswordHash: string(passwordHash),
		FullName:     "Disabled User",
		Status:       "disabled",
	}}
	app := fiber.New()
	handler := NewHandler(NewService(store, testAuthConfig()))
	app.Post("/auth/login", handler.Login)

	var responses []respond.ErrorResponse
	for _, password := range []string{"wrong-password", "correct-password"} {
		body, err := json.Marshal(LoginRequest{Email: store.user.Email, Password: password})
		if err != nil {
			t.Fatalf("marshal login request: %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("login password %q status = %d, want %d", password, response.StatusCode, http.StatusUnauthorized)
		}
		var errorResponse respond.ErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
			response.Body.Close()
			t.Fatalf("decode login response: %v", err)
		}
		response.Body.Close()
		responses = append(responses, errorResponse)
	}

	for i, response := range responses {
		if response.Error.Code != "INVALID_CREDENTIALS" || response.Error.Message != "Email or password is incorrect." {
			t.Fatalf("response %d = %#v, want generic invalid credentials", i, response)
		}
	}
	if responses[0] != responses[1] {
		t.Fatalf("disabled login responses differ: wrong=%#v correct=%#v", responses[0], responses[1])
	}
}

func TestBrowserSessionHandlersKeepRefreshTokenInSecureHttpOnlyCookie(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	store := &fakeAuthStore{user: &User{
		ID: "active-user", Email: "owner@example.com", PasswordHash: string(passwordHash), FullName: "Owner", Status: "active",
	}}
	cfg := testAuthConfig()
	cfg.AppEnv = "production"
	handler := NewHandler(NewService(store, cfg))
	app := fiber.New()
	app.Post("/api/auth/login", handler.Login)
	app.Post("/api/auth/refresh-token", handler.Refresh)
	app.Post("/api/auth/logout", handler.Logout)

	loginBody, _ := json.Marshal(LoginRequest{Email: store.user.Email, Password: "correct-password"})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse, err := app.Test(loginRequest)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", loginResponse.StatusCode)
	}
	var loginJSON map[string]any
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginJSON); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if _, exposed := loginJSON["refresh_token"]; exposed {
		t.Fatal("login JSON exposed refresh_token")
	}
	if loginJSON["access_token"] == "" {
		t.Fatal("login JSON omitted access_token")
	}
	loginCookie := requireRefreshCookie(t, loginResponse.Cookies())
	if loginCookie.Value == "" || !loginCookie.HttpOnly || !loginCookie.Secure || loginCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("login refresh cookie=%#v", loginCookie)
	}
	if loginCookie.Path != "/api/auth" || loginCookie.MaxAge < int((50*time.Minute)/time.Second) {
		t.Fatalf("login refresh cookie path/expiry=%#v", loginCookie)
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/api/auth/refresh-token", nil)
	refreshRequest.AddCookie(loginCookie)
	refreshResponse, err := app.Test(refreshRequest)
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer refreshResponse.Body.Close()
	if refreshResponse.StatusCode != http.StatusOK {
		t.Fatalf("refresh status=%d", refreshResponse.StatusCode)
	}
	var refreshJSON map[string]any
	if err := json.NewDecoder(refreshResponse.Body).Decode(&refreshJSON); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if _, exposed := refreshJSON["refresh_token"]; exposed {
		t.Fatal("refresh JSON exposed refresh_token")
	}
	replacementCookie := requireRefreshCookie(t, refreshResponse.Cookies())
	if replacementCookie.Value == loginCookie.Value || replacementCookie.Value != store.rotateReplacementToken {
		t.Fatalf("replacement cookie=%q repository successor=%q", replacementCookie.Value, store.rotateReplacementToken)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.AddCookie(replacementCookie)
	logoutResponse, err := app.Test(logoutRequest)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	defer logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusOK || store.revokedToken != replacementCookie.Value {
		t.Fatalf("logout status=%d revoked=%q", logoutResponse.StatusCode, store.revokedToken)
	}
	cleared := requireRefreshCookie(t, logoutResponse.Cookies())
	if cleared.Value != "" || !cleared.Expires.Before(time.Now()) {
		t.Fatalf("cleared refresh cookie=%#v", cleared)
	}
}

func requireRefreshCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == refreshCookieName {
			return cookie
		}
	}
	t.Fatalf("%s cookie missing: %#v", refreshCookieName, cookies)
	return nil
}
