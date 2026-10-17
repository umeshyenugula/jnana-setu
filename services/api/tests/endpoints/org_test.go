package endpoints

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"github.com/stretchr/testify/require"
)

func identityTestHandlers() *identity.Handlers {
	store := identity.NewMemoryStore()
	svc := identity.NewService(store)
	return &identity.Handlers{Service: svc, Version: "v1"}
}

func TestIdentityCreateOrganization(t *testing.T) {
	h := identityTestHandlers()

	// Valid create
	body := `{"name":"Acme Corp","slug":"acme"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-1")
	rec := httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	org := data["organization"].(map[string]any)
	require.Equal(t, "Acme Corp", org["name"])
	require.Equal(t, "acme", org["slug"])
	require.Equal(t, "user-1", org["creatorUserId"])
	membership := data["membership"].(map[string]any)
	require.Equal(t, string(identity.RoleOwner), membership["role"])
	require.Equal(t, org["id"], membership["organizationId"])

	// Duplicate slug
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-2")
	rec = httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)

	// Missing name
	body = `{"slug":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-3")
	rec = httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Missing slug
	body = `{"name":"Test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-3")
	rec = httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Invalid slug (uppercase gets lowercased, but a space stays invalid)
	body = `{"name":"Test","slug":"INVALID SLUG"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-3")
	rec = httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// No auth
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIdentityListOrganizations(t *testing.T) {
	h := identityTestHandlers()

	// Create org first
	body := `{"name":"Test Org","slug":"test-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-1")
	rec := httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	// List
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	req = withPrincipalAuth(req, "user-1")
	rec = httptest.NewRecorder()
	h.ListOrganizations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeSuccess(t, rec)
	orgs := data["organizations"].([]any)
	require.GreaterOrEqual(t, len(orgs), 1)

	// No auth
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	rec = httptest.NewRecorder()
	h.ListOrganizations(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIdentityGetOrganization(t *testing.T) {
	h := identityTestHandlers()

	// Create org
	body := `{"name":"Test Org","slug":"test-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "user-1")
	rec := httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	org := data["organization"].(map[string]any)
	orgID := org["id"].(string)

	// Get org
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/"+orgID, nil)
	req = withPrincipalAuth(req, "user-1")
	rec = httptest.NewRecorder()
	h.GetOrganization(rec, req, orgID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	org = data
	require.Equal(t, orgID, org["id"])

	// Non-member accessing org
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/"+orgID, nil)
	req = withPrincipalAuth(req, "user-99")
	rec = httptest.NewRecorder()
	h.GetOrganization(rec, req, orgID)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Unknown org
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/nonexistent-id", nil)
	req = withPrincipalAuth(req, "user-1")
	rec = httptest.NewRecorder()
	h.GetOrganization(rec, req, "nonexistent-id")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIdentityMembers(t *testing.T) {
	h := identityTestHandlers()

	// Create org
	body := `{"name":"Test Org","slug":"test-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	orgID := data["organization"].(map[string]any)["id"].(string)

	// List members (owner)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/"+orgID+"/members", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.ListMembers(rec, req, orgID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	members := data["members"].([]any)
	require.GreaterOrEqual(t, len(members), 1)
	require.Equal(t, string(identity.RoleOwner), members[0].(map[string]any)["role"])

	// Add member
	body = `{"userId":"new-user","role":"admin"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.AddMember(rec, req, orgID)
	require.Equal(t, http.StatusCreated, rec.Code)
	data = decodeSuccess(t, rec)
	member := data
	require.Equal(t, string(identity.RoleAdmin), member["role"])

	// Add a viewer member so the 403 (insufficient permissions) path is
	// exercised rather than 404 (not a member).
	body = `{"userId":"viewer","role":"viewer"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.AddMember(rec, req, orgID)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Viewer cannot add member
	body = `{"userId":"viewer2","role":"viewer"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "viewer")
	rec = httptest.NewRecorder()
	h.AddMember(rec, req, orgID)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Update member role
	body = `{"role":"viewer"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/"+orgID+"/members/viewer", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.UpdateMember(rec, req, orgID, "viewer")
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	member = data
	require.Equal(t, string(identity.RoleViewer), member["role"])

	// Delete member
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/organizations/"+orgID+"/members/viewer", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.DeleteMember(rec, req, orgID, "viewer")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestIdentityWidgets(t *testing.T) {
	h := identityTestHandlers()

	// Create org first
	body := `{"name":"Test Org","slug":"test-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.CreateOrganization(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	data := decodeSuccess(t, rec)
	orgID := data["organization"].(map[string]any)["id"].(string)

	// Create widget
	body = `{"displayName":"My Widget","allowedOrigins":["https://example.com"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/widgets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.CreateWidget(rec, req, orgID)
	require.Equal(t, http.StatusCreated, rec.Code)
	data = decodeSuccess(t, rec)
	widget := data
	require.Equal(t, "My Widget", widget["displayName"])
	require.Equal(t, "https://example.com", widget["allowedOrigins"].([]any)[0])
	require.True(t, widget["active"].(bool))
	widgetID := widget["id"].(string)

	// List widgets
	req = httptest.NewRequest(http.MethodGet, "/api/v1/organizations/"+orgID+"/widgets", nil)
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.ListWidgets(rec, req, orgID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	widgets := data["widgets"].([]any)
	require.Len(t, widgets, 1)

	// Update widget
	body = `{"displayName":"Updated Widget","allowedOrigins":["https://example.com","https://test.com"]}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/"+orgID+"/widgets/"+widgetID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.UpdateWidget(rec, req, orgID, widgetID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	widget = data
	require.Equal(t, "Updated Widget", widget["displayName"])
	require.Len(t, widget["allowedOrigins"].([]any), 2)

	// Add viewer as a member so the 403 (forbidden) path is exercised rather
	// than 404 (not a member).
	body = `{"userId":"viewer","role":"viewer"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.AddMember(rec, req, orgID)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Viewer cannot create widget
	body = `{"displayName":"Viewer Widget","allowedOrigins":["https://example.com"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/organizations/"+orgID+"/widgets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "viewer")
	rec = httptest.NewRecorder()
	h.CreateWidget(rec, req, orgID)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Widget config (public)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/widget-config/test-org-pub", nil)
	rec = httptest.NewRecorder()
	h.WidgetConfig(rec, req, "test-org-pub")
	require.Equal(t, http.StatusNotFound, rec.Code)
}
