package services

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yourusername/trough/models"
)

// SecurityHeaders provides security headers middleware
type SecurityHeaders struct {
	config   *SecurityConfig
	siteRepo models.SiteSettingsRepositoryInterface
}

// SecurityConfig contains security header configuration
type SecurityConfig struct {
	CSPEnabled         bool
	CSPPolicy          string
	HSTSEnabled        bool
	HSTSMaxAge         int64
	HSTSIncludeSub     bool
	FrameOptions       string
	ContentTypeOptions bool
	XSSProtection      bool
	ReferrerPolicy     string
	PermissionsPolicy  string
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		CSPEnabled:         true,
		CSPPolicy:          "default-src 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https:; script-src 'self' https://cdn.jsdelivr.net https://www.googletagmanager.com https:; connect-src 'self' https:; font-src 'self' data: https://fonts.gstatic.com https://fonts.googleapis.com; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; frame-src 'none'; block-all-mixed-content",
		HSTSEnabled:        true,
		HSTSMaxAge:         31536000, // 1 year
		HSTSIncludeSub:     true,
		FrameOptions:       "DENY",
		ContentTypeOptions: true,
		XSSProtection:      true,
		ReferrerPolicy:     "strict-origin-when-cross-origin",
		PermissionsPolicy:  "camera=(), microphone=(), geolocation=(), payment=()",
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

// WithSettings enables a dynamic Content-Security-Policy built from the configured
// analytics provider. This lets script-src/connect-src drop the blanket "https:"
// wildcard (which allowed loading scripts from any HTTPS origin — a major XSS gap)
// while still permitting the specific analytics host the admin configured.
func (sh *SecurityHeaders) WithSettings(repo models.SiteSettingsRepositoryInterface) *SecurityHeaders {
	sh.siteRepo = repo
	return sh
}

// buildCSP composes a tightened policy. Known-required hosts are hardcoded (the
// self-hosted app, the jsDelivr libs index.html loads, and Google Fonts); the only
// dynamic part is the analytics origin, added solely when analytics is enabled.
func (sh *SecurityHeaders) buildCSP() string {
	var set models.SiteSettings
	if sh.siteRepo != nil {
		set = GetCachedSettings(sh.siteRepo)
	}
	return buildCSPFromSettings(set)
}

func buildCSPFromSettings(set models.SiteSettings) string {
	scriptSrc := []string{"'self'", "https://cdn.jsdelivr.net"}
	connectSrc := []string{"'self'"}

	if set.AnalyticsEnabled {
		switch strings.ToLower(strings.TrimSpace(set.AnalyticsProvider)) {
		case "ga4":
			scriptSrc = append(scriptSrc, "https://www.googletagmanager.com")
			connectSrc = append(connectSrc,
				"https://www.googletagmanager.com",
				"https://www.google-analytics.com",
				"https://*.google-analytics.com",
				"https://*.analytics.google.com",
			)
		case "umami":
			if o := httpsOrigin(set.UmamiSrc); o != "" {
				scriptSrc = append(scriptSrc, o)
				connectSrc = append(connectSrc, o)
			}
		case "plausible":
			if o := httpsOrigin(set.PlausibleSrc); o != "" {
				scriptSrc = append(scriptSrc, o)
				connectSrc = append(connectSrc, o)
			}
		}
	}

	return strings.Join([]string{
		"default-src 'self'",
		// Images stay permissive: uploads may be served from an arbitrary CDN/S3 host.
		"img-src 'self' data: https:",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' data: https://fonts.gstatic.com https://fonts.googleapis.com",
		"script-src " + strings.Join(scriptSrc, " "),
		"connect-src " + strings.Join(connectSrc, " "),
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"frame-src 'none'",
		"block-all-mixed-content",
	}, "; ")
}

// httpsOrigin returns scheme://host for a valid https URL, or "" otherwise.
func httpsOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// Middleware returns the security headers middleware
func (sh *SecurityHeaders) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Set Content Security Policy. Prefer the tightened, analytics-aware policy
		// when a settings repo is available; fall back to the static config policy.
		if sh.config.CSPEnabled {
			policy := sh.config.CSPPolicy
			if sh.siteRepo != nil {
				policy = sh.buildCSP()
			}
			if policy != "" {
				c.Set("Content-Security-Policy", policy)
			}
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
		c.Set("X-Permitted-Cross-Domain-Policies", "none")
		c.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Set("Cross-Origin-Resource-Policy", "same-origin")

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
