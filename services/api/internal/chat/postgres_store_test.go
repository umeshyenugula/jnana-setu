package chat

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresStoreLive exercises the Postgres implementation against a real
// database. It requires TEST_SUPABASE_DB_URL plus migrations 0001 (identity)
// and 0005 (chat) applied, and a seeded organization + widget; skipped
// otherwise. Set TEST_CHAT_ORG_ID and TEST_CHAT_WIDGET_ID to existing rows.
func TestPostgresStoreLive(t *testing.T) {
	dsn := os.Getenv("TEST_SUPABASE_DB_URL")
	orgID := os.Getenv("TEST_CHAT_ORG_ID")
	widgetID := os.Getenv("TEST_CHAT_WIDGET_ID")
	if dsn == "" || orgID == "" || widgetID == "" {
		t.Skip("TEST_SUPABASE_DB_URL/TEST_CHAT_ORG_ID/TEST_CHAT_WIDGET_ID not set; live chat store test skipped")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool)

	conv, err := store.CreateConversation(ctx, orgID, widgetID, "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer store.DeleteConversation(ctx, orgID, conv.ID)

	key := "live-key-1"
	if _, err := store.AddMessage(ctx, ChatMessage{
		ConversationID: conv.ID, OrganizationID: orgID, Role: RoleUser,
		Content: "live ping", Status: MessageOK, Sources: []Source{},
		IdempotencyKey: &key,
	}); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if _, err := store.AddMessage(ctx, ChatMessage{
		ConversationID: conv.ID, OrganizationID: orgID, Role: RoleAssistant,
		Content: "live pong", Status: MessageOK,
		Sources: []Source{{DocumentID: "doc-1", VersionID: "ver-1", ChunkID: "c1", Rank: 1, Excerpt: "ex"}},
	}); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}
	user, reply, found, err := store.FindByIdempotencyKey(ctx, conv.ID, key)
	if err != nil || !found || reply == nil || user.Content != "live ping" || reply.Content != "live pong" {
		t.Fatalf("idempotency lookup: found=%v err=%v", found, err)
	}
	msgs, err := store.ListMessages(ctx, orgID, conv.ID, 10)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("list: n=%d err=%v", len(msgs), err)
	}
	if len(msgs[1].Sources) != 1 || msgs[1].Sources[0].ChunkID != "c1" {
		t.Fatalf("sources round-trip: %+v", msgs[1].Sources)
	}
	// Tenant scoping: wrong org sees nothing.
	if _, err := store.GetConversationBySession(ctx, "00000000-0000-0000-0000-000000000000", conv.SessionToken); err == nil {
		t.Fatal("cross-tenant conversation visible")
	}
}
