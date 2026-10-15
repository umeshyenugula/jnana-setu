package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func testHandlers() *Handlers {
	svc, _ := testService()
	return &Handlers{Service: svc, Auth: InternalAuth{Token: "test-token"}, Version: "v1"}
}

func TestHTTPQuerySuccess(t *testing.T) {
	h := testHandlers()
	body := `{"organizationId":"` + orgA + `","query":"refund question","topK":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/retrieval/query", bytes.NewBufferString(body))
	req.Header.Set("X-Internal-Service-Token", "test-token")
	req = req.WithContext(context.WithValue(req.Context(), ctxCorrKey{}, api.CorrelationID(req.Context())))
	rec := httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Content-Type"))
	var env struct {
		Success bool   `json:"success"`
		Data    Result `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.True(t, env.Success)
	require.Equal(t, StatusResultsFound, env.Data.Status)
}

type ctxCorrKey struct{}

func TestHTTPAuthEnforcement(t *testing.T) {
	h := testHandlers()
	body := `{"organizationId":"` + orgA + `","query":"refund question"}`
	// Missing token.
	rec := httptest.NewRecorder()
	h.Query(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	// Wrong token.
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("X-Internal-Service-Token", "wrong")
	rec = httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	// Unconfigured token fails closed.
	closed := &Handlers{Service: h.Service, Version: "v1"}
	req2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req2.Header.Set("X-Internal-Service-Token", "anything")
	rec2 := httptest.NewRecorder()
	closed.Query(rec2, req2)
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
	// Browser-style JWT must NOT authenticate this endpoint (no bearer path).
	req3 := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req3.Header.Set("Authorization", "Bearer some-jwt")
	rec3 := httptest.NewRecorder()
	h.Query(rec3, req3)
	require.Equal(t, http.StatusUnauthorized, rec3.Code)
}

func TestHTTPBadBody(t *testing.T) {
	h := testHandlers()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{bad json`))
	req.Header.Set("X-Internal-Service-Token", "test-token")
	rec := httptest.NewRecorder()
	h.Query(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestPostgresRepositoryLive requires a configured Supabase development
// database with migrations 0001-0004 applied plus indexed chunks.
// Set TEST_SUPABASE_DB_URL to run; skipped otherwise.
func TestPostgresRepositoryLive(t *testing.T) {
	dsn := os.Getenv("TEST_SUPABASE_DB_URL")
	if dsn == "" {
		t.Skip("TEST_SUPABASE_DB_URL not set; live pgvector test skipped")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	repo := NewPostgresRepository(pool)
	vec := make([]float32, EmbeddingDimensions)
	vec[0] = 1
	_, err = repo.Search(ctx, orgA, vec, 5, nil)
	require.NoError(t, err) // schema/call contract holds; empty index returns no rows
}
