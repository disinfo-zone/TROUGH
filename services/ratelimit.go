package services

import (
	"fmt"
	"net"
	"strconv"
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

// SecurityEvent represents a security event for logging
type SecurityEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"event_type"`
	IPAddress   string    `json:"ip_address"`
	UserAgent   string    `json:"user_agent"`
	Path        string    `json:"path"`
	Method      string    `json:"method"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
}

// ProgressiveRateLimitConfig defines configuration for progressive rate limiting
type ProgressiveRateLimitConfig struct {
	BaseWindow       time.Duration `yaml:"base_window" default:"1m"`
	MaxWindow        time.Duration `yaml:"max_window" default:"1h"`
	BaseCapacity     int           `yaml:"base_capacity" default:"60"`
	MinCapacity      int           `yaml:"min_capacity" default:"5"`
	BackoffFactor    float64       `yaml:"backoff_factor" default:"2.0"`
	LockoutThreshold int           `yaml:"lockout_threshold" default:"10"`
	LockoutDuration  time.Duration `yaml:"lockout_duration" default:"15m"`
	EnableLogging    bool          `yaml:"enable_logging" default:"true"`
}

// progressiveEntry represents a progressive rate limiting entry
type progressiveEntry struct {
	currentWindow       time.Time
	currentCapacity     int
	consecutiveFailures int
	totalAttempts       int
	firstFailure        time.Time
	isLockedOut         bool
	lockoutUntil        time.Time
	lastUpdated         time.Time
	ipAddress           string
}

