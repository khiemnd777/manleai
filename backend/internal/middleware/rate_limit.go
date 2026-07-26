package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/ratelimit"
	"github.com/manleai/ai-receptionist/internal/respond"
)

var (
	globalClientPolicy = ratelimit.Policy{Name: "global_client", Rate: 300, Window: time.Minute, Burst: 100}
	authLoginPolicy    = ratelimit.Policy{Name: "auth_login", Rate: 10, Window: time.Minute, Burst: 5}
	authSessionPolicy  = ratelimit.Policy{Name: "auth_session", Rate: 60, Window: time.Minute, Burst: 15}
	providerPolicy     = ratelimit.Policy{Name: "provider_callback", Rate: 600, Window: time.Minute, Burst: 120}
	publicReadPolicy   = ratelimit.Policy{Name: "public_read", Rate: 180, Window: time.Minute, Burst: 45}
	expensivePolicy    = ratelimit.Policy{Name: "expensive_operation", Rate: 30, Window: time.Minute, Burst: 8}
	protectedRead      = ratelimit.Policy{Name: "protected_read", Rate: 300, Window: time.Minute, Burst: 75}
	protectedWrite     = ratelimit.Policy{Name: "protected_write", Rate: 150, Window: time.Minute, Burst: 40}
)

func RateLimit(store ratelimit.Taker, jwtSecret, clientIPHeader string) fiber.Handler {
	secret := []byte(strings.TrimSpace(jwtSecret))
	return func(c *fiber.Ctx) error {
		policy, limited := requestRateLimitPolicy(c.Method(), c.Path())
		if !limited {
			return c.Next()
		}
		identity := rateLimitIdentity(c, secret, clientIPHeader)
		globalDecision, err := store.Take(c.UserContext(), identity, globalClientPolicy)
		if err != nil {
			return rateLimitDependencyError(c)
		}
		if !globalDecision.Allowed {
			return rateLimitDenied(c, globalDecision)
		}
		decision, err := store.Take(c.UserContext(), identity, policy)
		if err != nil {
			return rateLimitDependencyError(c)
		}
		setRateLimitHeaders(c, decision)
		if !decision.Allowed {
			return rateLimitDenied(c, decision)
		}
		return c.Next()
	}
}

func requestRateLimitPolicy(method, path string) (ratelimit.Policy, bool) {
	if !strings.HasPrefix(path, "/api/") {
		return ratelimit.Policy{}, false
	}
	switch path {
	case "/api/auth/login", "/api/auth/bootstrap-owner":
		return authLoginPolicy, true
	case "/api/auth/refresh-token", "/api/auth/logout", "/api/auth/bootstrap/status":
		return authSessionPolicy, true
	case "/api/integrations/square/callback", "/api/integrations/square/webhook", "/api/notifications/twilio/status":
		return providerPolicy, true
	}
	if strings.HasPrefix(path, "/api/notifications/twilio/inbound/") || strings.HasPrefix(path, "/api/voice/twilio/") {
		return providerPolicy, true
	}
	if strings.HasPrefix(path, "/api/public/") {
		return publicReadPolicy, true
	}
	if strings.HasSuffix(path, "/training/evaluate") {
		return expensivePolicy, true
	}
	if method == fiber.MethodGet || method == fiber.MethodHead {
		return protectedRead, true
	}
	return protectedWrite, true
}

func rateLimitIdentity(c *fiber.Ctx, secret []byte, clientIPHeader string) string {
	material := "ip\x00" + rateLimitClientIP(c, clientIPHeader)
	if userID := signedRateLimitUserID(c.Get(fiber.HeaderAuthorization), secret); userID != "" {
		material = "user\x00" + userID
	}
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte("manleai-rate-limit-identity-v1\x00"))
	_, _ = digest.Write([]byte(material))
	return hex.EncodeToString(digest.Sum(nil))
}

func rateLimitClientIP(c *fiber.Ctx, clientIPHeader string) string {
	value := strings.TrimSpace(c.Get(strings.TrimSpace(clientIPHeader)))
	if value == "" {
		value = strings.TrimSpace(c.IP())
	}
	if parsed := net.ParseIP(value); parsed != nil {
		return parsed.String()
	}
	return "unknown"
}

func signedRateLimitUserID(authorization string, secret []byte) string {
	if len(secret) == 0 || !strings.HasPrefix(authorization, "Bearer ") {
		return ""
	}
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), claims, func(token *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	userID := strings.TrimSpace(claims.UserID)
	if err != nil || !token.Valid || userID == "" || (claims.Subject != "" && claims.Subject != userID) {
		return ""
	}
	return userID
}

func setRateLimitHeaders(c *fiber.Ctx, decision ratelimit.Decision) {
	c.Set("RateLimit-Limit", strconv.Itoa(decision.Limit))
	c.Set("RateLimit-Remaining", strconv.Itoa(decision.Remaining))
	c.Set("RateLimit-Reset", strconv.Itoa(roundedUpSeconds(decision.ResetAfter)))
}

func rateLimitDenied(c *fiber.Ctx, decision ratelimit.Decision) error {
	setRateLimitHeaders(c, decision)
	c.Set(fiber.HeaderRetryAfter, strconv.Itoa(max(1, roundedUpSeconds(decision.RetryAfter))))
	return respond.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Retry later.")
}

func rateLimitDependencyError(c *fiber.Ctx) error {
	c.Set(fiber.HeaderRetryAfter, "1")
	return respond.Error(c, fiber.StatusServiceUnavailable, "RATE_LIMIT_UNAVAILABLE", "Request protection is temporarily unavailable.")
}

func roundedUpSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int((duration + time.Second - 1) / time.Second)
}
