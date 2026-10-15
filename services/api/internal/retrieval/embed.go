package retrieval

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/processing"
)

// QueryEmbedder is Module 3's documented local embedding worker interface,
// reused — not copied — by Module 4. processing.HTTPWorker and
// processing.StubEmbedder both satisfy it.
type QueryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Health(ctx context.Context) error
	Name() string
}

// Bounds for query embedding requests.
const (
	MaxQueryChars   = 2000
	DefaultEmbedTimeout = 30 * time.Second
	QueryModelName  = processing.EmbedModelName
)

// normalizeQuery applies the same compatible preprocessing as document
// indexing: trim + whitespace collapse, no case folding (indexing does not
// fold case either). No tenant-specific preprocessing exists, so there is
// nothing tenant-scoped to leak through a cache; results are not cached.
func normalizeQuery(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

// EmbedQuery validates the query, embeds it locally, and validates the
// returned 384-dimensional finite vector. It never falls back to a hosted
// provider: worker errors are returned as controlled failures.
func EmbedQuery(ctx context.Context, embedder QueryEmbedder, query string, timeout time.Duration) ([]float32, error) {
	normalized := normalizeQuery(query)
	if normalized == "" {
		return nil, &ServiceError{Code: "INVALID_REQUEST", Message: "query must not be empty", Status: 400}
	}
	if len([]rune(normalized)) > MaxQueryChars {
		return nil, &ServiceError{Code: "INVALID_REQUEST", Message: fmt.Sprintf("query exceeds maximum length of %d characters", MaxQueryChars), Status: 400}
	}
	if timeout <= 0 {
		timeout = DefaultEmbedTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	vecs, err := embedder.Embed(ctx, []string{normalized})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &ServiceError{Code: "EMBEDDING_TIMEOUT", Message: "embedding worker timed out", Status: 503}
		}
		return nil, &ServiceError{Code: "EMBEDDING_UNAVAILABLE", Message: "embedding worker failed", Status: 503}
	}
	if len(vecs) != 1 {
		return nil, &ServiceError{Code: "EMBEDDING_INVALID", Message: "embedding worker returned wrong vector count", Status: 502}
	}
	if err := processing.ValidateEmbeddings(vecs); err != nil {
		return nil, &ServiceError{Code: "EMBEDDING_INVALID", Message: "invalid embedding vector: " + err.Error(), Status: 502}
	}
	if len(vecs[0]) != EmbeddingDimensions {
		return nil, &ServiceError{Code: "EMBEDDING_INVALID", Message: "embedding dimension mismatch", Status: 502}
	}
	return vecs[0], nil
}

// CosineSimilarity derives similarity from pgvector cosine distance.
func CosineSimilarity(distance float64) float64 {
	if math.IsNaN(distance) || math.IsInf(distance, 0) {
		return math.NaN()
	}
	return 1 - distance
}
