// Package middleware — adminmux.go
//
// AdminMux dispatches HTTP requests whose first path segment matches the
// currently-active admin suffix to a dedicated Gin engine, and everything
// else to the public engine. The active suffix is stored in an atomic.Value
// so RotateSuffix can hot-swap it without any lock or engine rebuild.
//
// Why not the previous `r.Group("/" + suffix)`? Because Gin freezes routes
// at registration time — a runtime suffix change had no effect on the
// route tree, so `RotateSuffix` (and the daily rotator goroutine) silently
// left the old URL live and the new one 404.
package middleware

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// AdminMux routes /{active-suffix}/... to adminEngine, everything else to
// publicEngine. It also blocks direct access to the internal admin alias
// (see AdminInternalPrefix) so operators can't reach the panel by guessing
// the alias.
type AdminMux struct {
	publicEngine http.Handler
	adminEngine  http.Handler
	active       atomic.Value // string
}

// AdminInternalPrefix is the fixed alias the admin Gin engine is mounted
// under internally. External requests never reach it directly — AdminMux
// rewrites /{suffix}/... to this prefix before handing off.
const AdminInternalPrefix = "/__admin"

func NewAdminMux(publicEngine, adminEngine http.Handler, initialSuffix string) *AdminMux {
	m := &AdminMux{
		publicEngine: publicEngine,
		adminEngine:  adminEngine,
	}
	m.SetSuffix(initialSuffix)
	return m
}

// SetSuffix atomically updates the active admin suffix. Safe to call from
// any goroutine; the next incoming request sees the new value.
func (m *AdminMux) SetSuffix(suffix string) {
	m.active.Store(suffix)
}

func (m *AdminMux) currentSuffix() string {
	if v, ok := m.active.Load().(string); ok {
		return v
	}
	return ""
}

func (m *AdminMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Deny direct hits on the internal alias so nobody can reach the panel
	// by URL-guessing after the suffix rotates.
	if r.URL.Path == AdminInternalPrefix || strings.HasPrefix(r.URL.Path, AdminInternalPrefix+"/") {
		http.NotFound(w, r)
		return
	}

	suffix := m.currentSuffix()
	if suffix != "" {
		// Match "/<suffix>" or "/<suffix>/..."
		if r.URL.Path == "/"+suffix || strings.HasPrefix(r.URL.Path, "/"+suffix+"/") {
			// Rewrite path so the admin engine (mounted at AdminInternalPrefix)
			// sees a stable prefix regardless of the current suffix.
			rewritten := AdminInternalPrefix + strings.TrimPrefix(r.URL.Path, "/"+suffix)
			// Clone the request rather than mutate the caller's copy.
			r2 := r.Clone(r.Context())
			r2.URL.Path = rewritten
			if r2.URL.RawPath != "" {
				r2.URL.RawPath = AdminInternalPrefix + strings.TrimPrefix(r2.URL.RawPath, "/"+suffix)
			}
			m.adminEngine.ServeHTTP(w, r2)
			return
		}
	}

	m.publicEngine.ServeHTTP(w, r)
}
