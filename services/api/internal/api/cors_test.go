package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func publicTest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/v1/chat") ||
		strings.HasPrefix(r.URL.Path, "/api/v1/widget-config")
}

func TestCORSPublicReflectsOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CORSMiddleware([]string{"https://dashboard.example.com"}, publicTest)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", nil)
	req.Header.Set("Origin", "https://customer-site.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://customer-site.example.com" {
		t.Fatalf("origin not reflected: %q", got)
	}
}

func TestCORSStrictAllowlist(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CORSMiddleware([]string{"https://dashboard.example.com"}, publicTest)(next)

	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	allowed.Header.Set("Origin", "https://dashboard.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, allowed)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Fatalf("allowlisted origin missing: %q", got)
	}

	denied := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	denied.Header.Set("Origin", "https://evil.example.com")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, denied)
	if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unlisted origin permitted: %q", got)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("request itself must still run server-side: %d", rec2.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	// Auth would 401 a preflight (no Authorization header), so CORS must
	// answer OPTIONS before routing/auth run.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("preflight must not reach the handler")
	})
	h := CORSMiddleware([]string{"https://dashboard.example.com"}, publicTest)(next)

	pre := httptest.NewRequest(http.MethodOptions, "/api/v1/organizations", nil)
	pre.Header.Set("Origin", "https://dashboard.example.com")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, pre)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("missing allow-headers: %v", rec.Header())
	}

	blocked := httptest.NewRequest(http.MethodOptions, "/api/v1/organizations", nil)
	blocked.Header.Set("Origin", "https://evil.example.com")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, blocked)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("unlisted preflight status = %d", rec2.Code)
	}
}

func TestCORSNoOriginPassthrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := CORSMiddleware(nil, nil)(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if !called || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("non-browser request mishandled")
	}
}
