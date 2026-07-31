package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/ratelimit"
)

type fakeRateLimitStore struct {
	decisions  []ratelimit.Decision
	err        error
	identities []string
	policies   []ratelimit.Policy
}

func (s *fakeRateLimitStore) Take(_ context.Context, identity string, policy ratelimit.Policy) (ratelimit.Decision, error) {
	s.identities = append(s.identities, identity)
	s.policies = append(s.policies, policy)
	if s.err != nil {
		return ratelimit.Decision{}, s.err
	}
	decision := s.decisions[0]
	s.decisions = s.decisions[1:]
	return decision, nil
}

func TestRateLimitReturnsTyped429AndHidesRawIdentity(t *testing.T) {
	store := &fakeRateLimitStore{decisions: []ratelimit.Decision{
		{Allowed: true, Limit: 100, Remaining: 99, ResetAfter: time.Second},
		{Allowed: false, Limit: 5, Remaining: 0, RetryAfter: 1500 * time.Millisecond, ResetAfter: time.Minute},
	}}
	app := fiber.New()
	app.Use(RateLimit(store, "rate-limit-test-secret", "X-ManleAI-Client-IP"))
	app.Post("/api/auth/login", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	request := httptest.NewRequest(fiber.MethodPost, "/api/auth/login", nil)
	request.Header.Set("X-ManleAI-Client-IP", "203.0.113.42")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusTooManyRequests || response.Header.Get("Retry-After") != "2" || response.Header.Get("RateLimit-Limit") != "5" {
		t.Fatalf("status=%d headers=%#v", response.StatusCode, response.Header)
	}
	if len(store.identities) != 2 || store.identities[0] != store.identities[1] || strings.Contains(store.identities[0], "203.0.113.42") || len(store.identities[0]) != 64 {
		t.Fatalf("identities=%#v", store.identities)
	}
	if store.policies[0].Name != "global_client" || store.policies[1].Name != "auth_login" {
		t.Fatalf("policies=%#v", store.policies)
	}
}

func TestRateLimitFailsClosedWhenRedisIsUnavailable(t *testing.T) {
	store := &fakeRateLimitStore{err: errors.New("redis offline")}
	app := fiber.New()
	app.Use(RateLimit(store, "rate-limit-test-secret", "X-ManleAI-Client-IP"))
	app.Get("/api/public/salon", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	response, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/public/salon", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusServiceUnavailable || response.Header.Get("Retry-After") != "1" {
		t.Fatalf("status=%d headers=%#v", response.StatusCode, response.Header)
	}
}

func TestRateLimitExemptsHealthAndClassifiesProviderCallbacks(t *testing.T) {
	if _, limited := requestRateLimitPolicy(fiber.MethodGet, "/healthz"); limited {
		t.Fatal("health endpoint must remain available for dependency checks")
	}
	policy, limited := requestRateLimitPolicy(fiber.MethodPost, "/api/voice/twilio/recording")
	if !limited || policy.Name != "provider_callback" {
		t.Fatalf("provider policy=%#v limited=%t", policy, limited)
	}
}

func TestRateLimitClassifiesPublicRegistrationWriteSeparately(t *testing.T) {
	policy, limited := requestRateLimitPolicy(fiber.MethodPost, "/api/public/tenant-registration-requests")
	if !limited || policy.Name != "public_registration_write" || policy.Rate != 10 || policy.Window != time.Hour || policy.Burst != 3 {
		t.Fatalf("registration policy=%#v limited=%t", policy, limited)
	}
	policy, limited = requestRateLimitPolicy(fiber.MethodGet, "/api/public/tenant-registration-requests")
	if !limited || policy.Name != "public_read" {
		t.Fatalf("public read policy=%#v limited=%t", policy, limited)
	}
}
