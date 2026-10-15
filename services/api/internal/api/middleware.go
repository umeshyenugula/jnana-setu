package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type contextKey string

const correlationIDKey contextKey = "correlation_id"

// CorrelationID returns the request correlation ID stored in ctx.
func CorrelationID(ctx context.Context) string {
	if value, ok := ctx.Value(correlationIDKey).(string); ok {
		return value
	}
	return ""
}

// NewCorrelationID generates a URL-safe request ID without external dependencies.
func NewCorrelationID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(bytes)
}

// CorrelationMiddleware ensures every request and response has a correlation ID.
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if id == "" {
			id = NewCorrelationID()
		}
		ctx := context.WithValue(r.Context(), correlationIDKey, id)
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RecoveryMiddleware converts unexpected panics into the standard error envelope.
func RecoveryMiddleware(version string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred.", CorrelationID(r.Context()), version)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
