package services

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// SecurityHeaders provides security headers middleware
type SecurityHeaders struct {
	config *SecurityConfig
}

// SecurityConfig contains security header configuration
type SecurityConfig struct {
	CSPEnabled        bool
	CSPPolicy         string
	HSTSEnabled       bool
	HSTSMaxAge        int64
	HSTSIncludeSub    bool
	FrameOptions      string
	ContentTypeOptions bool
	XSSProtection     bool
	ReferrerPolicy    string
	PermissionsPolicy string
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		CSPEnabled:     true,
		CSPPolicy:      "default-src 'self'; img-src 'self' data: https: *; style-src 'self' 'unsafe-inline' https: *; script-src 'self' 'unsafe-inline' https: cdn.jsdelivr.net; connect-src 'self' https: *; font-src 'self' data: https: fonts.googleapis.com fonts.gstatic.com; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; frame-src https: *; block-all-mixed-content",
		HSTSEnabled:    true,
		HSTSMaxAge:     31536000, // 1 year
		HSTSIncludeSub: true,
		FrameOptions:   "DENY",
		ContentTypeOptions: true,
		XSSProtection:  true,
		ReferrerPolicy: "strict-origin-when-cross-origin",
		PermissionsPolicy: "camera=(), microphone=(), geolocation=(), payment=()",
	}
}

// NewSecurityHeaders creates a new security headers middleware
func NewSecurityHeaders(config *SecurityConfig) *SecurityHeaders {
	if config == nil {
		config = DefaultSecurityConfig()
	}
	
	return &SecurityHeaders{
		config: config,
	}
}

// Middleware returns the security headers middleware
func (sh *SecurityHeaders) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Set Content Security Policy
		if sh.config.CSPEnabled && sh.config.CSPPolicy != "" {
			c.Set("Content-Security-Policy", sh.config.CSPPolicy)
		}
		
		// Set HTTP Strict Transport Security
		if sh.config.HSTSEnabled {
			hstsValue := fmt.Sprintf("max-age=%d", sh.config.HSTSMaxAge)
			if sh.config.HSTSIncludeSub {
				hstsValue += "; includeSubDomains"
			}
			c.Set("Strict-Transport-Security", hstsValue)
		}
		
		// Set X-Frame-Options
		if sh.config.FrameOptions != "" {
			c.Set("X-Frame-Options", sh.config.FrameOptions)
		}
		
		// Set X-Content-Type-Options
		if sh.config.ContentTypeOptions {
			c.Set("X-Content-Type-Options", "nosniff")
		}
		
		// Set X-XSS-Protection
		if sh.config.XSSProtection {
			c.Set("X-XSS-Protection", "1; mode=block")
		}
		
		// Set Referrer-Policy
		if sh.config.ReferrerPolicy != "" {
			c.Set("Referrer-Policy", sh.config.ReferrerPolicy)
		}
		
		// Set Permissions-Policy
		if sh.config.PermissionsPolicy != "" {
			c.Set("Permissions-Policy", sh.config.PermissionsPolicy)
		}
		
		// Set additional security headers
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Permitted-Cross-Domain-Policies", "none")
		
		// Remove potentially revealing headers
		c.Set("Server", "")
		c.Set("X-Powered-By", "")
		
		return c.Next()
	}
}

// GetCSPNonce returns a CSP nonce for inline scripts/styles
func (sh *SecurityHeaders) GetCSPNonce() string {
	// Generate a random nonce
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		// Fallback to timestamp-based nonce
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", nonce)
}

// IsSafeURL checks if a URL is safe for CSP
func (sh *SecurityHeaders) IsSafeURL(url string) bool {
	// Allow empty URLs
	if url == "" {
		return true
	}
	
	// Allow same-origin URLs
	if strings.HasPrefix(url, "/") {
		return true
	}
	
	// Allow HTTPS URLs
	if strings.HasPrefix(url, "https://") {
		return true
	}
	
	// Allow data URLs
	if strings.HasPrefix(url, "data:") {
		return true
	}
	
	return false
}

// SanitizeHTML sanitizes HTML content for security
func (sh *SecurityHeaders) SanitizeHTML(html string) string {
	// Basic HTML sanitization - remove dangerous tags and attributes
	dangerousTags := []string{
		"script", "iframe", "object", "embed", "form", "input", "button",
		"style", "link", "meta", "base", "applet", "param",
	}
	
	dangerousAttrs := []string{
		"onload", "onerror", "onclick", "onmouseover", "onfocus", "onblur",
		"javascript:", "data:", "vbscript:", "expression(",
	}
	
	sanitized := html
	
	// Remove dangerous tags
	for _, tag := range dangerousTags {
		sanitized = strings.ReplaceAll(sanitized, "<"+tag, "<removed_"+tag)
		sanitized = strings.ReplaceAll(sanitized, "</"+tag, "</removed_"+tag)
	}
	
	// Remove dangerous attributes
	for _, attr := range dangerousAttrs {
		sanitized = strings.ReplaceAll(sanitized, attr+"=", "removed_"+attr+"=")
	}
	
	return sanitized
}