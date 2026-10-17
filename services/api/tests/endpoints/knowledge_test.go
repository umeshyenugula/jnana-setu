package endpoints

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
	"github.com/stretchr/testify/require"
)

func knowledgeTestHandlers() *knowledge.Handlers {
	resolver := &stubAuthResolver{
		members: map[string]identity.Membership{
			"org-1:owner":  {OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner},
			"org-1:admin":  {OrganizationID: "org-1", UserID: "admin", Role: identity.RoleAdmin},
			"org-1:viewer": {OrganizationID: "org-1", UserID: "viewer", Role: identity.RoleViewer},
			"org-2:owner":  {OrganizationID: "org-2", UserID: "owner", Role: identity.RoleOwner},
		},
		orgs: map[string]identity.Organization{
			"org-1": {ID: "org-1", Status: identity.OrganizationActive},
			"org-2": {ID: "org-2", Status: identity.OrganizationActive},
		},
	}
	svc := knowledge.NewService(knowledge.NewMemoryStore(), knowledge.NewMemoryStorage(), resolver)
	return &knowledge.Handlers{Service: svc, Version: "v1"}
}

func TestKnowledgeTextLifecycle(t *testing.T) {
	h := knowledgeTestHandlers()

	body := `{"title":"FAQ","contentText":"Hello customers"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	doc := data["document"].(map[string]any)
	docID := doc["id"].(string)
	ver := data["version"].(map[string]any)
	require.Equal(t, float64(1), ver["versionNumber"])
	require.Equal(t, "FAQ", doc["title"])
	require.Equal(t, "text", doc["sourceType"])
	require.True(t, doc["isActive"].(bool))
	require.NotNil(t, doc["currentVersionId"])

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-1/knowledge/"+docID, nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Get(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	doc = data["document"].(map[string]any)
	require.Equal(t, docID, doc["id"])

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-1/knowledge/"+docID+"/status", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Status(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	require.Equal(t, "pending", data["processingStatus"])

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-1/knowledge", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.List(rec, req, "org-1")
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	docs := data["documents"].([]any)
	require.GreaterOrEqual(t, len(docs), 1)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-1/knowledge/"+docID+"/versions", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.ListVersions(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	vers := data["versions"].([]any)
	require.Len(t, vers, 1)
	require.Equal(t, float64(1), vers[0].(map[string]any)["versionNumber"])

	patch := `{"title":"Updated FAQ"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/org-1/knowledge/"+docID, bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Patch(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	doc = data["document"].(map[string]any)
	require.Equal(t, "Updated FAQ", doc["title"])

	patch = `{"isActive":false}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/org-1/knowledge/"+docID, bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Patch(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	doc = data["document"].(map[string]any)
	require.False(t, doc["isActive"].(bool))

	patch = `{"isActive":true}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/org-1/knowledge/"+docID, bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Patch(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	doc = data["document"].(map[string]any)
	require.True(t, doc["isActive"].(bool))

	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/"+docID+"/reprocess", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Reprocess(rec, req, "org-1", docID)
	require.Equal(t, http.StatusAccepted, rec.Code)

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/organizations/org-1/knowledge/"+docID, nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Delete(rec, req, "org-1", docID)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-1/knowledge/"+docID, nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Get(rec, req, "org-1", docID)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKnowledgeTextValidation(t *testing.T) {
	h := knowledgeTestHandlers()

	body := `{"title":"","contentText":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"title":"FAQ","contentText":"   "}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"contentText":"hello"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"title":"` + strings.Repeat("a", 201) + `","contentText":"hello"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"title":"FAQ","contentText":"` + strings.Repeat("x", 200001) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestKnowledgePermissions(t *testing.T) {
	h := knowledgeTestHandlers()

	body := `{"title":"FAQ","contentText":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "viewer")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusForbidden, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "admin")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-nope/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-nope")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKnowledgeCrossTenant(t *testing.T) {
	h := knowledgeTestHandlers()

	body := `{"title":"Org1 Doc","contentText":"org1 content"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	docID := data["document"].(map[string]any)["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-2/knowledge/"+docID, nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Get(rec, req, "org-2", docID)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Cross-tenant create: viewer is not a member of org-2
	body = `{"title":"Org2 Doc","contentText":"cross"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-2/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "viewer")
	rec = httptest.NewRecorder()
	h.CreateText(rec, req, "org-2")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKnowledgeFileUpload(t *testing.T) {
	h := knowledgeTestHandlers()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "guide.pdf")
	_, _ = fw.Write([]byte("%PDF-1.4 body"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateFile(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, "application/pdf", data["version"].(map[string]any)["contentType"])

	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	fw2, _ := mw2.CreateFormFile("file", "evil.exe")
	_, _ = fw2.Write([]byte("MZ"))
	_ = mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/files", &buf2)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	req2 = withPrincipalAuth(req2, "owner")
	rec2 := httptest.NewRecorder()
	h.CreateFile(rec2, req2, "org-1")
	require.Equal(t, http.StatusBadRequest, rec2.Code)

	var buf3 bytes.Buffer
	mw3 := multipart.NewWriter(&buf3)
	fw3, _ := mw3.CreateFormFile("file", "empty.pdf")
	_, _ = fw3.Write([]byte{})
	_ = mw3.Close()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/files", &buf3)
	req3.Header.Set("Content-Type", mw3.FormDataContentType())
	req3 = withPrincipalAuth(req3, "owner")
	rec3 := httptest.NewRecorder()
	h.CreateFile(rec3, req3, "org-1")
	require.Equal(t, http.StatusBadRequest, rec3.Code)

	var buf4 bytes.Buffer
	mw4 := multipart.NewWriter(&buf4)
	_ = mw4.Close()
	req4 := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/files", &buf4)
	req4.Header.Set("Content-Type", mw4.FormDataContentType())
	req4 = withPrincipalAuth(req4, "owner")
	rec4 := httptest.NewRecorder()
	h.CreateFile(rec4, req4, "org-1")
	require.Equal(t, http.StatusBadRequest, rec4.Code)
}

func TestKnowledgeStoragePathSafe(t *testing.T) {
	h := knowledgeTestHandlers()

	body := `{"title":"T","contentText":"../../etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/org-1/knowledge/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateText(rec, req, "org-1")
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	ver := data["version"].(map[string]any)
	sp := ver["storagePath"].(string)
	require.NotContains(t, sp, "..")
	require.Contains(t, sp, "org/org-1/doc/")
}
