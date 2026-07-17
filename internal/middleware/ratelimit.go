package middleware

import (
	"fmt"
	"net"
	"net/http"
	"shortlink/internal/config"
	"shortlink/internal/repository"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var globalRateLimit atomic.Value // *config.RateLimitConfig
var globalWhitelist atomic.Value // []string (IP/CIDR entries)

func init() {
	globalRateLimit.Store(&config.RateLimitConfig{CreatePerMinute: 10, RedirectPerMinute: 60})
	globalWhitelist.Store([]string{})
}

// SetRateLimitConfig updates the rate limit config at runtime.
func SetRateLimitConfig(cfg *config.RateLimitConfig) {
	if cfg != nil {
		globalRateLimit.Store(cfg)
	}
}

// GetRateLimitConfig returns the current rate limit config.
func GetRateLimitConfig() *config.RateLimitConfig {
	return globalRateLimit.Load().(*config.RateLimitConfig)
}

// SetRateLimitWhitelist updates the IP/CIDR whitelist atomically.
// Accepts a comma-separated string (e.g. "1.2.3.4,10.0.0.0/8").
func SetRateLimitWhitelist(raw string) {
	var entries []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			entries = append(entries, s)
		}
	}
	globalWhitelist.Store(entries)
}

// isWhitelisted returns true if ip matches any entry in the whitelist.
func isWhitelisted(ip string) bool {
	entries := globalWhitelist.Load().([]string)
	parsed := net.ParseIP(ip)
	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && parsed != nil {
				if cidr.Contains(parsed) {
					return true
				}
			}
		} else if entry == ip {
			return true
		}
	}
	return false
}

func RateLimit(cache *repository.CacheRepository, cfg *config.RateLimitConfig) gin.HandlerFunc {
	if cfg != nil {
		SetRateLimitConfig(cfg)
	}
	return func(c *gin.Context) {
		ip := c.ClientIP()
		path := c.Request.URL.Path

		// Exempt healthcheck probe and whitelisted IPs from rate limiting
		if path == "/api/config" && (ip == "127.0.0.1" || ip == "::1") {
			c.Next()
			return
		}
		if isWhitelisted(ip) {
			c.Next()
			return
		}

		cfg := globalRateLimit.Load().(*config.RateLimitConfig)

		var limit int
		var window time.Duration

		if c.Request.Method == "POST" && (path == "/api/links" || path == "/api/v1/links" || path == "/api/v1/links/batch") {
			limit = cfg.CreatePerMinute
			window = time.Minute
		} else {
			limit = cfg.RedirectPerMinute
			window = time.Minute
		}

		allowed, err := cache.RateLimitCheck(c.Request.Context(), fmt.Sprintf("ratelimit:%s:%s", ip, path), limit, window)
		if err != nil {
			// Redis unavailable — allow the request
			c.Next()
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
