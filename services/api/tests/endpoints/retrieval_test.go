package endpoints

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
	"github.com/stretchr/testify/require"
)

func retrievalTestHandlers() *retrieval.Handlers {
	store := &stubRetriever{
		result: retrieval.Result{
			Status: retrieval.StatusResultsFound,
			Evidence: []retrieval.Evidence{
				{
					Rank:       1,
					ChunkID:    "chunk-1",
					DocumentID: "doc-1",
					VersionID:  "ver-1",
					Text:       "Refund policy: 30 days",
					Similarity: 0.9,
					Distance:   0.1,
					TokenCount: 5,
					SourceMeta: map[string]any{"page": 2},
				},
			},
		},
	}
	svc := retrieval.NewService(retrieval.DefaultConfig(), store, &MockEmbedder{vecs: map[string][]float32{}})
	return &retrieval.Handlers{Service: svc, Version: "v1", Auth: retrieval.InternalAuth{Token: "test-token"}}
}

func TestRetrievalQuery(t *testing.T) {
	h := retrievalTestHandlers()

	// Valid query
	body := `{"organizationId":"11111111-1111-1111-1111-111111111111","query":"refund question","topK":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, retrieval.StatusResultsFound, data["status"])
	evidence := data["evidence"].([]any)
	require.Len(t, evidence, 1)
	ev := evidence[0].(map[string]any)
	require.Equal(t, "chunk-1", ev["chunkId"])
	require.Equal(t, "doc-1", ev["documentId"])
	require.Equal(t, "ver-1", ev["versionId"])
	require.Equal(t, float64(1), ev["rank"])
	require.Equal(t, "Refund policy: 30 days", ev["text"])

	// Empty query
	body = `{"organizationId":"11111111-1111-1111-1111-111111111111","query":"   "}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Missing org ID
	body = `{"query":"refund question"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Invalid org ID
	body = `{"organizationId":"not-a-uuid","query":"refund question"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Too large topK
	body = `{"organizationId":"11111111-1111-1111-1111-111111111111","query":"refund question","topK":999}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Empty index
	emptyStore := &stubRetriever{
		result: retrieval.Result{Status: retrieval.StatusInsufficientEvidence, Evidence: []retrieval.Evidence{}},
	}
	emptySvc := retrieval.NewService(retrieval.DefaultConfig(), emptyStore, &MockEmbedder{vecs: map[string][]float32{}})
	emptyHandlers := &retrieval.Handlers{Service: emptySvc, Version: "v1", Auth: retrieval.InternalAuth{Token: "test-token"}}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(`{"organizationId":"11111111-1111-1111-1111-111111111111","query":"refund question"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	emptyHandlers.Query(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	require.Equal(t, retrieval.StatusInsufficientEvidence, data["status"])
	require.Empty(t, data["evidence"])
}

func TestRetrievalPermission(t *testing.T) {
	h := retrievalTestHandlers()

	body := `{"organizationId":"11111111-1111-1111-1111-111111111111","query":"refund question"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	// No auth
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRetrievalEmbeddingFailure(t *testing.T) {
	h := retrievalTestHandlers()

	body := `{"organizationId":"11111111-1111-1111-1111-111111111111","query":"refund question"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
