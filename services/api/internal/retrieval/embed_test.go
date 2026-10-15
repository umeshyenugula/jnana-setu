package retrieval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"github.com/stretchr/testify/require"
)

func TestNormalizeQuery(t *testing.T) {
	require.Equal(t, "hello world", normalizeQuery("  hello   world\n"))
	require.Equal(t, "Hello World", normalizeQuery("Hello World")) // no case folding (matches indexing)
}

func TestEmbedQueryValidation(t *testing.T) {
	stub := &processing.StubEmbedder{}
	_, err := EmbedQuery(context.Background(), stub, "   ", 0)
	require.Error(t, err)
	require.Equal(t, 400, err.(*ServiceError).Status)
	_, err = EmbedQuery(context.Background(), stub, strings.Repeat("a", MaxQueryChars+1), 0)
	require.Error(t, err)
	vec, err := EmbedQuery(context.Background(), stub, "hello world", 0)
	require.NoError(t, err)
	require.Len(t, vec, EmbeddingDimensions)
}

func TestEmbedQueryWorkerFailure(t *testing.T) {
	broken := &processing.StubEmbedder{FailWith: errors.New("model crashed")}
	_, err := EmbedQuery(context.Background(), broken, "hello", 0)
	require.Error(t, err)
	require.Equal(t, "EMBEDDING_UNAVAILABLE", err.(*ServiceError).Code)
}

func TestEmbedQueryTimeout(t *testing.T) {
	slow := &processing.StubEmbedder{Delay: 5 * time.Second}
	_, err := EmbedQuery(context.Background(), slow, "hello", 20*time.Millisecond)
	require.Error(t, err)
	require.Equal(t, "EMBEDDING_TIMEOUT", err.(*ServiceError).Code)
}

func TestEmbedQueryInvalidVector(t *testing.T) {
	bad := &fixedEmbedder{vec: make([]float32, 10)}
	_, err := EmbedQuery(context.Background(), bad, "hello", 0)
	require.Error(t, err)
	require.Equal(t, "EMBEDDING_INVALID", err.(*ServiceError).Code)
}

type fixedEmbedder struct {
	vec []float32
	err error
}

func (f *fixedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	if f.err != nil {
		return nil, f.err
	}
	return [][]float32{f.vec}, nil
}

func (f *fixedEmbedder) Health(ctx context.Context) error { _ = ctx; return nil }
func (f *fixedEmbedder) Name() string                     { return "fixed-test" }
