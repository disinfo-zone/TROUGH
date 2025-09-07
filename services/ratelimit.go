package services

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RateLimitConfig defines configuration for the enhanced rate limiter
type RateLimitConfig struct {
	MaxEntries      int           `yaml:"max_entries" default:"10000"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" default:"5m"`
	EntryTTL        time.Duration `yaml:"entry_ttl" default:"1h"`
	TrustedProxies  []string      `yaml:"trusted_proxies" default:"[\"127.0.0.1\", \"::1\"]"`
	EnableDebug     bool          `yaml:"enable_debug" default:"false"`
}

// RateLimitStats provides statistics about rate limiter usage
type RateLimitStats struct {
	TotalEntries    int64         `json:"total_entries"`
	EvictedCount    int64         `json:"evicted_count"`
	CleanupCount    int64         `json:"cleanup_count"`
	DeniedCount     int64         `json:"denied_count"`
	LastCleanupTime time.Time     `json:"last_cleanup_time"`
	MemoryUsage     int64         `json:"memory_usage_bytes"`
	Uptime          time.Duration `json:"uptime"`
}

// rlEntry represents a single rate limiting entry
type rlEntry struct {
	tokens    int
	refillAt  time.Time
	lastUsed  time.Time
	ipAddress string
}

// RateLimiter provides enhanced rate limiting with LRU eviction and cleanup
type RateLimiter struct {
	mu           sync.RWMutex
	entries      map[string]*rlEntry
	config       RateLimitConfig
	stats        RateLimitStats
	startTime    time.Time
	cleanupTimer  *time.Timer
	stopCleanup  chan struct{}
	trustedProxyMap map[string]bool
}

// NewRateLimiter creates a new enhanced rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 1000
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}
	if config.EntryTTL <= 0 {
		config.EntryTTL = 30 * time.Minute
	}

	// Build trusted proxy map for O(1) lookups
	trustedProxyMap := make(map[string]bool)
	for _, proxy := range config.TrustedProxies {
		trustedProxyMap[proxy] = true
	}

	rl := &RateLimiter{
		entries:        make(map[string]*rlEntry),
		config:         config,
		startTime:      time.Now(),
		stopCleanup:    make(chan struct{}),
		trustedProxyMap: trustedProxyMap,
	}

	// Start background cleanup
	rl.startCleanup()

	return rl
}

// Middleware returns a Fiber middleware for rate limiting
func (rl *RateLimiter) Middleware(capacity int, refill time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ip := rl.getClientIP(c)
		if ip == "" {
			// If we can't get a valid IP, allow the request but log it
			if rl.config.EnableDebug {
				rl.logDebug("Unable to determine client IP, allowing request")
			}
			return c.Next()
		}

		allowed := rl.allowRequest(ip, capacity, refill)
		if !allowed {
			rl.stats.DeniedCount++
			if rl.config.EnableDebug {
				rl.logDebug("Rate limit exceeded for IP: %s", ip)
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many requests",
			})
		}

		return c.Next()
	}
}

// allowRequest checks if a request from the given IP should be allowed
func (rl *RateLimiter) allowRequest(ip string, capacity int, refill time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[ip]

	// Clean up expired entries or create new one
	if !exists || now.After(entry.refillAt) {
		entry = &rlEntry{
			tokens:    capacity,
			refillAt:  now.Add(refill),
			lastUsed:  now,
			ipAddress: ip,
		}
		rl.entries[ip] = entry
		rl.stats.TotalEntries++
	}

	// Update last used time
	entry.lastUsed = now

	// Check if we need to evict entries
	if len(rl.entries) > rl.config.MaxEntries {
		rl.evictLRU()
	}

	// Check if request is allowed
	if entry.tokens <= 0 {
		return false
	}

	entry.tokens--
	return true
}

