package middleware

import (
	"errors"
	"os"
	"strings"
	"sync"
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
	// Small cache for password_changed_at to reduce DB lookups on hot path
	// Short TTL preserves security while improving performance.
	type cacheEntry struct {
		t   time.Time
		exp time.Time
	}
	var (
		pwMu    sync.RWMutex
		pwCache = make(map[uuid.UUID]cacheEntry)
		pwCap   = 1024
	)
	getChangedAt := func(userID uuid.UUID) time.Time {
		now := time.Now()
		pwMu.RLock()
		if e, ok := pwCache[userID]; ok && now.Before(e.exp) {
			pwMu.RUnlock()
			return e.t
		}
		pwMu.RUnlock()
		var changedAt time.Time
		_ = models.DB().QueryRowx(`SELECT COALESCE(password_changed_at, to_timestamp(0)) FROM users WHERE id = $1`, userID).Scan(&changedAt)
		pwMu.Lock()
		if len(pwCache) >= pwCap {
			// Simple bound: reset map when capacity reached
			pwCache = make(map[uuid.UUID]cacheEntry)
		}
		pwCache[userID] = cacheEntry{t: changedAt, exp: now.Add(5 * time.Minute)}
		pwMu.Unlock()
		return changedAt
	}
	return func(c *fiber.Ctx) error {
		tokenString := c.Get("Authorization")
		if tokenString != "" {
			if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
				tokenString = tokenString[7:]
			}
		} else {
			// Fallback to auth cookie if Authorization header is absent
			if v := c.Cookies("auth_token"); strings.TrimSpace(v) != "" {
				tokenString = v
			}
		}
		if tokenString == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing authorization token"})
		}

		secret := getJWTSecret()
		if len(secret) < 32 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
		}
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			// Enforce expected signing method
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, errors.New("invalid signing method")
			}
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
			changedAt := getChangedAt(claims.UserID)
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
	if tokenString != "" && len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}
	if tokenString == "" {
		// Fallback to cookie when header missing
		tokenString = strings.TrimSpace(c.Cookies("auth_token"))
	}
	secret := getJWTSecret()
	if tokenString == "" || len(secret) < 32 {
		return uuid.Nil
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("invalid signing method")
		}
		return []byte(secret), nil
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

// InvalidatePasswordChangeCache can be called by handlers after a password update
// to ensure subsequent requests enforce new invalidation without TTL delay.
// No-op in this file since cache is local to Protected closure; we provide a
// simple endpoint to refresh via Short-lived token issuance post-change.
// Keeping this function for API stability if we move cache global later.
func InvalidatePasswordChangeCache(userID uuid.UUID) {
	_ = userID
}
