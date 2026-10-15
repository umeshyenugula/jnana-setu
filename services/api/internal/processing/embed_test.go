package processing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateEmbeddings(t *testing.T) {
	ok := make([]float32, EmbeddingDim)
	require.NoError(t, ValidateEmbeddings([][]float32{ok}))
	require.Error(t, ValidateEmbeddings([][]float32{make([]float32, 10)}))
	bad := make([]float32, EmbeddingDim)
	bad[3] = float32(math.NaN())
	require.Error(t, ValidateEmbeddings([][]float32{bad}))
	inf := make([]float32, EmbeddingDim)
	inf[0] = float32(math.Inf(1))
	require.Error(t, ValidateEmbeddings([][]float32{inf}))
}

func TestStubEmbedderDims(t *testing.T) {
	s := &StubEmbedder{}
	vecs, err := s.Embed(context.Background(), []string{"hello world", "another text"})
	require.NoError(t, err)
	require.Len(t, vecs, 2)
	require.NoError(t, ValidateEmbeddings(vecs))
	// Deterministic + unit-norm.
	again, _ := s.Embed(context.Background(), []string{"hello world"})
	require.Equal(t, vecs[0], again[0])
	var norm float64
	for _, f := range vecs[0] {
		norm += float64(f * f)
	}
	require.InDelta(t, 1.0, norm, 1e-5)
}

func workerServer(vec []float32, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embed":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
		case "/ready":
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "modelLoaded": true})
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
	}))
}

func TestHTTPWorkerSuccessAndValidation(t *testing.T) {
	vec := make([]float32, EmbeddingDim)
	vec[0] = 1
	srv := workerServer(vec, http.StatusOK)
	defer srv.Close()
	w := NewHTTPWorker(srv.URL, 5*time.Second)
	require.NoError(t, w.Health(context.Background()))
	out, err := w.Embed(context.Background(), []string{"hi"})
	require.NoError(t, err)
	require.Len(t, out, 1)
}

func TestHTTPWorkerRejectsBadDims(t *testing.T) {
	srv := workerServer(make([]float32, 10), http.StatusOK)
	defer srv.Close()
	w := NewHTTPWorker(srv.URL, 5*time.Second)
	_, err := w.Embed(context.Background(), []string{"hi"})
	require.Error(t, err)
}

func TestHTTPWorkerFailureAndTimeout(t *testing.T) {
	// Connection refused.
	w := NewHTTPWorker("http://127.0.0.1:1", 2*time.Second)
	require.Error(t, w.Health(context.Background()))
	_, err := w.Embed(context.Background(), []string{"hi"})
	require.Error(t, err)

	// Slow server exceeds client timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer slow.Close()
	wt := NewHTTPWorker(slow.URL, 50*time.Millisecond)
	_, err = wt.Embed(context.Background(), []string{"hi"})
	require.Error(t, err)

	// Context cancellation with stub delay.
	stub := &StubEmbedder{Delay: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = stub.Embed(ctx, []string{"hi"})
	require.Error(t, err)

	// Model failure surfaces controlled error.
	broken := &StubEmbedder{FailWith: errors.New("model crashed")}
	_, err = broken.Embed(context.Background(), []string{"hi"})
	require.Error(t, err)
	require.Error(t, broken.Health(context.Background()))
}
