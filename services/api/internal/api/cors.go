package api

import (
	"net/http"
	"strings"
)

// Browser cross-origin policy for the API.
//
//   - Public endpoints (no Authorization header: health, readiness,
//     widget-config, chat, inference health/ready) reflect the request
//     Origin. This is safe: they carry no cookies or ambient credentials,
//     and per-widget/per-tenant authorization is still enforced server-side
//     on every request.
//   - Every other endpoint requires an exact allowlist match
//     (CORSAllowedOrigins). No match means no CORS headers and the browser
//     blocks the call (fail closed).
//   - Preflight OPTIONS is answered here (204) so it never reaches the
//     auth middleware, which would otherwise reject it for the missing
//     Authorization header.
//
// CORS alone is never authorization; it only tells browsers what the
// already-authorized server permits.
func CORSMiddleware(allowlist []string, isPublic func(r *http.Request) bool) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range allowlist {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(w, r) // non-browser caller: nothing to do
				return
			}
			public := isPublic != nil && isPublic(r)
			permitted := public || allowed[strings.TrimRight(origin, "/")]
			if r.Method == http.MethodOptions {
				if !permitted {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Correlation-ID, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if permitted {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			next.ServeHTTP(w, r)
		})
	}
}