// ProgressiveRateLimiter provides progressive rate limiting with backoff
type ProgressiveRateLimiter struct {
	mu              sync.RWMutex
	entries         map[string]*progressiveEntry
	config          ProgressiveRateLimitConfig
	baseConfig      RateLimitConfig
	stats           RateLimitStats
	startTime       time.Time
	cleanupTicker   *time.Ticker
	stopCleanup     chan struct{}
	trustedProxyMap map[string]bool
	securityEvents  []SecurityEvent
	eventCallback   func(SecurityEvent)
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
	mu              sync.RWMutex
	entries         map[string]*rlEntry
	config          RateLimitConfig
	stats           RateLimitStats
	startTime       time.Time
	cleanupTicker   *time.Ticker
	stopCleanup     chan struct{}
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
		p := strings.TrimSpace(proxy)
		if p == "" {
			continue
		}
		if parsed := net.ParseIP(p); parsed != nil {
			trustedProxyMap[parsed.String()] = true
			continue
		}
		trustedProxyMap[p] = true
	}

	rl := &RateLimiter{
		entries:         make(map[string]*rlEntry),
		config:          config,
		startTime:       time.Now(),
		stopCleanup:     make(chan struct{}),
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
			rl.mu.Lock()
			rl.stats.DeniedCount++
			rl.mu.Unlock()
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

func (rl *RateLimiter) isTrustedProxy(ip string) bool {
	if ip == "" {
		return false
	}
	normalized := rl.normalizeIP(ip)
	return rl.trustedProxyMap[normalized]
}

func isLoopbackAddress(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

func extractForwardedClientIP(getHeader func(string) string, isValid func(string) bool, normalize func(string) string, isTrustedHop func(string) bool) string {
	// Cloudflare edge headers are authoritative: when a request genuinely transits
	// Cloudflare, CF-Connecting-IP is set by the edge and cannot be forged by the
	// origin client. True-Client-IP is intentionally NOT trusted here because it is
	// only stripped/rewritten on Enterprise plans and otherwise passes through
	// unmodified, making it client-spoofable.
	for _, headerName := range []string{"CF-Connecting-IP", "CF-Connecting-IPv6"} {
		if value := strings.TrimSpace(getHeader(headerName)); value != "" && isValid(value) {
			return normalize(value)
		}
	}

	// Standard proxy chain. Conforming proxies APPEND the peer they received the
	// connection from to the right of X-Forwarded-For, so the right-most entry that
	// is not one of our own trusted hops is the real client. Anything the client
	// prepends stays to the left and is ignored. Walking left-to-right (the previous
	// behaviour) trusted the first value, which is fully attacker-controlled and
	// allowed both rate-limit evasion and targeted lockout of a spoofed victim IP.
	if forwarded := strings.TrimSpace(getHeader("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			if candidate == "" || !isValid(candidate) {
				continue
			}
			normalized := normalize(candidate)
			// Skip our own trusted proxy hops; the first non-trusted address from
			// the right is the client as seen by the nearest trusted proxy.
			if isTrustedHop(normalized) {
				continue
			}
			return normalized
		}
	}

	if realIP := strings.TrimSpace(getHeader("X-Real-IP")); realIP != "" && isValid(realIP) {
		return normalize(realIP)
	}

	return ""
}

// getClientIP extracts the real client IP address, handling proxies
func (rl *RateLimiter) getClientIP(c *fiber.Ctx) string {
	remoteAddr := c.IP()
	if remoteAddr == "" || !rl.isValidIP(remoteAddr) {
		return ""
	}
	remoteAddr = rl.normalizeIP(remoteAddr)

	// Only trust forwarding headers from known proxy hops.
	// Loopback is always trusted to support local tunnel agents (e.g. cloudflared).
	if rl.isTrustedProxy(remoteAddr) || isLoopbackAddress(remoteAddr) {
		isTrustedHop := func(ip string) bool { return rl.isTrustedProxy(ip) || isLoopbackAddress(ip) }
		if clientIP := extractForwardedClientIP(func(key string) string { return c.Get(key) }, rl.isValidIP, rl.normalizeIP, isTrustedHop); clientIP != "" {
			return clientIP
		}
	}

	// Fall back to the direct peer address.
	return remoteAddr
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
	rl.cleanupTicker = time.NewTicker(rl.config.CleanupInterval)

	go func() {
		for {
			select {
			case <-rl.cleanupTicker.C:
				rl.cleanup()
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
	if rl.cleanupTicker != nil {
		rl.cleanupTicker.Stop()
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

// NewProgressiveRateLimiter creates a new progressive rate limiter
func NewProgressiveRateLimiter(config ProgressiveRateLimitConfig, baseConfig RateLimitConfig) *ProgressiveRateLimiter {
	// Validate and set defaults
	if config.BaseWindow <= 0 {
		config.BaseWindow = 1 * time.Minute
	}
	if config.MaxWindow <= 0 {
		config.MaxWindow = 1 * time.Hour
	}
	if config.BaseCapacity <= 0 {
		config.BaseCapacity = 60
	}
	if config.MinCapacity <= 0 {
		config.MinCapacity = 5
	}
	if config.BackoffFactor < 1.0 {
		config.BackoffFactor = 2.0
	}
	if config.LockoutThreshold <= 0 {
		config.LockoutThreshold = 10
	}
	if config.LockoutDuration <= 0 {
		config.LockoutDuration = 15 * time.Minute
	}

	prl := &ProgressiveRateLimiter{
		entries:         make(map[string]*progressiveEntry),
		config:          config,
		baseConfig:      baseConfig,
		startTime:       time.Now(),
		stopCleanup:     make(chan struct{}),
		trustedProxyMap: make(map[string]bool),
		securityEvents:  make([]SecurityEvent, 0),
	}
	for _, proxy := range baseConfig.TrustedProxies {
		p := strings.TrimSpace(proxy)
		if p == "" {
			continue
		}
		if parsed := net.ParseIP(p); parsed != nil {
			prl.trustedProxyMap[parsed.String()] = true
			continue
		}
		prl.trustedProxyMap[p] = true
	}

	// Start background cleanup
	prl.startCleanup()

	return prl
}

// Middleware returns a Fiber middleware for progressive rate limiting
func (prl *ProgressiveRateLimiter) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ip := prl.getClientIP(c)
		if ip == "" {
			// If we can't get a valid IP, allow the request but log it
			prl.mu.Lock()
			prl.logSecurityEvent("UNKNOWN_IP", ip, c.Path(), c.Method(), "low", "Unable to determine client IP")
			prl.mu.Unlock()
			return c.Next()
		}

		allowed, retryAfter := prl.allowRequest(ip, c)
		if !allowed {
			prl.mu.Lock()
			prl.stats.DeniedCount++
			prl.mu.Unlock()

			// Log security event
			eventType := "RATE_LIMIT_EXCEEDED"
			severity := "medium"
			if prl.isLockedOut(ip) {
				eventType = "ACCOUNT_LOCKOUT"
				severity = "high"
			}

			prl.logSecurityEvent(eventType, ip, c.Path(), c.Method(), severity,
				fmt.Sprintf("Rate limit exceeded. Retry after: %s", retryAfter))

			// Set retry-after header
			if retryAfter > 0 {
				c.Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				c.Set("X-RateLimit-Limit", strconv.Itoa(prl.getCurrentCapacity(ip)))
				c.Set("X-RateLimit-Remaining", "0")
				c.Set("X-RateLimit-Reset", strconv.Itoa(int(time.Until(prl.getResetTime(ip)).Seconds())))
			}

			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":       "Too many requests",
				"retry_after": retryAfter,
				"locked_out":  prl.isLockedOut(ip),
			})
		}

		// Add rate limit headers for successful requests
		c.Set("X-RateLimit-Limit", strconv.Itoa(prl.getCurrentCapacity(ip)))
		c.Set("X-RateLimit-Remaining", strconv.Itoa(prl.getRemainingTokens(ip)))
		c.Set("X-RateLimit-Reset", strconv.Itoa(int(time.Until(prl.getResetTime(ip)).Seconds())))

		return c.Next()
	}
}

// allowRequest checks if a request from the given IP should be allowed with progressive backoff
func (prl *ProgressiveRateLimiter) allowRequest(ip string, c *fiber.Ctx) (bool, time.Duration) {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	now := time.Now()
	entry, exists := prl.entries[ip]

	// Check if IP is locked out
	if exists && entry.isLockedOut && now.Before(entry.lockoutUntil) {
		return false, time.Until(entry.lockoutUntil)
	}

	// Reset lockout if period has passed
	if exists && entry.isLockedOut && now.After(entry.lockoutUntil) {
		entry.isLockedOut = false
		entry.consecutiveFailures = 0
		entry.firstFailure = time.Time{}
		prl.logSecurityEvent("LOCKOUT_RESET", ip, c.Path(), c.Method(), "low", "Lockout period reset for IP")
	}

	// Create new entry if it doesn't exist or window has expired
	if !exists || now.After(entry.currentWindow.Add(prl.config.BaseWindow)) {
		capacity := prl.config.BaseCapacity

		// Reduce capacity based on previous failures
		if exists && entry.consecutiveFailures > 0 {
			reductionFactor := prl.config.BackoffFactor
			for i := 0; i < entry.consecutiveFailures && i < 10; i++ {
				capacity = int(float64(capacity) / reductionFactor)
				if capacity < prl.config.MinCapacity {
					capacity = prl.config.MinCapacity
					break
				}
			}
		}

		entry = &progressiveEntry{
			currentWindow:       now,
			currentCapacity:     capacity,
			consecutiveFailures: 0,
			totalAttempts:       0,
			firstFailure:        time.Time{},
			isLockedOut:         false,
			lockoutUntil:        time.Time{},
			lastUpdated:         now,
			ipAddress:           ip,
		}
		prl.entries[ip] = entry
		prl.stats.TotalEntries++
	}

	// Update last used time
	entry.lastUpdated = now
	entry.totalAttempts++

	// Check if request is allowed
	if entry.currentCapacity <= 0 {
		entry.consecutiveFailures++

		// Check if we should lock out this IP
		if entry.consecutiveFailures >= prl.config.LockoutThreshold {
			entry.isLockedOut = true
			entry.lockoutUntil = now.Add(prl.config.LockoutDuration)

			prl.logSecurityEvent("ACCOUNT_LOCKOUT", ip, c.Path(), c.Method(), "high",
				fmt.Sprintf("IP locked out after %d consecutive failures", entry.consecutiveFailures))

			return false, prl.config.LockoutDuration
		}

		// Calculate progressive backoff
		backoffWindow := prl.config.BaseWindow
		for i := 1; i < entry.consecutiveFailures && i < 10; i++ {
			backoffWindow = time.Duration(float64(backoffWindow) * prl.config.BackoffFactor)
			if backoffWindow > prl.config.MaxWindow {
				backoffWindow = prl.config.MaxWindow
				break
			}
		}

		// Extend the current window
		entry.currentWindow = now.Add(backoffWindow)
		entry.currentCapacity = prl.config.BaseCapacity / (entry.consecutiveFailures + 1)
		if entry.currentCapacity < prl.config.MinCapacity {
			entry.currentCapacity = prl.config.MinCapacity
		}

		prl.logSecurityEvent("PROGRESSIVE_BACKOFF", ip, c.Path(), c.Method(), "medium",
			fmt.Sprintf("Progressive backoff applied: %d consecutive failures, window: %s",
				entry.consecutiveFailures, backoffWindow))

		return false, backoffWindow
	}

	entry.currentCapacity--
	return true, 0
}

// RecordFailure records a failed authentication attempt for progressive backoff
func (prl *ProgressiveRateLimiter) RecordFailure(ip string, c *fiber.Ctx) {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	now := time.Now()
	entry, exists := prl.entries[ip]

	if !exists {
		entry = &progressiveEntry{
			currentWindow:       now,
			currentCapacity:     prl.config.BaseCapacity,
			consecutiveFailures: 0,
			totalAttempts:       0,
			firstFailure:        time.Time{},
			isLockedOut:         false,
			lockoutUntil:        time.Time{},
			lastUpdated:         now,
			ipAddress:           ip,
		}
		prl.entries[ip] = entry
		prl.stats.TotalEntries++
	}

	entry.consecutiveFailures++
	entry.totalAttempts++

	if entry.firstFailure.IsZero() {
		entry.firstFailure = now
	}

	// Check for immediate lockout (for repeated auth failures)
	if entry.consecutiveFailures >= prl.config.LockoutThreshold {
		entry.isLockedOut = true
		entry.lockoutUntil = now.Add(prl.config.LockoutDuration)

		prl.logSecurityEvent("AUTH_FAILURE_LOCKOUT", ip, c.Path(), c.Method(), "high",
			fmt.Sprintf("Authentication failure lockout: %d consecutive failures", entry.consecutiveFailures))
	} else {
		prl.logSecurityEvent("AUTH_FAILURE", ip, c.Path(), c.Method(), "medium",
			fmt.Sprintf("Authentication failure recorded: %d consecutive failures", entry.consecutiveFailures))
	}
}

// RecordSuccess resets the failure counter for successful authentication
func (prl *ProgressiveRateLimiter) RecordSuccess(ip string, c *fiber.Ctx) {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	entry, exists := prl.entries[ip]
	if exists {
		// Reset failure counter on successful authentication
		if entry.consecutiveFailures > 0 {
			prl.logSecurityEvent("AUTH_SUCCESS", ip, c.Path(), c.Method(), "low",
				fmt.Sprintf("Authentication success after %d failures", entry.consecutiveFailures))
		}

		entry.consecutiveFailures = 0
		entry.firstFailure = time.Time{}
		entry.currentCapacity = prl.config.BaseCapacity
		entry.isLockedOut = false
		entry.lockoutUntil = time.Time{}
	}
}

// Helper methods
func (prl *ProgressiveRateLimiter) getClientIP(c *fiber.Ctx) string {
	remoteAddr := c.IP()
	if remoteAddr == "" || !prl.isValidIP(remoteAddr) {
		return ""
	}
	remoteAddr = prl.normalizeIP(remoteAddr)

	// Only trust forwarding headers from known proxy hops.
	// Loopback is always trusted to support local tunnel agents (e.g. cloudflared).
	if prl.trustedProxyMap[remoteAddr] || isLoopbackAddress(remoteAddr) {
		isTrustedHop := func(ip string) bool { return prl.trustedProxyMap[prl.normalizeIP(ip)] || isLoopbackAddress(ip) }
		if clientIP := extractForwardedClientIP(func(key string) string { return c.Get(key) }, prl.isValidIP, prl.normalizeIP, isTrustedHop); clientIP != "" {
			return clientIP
		}
	}

	return remoteAddr
}

func (prl *ProgressiveRateLimiter) isValidIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil
}

func (prl *ProgressiveRateLimiter) normalizeIP(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ip
	}

	if parsedIP.To4() != nil {
		return parsedIP.String()
	}

	return parsedIP.String()
}

