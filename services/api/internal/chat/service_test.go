package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
)

type stubTenants struct {
	widgets map[string]WidgetAccess
	err     error
}

func (s *stubTenants) ResolveWidget(ctx context.Context, id string) (WidgetAccess, error) {
	_ = ctx
	if s.err != nil {
		return WidgetAccess{}, s.err
	}
	w, ok := s.widgets[id]
	if !ok {
		return WidgetAccess{}, errors.New("not found")
	}
	return w, nil
}

type stubRetriever struct {
	result retrieval.Result
	err    error
	calls  []retrieval.Query
}

func (s *stubRetriever) Retrieve(ctx context.Context, q retrieval.Query) (retrieval.Result, error) {
	_ = ctx
	s.calls = append(s.calls, q)
	if s.err != nil {
		return retrieval.Result{}, s.err
	}
	return s.result, nil
}

type stubGenerator struct {
	text  string
	err   error
	calls []inference.GenerationRequest
}

func (s *stubGenerator) Generate(ctx context.Context, req inference.GenerationRequest) (inference.GenerationResult, error) {
	_ = ctx
	s.calls = append(s.calls, req)
	if s.err != nil {
		return inference.GenerationResult{}, s.err
	}
	return inference.GenerationResult{Text: s.text, FinishReason: inference.FinishStop, Model: "qwen3-4b"}, nil
}

func testDeps() (*stubTenants, *stubRetriever, *stubGenerator, *MemoryStore) {
	tenants := &stubTenants{widgets: map[string]WidgetAccess{
		"wdg_shop": {OrganizationID: "org-a", WidgetID: "wid-a", DisplayName: "Shop", Active: true,
			AllowedOrigins: []string{"https://shop.example.com"}},
		"wdg_open": {OrganizationID: "org-a", WidgetID: "wid-open", DisplayName: "Open", Active: true},
		"wdg_other": {OrganizationID: "org-b", WidgetID: "wid-b", DisplayName: "Other", Active: true},
		"wdg_off":   {OrganizationID: "org-a", WidgetID: "wid-off", DisplayName: "Off", Active: false},
	}}
	evidence := []retrieval.Evidence{
		{Rank: 1, ChunkID: "chunk-1", DocumentID: "doc-1", VersionID: "ver-1",
			Text: "Returns are accepted within 30 days.", Similarity: 0.9, Distance: 0.1,
			TokenCount: 7, SourceMeta: map[string]any{"page": 2}},
	}
	retriever := &stubRetriever{result: retrieval.Result{
		Status: retrieval.StatusResultsFound, Evidence: evidence,
	}}
	generator := &stubGenerator{text: "You can return items within 30 days."}
	return tenants, retriever, generator, NewMemoryStore()
}

func testService(t *testing.T, tenants *stubTenants, retriever *stubRetriever, generator *stubGenerator, store *MemoryStore) *Service {
	t.Helper()
	cfg := DefaultConfig()
	cfg.RateLimitPerMin = 1000
	return NewService(cfg, tenants, retriever, generator, store)
}

