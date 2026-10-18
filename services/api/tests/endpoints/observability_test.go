package endpoints

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/observability"
	"github.com/stretchr/testify/require"
)

func observabilityTestHandlers() *observability.Handlers {
	reg := observability.NewRegistry()
	readyGauge := reg.RegisterGauge("platform_ready", "Readiness.", observability.LabelService)

	checks := []observability.Check{
		{
			Name:      "db",
			Mandatory: true,
			Probe: func(ctx context.Context) error { return nil },
		},
		{
			Name:      "embeddings",
			Mandatory: false,
			Probe:     func(ctx context.Context) error { return nil },
		},
	}
	checker := observability.NewChecker("api", checks, 0, 0, readyGauge)
	observability.RegisterInferenceCollector(reg, func() observability.InferenceSnapshot {
		return observability.InferenceSnapshot{}
	})
	handler := &observability.Handlers{
		Version:   "v1",
		Checker:   checker,
		Reg:       reg,
		Token:     "test-token",
	}
	return handler
}

func TestReadinessEndpoint(t *testing.T) {
	h := observabilityTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, "ready", data["status"])
	require.Equal(t, "api", data["service"])
	require.Len(t, data["dependencies"], 2)
}

func TestReadinessNotReady(t *testing.T) {
	reg := observability.NewRegistry()
	readyGauge := reg.RegisterGauge("platform_ready", "Readiness.", observability.LabelService)

	checks := []observability.Check{
		{
			Name:      "db",
			Mandatory: true,
			Probe:     func(ctx context.Context) error { return errors.New("db down") },
		},
	}
	checker := observability.NewChecker("api", checks, 0, 0, readyGauge)
	h := &observability.Handlers{
		Version:   "v1",
		Checker:   checker,
		Reg:       reg,
		Token:     "test-token",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, "not_ready", data["status"])
}

func TestMetricsAuth(t *testing.T) {
	h := observabilityTestHandlers()

	// No auth
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	h.Metrics(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Wrong token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	h.Metrics(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Correct token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	h.Metrics(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/plain; version=0.0.4; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	require.Contains(t, body, "llm_requests_total")
	require.Contains(t, body, "llm_overloaded_total")
	require.Contains(t, body, "platform_ready")
}

func TestReadinessToken(t *testing.T) {
	h := observabilityTestHandlers()

	// Readiness endpoint is public - no auth required
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