func (prl *ProgressiveRateLimiter) isLockedOut(ip string) bool {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	entry, exists := prl.entries[ip]
	if !exists {
		return false
	}

	now := time.Now()
	return entry.isLockedOut && now.Before(entry.lockoutUntil)
}

func (prl *ProgressiveRateLimiter) getCurrentCapacity(ip string) int {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	entry, exists := prl.entries[ip]
	if !exists {
		return prl.config.BaseCapacity
	}
	return entry.currentCapacity
}

func (prl *ProgressiveRateLimiter) getRemainingTokens(ip string) int {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	entry, exists := prl.entries[ip]
	if !exists {
		return prl.config.BaseCapacity
	}
	return entry.currentCapacity
}

func (prl *ProgressiveRateLimiter) getResetTime(ip string) time.Time {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	entry, exists := prl.entries[ip]
	if !exists {
		return time.Now().Add(prl.config.BaseWindow)
	}
	return entry.currentWindow.Add(prl.config.BaseWindow)
}

// startCleanup starts the background cleanup goroutine
func (prl *ProgressiveRateLimiter) startCleanup() {
	prl.cleanupTicker = time.NewTicker(prl.baseConfig.CleanupInterval)

	go func() {
		for {
			select {
			case <-prl.cleanupTicker.C:
				prl.cleanup()
			case <-prl.stopCleanup:
				return
			}
		}
	}()
}

