package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"github.com/stretchr/testify/require"
)

func testHandlers() (*Handlers, *stubResolver) {
	resolver := &stubResolver{
		memberships: map[string]identity.Membership{
			"org-1:owner":  {OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner},
			"org-1:viewer": {OrganizationID: "org-1", UserID: "viewer", Role: identity.RoleViewer},
		},
		orgs: map[string]identity.Organization{"org-1": {ID: "org-1", Status: identity.OrganizationActive}},
	}
	svc := NewService(NewMemoryStore(), NewMemoryStorage(), resolver)
	return &Handlers{Service: svc, Version: "v1"}, resolver
}

func withPrincipal(req *http.Request, userID string) *http.Request {
	return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserID: userID}))
}

func decodeSuccess(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.True(t, env.Success)
	return env.Data
}

func TestHTTPTextLifecycle(t *testing.T) {
	h, _ := testHandlers()
	body := `{"title":"FAQ","contentText":"hello customers"}`
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body)), "owner")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	doc := decodeSuccess(t, rec)["document"].(map[string]any)
	docID := doc["id"].(string)

	// Get.
	rec = httptest.NewRecorder()
	h.Get(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), "owner"), "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)

	// Status.
	rec = httptest.NewRecorder()
	h.Status(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), "owner"), "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)

	// Deactivate via PATCH.
	patch := `{"isActive":false}`
	req2 := withPrincipal(httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(patch)), "owner")
	req2.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Patch(rec, req2, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)

	// Reprocess.
	rec = httptest.NewRecorder()
	h.Reprocess(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/", nil), "owner"), "org-1", docID)
	require.Equal(t, http.StatusAccepted, rec.Code)

	// Delete.
	rec = httptest.NewRecorder()
	h.Delete(rec, withPrincipal(httptest.NewRequest(http.MethodDelete, "/", nil), "owner"), "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHTTPFileUploadAndValidation(t *testing.T) {
	h, _ := testHandlers()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "guide.pdf")
	_, _ = fw.Write([]byte("%PDF-1.4 body"))
	_ = mw.Close()
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/x", &buf), "owner")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.CreateFile(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)

	// Unsupported type rejected with 400 envelope.
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	fw2, _ := mw2.CreateFormFile("file", "evil.exe")
	_, _ = fw2.Write([]byte("MZ"))
	_ = mw2.Close()
	req2 := withPrincipal(httptest.NewRequest(http.MethodPost, "/x", &buf2), "owner")
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	h.CreateFile(rec2, req2, "org-1")
	require.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestHTTPPermissionsAndTenantIsolation(t *testing.T) {
	h, _ := testHandlers()
	body := `{"title":"A","contentText":"x"}`
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)), "viewer")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusForbidden, rec.Code)

	// No principal => 401.
	req2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.CreateText(rec2, req2, "org-1")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Unknown org => 404 (not leak).
	req3 := withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), "owner")
	rec3 := httptest.NewRecorder()
	h.List(rec3, req3, "org-nope")
	require.Equal(t, http.StatusNotFound, rec3.Code)
}

func TestHTTPStoragePathIsServerGenerated(t *testing.T) {
	h, _ := testHandlers()
	body := `{"title":"T","contentText":"../../etc/passwd"}`
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)), "owner")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	ver := data["version"].(map[string]any)
	sp := ver["storagePath"].(string)
	require.NotContains(t, sp, "..")
	require.Contains(t, sp, "org/org-1/doc/")
	_ = context.Background()
	_ = api.APIVersion
}
