package services

import (
	"sync"
	"time"

	"github.com/yourusername/trough/models"
)

// settingsCache provides a small, threadsafe cache for site settings
// to avoid repeated DB reads on hot paths (CORS checks, SSR meta).
// TTL keeps values reasonably fresh while protecting performance.
var (
	settingsCache struct {
		mu       sync.RWMutex
		settings models.SiteSettings
		expires  time.Time
		ttl      time.Duration
	}
)

func init() {
	settingsCache.ttl = 30 * time.Second
}

// GetCachedSettings returns cached settings when fresh, otherwise fetches
// from the provided repo, updates the cache, and returns the value.
func GetCachedSettings(repo models.SiteSettingsRepositoryInterface) models.SiteSettings {
	now := time.Now()
	settingsCache.mu.RLock()
	if !settingsCache.expires.IsZero() && now.Before(settingsCache.expires) {
		s := settingsCache.settings
		settingsCache.mu.RUnlock()
		return s
	}
	settingsCache.mu.RUnlock()

	// Refresh under write lock
	settingsCache.mu.Lock()
	defer settingsCache.mu.Unlock()
	// Double-check in case another goroutine refreshed
	if !settingsCache.expires.IsZero() && time.Now().Before(settingsCache.expires) {
		return settingsCache.settings
	}
	if repo != nil {
		if s, err := repo.Get(); err == nil && s != nil {
			settingsCache.settings = *s
			settingsCache.expires = time.Now().Add(settingsCache.ttl)
			return settingsCache.settings
		}
	}
	// Return whatever we have, even if zero value
	return settingsCache.settings
}

// UpdateCachedSettings replaces the cache immediately and extends TTL.
func UpdateCachedSettings(s models.SiteSettings) {
	settingsCache.mu.Lock()
	settingsCache.settings = s
	settingsCache.expires = time.Now().Add(settingsCache.ttl)
	settingsCache.mu.Unlock()
}

// InvalidateSettingsCache clears the cache forcing next read to hit the repo.
func InvalidateSettingsCache() {
	settingsCache.mu.Lock()
	settingsCache.expires = time.Time{}
	settingsCache.mu.Unlock()
}