// cleanup removes expired entries
func (prl *ProgressiveRateLimiter) cleanup() {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, entry := range prl.entries {
		// Remove entries that haven't been used for the TTL period
		if now.After(entry.lastUpdated.Add(prl.baseConfig.EntryTTL)) {
			delete(prl.entries, key)
			expiredCount++
		}
	}

	prl.stats.TotalEntries -= int64(expiredCount)
	prl.stats.CleanupCount++
	prl.stats.LastCleanupTime = now
}

// GetProgressiveStats returns enhanced statistics for progressive rate limiting
func (prl *ProgressiveRateLimiter) GetProgressiveStats() map[string]interface{} {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	stats := make(map[string]interface{})

	// Basic stats
	stats["total_entries"] = len(prl.entries)
	stats["denied_count"] = prl.stats.DeniedCount
	stats["uptime"] = time.Since(prl.startTime).String()

	// Progressive stats
	lockedOutCount := 0
	totalFailures := 0
	totalAttempts := 0

	for _, entry := range prl.entries {
		if entry.isLockedOut {
			lockedOutCount++
		}
		totalFailures += entry.consecutiveFailures
		totalAttempts += entry.totalAttempts
	}

	stats["locked_out_ips"] = lockedOutCount
	stats["total_failures"] = totalFailures
	stats["total_attempts"] = totalAttempts
	stats["security_events"] = len(prl.securityEvents)

	// Estimate memory usage
	estimatedMemory := int64(len(prl.entries)) * 120 // Rough estimate
	stats["memory_usage_bytes"] = estimatedMemory

	return stats
}

