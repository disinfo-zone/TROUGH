package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CSRFProtection provides CSRF protection middleware
type CSRFProtection struct {
	secretKey   []byte
	cookieName  string
	headerName  string
	expiry      time.Duration
	isProduction bool
}

// NewCSRFProtection creates a new CSRF protection middleware
func NewCSRFProtection(secretKey string) *CSRFProtection {
	if secretKey == "" {
		// Generate a random secret if none provided
		secret := make([]byte, 32)
		rand.Read(secret)
		secretKey = string(secret)
	}
	
	return &CSRFProtection{
		secretKey:   []byte(secretKey),
		cookieName:  "csrf_token",
		headerName:  "X-CSRF-Token",
		expiry:      24 * time.Hour,
		isProduction: os.Getenv("GO_ENV") == "production" || os.Getenv("ENVIRONMENT") == "production",
	}
}

// GenerateToken generates a new simple CSRF token
func (cp *CSRFProtection) GenerateToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return hex.EncodeToString(token), nil
}

// ValidateToken validates a CSRF token (simple approach - just check basic structure)
func (cp *CSRFProtection) ValidateToken(token string) bool {
	if token == "" {
		return false
	}
	
	// Simple validation: check if it's reasonable length for either hex (64) or base64 (88)
	length := len(token)
	if length != 64 && length != 88 {
		return false
	}
	
	// Basic character validation - should be alphanumeric with common safe chars
	for _, char := range token {
		if !(('a' <= char && char <= 'z') ||
			('A' <= char && char <= 'Z') ||
			('0' <= char && char <= '9') ||
			char == '_' || char == '-' || char == '=' || char == '+' || char == '/') {
			return false
		}
	}
	
	return true
}

// Middleware returns the CSRF protection middleware
func (cp *CSRFProtection) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip CSRF for GET, HEAD, OPTIONS
		if c.Method() == "GET" || c.Method() == "HEAD" || c.Method() == "OPTIONS" {
			return c.Next()
		}
		
		// Skip CSRF for authentication endpoints
		path := c.Path()
		if strings.HasPrefix(path, "/api/register") || 
		   strings.HasPrefix(path, "/api/login") ||
		   strings.HasPrefix(path, "/api/logout") ||
		   strings.HasPrefix(path, "/api/forgot-password") ||
		   strings.HasPrefix(path, "/api/reset-password") ||
		   strings.HasPrefix(path, "/api/verify-email") ||
		   strings.HasPrefix(path, "/api/validate-invite") ||
		   strings.HasPrefix(path, "/api/me/resend-verification") ||
		   strings.Contains(path, "/send-verification") {
			return c.Next()
		}
		
		// Skip CSRF for public endpoints
		if strings.HasPrefix(path, "/api/feed") || 
		   strings.HasPrefix(path, "/api/images/") && c.Method() == "GET" ||
		   strings.HasPrefix(path, "/api/users/") && c.Method() == "GET" ||
		   strings.HasPrefix(path, "/api/site") && c.Method() == "GET" {
			return c.Next()
		}
		
		// Get token from cookie
		cookieToken := c.Cookies(cp.cookieName)
		if cookieToken == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "CSRF token missing",
			})
		}
		
		// Get token from request (header or form)
		requestToken := c.Get(cp.headerName)
		if requestToken == "" && c.Method() == "POST" {
			// Try to get from form data for multipart forms
			requestToken = c.FormValue("csrf_token")
		}
		
		if requestToken == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "CSRF token required",
			})
		}
		
		// Simple validation: check if tokens match and are valid format
		if cookieToken == requestToken && cp.ValidateToken(cookieToken) {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Invalid CSRF token",
		})
	}
}

// SetCSRFToken sets the CSRF token in the response
func (cp *CSRFProtection) SetCSRFToken(c *fiber.Ctx) error {
	token, err := cp.GenerateToken()
	if err != nil {
		return err
	}
	
	// Set token in cookie with security flags
	secure := cp.isProduction
	sameSite := "Lax"
	if cp.isProduction {
		sameSite = "Strict"
	}
	
	cookie := &fiber.Cookie{
		Name:     cp.cookieName,
		Value:    token,
		Expires:  time.Now().Add(cp.expiry),
		HTTPOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Path:     "/",
	}
	
	c.Cookie(cookie)
	
	// Also set token in header for easy access by frontend
	c.Set("X-CSRF-Token", token)
	
	return nil
}

// GetCSRFToken returns the current CSRF token
func (cp *CSRFProtection) GetCSRFToken(c *fiber.Ctx) string {
	return c.Cookies(cp.cookieName)
}

// RequireCSRF is a convenience middleware that ensures CSRF token is set
func (cp *CSRFProtection) RequireCSRF() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Set CSRF token for GET requests
		if c.Method() == "GET" {
			if err := cp.SetCSRFToken(c); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to generate CSRF token",
				})
			}
		}
		return c.Next()
	}
}