package api

import "net/http"

// Router constructs the API HTTP handler. Route paths are versioned under /api/v1.
func Router(version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			if r.Method != http.MethodGet {
				WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The request method is not supported for this resource.", CorrelationID(r.Context()), APIVersion)
				return
			}
			Health(w, r, CorrelationID(r.Context()), version)
		default:
			WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.", CorrelationID(r.Context()), APIVersion)
		}
	})
}
