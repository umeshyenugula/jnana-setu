package endpoints

import (
	"context"
	"errors"
	"strings"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/chat"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
)

// stubAuthResolver implements Chat's TenantResolver for tests.
type stubAuthResolver struct {
	members map[string]identity.Membership
	orgs    map[string]identity.Organization
}

func (s *stubAuthResolver) ResolveWidget(ctx context.Context, publicWidgetID string) (chat.WidgetAccess, error) {
	_ = ctx
	// Check if publicWidgetID matches an org ID (used in tests where org ID is used as widget ID)
	if o, ok := s.orgs[publicWidgetID]; ok {
		return chat.WidgetAccess{
			OrganizationID: o.ID,
			WidgetID:       publicWidgetID,
			Active:         true,
		}, nil
	}
	// Check if publicWidgetID matches a user's org (user is member of org)
	for _, m := range s.members {
		if m.UserID == publicWidgetID && m.OrganizationID != "" {
			if o, ok := s.orgs[m.OrganizationID]; ok {
				return chat.WidgetAccess{
					OrganizationID: o.ID,
					WidgetID:       m.OrganizationID,
					Active:         true,
				}, nil
			}
		}
	}
	return chat.WidgetAccess{}, &identity.ServiceError{Code: "NOT_FOUND", Message: "widget not found", Status: 404}
}

func (s *stubAuthResolver) GetOrganization(ctx context.Context, id string) (identity.Organization, error) {
	_ = ctx
	o, ok := s.orgs[id]
	if !ok {
		return identity.Organization{}, &identity.ServiceError{Code: "NOT_FOUND", Message: "org not found", Status: 404}
	}
	return o, nil
}

func (s *stubAuthResolver) GetMembership(ctx context.Context, organizationID, userID string) (identity.Membership, bool, error) {
	_ = ctx
	m, ok := s.members[organizationID+":"+userID]
	return m, ok, nil
}

// stubEmbedder implements processing.Embedder for tests.
type stubEmbedder struct{}

func (s *stubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, processing.EmbeddingDim)
		out[i][0] = float32(1.0)
	}
	return out, nil
}

func (s *stubEmbedder) Health(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s *stubEmbedder) Name() string { return "stub" }

// stubContentSource implements processing.ContentSource for tests.
type stubContentSource struct {
	versions map[string]VersionContent
}

func (s *stubContentSource) FetchVersion(ctx context.Context, orgID, docID, versionID string) (VersionContent, error) {
	_ = ctx
	key := orgID + "/" + docID + "/" + versionID
	vc, ok := s.versions[key]
	if !ok {
		return VersionContent{}, &knowledge.ServiceError{Code: "NOT_FOUND", Message: "version not found", Status: 404}
	}
	return vc, nil
}

// stubReporter implements processing.StatusReporter for tests.
type stubReporter struct{}

func (s *stubReporter) MarkProcessing(ctx context.Context, orgID, docID string) error {
	_ = ctx
	_ = orgID
	_ = docID
	return nil
}

func (s *stubReporter) ReportResult(ctx context.Context, orgID, docID, versionID string, ok bool, detail string) error {
	_ = ctx
	_ = orgID
	_ = docID
	_ = versionID
	_ = ok
	_ = detail
	return nil
}

// VersionContent is alias for processing.VersionContent
type VersionContent = processing.VersionContent

// stubRetriever implements retrieval.Retriever for tests.
type stubRetriever struct {
	result retrieval.Result
	err    error
}

func (s *stubRetriever) Retrieve(ctx context.Context, q retrieval.Query) (retrieval.Result, error) {
	_ = ctx
	if s.err != nil {
		return retrieval.Result{}, s.err
	}
	return s.result, nil
}

// Search implements retrieval.ChunkRepository so the stub can back the
// retrieval service in endpoint tests.
func (s *stubRetriever) Search(ctx context.Context, orgID string, queryVec []float32, limit int, docIDs []string) ([]retrieval.Candidate, error) {
	_ = ctx
	_ = orgID
	_ = queryVec
	_ = limit
	_ = docIDs
	out := make([]retrieval.Candidate, 0, len(s.result.Evidence))
	for _, e := range s.result.Evidence {
		out = append(out, retrieval.Candidate{
			ChunkID:    e.ChunkID,
			DocumentID: e.DocumentID,
			VersionID:  e.VersionID,
			Text:       e.Text,
			TokenCount: e.TokenCount,
			SourceMeta: e.SourceMeta,
			Distance:   e.Distance,
		})
	}
	return out, nil
}

// stubGenerator implements inference.Runtime for tests.
type stubGenerator struct {
	text  string
	err   error
	delay time.Duration
}

func (s *stubGenerator) Generate(ctx context.Context, req inference.GenerationRequest) (inference.RawResult, error) {
	_ = ctx
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return inference.RawResult{}, ctx.Err()
		case <-time.After(s.delay):
		}
	}
	if s.err != nil {
		return inference.RawResult{}, s.err
	}
	return inference.RawResult{
		Text:             s.text,
		FinishReason:     inference.FinishStop,
		PromptTokens:     10,
		CompletionTokens: 5,
	}, nil
}

func (s *stubGenerator) Ready(ctx context.Context) error { return nil }
func (s *stubGenerator) Name() string                  { return "stub" }

// MockEmbedder implements retrieval.QueryEmbedder for tests.
type MockEmbedder struct {
	vecs map[string][]float32
	err  error
}

func (m *MockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := m.vecs[t]; ok {
			out[i] = v
		} else {
			out[i] = make([]float32, retrieval.EmbeddingDimensions)
		}
	}
	return out, nil
}

func (m *MockEmbedder) Health(ctx context.Context) error {
	_ = ctx
	return nil
}

func (m *MockEmbedder) Name() string { return "mock-embedder" }

// stubStorage implements knowledge.Storage for tests.
type stubStorage struct {
	objects map[string][]byte
}

func (s *stubStorage) Put(ctx context.Context, path, contentType string, data []byte) error {
	_ = ctx
	_ = contentType
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[path] = append([]byte(nil), data...)
	return nil
}

func (s *stubStorage) Get(ctx context.Context, path string) ([]byte, error) {
	_ = ctx
	d, ok := s.objects[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), d...), nil
}

func (s *stubStorage) Delete(ctx context.Context, path string) error {
	_ = ctx
	delete(s.objects, path)
	return nil
}

func (s *stubStorage) DeletePrefix(ctx context.Context, prefix string) error {
	_ = ctx
	for p := range s.objects {
		if strings.HasPrefix(p, prefix) {
			delete(s.objects, p)
		}
	}
	return nil
}
