package auth

import (
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

func Middleware(verifier *Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			api.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid authorization header", api.CorrelationID(r.Context()), api.APIVersion)
			return
		}
		principal, err := verifier.VerifyToken(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			api.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired token", api.CorrelationID(r.Context()), api.APIVersion)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}
