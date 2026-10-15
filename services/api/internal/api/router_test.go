package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appversion "example.com/ai-knowledge-assistant/services/api/internal/version"
)

func TestHealthEndpoint(t *testing.T) {
	handler := CorrelationMiddleware(RecoveryMiddleware(appversion.Version, Router(appversion.Version)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("missing correlation ID header")
	}

	var response SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !response.Success {
		t.Fatal("expected success=true")
	}
	data, ok := response.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", response.Data)
	}
	if data["status"] != "ok" || data["service"] != "api" {
		t.Fatalf("unexpected health data: %#v", data)
	}
}

func TestNotFoundUsesErrorEnvelope(t *testing.T) {
	handler := CorrelationMiddleware(RecoveryMiddleware(appversion.Version, Router(appversion.Version)))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if response.Success || response.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