func TestValidWorkflow(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	res, err := svc.Chat(context.Background(), IncomingChat{
		WidgetID: "wdg_shop", Message: "What is the return policy?",
		Origin: "https://shop.example.com", ClientIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if res.Status != StatusAnswered {
		t.Fatalf("status = %q", res.Status)
	}
	if res.Answer != "You can return items within 30 days." {
		t.Fatalf("answer = %q", res.Answer)
	}
	if res.SessionID == "" || len(res.SessionID) != 36 || res.SessionID[:4] != "ses_" {
		t.Fatalf("bad session id %q", res.SessionID)
	}
	if len(res.Sources) != 1 || res.Sources[0].DocumentID != "doc-1" || res.Sources[0].VersionID != "ver-1" || res.Sources[0].ChunkID != "chunk-1" {
		t.Fatalf("bad sources %+v", res.Sources)
	}
	if res.Sources[0].SourceMeta["page"] != 2 {
		t.Fatalf("source meta lost: %+v", res.Sources[0].SourceMeta)
	}
	// Retrieval received the trusted tenant, never a browser value.
	if len(retriever.calls) != 1 || retriever.calls[0].OrganizationID != "org-a" {
		t.Fatalf("retrieval calls: %+v", retriever.calls)
	}
	// History persisted: user + assistant turn.
	msgs, err := svc.History(context.Background(), "wdg_shop", res.SessionID, "https://shop.example.com")
	if err != nil {
		t.Fatalf("history failed: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Fatalf("history = %+v", msgs)
	}
}

func TestSessionReuseAndHistoryContext(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	first, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "First question?"})
	if err != nil {
		t.Fatalf("first chat failed: %v", err)
	}
	second, err := svc.Chat(context.Background(), IncomingChat{
		WidgetID: "wdg_shop", SessionID: first.SessionID, Message: "And then?",
	})
	if err != nil {
		t.Fatalf("second chat failed: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatal("session was not reused")
	}
	if len(generator.calls) != 2 {
		t.Fatalf("generator calls = %d", len(generator.calls))
	}
	prompt := generator.calls[1].Messages[1].Content
	if !strings.Contains(prompt, "First question?") || !strings.Contains(prompt, "And then?") {
		t.Fatalf("history missing from prompt:\n%s", prompt)
	}
}

func TestInvalidWidget(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	_, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "", Message: "Hi"})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeInvalidRequest {
		t.Fatalf("empty widget: got %v", err)
	}
	for _, widget := range []string{"wdg_missing", "wdg_off"} {
		_, err := svc.Chat(context.Background(), IncomingChat{WidgetID: widget, Message: "Hi"})
		se, ok := err.(*ServiceError)
		if !ok || se.Code != CodeWidgetNotFound || se.Status != 404 {
			t.Fatalf("widget %q: got %v", widget, err)
		}
	}
}

func TestOriginEnforcement(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	_, err := svc.Chat(context.Background(), IncomingChat{
		WidgetID: "wdg_shop", Message: "Hi", Origin: "https://evil.example.com",
	})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeOriginNotAllowed || se.Status != 403 {
		t.Fatalf("got %v", err)
	}
	// Allowed origin passes; absent origin passes (non-browser caller).
	for _, origin := range []string{"https://shop.example.com", ""} {
		if _, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Hi", Origin: origin}); err != nil {
			t.Fatalf("origin %q: got %v", origin, err)
		}
	}
	// Widget without an allow-list accepts any origin.
	if _, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_open", Message: "Hi", Origin: "https://any.example.com"}); err != nil {
		t.Fatalf("open widget: got %v", err)
	}
}

func TestCrossTenantSession(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	res, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Hi"})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	// Same session token presented to another tenant's widget.
	_, err = svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_other", SessionID: res.SessionID, Message: "Hi"})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeSessionNotFound || se.Status != 404 {
		t.Fatalf("got %v", err)
	}
	// Malformed session identifiers are not honored.
	_, err = svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", SessionID: "ses_nothex", Message: "Hi"})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeSessionNotFound {
		t.Fatalf("got %v", err)
	}
	// History is equally tenant-bound.
	_, err = svc.History(context.Background(), "wdg_other", res.SessionID, "")
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeSessionNotFound {
		t.Fatalf("history leak: %v", err)
	}
}

func TestInsufficientEvidenceFallback(t *testing.T) {
	tenants, _, generator, store := testDeps()
	empty := &stubRetriever{result: retrieval.Result{Status: retrieval.StatusInsufficientEvidence}}
	svc := testService(t, tenants, empty, generator, store)
	res, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Obscure question?"})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if res.Status != StatusInsufficientEvidence {
		t.Fatalf("status = %q", res.Status)
	}
	if res.Answer == "" || len(res.Sources) != 0 {
		t.Fatalf("bad fallback %+v", res)
	}
	if len(generator.calls) != 0 {
		t.Fatal("LLM must not be called without evidence")
	}
	msgs, _ := svc.History(context.Background(), "wdg_shop", res.SessionID, "")
	if len(msgs) != 2 || msgs[1].Status != MessageFallback {
		t.Fatalf("fallback not persisted: %+v", msgs)
	}
}

