package chat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store against Supabase Postgres. All statements
// are parameterized and tenant-scoped; privileged credentials stay
// server-side in the pool configuration.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateConversation(ctx context.Context, orgID, widgetID, sessionToken string) (Conversation, error) {
	var c Conversation
	err := s.pool.QueryRow(ctx, `
		insert into public.chat_conversations (organization_id, widget_id, session_token)
		values ($1, $2, $3)
		returning id, organization_id, widget_id, session_token, status,
		          last_message_at, created_at, updated_at`,
		orgID, widgetID, sessionToken,
	).Scan(&c.ID, &c.OrganizationID, &c.WidgetID, &c.SessionToken, &c.Status,
		&c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Conversation{}, &ServiceError{Code: CodeInternalError, Message: "could not create conversation", Status: 500}
	}
	return c, nil
}

func (s *PostgresStore) GetConversationBySession(ctx context.Context, orgID, sessionToken string) (Conversation, error) {
	var c Conversation
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, widget_id, session_token, status,
		       last_message_at, created_at, updated_at
		from public.chat_conversations
		where organization_id = $1 and session_token = $2`,
		orgID, sessionToken,
	).Scan(&c.ID, &c.OrganizationID, &c.WidgetID, &c.SessionToken, &c.Status,
		&c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Conversation{}, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	return c, nil
}

func (s *PostgresStore) TouchConversation(ctx context.Context, conversationID string) error {
	cmd, err := s.pool.Exec(ctx, `
		update public.chat_conversations
		set last_message_at = now(), updated_at = now()
		where id = $1`, conversationID)
	if err != nil {
		return &ServiceError{Code: CodeInternalError, Message: "could not update conversation", Status: 500}
	}
	if cmd.RowsAffected() == 0 {
		return &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	return nil
}

func (s *PostgresStore) AddMessage(ctx context.Context, msg ChatMessage) (ChatMessage, error) {
	rawSources, err := json.Marshal(msg.Sources)
	if err != nil {
		return ChatMessage{}, &ServiceError{Code: CodeInternalError, Message: "could not encode sources", Status: 500}
	}
	var out ChatMessage
	var rawOut []byte
	var key *string = msg.IdempotencyKey
	err = s.pool.QueryRow(ctx, `
		insert into public.chat_messages
		    (conversation_id, organization_id, role, content, status, sources, idempotency_key)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, conversation_id, organization_id, role, content, status,
		          sources, idempotency_key, created_at`,
		msg.ConversationID, msg.OrganizationID, msg.Role, msg.Content,
		msg.Status, rawSources, key,
	).Scan(&out.ID, &out.ConversationID, &out.OrganizationID, &out.Role,
		&out.Content, &out.Status, &rawOut, &out.IdempotencyKey, &out.CreatedAt)
	if err != nil {
		return ChatMessage{}, &ServiceError{Code: CodeInternalError, Message: "could not store message", Status: 500}
	}
	out.Sources = decodeSources(rawOut)
	return out, nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, orgID, conversationID string, limit int) ([]ChatMessage, error) {
	rows, err := s.pool.Query(ctx, `
		select m.id, m.conversation_id, m.organization_id, m.role, m.content,
		       m.status, m.sources, m.idempotency_key, m.created_at
		from public.chat_messages m
		join public.chat_conversations c on c.id = m.conversation_id
		where c.id = $1 and c.organization_id = $2 and m.organization_id = $2
		order by m.created_at asc`,
		conversationID, orgID)
	if err != nil {
		return nil, &ServiceError{Code: CodeInternalError, Message: "could not list messages", Status: 500}
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var raw []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.OrganizationID, &m.Role,
			&m.Content, &m.Status, &raw, &m.IdempotencyKey, &m.CreatedAt); err != nil {
			return nil, &ServiceError{Code: CodeInternalError, Message: "could not decode messages", Status: 500}
		}
		m.Sources = decodeSources(raw)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, &ServiceError{Code: CodeInternalError, Message: "could not list messages", Status: 500}
	}
	if out == nil {
		out = []ChatMessage{}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *PostgresStore) FindByIdempotencyKey(ctx context.Context, conversationID, key string) (ChatMessage, *ChatMessage, bool, error) {
	var user ChatMessage
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		select id, conversation_id, organization_id, role, content, status,
		       sources, idempotency_key, created_at
		from public.chat_messages
		where conversation_id = $1 and idempotency_key = $2 and role = 'user'
		order by created_at asc limit 1`,
		conversationID, key,
	).Scan(&user.ID, &user.ConversationID, &user.OrganizationID, &user.Role,
		&user.Content, &user.Status, &raw, &user.IdempotencyKey, &user.CreatedAt)
	if err != nil {
		return ChatMessage{}, nil, false, nil // not found is not an error
	}
	user.Sources = decodeSources(raw)
	var reply ChatMessage
	var rawReply []byte
	err = s.pool.QueryRow(ctx, `
		select id, conversation_id, organization_id, role, content, status,
		       sources, idempotency_key, created_at
		from public.chat_messages
		where conversation_id = $1 and role = 'assistant' and created_at > $2
		order by created_at asc limit 1`,
		conversationID, user.CreatedAt,
	).Scan(&reply.ID, &reply.ConversationID, &reply.OrganizationID, &reply.Role,
		&reply.Content, &reply.Status, &rawReply, &reply.IdempotencyKey, &reply.CreatedAt)
	if err != nil {
		return user, nil, true, nil
	}
	reply.Sources = decodeSources(rawReply)
	return user, &reply, true, nil
}

func (s *PostgresStore) DeleteConversation(ctx context.Context, orgID, conversationID string) error {
	cmd, err := s.pool.Exec(ctx, `
		delete from public.chat_conversations
		where id = $1 and organization_id = $2`,
		conversationID, orgID)
	if err != nil {
		return &ServiceError{Code: CodeInternalError, Message: "could not delete conversation", Status: 500}
	}
	if cmd.RowsAffected() == 0 {
		return &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	return nil
}

func (s *PostgresStore) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	cmd, err := s.pool.Exec(ctx, `
		delete from public.chat_conversations where created_at < $1`, before)
	if err != nil {
		return 0, &ServiceError{Code: CodeInternalError, Message: "could not purge conversations", Status: 500}
	}
	return cmd.RowsAffected(), nil
}

func decodeSources(raw []byte) []Source {
	if len(raw) == 0 {
		return []Source{}
	}
	var out []Source
	if err := json.Unmarshal(raw, &out); err != nil {
		return []Source{}
	}
	if out == nil {
		return []Source{}
	}
	return out
}
