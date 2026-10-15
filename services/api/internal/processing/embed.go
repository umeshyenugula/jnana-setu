package processing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder generates one 384-dim embedding per input text. Implementations:
// HTTPWorker (local Python worker over loopback) and Stub (deterministic,
// dependency-free, for tests/dev without model weights).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Health(ctx context.Context) error
	Name() string
}

// Limits enforced before any model call.
const (
	MaxEmbedBatch  = 64
	MaxEmbedChars  = 8000
	EmbedTimeout   = 120 * time.Second
	EmbedModelName = "sentence-transformers/all-MiniLM-L6-v2"
)

// ValidateEmbeddings enforces dimension + finite values on every vector.
func ValidateEmbeddings(vecs [][]float32) error {
	for i, v := range vecs {
		if len(v) != EmbeddingDim {
			return fmt.Errorf("embedding %d: dimension %d, want %d", i, len(v), EmbeddingDim)
		}
		for j, f := range v {
			if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
				return fmt.Errorf("embedding %d: non-finite value at index %d", i, j)
			}
		}
	}
	return nil
}

// -------------------------------------------------------------- HTTP worker

// HTTPWorker talks to the local embedding worker's documented internal API:
//   POST {base}/embed  {"texts":[...]} -> {"embeddings":[[...]],"model":...}
//   GET  {base}/health -> {"status":"ok",...}
//   GET  {base}/ready  -> {"ready":true,"modelLoaded":true,...}
// The worker binds loopback only and is never publicly exposed; no hosted
// inference provider is ever invoked.
type HTTPWorker struct {
	base   string
	client *http.Client
}

func NewHTTPWorker(baseURL string, timeout time.Duration) *HTTPWorker {
	if timeout <= 0 {
		timeout = EmbedTimeout
	}
	return &HTTPWorker{base: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (w *HTTPWorker) Name() string { return "http-worker:" + w.base }

func (w *HTTPWorker) Health(ctx context.Context) error {
	for _, p := range []string{"/ready", "/health"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.base+p, nil)
		if err != nil {
			return err
		}
		resp, err := w.client.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		if p == "/ready" {
			var r struct {
				Ready       bool `json:"ready"`
				ModelLoaded bool `json:"modelLoaded"`
			}
			if err := json.Unmarshal(body, &r); err != nil || !r.Ready || !r.ModelLoaded {
				return fmt.Errorf("embedding worker not ready")
			}
		}
		return nil
	}
	return fmt.Errorf("embedding worker unhealthy")
}

func (w *HTTPWorker) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts to embed")
	}
	if len(texts) > MaxEmbedBatch {
		return nil, fmt.Errorf("batch of %d exceeds limit %d", len(texts), MaxEmbedBatch)
	}
	for i, t := range texts {
		if len(t) > MaxEmbedChars {
			return nil, fmt.Errorf("text %d exceeds %d chars", i, MaxEmbedChars)
		}
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("text %d is empty", i)
		}
	}
	payload, _ := json.Marshal(map[string]any{"texts": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, &embedError{code: ErrEmbeddingFailed, err: fmt.Sprintf("worker request failed: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &embedError{code: ErrEmbeddingFailed, err: fmt.Sprintf("worker status %s", resp.Status)}
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &embedError{code: ErrEmbeddingFailed, err: "invalid worker response"}
	}
	if out.Error != "" {
		return nil, &embedError{code: ErrEmbeddingFailed, err: out.Error}
	}
	if len(out.Embeddings) != len(texts) {
		return nil, &embedError{code: ErrEmbeddingFailed, err: "worker returned wrong embedding count"}
	}
	if err := ValidateEmbeddings(out.Embeddings); err != nil {
		return nil, &embedError{code: ErrEmbeddingFailed, err: err.Error()}
	}
	return out.Embeddings, nil
}

type embedError struct {
	code string
	err  string
}

func (e *embedError) Error() string { return e.code + ": " + e.err }

// ------------------------------------------------------------------- stub

// StubEmbedder is deterministic and dependency-free: each text maps to a
// unit-norm 384-dim vector derived from word hashes. Used for unit tests,
// offline development without model weights, and failure-injection tests.
// It must never be mistaken for real semantic embeddings.
type StubEmbedder struct {
	FailWith error
	Delay    time.Duration
}

func (s *StubEmbedder) Name() string { return "stub" }

func (s *StubEmbedder) Health(ctx context.Context) error {
	_ = ctx
	if s.FailWith != nil {
		return s.FailWith
	}
	return nil
}

func (s *StubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if s.Delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.Delay):
		}
	}
	if s.FailWith != nil {
		return nil, s.FailWith
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = stubVector(t)
	}
	return out, nil
}

func stubVector(text string) []float32 {
	v := make([]float32, EmbeddingDim)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := uint32(2166136261)
		for j := 0; j < len(w); j++ {
			h ^= uint32(w[j])
			h *= 16777619
		}
		v[int(h%EmbeddingDim)] += 1
	}
	var norm float64
	for _, f := range v {
		norm += float64(f * f)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		v[0] = 1
		return v
	}
	for i := range v {
		v[i] /= float32(norm)
	}
	return v
}
