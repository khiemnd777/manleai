package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/respond"
)

const (
	LocalUserID                  = "user_id"
	LocalSalonID                 = "salon_id"
	LocalRoles                   = "roles"
	localAccessPrincipalResolver = "access_principal_resolver"
)

// AccessPrincipalResolver loads the current server-owned authentication
// principal for an already signature-verified access token. Implementations
// must return only active users and current tenant/role assignments.
type AccessPrincipalResolver interface {
	ResolveAccessPrincipal(ctx context.Context, userID string) (resolvedUserID string, salonID string, roles []string, err error)
}

type Claims struct {
	UserID  string   `json:"user_id"`
	SalonID string   `json:"salon_id,omitempty"`
	Roles   []string `json:"roles"`
	jwt.RegisteredClaims
}

// WithAccessPrincipalResolver attaches the server-owned principal resolver to
// an API router. RequireAuth fails closed when this dependency is absent.
func WithAccessPrincipalResolver(resolver AccessPrincipalResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(localAccessPrincipalResolver, resolver)
		return c.Next()
	}
}

func RequireAuth(jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Missing bearer token.")
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || !token.Valid {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}

		userID := strings.TrimSpace(claims.UserID)
		if userID == "" || (claims.Subject != "" && claims.Subject != userID) {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}
		resolver, ok := c.Locals(localAccessPrincipalResolver).(AccessPrincipalResolver)
		if !ok || resolver == nil {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}
		resolvedUserID, salonID, roles, err := resolver.ResolveAccessPrincipal(c.UserContext(), userID)
		if err != nil || resolvedUserID == "" || resolvedUserID != userID {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}

		c.Locals(LocalUserID, resolvedUserID)
		c.Locals(LocalSalonID, salonID)
		c.Locals(LocalRoles, roles)
		return c.Next()
	}
}

func UserID(c *fiber.Ctx) string {
	value, _ := c.Locals(LocalUserID).(string)
	return value
}

func SalonID(c *fiber.Ctx) string {
	value, _ := c.Locals(LocalSalonID).(string)
	return value
}
