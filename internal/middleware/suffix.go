package middleware

import (
	"shortlink/internal/repository"
	"shortlink/internal/service"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// suffixCacheState is the per-process cached suffix used by SuffixCheck.
// We keep it at package level so callers (main.go, admin handler) can
// invalidate it via InvalidateSuffixCache when the operator rotates.
var (
	suffixMu     sync.RWMutex
	suffixValue  string
	suffixExpiry time.Time
)

// InvalidateSuffixCache drops the in-memory cached suffix. Call this
// immediately after RotateSuffix so the next request re-reads from Redis
// or the database instead of serving the stale value for up to 5 minutes.
func InvalidateSuffixCache() {
	suffixMu.Lock()
	suffixValue = ""
	suffixExpiry = time.Time{}
	suffixMu.Unlock()
}

func SuffixCheck(adminSvc *service.AdminService, cache *repository.CacheRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 {
			c.Next()
			return
		}

		prefix := parts[0]

		// Fast path: check in-memory cache
		suffixMu.RLock()
		if suffixValue != "" && time.Now().Before(suffixExpiry) {
			if prefix == suffixValue {
				c.Set("is_admin_path", true)
				c.Set("admin_prefix", prefix)
			}
			suffixMu.RUnlock()
			c.Next()
			return
		}
		suffixMu.RUnlock()

		// Try Redis cache
		suffix, err := cache.GetSuffix(c.Request.Context())
		if err == nil && suffix != "" {
			suffixMu.Lock()
			suffixValue = suffix
			suffixExpiry = time.Now().Add(5 * time.Minute)
			suffixMu.Unlock()

			if prefix == suffix {
				c.Set("is_admin_path", true)
				c.Set("admin_prefix", prefix)
			}
			c.Next()
			return
		}

		// Fallback to database
		settings, err := adminSvc.GetSettings(c.Request.Context())
		if err == nil && settings != nil && settings.Suffix != "" {
			_ = cache.SetSuffix(c.Request.Context(), settings.Suffix, 1*time.Hour)

			suffixMu.Lock()
			suffixValue = settings.Suffix
			suffixExpiry = time.Now().Add(5 * time.Minute)
			suffixMu.Unlock()

			if prefix == settings.Suffix {
				c.Set("is_admin_path", true)
				c.Set("admin_prefix", prefix)
			}
		}
		c.Next()
	}
}
