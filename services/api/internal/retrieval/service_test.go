package retrieval

import (
	"context"
	"errors"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"github.com/stretchr/testify/require"
)

const (
	orgA = "11111111-1111-4111-8111-111111111111"
	orgB = "22222222-2222-4222-8222-222222222222"
	doc1 = "33333333-3333-4333-8333-333333333333"
	doc2 = "44444444-4444-4344-8344-444444444444"
	ver1 = "55555555-5555-4555-8555-555555555555"
	ver0 = "66666666-6666-4666-8666-666666666666"
)

// mapEmbedder returns a fixed vector per normalized query text.
type mapEmbedder struct {
	vecs map[string][]float32
	err  error
}

func unit(dim int, idx ...int) []float32 {
	v := make([]float32, dim)
	for _, i := range idx {
		v[i% dim] = 1
	}
	var n float64
	for _, f := range v {
		n += float64(f * f)
	}
	if n == 0 {
		v[0] = 1
		return v
	}
	for i := range v {
		v[i] /= float32(sqrtN(n))
	}
	return v
}

func sqrtN(n float64) float64 {
	if n <= 0 {
		return 0
	}
	x := n
	for i := 0; i < 50; i++ {
		x = 0.5 * (x + n/x)
	}
	return x
}

func (m *mapEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := m.vecs[t]
		if !ok {
			return nil, errors.New("no vector for query")
		}
		out[i] = v
	}
	return out, nil
}

func (m *mapEmbedder) Health(ctx context.Context) error { _ = ctx; return nil }
func (m *mapEmbedder) Name() string                     { return "map-test" }

func seedRepo() *MemoryRepository {
	r := NewMemoryRepository()
	r.Seed([]StoredChunk{
		{Candidate: Candidate{ChunkID: "chunk-a1", DocumentID: doc1, VersionID: ver1, Text: "refund period is 14 days", TokenCount: 6, SourceMeta: map[string]any{"page": 1}}, Embedding: unit(384, 1, 2), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
		{Candidate: Candidate{ChunkID: "chunk-a2", DocumentID: doc1, VersionID: ver1, Text: "contact billing support for invoices", TokenCount: 5}, Embedding: unit(384, 3, 4), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
		{Candidate: Candidate{ChunkID: "chunk-stale", DocumentID: doc1, VersionID: ver0, Text: "refund period is 14 days", TokenCount: 6}, Embedding: unit(384, 1, 2), OrgID: orgA, Active: false, DocActive: true, IsCurrent: false},
		{Candidate: Candidate{ChunkID: "chunk-b1", DocumentID: doc2, VersionID: ver1, Text: "refund period is 14 days", TokenCount: 6}, Embedding: unit(384, 1, 2), OrgID: orgB, Active: true, DocActive: true, IsCurrent: true},
		{Candidate: Candidate{ChunkID: "chunk-inactive-doc", DocumentID: doc2, VersionID: ver1, Text: "deactivated doc chunk", TokenCount: 3}, Embedding: unit(384, 1, 2), OrgID: orgA, Active: true, DocActive: false, IsCurrent: true},
		{Candidate: Candidate{ChunkID: "chunk-deleted", DocumentID: doc2, VersionID: ver1, Text: "deleted doc chunk", TokenCount: 3}, Embedding: unit(384, 1, 2), OrgID: orgA, Active: true, DocActive: true, DocDeleted: true, IsCurrent: true},
	})
	return r
}

func testService() (*Service, *MemoryRepository) {
	repo := seedRepo()
	emb := &mapEmbedder{vecs: map[string][]float32{
		"refund question": unit(384, 1, 2),
		"billing query":   unit(384, 3, 4),
		"far away query":  unit(384, 300),
	}}
	cfg := DefaultConfig()
	cfg.Rank.MinSimilarity = 0.1
	return NewService(cfg, repo, emb), repo
}

func TestRetrieveSuccess(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 5})
	require.NoError(t, err)
	require.Equal(t, StatusResultsFound, res.Status)
	require.NotEmpty(t, res.Evidence)
	top := res.Evidence[0]
	require.Equal(t, 1, top.Rank)
	require.Equal(t, "chunk-a1", top.ChunkID)
	require.Equal(t, doc1, top.DocumentID)
	require.Equal(t, ver1, top.VersionID)
	require.Equal(t, "refund period is 14 days", top.Text)
	require.Equal(t, map[string]any{"page": 1}, top.SourceMeta)
	require.Equal(t, QueryModelName, res.Model)
	require.Equal(t, EmbeddingDimensions, res.Dimensions)
	// Deterministic: repeat gives identical metadata.
	res2, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 5})
	require.NoError(t, err)
	require.Equal(t, res.Evidence, res2.Evidence)
}

