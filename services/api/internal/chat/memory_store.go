package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// MemoryStore is an isolated in-process Store for unit tests and local
// development. Tenant scoping mirrors the Postgres implementation.
type MemoryStore struct {
	mu            sync.Mutex
	conversations map[string]Conversation // by ID
	bySession     map[string]string       // orgID + sessionToken -> conversation ID
	messages      map[string][]ChatMessage
	failNext      error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		conversations: map[string]Conversation{},
		bySession:     map[string]string{},
		messages:      map[string][]ChatMessage{},
	}
}

// newRowID mints a random storage identifier (not a session secret).
func newRowID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// InjectFailure makes the next store call return err (failure tests).
func (s *MemoryStore) InjectFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = err
}

func (s *MemoryStore) takeFailure() error {
	err := s.failNext
	s.failNext = nil
	return err
}

func (s *MemoryStore) CreateConversation(ctx context.Context, orgID, widgetID, sessionToken string) (Conversation, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return Conversation{}, err
	}
	now := time.Now().UTC()
	c := Conversation{
		ID: newRowID(), OrganizationID: orgID, WidgetID: widgetID,
		SessionToken: sessionToken, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	s.conversations[c.ID] = c
	s.bySession[orgID+"\x00"+sessionToken] = c.ID
	return c, nil
}

func (s *MemoryStore) GetConversationBySession(ctx context.Context, orgID, sessionToken string) (Conversation, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return Conversation{}, err
	}
	id, ok := s.bySession[orgID+"\x00"+sessionToken]
	if !ok {
		return Conversation{}, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	return s.conversations[id], nil
}

func (s *MemoryStore) TouchConversation(ctx context.Context, conversationID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	c, ok := s.conversations[conversationID]
	if !ok {
		return &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	now := time.Now().UTC()
	c.LastMessageAt = &now
	c.UpdatedAt = now
	s.conversations[conversationID] = c
	return nil
}

func (s *MemoryStore) AddMessage(ctx context.Context, msg ChatMessage) (ChatMessage, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return ChatMessage{}, err
	}
	c, ok := s.conversations[msg.ConversationID]
	if !ok || c.OrganizationID != msg.OrganizationID {
		return ChatMessage{}, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	msg.ID = newRowID()
	msg.CreatedAt = time.Now().UTC()
	if msg.Sources == nil {
		msg.Sources = []Source{}
	}
	s.messages[msg.ConversationID] = append(s.messages[msg.ConversationID], msg)
	return msg, nil
}

func (s *MemoryStore) ListMessages(ctx context.Context, orgID, conversationID string, limit int) ([]ChatMessage, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return nil, err
	}
	c, ok := s.conversations[conversationID]
	if !ok || c.OrganizationID != orgID {
		return nil, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	all := s.messages[conversationID]
	if limit > 0 && len(all) > limit {
		all = append([]ChatMessage(nil), all[len(all)-limit:]...)
	} else {
		all = append([]ChatMessage(nil), all...)
	}
	return all, nil
}

func (s *MemoryStore) FindByIdempotencyKey(ctx context.Context, conversationID, key string) (ChatMessage, *ChatMessage, bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return ChatMessage{}, nil, false, err
	}
	msgs := s.messages[conversationID]
	for i, m := range msgs {
		if m.Role == RoleUser && m.IdempotencyKey != nil && *m.IdempotencyKey == key {
			user := m
			// The assistant reply is the next assistant message after the user turn.
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].Role == RoleAssistant {
					a := msgs[j]
					return user, &a, true, nil
				}
			}
			return user, nil, true, nil
		}
	}
	return ChatMessage{}, nil, false, nil
}

func (s *MemoryStore) DeleteConversation(ctx context.Context, orgID, conversationID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	c, ok := s.conversations[conversationID]
	if !ok || c.OrganizationID != orgID {
		return &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	delete(s.conversations, conversationID)
	delete(s.messages, conversationID)
	for k, id := range s.bySession {
		if id == conversationID {
			delete(s.bySession, k)
		}
	}
	return nil
}

func (s *MemoryStore) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, c := range s.conversations {
		if c.CreatedAt.Before(before) {
			delete(s.conversations, id)
			delete(s.messages, id)
			n++
		}
	}
	for k, id := range s.bySession {
		if _, ok := s.conversations[id]; !ok {
			delete(s.bySession, k)
		}
	}
	return n, nil
}
