package chat

import (
	"context"
	"time"
)

// Store is Module 6's persistence boundary. Memory and Postgres
// implementations live behind it; handlers and orchestration program to the
// interface. Every read and write is tenant-scoped.
type Store interface {
	CreateConversation(ctx context.Context, orgID, widgetID, sessionToken string) (Conversation, error)
	GetConversationBySession(ctx context.Context, orgID, sessionToken string) (Conversation, error)
	TouchConversation(ctx context.Context, conversationID string) error
	AddMessage(ctx context.Context, msg ChatMessage) (ChatMessage, error)
	ListMessages(ctx context.Context, orgID, conversationID string, limit int) ([]ChatMessage, error)
	FindByIdempotencyKey(ctx context.Context, conversationID, key string) (user ChatMessage, assistant *ChatMessage, found bool, err error)
	DeleteConversation(ctx context.Context, orgID, conversationID string) error
	PurgeExpired(ctx context.Context, before time.Time) (int64, error)
}
