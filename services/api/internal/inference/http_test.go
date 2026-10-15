package inference

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

func testHandlers(svc *Service) *Handlers {
	if svc == nil {
		svc = testService(&MockRuntime{Text: "Hello."})
	}
	return &Handlers{Service: svc, Auth: InternalAuth{Token: "test-token"}, Version: "v1"}
}

func authed(t *testing.T, h *Handlers, body string, call func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/generate", bytes.NewBufferString(body))
	req.Header.Set("X-Internal-Service-Token", "test-token")
	rec := httptest.NewRecorder()
	api.CorrelationMiddleware(http.HandlerFunc(call)).ServeHTTP(rec, req)
	_ = h
	return rec
}

func TestHTTPGenerateValid(t *testing.T) {
	h := testHandlers(nil)
	rec := authed(t, h, `{"messages":[{"role":"user","content":"Hi"}]}`, h.Generate)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success envelope")
	}
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("missing correlation ID")
	}
}

func TestHTTPUnauthorized(t *testing.T) {
	h := testHandlers(nil)
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/generate", bytes.NewBufferString(`{}`))
		if token != "" {
			req.Header.Set("X-Internal-Service-Token", token)
		}
		rec := httptest.NewRecorder()
		h.Generate(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status=%d", token, rec.Code)
		}
	}
	// Fail-closed when no token is configured server-side.
	closed := &Handlers{Service: h.Service, Version: "v1"}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/generate", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Internal-Service-Token", "test-token")
	rec := httptest.NewRecorder()
	closed.Generate(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured token: status=%d", rec.Code)
	}
}

func TestHTTPInvalidBody(t *testing.T) {
	h := testHandlers(nil)
	for _, body := range []string{
		`not-json`,
		`{"messages":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"modelPath":"/evil"}`,
	} {
		rec := authed(t, h, body, h.Generate)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status=%d", body, rec.Code)
		}
	}
}

func TestHTTPOverloadMapping(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := testHandlers(testService(&blockingRuntime{release: release}))
	// Occupy the single slot.
	done := make(chan struct{})
	go func() {
		defer close(done)
		authed(t, h, `{"messages":[{"role":"user","content":"hi"}]}`, h.Generate)
	}()
	waitForSlot(t, h)
	rec := authed(t, h, `{"messages":[{"role":"user","content":"hi"}]}`, h.Generate)
	if rec.Code != 429 {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	<-done
}

func waitForSlot(t *testing.T, h *Handlers) {
	t.Helper()
	for i := 0; i < 200; i++ {
		h.Service.metrics.mu.Lock()
		n := h.Service.metrics.Requests
		h.Service.metrics.mu.Unlock()
		if n >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("slot never occupied")
}

func TestHTTPHealthReadiness(t *testing.T) {
	h := testHandlers(nil)
	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/internal/v1/inference/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/internal/v1/inference/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready status=%d", rec.Code)
	}

	notReady := testHandlers(testService(&MockRuntime{NotReady: true}))
	rec = httptest.NewRecorder()
	notReady.Readiness(rec, httptest.NewRequest(http.MethodGet, "/internal/v1/inference/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
