package endpoints

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"github.com/stretchr/testify/require"
)

func TestCORSPublicEndpoints(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := api.CORSMiddleware([]string{"https://dashboard.example.com"}, isPublicRouteLocal)(next)

	// Health endpoint - public, reflect origin
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://customer.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "https://customer.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "Origin", rec.Header().Get("Vary"))

	// Readiness endpoint - public
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	req.Header.Set("Origin", "https://customer.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "https://customer.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSAuthenticatedEndpoints(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := api.CORSMiddleware([]string{"https://dashboard.example.com"}, isPublicRouteLocal)(next)

	// Allowed origin
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "https://dashboard.example.com", rec.Header().Get("Access-Control-Allow-Origin"))

	// Disallowed origin
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))

	// No origin
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSPreflight(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight should not reach handler")
	})

	h := api.CORSMiddleware([]string{"https://dashboard.example.com"}, isPublicRouteLocal)(next)

	// Allowed origin preflight
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/organizations", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://dashboard.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Content-Type")
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")

	// Disallowed origin preflight
	req = httptest.NewRequest(http.MethodOptions, "/api/v1/organizations", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCORSNoOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := api.CORSMiddleware([]string{"https://dashboard.example.com"}, isPublicRouteLocal)(next)

	// No origin header - should pass through
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

// isPublicRouteLocal is a local copy for testing CORS middleware.
func isPublicRouteLocal(r *http.Request) bool {
	segs := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segs) < 3 || segs[0] != "api" || segs[1] != "v1" {
		return false
	}
	rest := segs[2:]
	switch {
	case len(rest) == 1 && (rest[0] == "health" || rest[0] == "ready"):
		return true
	case len(rest) == 2 && rest[0] == "widget-config":
		return true
	case len(rest) == 2 && rest[0] == "processing" && (rest[1] == "health" || rest[1] == "ready"):
		return true
	case len(rest) >= 1 && rest[0] == "chat":
		return true
	case len(rest) == 3 && rest[0] == "internal" && rest[1] == "inference" &&
		(rest[2] == "health" || rest[2] == "ready"):
		return true
	default:
		return false
	}
}
