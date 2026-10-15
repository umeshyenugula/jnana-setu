package processing

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"github.com/stretchr/testify/require"
)

func testHandlers() *Handlers {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("hello"), ContentType: "text/plain", IsActive: true},
	}}
	svc, _, _, _ := testService(content)
	return &Handlers{Service: svc, Version: "v1"}
}

func withPrincipal(req *http.Request, userID string) *http.Request {
	return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserID: userID}))
}

func TestHTTPJobLifecycle(t *testing.T) {
	h := testHandlers()
	body := `{"organizationId":"org-1","documentId":"doc-1","versionId":"ver-1","operation":"index","idempotencyKey":"http-1"}`
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body)), "owner")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Job Job `json:"job"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	jobID := env.Data.Job.ID

	// Get as member of same org.
	rec = httptest.NewRecorder()
	h.Get(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), "owner"), jobID)
	require.Equal(t, http.StatusOK, rec.Code)

	// Process to completion, then retry must conflict.
	_, err := h.Service.Process(httptest.NewRequest(http.MethodGet, "/", nil).Context(), jobID)
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	h.Retry(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/", nil), "owner"), jobID)
	require.Equal(t, http.StatusConflict, rec.Code)

	// Cancel a fresh queued job.
	body2 := `{"organizationId":"org-1","documentId":"doc-1","versionId":"ver-1","operation":"index","idempotencyKey":"http-2"}`
	req2 := withPrincipal(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body2)), "owner")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.Submit(rec2, req2)
	var env2 struct {
		Data struct {
			Job Job `json:"job"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &env2))
	rec3 := httptest.NewRecorder()
	h.Cancel(rec3, withPrincipal(httptest.NewRequest(http.MethodPost, "/", nil), "owner"), env2.Data.Job.ID)
	require.Equal(t, http.StatusOK, rec3.Code)

	// Viewer submit forbidden; anonymous unauthorized.
	rec4 := httptest.NewRecorder()
	h.Submit(rec4, withPrincipal(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body2)), "viewer"))
	require.Equal(t, http.StatusForbidden, rec4.Code)
	rec5 := httptest.NewRecorder()
	h.Submit(rec5, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body2)))
	require.Equal(t, http.StatusUnauthorized, rec5.Code)
}

func TestHTTPHealthReadiness(t *testing.T) {
	h := testHandlers()
	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	rec = httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code) // stub embedder is always ready
}
