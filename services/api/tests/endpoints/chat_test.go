package endpoints

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"example.com/ai-knowledge-assistant/services/api/internal/chat"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
	"github.com/stretchr/testify/require"
)

func chatTestHandlers() *chat.Handlers {
	stores := chat.NewMemoryStore()
	tenants := &stubAuthResolver{
		members: map[string]identity.Membership{
			"org-1:owner":  {OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner},
			"org-1:viewer": {OrganizationID: "org-1", UserID: "viewer", Role: identity.RoleViewer},
			"org-2:owner":  {OrganizationID: "org-2", UserID: "owner", Role: identity.RoleOwner},
		},
		orgs: map[string]identity.Organization{
			"org-1": {ID: "org-1", Status: identity.OrganizationActive},
			"org-2": {ID: "org-2", Status: identity.OrganizationActive},
		},
	}
	retriever := &stubRetriever{
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
	gen := &chatGeneratorStub{text: "You can return items within 30 days."}
	svc := chat.NewService(
		chat.DefaultConfig(),
		tenants,
		retriever,
		gen,
		stores,
	)
	return &chat.Handlers{Service: svc, Version: "v1"}
}

type chatGeneratorStub struct {
	text string
	err  error
}

func (g *chatGeneratorStub) Generate(ctx context.Context, req inference.GenerationRequest) (inference.GenerationResult, error) {
	_ = ctx
	if g.err != nil {
		return inference.GenerationResult{}, g.err
	}
	return inference.GenerationResult{
		Text:             g.text,
		FinishReason:     inference.FinishStop,
		Model:            "qwen3-4b",
		ThinkingStripped: false,
	}, nil
}

func withPrincipalAuth(req *http.Request, userID string) *http.Request {
	return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserID: userID}))
}

func TestChatValidWorkflow(t *testing.T) {
	h := chatTestHandlers()

	body := `{"widgetId":"org-1","message":"What is the refund policy?"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://shop.example.com")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeSuccess(t, rec)
	require.Equal(t, "answered", data["status"])
	require.Equal(t, "You can return items within 30 days.", data["answer"])
	require.NotEmpty(t, data["sessionId"])
	require.Len(t, data["sources"], 1)
	source := data["sources"].([]any)[0].(map[string]any)
	require.Equal(t, "doc-1", source["documentId"])
	require.Equal(t, "ver-1", source["versionId"])
	require.Equal(t, float64(1), source["rank"])
	require.Equal(t, "Refund policy: 30 days", source["excerpt"])

	sessionID := data["sessionId"].(string)

	body = `{"widgetId":"org-1","sessionId":"` + sessionID + `","message":"Follow up?"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://shop.example.com")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	require.Equal(t, sessionID, data["sessionId"])

	req = httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions/"+sessionID+"?widgetId=org-1", nil)
	req.Header.Set("Origin", "https://shop.example.com")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.History(rec, req, sessionID)
	require.Equal(t, http.StatusOK, rec.Code)
	data = decodeSuccess(t, rec)
	messages := data["messages"].([]any)
	require.GreaterOrEqual(t, len(messages), 2)
}

func TestChatValidation(t *testing.T) {
	h := chatTestHandlers()

	body := `{"widgetId":"org-1","message":"   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"widgetId":"","message":"hello"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body = `{"widgetId":"org-1","message":"` + strings.Repeat("x", 2001) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestChatCrossTenant(t *testing.T) {
	h := chatTestHandlers()

	body := `{"widgetId":"org-1","message":"What is the refund policy?"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec := httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"widgetId":"nonexistent-widget","message":"What is the refund policy?"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipalAuth(req, "owner")
	rec = httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestChatNoAuth(t *testing.T) {
	h := chatTestHandlers()

	body := `{"widgetId":"org-1","message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Chat(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
