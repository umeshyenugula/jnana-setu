package endpoints

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestAuthVerifier(t *testing.T) {
	verifier := auth.NewVerifier(auth.VerifierConfig{
		URL:       "http://localhost:8080",
		JWTSecret: "test-secret-key",
		Issuer:    "http://localhost:8080/auth/v1",
		Audience:  "authenticated",
	})

	// Valid HS256 token
	token := createTestJWT(t, "user-1", "user@example.com", "test-secret-key", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(time.Hour))
	principal, err := verifier.VerifyToken(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "user-1", principal.UserID)
	require.Equal(t, "user@example.com", principal.Email)

	// Invalid signature
	badToken := createTestJWT(t, "user-1", "user@example.com", "wrong-secret", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(time.Hour))
	_, err = verifier.VerifyToken(context.Background(), badToken)
	require.Error(t, err)

	// Expired token
	expiredToken := createTestJWT(t, "user-1", "user@example.com", "test-secret-key", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(-time.Hour))
	_, err = verifier.VerifyToken(context.Background(), expiredToken)
	require.Error(t, err)

	// Missing subject
	noSubToken := createTestJWTNoSub(t, "test-secret-key", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(time.Hour))
	_, err = verifier.VerifyToken(context.Background(), noSubToken)
	require.Error(t, err)
}

func TestAuthMiddleware(t *testing.T) {
	verifier := auth.NewVerifier(auth.VerifierConfig{
		URL:       "http://localhost:8080",
		JWTSecret: "test-secret-key",
		Issuer:    "http://localhost:8080/auth/v1",
		Audience:  "authenticated",
	})

	// Valid token
	token := createTestJWT(t, "user-1", "user@example.com", "test-secret-key", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(time.Hour))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFromContext(r.Context())
		require.True(t, ok)
		require.Equal(t, "user-1", p.UserID)
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(verifier, next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// No auth header
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Invalid token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Wrong scheme
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Basic some-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddlewareValidatesAudience(t *testing.T) {
	verifier := auth.NewVerifier(auth.VerifierConfig{
		URL:       "http://localhost:8080",
		JWTSecret: "test-secret-key",
		Issuer:    "http://localhost:8080/auth/v1",
		Audience:  "other-audience",
	})

	// Wrong audience
	token := createTestJWT(t, "user-1", "user@example.com", "test-secret-key", "http://localhost:8080/auth/v1", "authenticated", time.Now().Add(time.Hour))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(verifier, next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