// getClientIP extracts the real client IP address, handling proxies
func (rl *RateLimiter) getClientIP(c *fiber.Ctx) string {
	// Try to get real IP from X-Forwarded-For header
	if forwarded := c.Get("X-Forwarded-For"); forwarded != "" {
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			// Get the leftmost IP (original client)
			clientIP := strings.TrimSpace(ips[0])
			if rl.isValidIP(clientIP) {
				return rl.normalizeIP(clientIP)
			}
		}
	}

	// Try X-Real-IP header
	if realIP := c.Get("X-Real-IP"); realIP != "" {
		if rl.isValidIP(realIP) {
			return rl.normalizeIP(realIP)
		}
	}

	// Fall back to remote address
	remoteAddr := c.IP()
	if remoteAddr != "" && rl.isValidIP(remoteAddr) {
		return rl.normalizeIP(remoteAddr)
	}

	return ""
}

// isValidIP checks if an IP address is valid
func (rl *RateLimiter) isValidIP(ip string) bool {
	// Check if it's a valid IP address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Additional validation for localhost/private networks if needed
	// For now, accept any valid IP
	return true
}

// normalizeIP normalizes an IP address for consistent storage
func (rl *RateLimiter) normalizeIP(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ip
	}

	// For IPv4, return as-is
	if parsedIP.To4() != nil {
		return parsedIP.String()
	}

	// For IPv6, return the compressed form
	return parsedIP.String()
}

// evictLRU removes the least recently used entries
func (rl *RateLimiter) evictLRU() {
	if len(rl.entries) <= rl.config.MaxEntries {
		return
	}

	// Find the oldest entry
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range rl.entries {
		if first || entry.lastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastUsed
			first = false
		}
	}

	// Remove the oldest entry
	if oldestKey != "" {
		delete(rl.entries, oldestKey)
		rl.stats.EvictedCount++
		rl.stats.TotalEntries--

		if rl.config.EnableDebug {
			rl.logDebug("Evicted LRU entry for IP: %s", oldestKey)
		}
	}
}

// startCleanup starts the background cleanup goroutine
func (rl *RateLimiter) startCleanup() {
	rl.cleanupTimer = time.NewTimer(rl.config.CleanupInterval)
	
	go func() {
		for {
			select {
			case <-rl.cleanupTimer.C:
				rl.cleanup()
				rl.cleanupTimer.Reset(rl.config.CleanupInterval)
			case <-rl.stopCleanup:
				return
			}
		}
	}()
}

// cleanup removes expired entries
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, entry := range rl.entries {
		if now.After(entry.lastUsed.Add(rl.config.EntryTTL)) {
			delete(rl.entries, key)
			expiredCount++
		}
	}

	rl.stats.TotalEntries -= int64(expiredCount)
	rl.stats.CleanupCount++
	rl.stats.LastCleanupTime = now

	if rl.config.EnableDebug && expiredCount > 0 {
		rl.logDebug("Cleaned up %d expired entries", expiredCount)
	}
}

// GetStats returns current rate limiter statistics
func (rl *RateLimiter) GetStats() RateLimitStats {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	stats := rl.stats
	stats.TotalEntries = int64(len(rl.entries))
	stats.Uptime = time.Since(rl.startTime)
	
	// Estimate memory usage (rough calculation)
	// Each entry: ~88 bytes (map overhead + entry struct)
	estimatedMemory := stats.TotalEntries * 88
	stats.MemoryUsage = estimatedMemory

	return stats
}

// Stop gracefully shuts down the rate limiter
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
	if rl.cleanupTimer != nil {
		rl.cleanupTimer.Stop()
	}
}

// logDebug logs debug messages if enabled
func (rl *RateLimiter) logDebug(format string, args ...interface{}) {
	if rl.config.EnableDebug {
		// In a real implementation, you might use a proper logger
		// For now, we'll just format the message
		// log.Printf("[RateLimiter DEBUG] "+format, args...)
	}
}

// GetClientIPForTesting is a helper method for testing IP extraction
func (rl *RateLimiter) GetClientIPForTesting(c *fiber.Ctx) string {
	return rl.getClientIP(c)
}