func TestTenantIsolation(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 10})
	require.NoError(t, err)
	for _, e := range res.Evidence {
		require.NotEqual(t, "chunk-b1", e.ChunkID, "never return another tenant's documents")
	}
	resB, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgB, Text: "refund question", TopK: 10})
	require.NoError(t, err)
	require.Len(t, resB.Evidence, 1)
	require.Equal(t, "chunk-b1", resB.Evidence[0].ChunkID)
}

func TestVersionAndPublicationFiltering(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 10})
	require.NoError(t, err)
	for _, e := range res.Evidence {
		require.NotEqual(t, "chunk-stale", e.ChunkID)
		require.NotEqual(t, "chunk-inactive-doc", e.ChunkID)
		require.NotEqual(t, "chunk-deleted", e.ChunkID)
	}
}

func TestDocumentFilter(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "billing query", TopK: 5, Filters: Filters{DocumentIDs: []string{doc1}}})
	require.NoError(t, err)
	require.Equal(t, StatusResultsFound, res.Status)
	_, err = svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "billing query", Filters: Filters{DocumentIDs: []string{"not-a-uuid"}}})
	require.Error(t, err)
}

func TestQueryValidation(t *testing.T) {
	svc, _ := testService()
	_, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "   "})
	require.Error(t, err)
	_, err = svc.Retrieve(context.Background(), Query{OrganizationID: "not-a-uuid", Text: "refund question"})
	require.Error(t, err)
	_, err = svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 999})
	require.Error(t, err)
}

func TestTopKLimit(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 1})
	require.NoError(t, err)
	require.Len(t, res.Evidence, 1)
}

func TestInsufficientEvidence(t *testing.T) {
	svc, _ := testService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "far away query", TopK: 5})
	require.NoError(t, err)
	require.Equal(t, StatusInsufficientEvidence, res.Status)
	require.Empty(t, res.Evidence)
}

func TestEmptyIndex(t *testing.T) {
	emb := &mapEmbedder{vecs: map[string][]float32{"q": unit(384, 1)}}
	svc := NewService(DefaultConfig(), NewMemoryRepository(), emb)
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "q"})
	require.NoError(t, err)
	require.Equal(t, StatusInsufficientEvidence, res.Status)
}

func TestEmbeddingFailure(t *testing.T) {
	repo := seedRepo()
	svc := NewService(DefaultConfig(), repo, &processing.StubEmbedder{FailWith: errors.New("down")})
	_, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question"})
	require.Error(t, err)
	require.Equal(t, "EMBEDDING_UNAVAILABLE", err.(*ServiceError).Code)
}

func TestDatabaseFailure(t *testing.T) {
	svc, repo := testService()
	repo.InjectFailure(errors.New("connection refused"))
	_, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question"})
	require.Error(t, err)
}

func TestContextBudget(t *testing.T) {
	repo := seedRepo()
	emb := &mapEmbedder{vecs: map[string][]float32{"refund question": unit(384, 1, 2)}}
	cfg := DefaultConfig()
	cfg.Rank.MinSimilarity = -1
	cfg.Rank.MaxEvidenceTokens = 6 // only the first 6-token chunk fits
	svc := NewService(cfg, repo, emb)
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "refund question", TopK: 10})
	require.NoError(t, err)
	require.Len(t, res.Evidence, 1)
}
