package middleware

import (
	"errors"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/yourusername/trough/models"
)

type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	jwt.RegisteredClaims
}

func getJWTSecret() string {
	// Do not provide a default. Startup must ensure JWT_SECRET is set.
	return os.Getenv("JWT_SECRET")
}

func GenerateToken(userID uuid.UUID, username string) (string, error) {
	secret := getJWTSecret()
	if len(secret) < 32 {
		return "", errors.New("JWT secret not configured or too weak")
	}
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func Protected() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenString := c.Get("Authorization")
		if tokenString == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing authorization token",
			})
		}

		if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
			tokenString = tokenString[7:]
		}

		secret := getJWTSecret()
		if len(secret) < 32 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
		}
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid token",
			})
		}

		claims, ok := token.Claims.(*Claims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid token claims",
			})
		}

		// Optional token invalidation on password change: reject if token iat < password_changed_at
		if claims.IssuedAt != nil {
			var changedAt time.Time
			// Ignore query errors; only enforce when we can read a non-zero changedAt
			_ = models.DB().QueryRowx(`SELECT COALESCE(password_changed_at, to_timestamp(0)) FROM users WHERE id = $1`, claims.UserID).Scan(&changedAt)
			if !changedAt.IsZero() && changedAt.After(claims.IssuedAt.Time) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
			}
		}
		c.Locals("user_id", claims.UserID)
		c.Locals("username", claims.Username)

		return c.Next()
	}
}

func OptionalUserID(c *fiber.Ctx) uuid.UUID {
	tokenString := c.Get("Authorization")
	if tokenString == "" {
		return uuid.Nil
	}
	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(getJWTSecret()), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil
	}
	if claims, ok := token.Claims.(*Claims); ok {
		return claims.UserID
	}
	return uuid.Nil
}

func GetUserID(c *fiber.Ctx) uuid.UUID {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return userID
}

func GetUsername(c *fiber.Ctx) string {
	username, ok := c.Locals("username").(string)
	if !ok {
		return ""
	}
	return username
}
