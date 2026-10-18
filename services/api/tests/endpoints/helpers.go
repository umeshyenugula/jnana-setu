// Package endpoints provides comprehensive end-to-end API tests for every
// endpoint and error path across all modules (0-8).
package endpoints

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

// --- shared test helpers ---

// jsonBody creates a JSON request body.
func jsonBody(v any) *bytes.Buffer {
	b := new(bytes.Buffer)
	_ = json.NewEncoder(b).Encode(v)
	return b
}

// expectStatus checks the response status code.
func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, msg string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status=%d body=%s want=%d", msg, rec.Code, rec.Body.String(), want)
	}
}

// expectSuccess checks the response is a success envelope.
func expectSuccess(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad success envelope: %v body=%s", err, rec.Body.String())
	}
	if !env.Success {
		t.Fatalf("expected success=true body=%s", rec.Body.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T want map[string]any", env.Data)
	}
	return data
}

// expectError checks the response is an error envelope with the given code.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var env api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Success {
		t.Fatalf("expected success=false body=%s", rec.Body.String())
	}
	if env.Error.Code != wantCode {
		t.Fatalf("error code=%q want=%q body=%s", env.Error.Code, wantCode, rec.Body.String())
	}
}
