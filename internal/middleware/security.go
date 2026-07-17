package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"shortlink/internal/service"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// assetOrigins tracks origins (scheme://host) used by operator-configured
// display assets (background image/video, favicon). Refreshed on settings save
// via ReloadCSPAssets.
var assetOrigins atomic.Value // []string

// SetCSPAssetOrigins sets the currently-active asset origins. Empty entries
// are ignored; pass nil/empty to clear.
func SetCSPAssetOrigins(origins []string) {
	clean := make([]string, 0, len(origins))
	seen := map[string]struct{}{}
	for _, origin := range origins {
		if origin == "" {
			continue
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		clean = append(clean, origin)
	}
	assetOrigins.Store(clean)
}

// ReloadCSPAssets reads the admin settings, extracts current display asset URLs,
// and updates the CSP whitelist. Call after settings save.
func ReloadCSPAssets(ctx context.Context, adminSvc *service.AdminService) {
	settings, err := adminSvc.GetSettings(ctx)
	if err != nil || settings == nil || settings.SettingsEnc == "" {
		SetCSPAssetOrigins(nil)
		return
	}
	plain, err := adminSvc.DecryptSettings(settings.SettingsEnc)
	if err != nil {
		SetCSPAssetOrigins(nil)
		return
	}
	var s struct {
		BGURL      string `json:"bg_url"`
		FaviconURL string `json:"favicon_url"`
		QRLogoURL  string `json:"qr_logo_url"`
	}
	if err := json.Unmarshal([]byte(plain), &s); err != nil {
		SetCSPAssetOrigins(nil)
		return
	}
	SetCSPAssetOrigins([]string{originOf(s.BGURL), originOf(s.FaviconURL), originOf(s.QRLogoURL)})
}

func originOf(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func currentAssetOrigins() []string {
	if v, ok := assetOrigins.Load().([]string); ok {
		return v
	}
	return nil
}

// Security is a unified security middleware that replaces the old nginx bridge.
// Provides CSP, HSTS, XSS protection, frame options, and request sanitization.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Content type sniffing protection
		c.Header("X-Content-Type-Options", "nosniff")

		// Clickjacking protection
		c.Header("X-Frame-Options", "DENY")

		// XSS filter (legacy browsers)
		c.Header("X-XSS-Protection", "1; mode=block")

		// Referrer policy
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy — when the operator has configured a
		// background URL, the source may redirect through arbitrary CDNs
		// (some public wallpaper APIs randomize the final host per
		// request), so a single-origin whitelist is not enough. Allow any
		// https: source for images/videos in that case. This is a display
		// widening only — script/style/connect stay locked down.
		imgSrc := "'self' data: blob:"
		mediaSrc := "'self' blob:"
		origins := currentAssetOrigins()
		for _, origin := range origins {
			imgSrc += " " + origin
			mediaSrc += " " + origin
		}
		if len(origins) > 0 {
			// Background APIs often redirect through randomized CDN hosts; keep
			// only display asset buckets widened, never script/style/connect.
			imgSrc += " https:"
			mediaSrc += " https:"
		}

		c.Header("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://unpkg.com https://cdnjs.cloudflare.com https://challenges.cloudflare.com https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.jsdelivr.net; "+
				"img-src "+imgSrc+"; "+
				"media-src "+mediaSrc+"; "+
				"frame-src https://challenges.cloudflare.com https://cdn.jsdelivr.net; "+
				"worker-src 'self' blob:; "+
				"connect-src 'self' https://cdn.jsdelivr.net")

		// Feature policy
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		// HSTS (HTTPS only)
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Remove server fingerprint
		c.Header("Server", "")

		c.Next()
	}
}

// BodyLimit limits request body size (default 1MB).
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = 1 << 20 // 1MB
	}
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// TrustedProxyCheck marks bot traffic for analytics.
func TrustedProxyCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		ua := c.GetHeader("User-Agent")
		if ua == "" || strings.Contains(strings.ToLower(ua), "bot") {
			c.Set("is_bot", true)
		}
		c.Next()
	}
}

// InputSanitize strips null bytes and trims whitespace from form/query inputs.
func InputSanitize() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Sanitize query parameters
		query := c.Request.URL.Query()
		changed := false
		for key, vals := range query {
			for i, v := range vals {
				cleaned := strings.ReplaceAll(v, "\x00", "")
				cleaned = strings.TrimSpace(cleaned)
				if cleaned != v {
					vals[i] = cleaned
					changed = true
				}
			}
			query[key] = vals
		}
		if changed {
			c.Request.URL.RawQuery = query.Encode()
		}
		c.Next()
	}
}
