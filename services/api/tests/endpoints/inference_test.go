package endpoints

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"github.com/stretchr/testify/require"
)

func inferenceTestHandlers() *inference.Handlers {
	svc := inference.NewService(
		inference.DefaultConfig(),
		&testRuntime{text: "This is the generated answer."},
	)
	return &inference.Handlers{Service: svc, Version: "v1", Auth: inference.InternalAuth{Token: "test-token"}}
}

type testRuntime struct {
	text string
	err  error
}

func (s *testRuntime) Generate(ctx context.Context, req inference.GenerationRequest, settings inference.GenerationSettings) (inference.RawResult, error) {
	_ = ctx
	_ = settings
	if s.err != nil {
		return inference.RawResult{}, s.err
	}
	return inference.RawResult{
		Text:             s.text,
		FinishReason:     inference.FinishStop,
		PromptTokens:     10,
		CompletionTokens: 5,
	}, nil
}

func (s *testRuntime) Ready(ctx context.Context) error { return nil }
func (s *testRuntime) Name() string                   { return "stub" }

func authHeader(req *http.Request) *http.Request {
	req.Header.Set("X-Internal-Service-Token", "test-token")
	return req
}

func TestInferenceGenerate(t *testing.T) {
	h := inferenceTestHandlers()

	body := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"What is the refund policy?"}],"settings":{"temperature":0.3,"topP":0.9,"maxTokens":512}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, "This is the generated answer.", data["text"])
	require.Equal(t, "stop", data["finishReason"])
	require.Equal(t, "qwen3-4b", data["model"])
	require.False(t, data["thinkingStripped"].(bool))

	// Empty messages
	body = `{"messages":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Empty message content
	body = `{"messages":[{"role":"user","content":"   "}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Invalid temperature
	body = `{"messages":[{"role":"user","content":"hello"}],"settings":{"temperature":5.0}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Invalid topP
	body = `{"messages":[{"role":"user","content":"hello"}],"settings":{"topP":1.5}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Too many messages
	messages := make([]map[string]string, 21)
	for i := 0; i < 21; i++ {
		messages[i] = map[string]string{"role": "user", "content": "hi"}
	}
	bodyBytes, _ := json.Marshal(map[string]any{"messages": messages})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// No auth
	body = `{"messages":[{"role":"user","content":"hello"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestInferenceGeneratorFailure(t *testing.T) {
	svc := inference.NewService(
		inference.DefaultConfig(),
		&testRuntime{text: "", err: errors.New("model crashed")},
	)
	h := &inference.Handlers{Service: svc, Version: "v1", Auth: inference.InternalAuth{Token: "test-token"}}

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestInferenceGeneratorEmpty(t *testing.T) {
	svc := inference.NewService(
		inference.DefaultConfig(),
		&testRuntime{text: ""},
	)
	h := &inference.Handlers{Service: svc, Version: "v1", Auth: inference.InternalAuth{Token: "test-token"}}

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/inference/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	h.Generate(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestInferenceHealthReadiness(t *testing.T) {
	h := inferenceTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/internal/inference/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/internal/inference/ready", nil)
	rec = httptest.NewRecorder()
	h.Readiness(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