// GetSecurityEvents returns recent security events
func (prl *ProgressiveRateLimiter) GetSecurityEvents(limit int) []SecurityEvent {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	if limit <= 0 || limit >= len(prl.securityEvents) {
		return prl.securityEvents
	}

	start := len(prl.securityEvents) - limit
	return prl.securityEvents[start:]
}

// SetEventCallback sets a callback function for security events
func (prl *ProgressiveRateLimiter) SetEventCallback(callback func(SecurityEvent)) {
	prl.mu.Lock()
	defer prl.mu.Unlock()
	prl.eventCallback = callback
}

// logSecurityEvent logs a security event.
// IMPORTANT: This method does NOT acquire a lock, and should only be called by methods that have already acquired the lock.
func (prl *ProgressiveRateLimiter) logSecurityEvent(eventType, ip, path, method, severity, description string) {
	if !prl.config.EnableLogging {
		return
	}

	event := SecurityEvent{
		Timestamp:   time.Now(),
		EventType:   eventType,
		IPAddress:   ip,
		Path:        path,
		Method:      method,
		Severity:    severity,
		Description: description,
	}

	prl.securityEvents = append(prl.securityEvents, event)

	// Keep only last 1000 events
	if len(prl.securityEvents) > 1000 {
		prl.securityEvents = prl.securityEvents[len(prl.securityEvents)-1000:]
	}

	// Call callback if set
	if prl.eventCallback != nil {
		go prl.eventCallback(event)
	}
}

// Stop gracefully shuts down the progressive rate limiter
func (prl *ProgressiveRateLimiter) Stop() {
	close(prl.stopCleanup)
	if prl.cleanupTicker != nil {
		prl.cleanupTicker.Stop()
	}
}
