package services

import (
	"strings"
	"testing"

	"github.com/yourusername/trough/models"
)

// The tightened CSP must never contain a bare "https:" wildcard in script-src or
// connect-src (the old hole that allowed loading scripts from any HTTPS origin).
func cspDirective(csp, name string) string {
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, name+" ") {
			return d
		}
	}
	return ""
}

// hasBareHTTPSWildcard reports whether any source token is exactly "https:" (the
// wildcard), as opposed to a concrete origin like "https://cdn.jsdelivr.net".
func hasBareHTTPSWildcard(directive string) bool {
	for _, tok := range strings.Fields(directive) {
		if tok == "https:" {
			return true
		}
	}
	return false
}

func TestCSPDropsWildcardWhenAnalyticsDisabled(t *testing.T) {
	csp := buildCSPFromSettings(models.SiteSettings{AnalyticsEnabled: false})

	script := cspDirective(csp, "script-src")
	connect := cspDirective(csp, "connect-src")

	if hasBareHTTPSWildcard(script) {
		t.Fatalf("script-src must not contain a bare https: wildcard: %q", script)
	}
	if hasBareHTTPSWildcard(connect) {
		t.Fatalf("connect-src must not contain a bare https: wildcard: %q", connect)
	}
	if !strings.Contains(script, "https://cdn.jsdelivr.net") {
		t.Fatalf("script-src must still allow jsDelivr: %q", script)
	}
	if strings.Contains(script, "googletagmanager") {
		t.Fatalf("analytics host should not appear when analytics disabled: %q", script)
	}
}

func TestCSPAddsGA4Origins(t *testing.T) {
	csp := buildCSPFromSettings(models.SiteSettings{AnalyticsEnabled: true, AnalyticsProvider: "ga4"})
	if !strings.Contains(cspDirective(csp, "script-src"), "https://www.googletagmanager.com") {
		t.Fatalf("GA4 script host missing: %q", csp)
	}
	if !strings.Contains(cspDirective(csp, "connect-src"), "https://www.google-analytics.com") {
		t.Fatalf("GA4 connect host missing: %q", csp)
	}
}

func TestCSPAddsPlausibleConfiguredOrigin(t *testing.T) {
	csp := buildCSPFromSettings(models.SiteSettings{
		AnalyticsEnabled:  true,
		AnalyticsProvider: "plausible",
		PlausibleSrc:      "https://plausible.example.com/js/script.js",
	})
	script := cspDirective(csp, "script-src")
	connect := cspDirective(csp, "connect-src")
	if !strings.Contains(script, "https://plausible.example.com") || strings.Contains(script, "/js/script.js") {
		t.Fatalf("expected plausible origin (not full URL) in script-src: %q", script)
	}
	if !strings.Contains(connect, "https://plausible.example.com") {
		t.Fatalf("expected plausible origin in connect-src: %q", connect)
	}
}

func TestHTTPSOriginRejectsNonHTTPS(t *testing.T) {
	if got := httpsOrigin("http://insecure.example/js.js"); got != "" {
		t.Fatalf("expected non-https to be rejected, got %q", got)
	}
	if got := httpsOrigin("https://ok.example:8443/a/b.js"); got != "https://ok.example:8443" {
		t.Fatalf("unexpected origin: %q", got)
	}
}
