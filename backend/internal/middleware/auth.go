package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/respond"
)

const (
	LocalUserID                  = "user_id"
	LocalSalonID                 = "salon_id"
	LocalRoles                   = "roles"
	LocalActorContext            = "actor_context"
	localAccessPrincipalResolver = "access_principal_resolver"
)

// ActorContext is the server-owned identity and coarse access context attached
// to every authenticated request. A signed token proves identity only; these
// values are always rebuilt from current PostgreSQL state. Route handlers own
// the access surface (tenant or platform) and must never accept it from a
// request header or body.
type ActorContext struct {
	UserID         string   `json:"user_id"`
	PrimarySalonID string   `json:"primary_salon_id,omitempty"`
	Roles          []string `json:"roles"`
}

func (a ActorContext) HasRole(role string) bool {
	role = strings.TrimSpace(role)
	for _, candidate := range a.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

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
		actorContext := databasecontext.WithActor(c.UserContext(), userID)
		resolvedUserID, salonID, roles, err := resolver.ResolveAccessPrincipal(actorContext, userID)
		if err != nil || resolvedUserID == "" || resolvedUserID != userID {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}

		c.Locals(LocalUserID, resolvedUserID)
		c.Locals(LocalSalonID, salonID)
		c.Locals(LocalRoles, roles)
		c.Locals(LocalActorContext, ActorContext{
			UserID:         resolvedUserID,
			PrimarySalonID: salonID,
			Roles:          append([]string(nil), roles...),
		})
		c.SetUserContext(databasecontext.WithActor(c.UserContext(), resolvedUserID))
		return c.Next()
	}
}

func DatabaseScope(scope string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.SetUserContext(databasecontext.WithScope(c.UserContext(), scope))
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

func Roles(c *fiber.Ctx) []string {
	value, _ := c.Locals(LocalRoles).([]string)
	return append([]string(nil), value...)
}

func Actor(c *fiber.Ctx) ActorContext {
	value, _ := c.Locals(LocalActorContext).(ActorContext)
	value.Roles = append([]string(nil), value.Roles...)
	return value
}
