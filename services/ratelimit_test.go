package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiterBasics(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	// Test that rate limiter is created
	assert.NotNil(t, limiter)
	assert.NotNil(t, limiter.entries)

	// Test basic rate limiting functionality
	allowed := limiter.allowRequest("192.168.1.1", 2, time.Minute)
	assert.True(t, allowed, "First request should be allowed")

	allowed = limiter.allowRequest("192.168.1.1", 2, time.Minute)
	assert.True(t, allowed, "Second request should be allowed")

	allowed = limiter.allowRequest("192.168.1.1", 2, time.Minute)
	assert.False(t, allowed, "Third request should be blocked")

	// Test different IPs are independent
	allowed = limiter.allowRequest("192.168.1.2", 2, time.Minute)
	assert.True(t, allowed, "Different IP should be allowed")
}

func TestRateLimiterMiddleware(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	app := fiber.New()
	
	// Add rate limiting middleware
	app.Use(limiter.Middleware(2, time.Minute))
	
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("test")
	})
	
	// First request should succeed
	req, _ := http.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	
	// Second request should succeed
	req, _ = http.NewRequest("GET", "/test", nil)
	resp, err = app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	
	// Third request should be rate limited
	req, _ = http.NewRequest("GET", "/test", nil)
	resp, err = app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode)
}

func TestRateLimiterTokenRefill(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.3"
	
	// Use up all tokens
	for i := 0; i < 2; i++ {
		allowed := limiter.allowRequest(ip, 2, 50*time.Millisecond)
		assert.True(t, allowed, "Request %d should be allowed", i+1)
	}

	// This should be blocked
	allowed := limiter.allowRequest(ip, 2, 50*time.Millisecond)
	assert.False(t, allowed, "Request should be blocked when tokens are exhausted")

	// Wait for tokens to refill
	time.Sleep(60 * time.Millisecond)

	// This should be allowed again
	allowed = limiter.allowRequest(ip, 2, 50*time.Millisecond)
	assert.True(t, allowed, "Request should be allowed after token refill")
}

func TestRateLimiterStats(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	// Add some activity
	limiter.allowRequest("192.168.1.4", 2, time.Minute)
	limiter.allowRequest("192.168.1.4", 2, time.Minute)
	limiter.allowRequest("192.168.1.4", 2, time.Minute) // This should be denied

	stats := limiter.GetStats()
	assert.Equal(t, int64(1), stats.TotalEntries)
	// Note: DeniedCount is tracked in the middleware, not the allowRequest method
	assert.GreaterOrEqual(t, stats.Uptime, time.Duration(0))
	assert.GreaterOrEqual(t, stats.MemoryUsage, int64(0))
}

func TestRateLimiterIPValidation(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	// Test IP validation
	assert.True(t, limiter.isValidIP("192.168.1.1"))
	assert.True(t, limiter.isValidIP("127.0.0.1"))
	assert.True(t, limiter.isValidIP("::1"))
	assert.True(t, limiter.isValidIP("2001:db8::1"))
	
	// Test invalid IPs
	assert.False(t, limiter.isValidIP(""))
	assert.False(t, limiter.isValidIP("invalid"))
	assert.False(t, limiter.isValidIP("999.999.999.999"))
}

func TestRateLimiterCleanup(t *testing.T) {
	config := RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 50 * time.Millisecond,
		EntryTTL:        100 * time.Millisecond,
		EnableDebug:     true,
	}
	limiter := NewRateLimiter(config)
	defer limiter.Stop()

	// Add some entries
	limiter.allowRequest("test1", 1, time.Minute)
	limiter.allowRequest("test2", 1, time.Minute)
	limiter.allowRequest("test3", 1, time.Minute)

	// Wait for entries to expire and cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Check that entries were cleaned up
	stats := limiter.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)
	assert.Greater(t, stats.CleanupCount, int64(0))
}