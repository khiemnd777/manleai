package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/respond"
)

const (
	LocalUserID  = "user_id"
	LocalSalonID = "salon_id"
	LocalRoles   = "roles"
)

type Claims struct {
	UserID  string   `json:"user_id"`
	SalonID string   `json:"salon_id,omitempty"`
	Roles   []string `json:"roles"`
	jwt.RegisteredClaims
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
		})
		if err != nil || !token.Valid {
			return respond.Error(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", "Invalid or expired access token.")
		}

		c.Locals(LocalUserID, claims.UserID)
		c.Locals(LocalSalonID, claims.SalonID)
		c.Locals(LocalRoles, claims.Roles)
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
