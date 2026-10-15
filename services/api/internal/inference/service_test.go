package inference

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testService(rt Runtime) *Service {
	cfg := DefaultConfig()
	cfg.RequestTimeout = 5 * time.Second
	cfg.RuntimeProbeTime = 2 * time.Second
	return NewService(cfg, rt)
}

func TestGenerateValid(t *testing.T) {
	svc := testService(&MockRuntime{Text: "Our hours are 9-5."})
	res, err := svc.Generate(context.Background(), validReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "Our hours are 9-5." || res.FinishReason != FinishStop || res.Model != "qwen3-4b" {
		t.Fatalf("unexpected result: %+v", res)
	}
	m := svc.Metrics().Snapshot()
	if m["requests"] != int64(1) || m["success"] != int64(1) {
		t.Fatalf("metrics not recorded: %v", m)
	}
}

func TestGenerateStripsThinking(t *testing.T) {
	svc := testService(&MockRuntime{Text: "<think>plan</think>Answer."})
	res, err := svc.Generate(context.Background(), validReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "Answer." || !res.ThinkingStripped {
		t.Fatalf("thinking leaked: %+v", res)
	}
}

func TestGenerateEmptyOutput(t *testing.T) {
	svc := testService(&MockRuntime{Text: "<think>only thinking</think>"})
	_, err := svc.Generate(context.Background(), validReq())
	se, ok := err.(*ServiceError)
	if !ok || se.Code != CodeGenerationFailed {
		t.Fatalf("expected GENERATION_FAILED, got %v", err)
	}
}

func TestGenerateRuntimeNotReady(t *testing.T) {
	svc := testService(&MockRuntime{NotReady: true})
	if err := svc.CheckReady(context.Background()); err == nil {
		t.Fatal("expected not-ready error")
	} else if se := err.(*ServiceError); se.Code != CodeModelNotReady || se.Status != 503 {
		t.Fatalf("wrong error: %+v", se)
	}
}

func TestGenerateTimeout(t *testing.T) {
	svc := testService(&MockRuntime{Delay: 10 * time.Second})
	_, err := svc.Generate(context.Background(), validReq())
	se, ok := err.(*ServiceError)
	if !ok || se.Code != CodeGatewayTimeout {
		t.Fatalf("expected GENERATION_TIMEOUT, got %v", err)
	}
	if svc.Metrics().Snapshot()["timeouts"] != int64(1) {
		t.Fatal("timeout not counted")
	}
}

func TestGenerateCancellation(t *testing.T) {
	svc := testService(&MockRuntime{Delay: 10 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := svc.Generate(ctx, validReq())
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestQueueExhaustion(t *testing.T) {
	release := make(chan struct{})
	blocking := &blockingRuntime{release: release}
	svc := testService(blocking)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = svc.Generate(context.Background(), validReq())
	}()
	time.Sleep(200 * time.Millisecond) // let first request take the slot
	_, err := svc.Generate(context.Background(), validReq())
	se, ok := err.(*ServiceError)
	if !ok || se.Code != CodeModelOverloaded || se.Status != 429 {
		t.Fatalf("expected MODEL_OVERLOADED/429, got %v", err)
	}
	close(release)
	wg.Wait()
	if svc.Metrics().Snapshot()["overloaded"] != int64(1) {
		t.Fatal("overload not counted")
	}
}

type blockingRuntime struct {
	release chan struct{}
}

func (b *blockingRuntime) Name() string { return "blocking" }
func (b *blockingRuntime) Ready(ctx context.Context) error {
	_ = ctx
	return nil
}
func (b *blockingRuntime) Generate(ctx context.Context, _ GenerationRequest, _ GenerationSettings) (RawResult, error) {
	select {
	case <-b.release:
		return RawResult{Text: "done", FinishReason: FinishStop}, nil
	case <-ctx.Done():
		return RawResult{}, ctx.Err()
	}
}

func TestGracefulShutdown(t *testing.T) {
	svc := testService(&MockRuntime{Delay: 300 * time.Millisecond})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Generate(context.Background(), validReq())
	}()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	<-done
}

func TestShutdownTimeout(t *testing.T) {
	svc := testService(&MockRuntime{Delay: 5 * time.Second})
	go func() { _, _ = svc.Generate(context.Background(), validReq()) }()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := svc.Shutdown(ctx); err == nil {
		t.Fatal("expected shutdown timeout error")
	}
}

// fakeTransport returns canned llama.cpp payloads for runtime client tests.
type fakeTransport struct {
	status int
	body   string
	err    error
}

func (f *fakeTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Status:     http.StatusText(f.status),
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func TestLlamaCppMalformedResponse(t *testing.T) {
	rt := NewLlamaCppRuntime(LlamaCppConfig{
		BaseURL:    "http://127.0.0.1:9",
		Model:      "qwen3-4b",
		HTTPClient: &http.Client{Transport: &fakeTransport{status: 200, body: "{not json"}},
	})
	_, err := rt.Generate(context.Background(), validReq(), GenerationSettings{})
	se, ok := err.(*ServiceError)
	if !ok || se.Code != CodeGenerationFailed {
		t.Fatalf("expected GENERATION_FAILED, got %v", err)
	}
}

func TestLlamaCppEmptyChoices(t *testing.T) {
	rt := NewLlamaCppRuntime(LlamaCppConfig{
		BaseURL:    "http://127.0.0.1:9",
		Model:      "qwen3-4b",
		HTTPClient: &http.Client{Transport: &fakeTransport{status: 200, body: `{"choices":[]}`}},
	})
	_, err := rt.Generate(context.Background(), validReq(), GenerationSettings{})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestLlamaCppNotReadyStatus(t *testing.T) {
	rt := NewLlamaCppRuntime(LlamaCppConfig{
		BaseURL:    "http://127.0.0.1:9",
		Model:      "qwen3-4b",
		HTTPClient: &http.Client{Transport: &fakeTransport{status: 503, body: `{"error":"loading"}`}},
	})
	_, err := rt.Generate(context.Background(), validReq(), GenerationSettings{})
	se, ok := err.(*ServiceError)
	if !ok || se.Code != CodeModelNotReady {
		t.Fatalf("expected MODEL_NOT_READY, got %v", err)
	}
}

func TestLlamaCppConnectionRefused(t *testing.T) {
	rt := NewLlamaCppRuntime(LlamaCppConfig{
		BaseURL:    "http://127.0.0.1:9",
		Model:      "qwen3-4b",
		HTTPClient: &http.Client{Transport: &fakeTransport{err: errors.New("refused")}},
	})
	if err := rt.Ready(context.Background()); err == nil {
		t.Fatal("expected readiness error")
	}
}

func TestLlamaCppSuccessParsing(t *testing.T) {
	body := "{\"choices\":[{\"message\":{\"content\":\"Hello!\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	think := false
	rt := NewLlamaCppRuntime(LlamaCppConfig{BaseURL: srv.URL, Model: "qwen3-4b", EnableThinking: &think})
	if err := rt.Ready(context.Background()); err != nil {
		t.Fatalf("ready failed: %v", err)
	}
	raw, err := rt.Generate(context.Background(), validReq(), GenerationSettings{})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if raw.Text != "Hello!" || raw.PromptTokens != 5 || raw.CompletionTokens != 2 {
		t.Fatalf("unexpected raw result: %+v", raw)
	}
}
