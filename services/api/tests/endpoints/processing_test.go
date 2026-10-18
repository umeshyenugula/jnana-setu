package endpoints

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"github.com/stretchr/testify/require"
)

func processingTestHandlers() *processing.Handlers {
	stores := processing.NewMemoryStores()
	auth := &stubAuthResolver{
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
	versions := map[string]processing.VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("test content"), ContentType: "text/plain", IsActive: true, CurrentVersionID: "ver-1"},
	}
	svc := processing.NewService(
		processing.DefaultConfig(),
		stores,
		stores,
		&stubEmbedder{},
		&stubContentSource{versions: versions},
		&stubReporter{},
		auth,
		stores,
	)
	return &processing.Handlers{Service: svc, Version: "v1"}
}

func TestProcessingJobLifecycle(t *testing.T) {
	h := processingTestHandlers()

	// Submit a job
	body := `{"organizationId":"org-1","documentId":"doc-1","versionId":"ver-1","operation":"index"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	job := data["job"].(map[string]any)
	require.Equal(t, "queued", job["status"])
	require.Equal(t, "org-1", job["organizationId"])
	require.Equal(t, "doc-1", job["documentId"])
	require.Equal(t, "ver-1", job["versionId"])
	require.Equal(t, "index", job["operation"])
	jobID := job["id"].(string)

	// Get job status
	req = httptest.NewRequest(http.MethodGet, "/api/v1/processing/jobs/"+jobID, nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Get(rec, req, jobID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	job = data["job"].(map[string]any)
	require.Equal(t, jobID, job["id"])

	// Cancel queued job
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs/"+jobID+"/cancel", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Cancel(rec, req, jobID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	job = data["job"].(map[string]any)
	require.Equal(t, "cancelled", job["status"])

	// Retry cancelled job should fail
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs/"+jobID+"/retry", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Retry(rec, req, jobID)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestProcessingPermissions(t *testing.T) {
	h := processingTestHandlers()

	body := `{"organizationId":"org-1","documentId":"doc-1","versionId":"ver-1","operation":"index"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	// Viewer cannot submit jobs
	req = withPrincipalAuth(req, "viewer")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// No auth
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Unknown org
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(`{"organizationId":"org-nope","documentId":"doc-1","versionId":"ver-1","operation":"index"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProcessingValidation(t *testing.T) {
	h := processingTestHandlers()

	// Missing fields
	body := `{"organizationId":"org-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Invalid operation
	body = `{"organizationId":"org-1","documentId":"doc-1","versionId":"ver-1","operation":"invalid"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Unknown org (org lookup happens before operation validation)
	body = `{"organizationId":"not-a-uuid","documentId":"doc-1","versionId":"ver-1","operation":"index"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/processing/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Submit(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProcessingHealthReadiness(t *testing.T) {
	h := processingTestHandlers()

	// Health endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/v1/processing/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Readiness endpoint
	req = httptest.NewRequest(http.MethodGet, "/api/v1/processing/ready", nil)
	rec = httptest.NewRecorder()
	h.Readiness(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