func TestGenerationFailure(t *testing.T) {
	tenants, retriever, _, store := testDeps()
	broken := &stubGenerator{err: errors.New("runtime down")}
	svc := testService(t, tenants, retriever, broken, store)
	res, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Hi", IdempotencyKey: "k1"})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeTemporarilyUnavailable || se.Status != 503 {
		t.Fatalf("got %v / %+v", err, res)
	}
	// No fabricated assistant answer persisted; only the user turn.
	msgs, _ := store.ListMessages(context.Background(), "org-a", conversationOf(t, store, "org-a"), 10)
	if len(msgs) != 1 || msgs[0].Role != RoleUser {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func conversationOf(t *testing.T, store *MemoryStore, org string) string {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for id, c := range store.conversations {
		if c.OrganizationID == org {
			return id
		}
	}
	t.Fatal("no conversation stored")
	return ""
}

func TestRetrievalFailure(t *testing.T) {
	tenants, _, generator, store := testDeps()
	down := &stubRetriever{err: errors.New("vector db down")}
	svc := testService(t, tenants, down, generator, store)
	_, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Hi"})
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeTemporarilyUnavailable {
		t.Fatalf("got %v", err)
	}
	if len(generator.calls) != 0 {
		t.Fatal("LLM must not be called when retrieval fails")
	}
}

func TestMessageLimits(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	for _, msg := range []string{"", "   ", strings.Repeat("x", DefaultConfig().MaxMessageChars+1)} {
		_, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: msg})
		if se, ok := err.(*ServiceError); !ok || se.Code != CodeInvalidRequest {
			t.Fatalf("oversized/empty message: got %v", err)
		}
	}
}

func TestIdempotentRetry(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	svc := testService(t, tenants, retriever, generator, store)
	in := IncomingChat{WidgetID: "wdg_shop", Message: "Same question?", IdempotencyKey: "idem-1"}
	first, err := svc.Chat(context.Background(), in)
	if err != nil {
		t.Fatalf("first chat failed: %v", err)
	}
	// Safe retries carry the server-issued session alongside the key.
	in.SessionID = first.SessionID
	second, err := svc.Chat(context.Background(), in)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if first.SessionID != second.SessionID || first.Status != second.Status ||
		first.Answer != second.Answer || len(first.Sources) != len(second.Sources) {
		t.Fatalf("retry response differs: %+v vs %+v", first, second)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator called %d times", len(generator.calls))
	}
	msgs, _ := svc.History(context.Background(), "wdg_shop", first.SessionID, "")
	if len(msgs) != 2 {
		t.Fatalf("duplicate messages stored: %d", len(msgs))
	}
}

func TestRateLimiting(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	cfg := DefaultConfig()
	cfg.RateLimitPerMin = 2
	svc := NewService(cfg, tenants, retriever, generator, store)
	in := IncomingChat{WidgetID: "wdg_shop", Message: "Hi", ClientIP: "9.9.9.9"}
	for i := 0; i < 2; i++ {
		if _, err := svc.Chat(context.Background(), in); err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	_, err := svc.Chat(context.Background(), in)
	if se, ok := err.(*ServiceError); !ok || se.Code != CodeRateLimited || se.Status != 429 {
		t.Fatalf("got %v", err)
	}
}

func TestDatabaseFailure(t *testing.T) {
	tenants, retriever, generator, store := testDeps()
	store.InjectFailure(errors.New("db down"))
	svc := testService(t, tenants, retriever, generator, store)
	_, err := svc.Chat(context.Background(), IncomingChat{WidgetID: "wdg_shop", Message: "Hi"})
	if se, ok := err.(*ServiceError); !ok || se.Status != 500 {
		t.Fatalf("got %v", err)
	}
}

func TestRetentionPurge(t *testing.T) {
	store := NewMemoryStore()
	old, err := store.CreateConversation(context.Background(), "org-a", "wid-a", "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	c := store.conversations[old.ID]
	c.CreatedAt = c.CreatedAt.Add(-2400 * time.Hour) // ~100 days ago
	store.conversations[old.ID] = c
	store.mu.Unlock()
	n, err := store.PurgeExpired(context.Background(), time.Now().Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("purged=%d err=%v", n, err)
	}
	if _, err := store.GetConversationBySession(context.Background(), "org-a", old.SessionToken); err == nil {
		t.Fatal("expired conversation still present")
	}
}
