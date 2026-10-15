package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
)

// Config bounds the chat workflow. Every value is env-configurable.
type Config struct {
	MaxMessageChars int
	MaxRequestBytes int64
	HistoryLimit    int
	RetrievalTopK   int
	MaxPromptChars  int
	FallbackText    string
	RetentionDays   int
	RateLimitPerMin int
}

func DefaultConfig() Config {
	return Config{
		MaxMessageChars: 2000,
		MaxRequestBytes: 64 * 1024,
		HistoryLimit:    10,
		RetrievalTopK:   5,
		MaxPromptChars:  8000,
		FallbackText:    "I don't have verified information about that yet. Please contact support and we'll help you directly.",
		RetentionDays:   90,
		RateLimitPerMin: 30,
	}
}

var sessionTokenRe = regexp.MustCompile(`^ses_[0-9a-f]{32}$`)

// Service orchestrates tenant resolution, sessions, retrieval, generation,
// and persistence. It holds no index, model, or identity state of its own.
type Service struct {
	cfg       Config
	tenants   TenantResolver
	retrieve  Retriever
	generate  Generator
	store     Store
	limiter   *Limiter
}

func NewService(cfg Config, tenants TenantResolver, retrieve Retriever, generate Generator, store Store) *Service {
	return &Service{
		cfg:      cfg,
		tenants:  tenants,
		retrieve: retrieve,
		generate: generate,
		store:    store,
		limiter:  NewLimiter(cfg.RateLimitPerMin, time.Minute),
	}
}

// ChatRequest context supplied by the HTTP boundary.
type IncomingChat struct {
	WidgetID       string
	SessionID      string
	Message        string
	IdempotencyKey string
	Origin         string
	ClientIP       string
}

func newSessionToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ses_" + hex.EncodeToString(b[:]), nil
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

func originAllowed(configured []string, origin string) bool {
	if origin == "" {
		return true // non-browser callers send no Origin; tenant binding still enforced
	}
	for _, o := range configured {
		if normalizeOrigin(o) == origin {
			return true
		}
	}
	return false
}

// Chat runs one customer turn: resolve tenant, bind session, retrieve,
// generate (or fall back), persist, and respond.
func (s *Service) Chat(ctx context.Context, in IncomingChat) (ChatResponse, error) {
	widgetID := strings.TrimSpace(in.WidgetID)
	if widgetID == "" || len(widgetID) > 128 {
		return ChatResponse{}, &ServiceError{Code: CodeInvalidRequest, Message: "valid widgetId is required", Status: 400}
	}
	if !s.limiter.Allow(widgetID + "|" + in.ClientIP) {
		return ChatResponse{}, &ServiceError{Code: CodeRateLimited, Message: "too many requests; retry shortly", Status: 429}
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return ChatResponse{}, &ServiceError{Code: CodeInvalidRequest, Message: "message must not be empty", Status: 400}
	}
	if len([]rune(message)) > s.cfg.MaxMessageChars {
		return ChatResponse{}, &ServiceError{Code: CodeInvalidRequest, Message: "message exceeds maximum length", Status: 400}
	}
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID != "" && !sessionTokenRe.MatchString(sessionID) {
		return ChatResponse{}, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if len(key) > 128 {
		return ChatResponse{}, &ServiceError{Code: CodeInvalidRequest, Message: "idempotency key too long", Status: 400}
	}

	access, err := s.tenants.ResolveWidget(ctx, widgetID)
	if err != nil {
		return ChatResponse{}, &ServiceError{Code: CodeWidgetNotFound, Message: "chat is not available", Status: 404}
	}
	if !access.Active {
		return ChatResponse{}, &ServiceError{Code: CodeWidgetNotFound, Message: "chat is not available", Status: 404}
	}
	origin := normalizeOrigin(in.Origin)
	if in.Origin != "" && origin == "" {
		return ChatResponse{}, &ServiceError{Code: CodeInvalidRequest, Message: "invalid request origin", Status: 400}
	}
	if len(access.AllowedOrigins) > 0 && !originAllowed(access.AllowedOrigins, origin) {
		return ChatResponse{}, &ServiceError{Code: CodeOriginNotAllowed, Message: "website origin is not allowed", Status: 403}
	}

	var conv Conversation
	if sessionID == "" {
		token, err := newSessionToken()
		if err != nil {
			return ChatResponse{}, &ServiceError{Code: CodeInternalError, Message: "could not start session", Status: 500}
		}
		conv, err = s.store.CreateConversation(ctx, access.OrganizationID, access.WidgetID, token)
		if err != nil {
			return ChatResponse{}, coerceError(err)
		}
	} else {
		conv, err = s.store.GetConversationBySession(ctx, access.OrganizationID, sessionID)
		if err != nil {
			return ChatResponse{}, coerceError(err)
		}
		if conv.WidgetID != access.WidgetID {
			return ChatResponse{}, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
		}
	}

	if key != "" {
		if _, reply, found, err := s.store.FindByIdempotencyKey(ctx, conv.ID, key); err == nil && found && reply != nil {
			return responseFromStored(conv.SessionToken, *reply), nil
		}
	}

	var idemKey *string
	if key != "" {
		idemKey = &key
	}
	if _, err := s.store.AddMessage(ctx, ChatMessage{
		ConversationID: conv.ID, OrganizationID: conv.OrganizationID,
		Role: RoleUser, Content: message, Status: MessageOK,
		Sources: []Source{}, IdempotencyKey: idemKey,
	}); err != nil {
		return ChatResponse{}, coerceError(err)
	}

	history, err := s.conversationHistory(ctx, conv, s.cfg.HistoryLimit)
	if err != nil {
		return ChatResponse{}, coerceError(err)
	}

	result, err := s.retrieve.Retrieve(ctx, retrieval.Query{
		OrganizationID: conv.OrganizationID, Text: message, TopK: s.cfg.RetrievalTopK,
	})
	if err != nil {
		return ChatResponse{}, &ServiceError{Code: CodeTemporarilyUnavailable, Message: "chat is temporarily unavailable", Status: 503}
	}
	if result.Status == retrieval.StatusInsufficientEvidence || len(result.Evidence) == 0 {
		reply, err := s.store.AddMessage(ctx, ChatMessage{
			ConversationID: conv.ID, OrganizationID: conv.OrganizationID,
			Role: RoleAssistant, Content: s.cfg.FallbackText, Status: MessageFallback,
			Sources: []Source{},
		})
		if err != nil {
			return ChatResponse{}, coerceError(err)
		}
		_ = s.store.TouchConversation(ctx, conv.ID)
		return responseFromStored(conv.SessionToken, reply), nil
	}

	genReq := BuildPrompt(message, result.Evidence, history, s.cfg.MaxPromptChars)
	gen, err := s.generate.Generate(ctx, genReq)
	if err != nil {
		// No fabricated answer is stored on generation failure.
		return ChatResponse{}, &ServiceError{Code: CodeTemporarilyUnavailable, Message: "chat is temporarily unavailable", Status: 503}
	}
	if strings.TrimSpace(gen.Text) == "" {
		return ChatResponse{}, &ServiceError{Code: CodeTemporarilyUnavailable, Message: "chat is temporarily unavailable", Status: 503}
	}
	sources := AssembleSources(result.Evidence)
	reply, err := s.store.AddMessage(ctx, ChatMessage{
		ConversationID: conv.ID, OrganizationID: conv.OrganizationID,
		Role: RoleAssistant, Content: gen.Text, Status: MessageOK, Sources: sources,
	})
	if err != nil {
		return ChatResponse{}, coerceError(err)
	}
	_ = s.store.TouchConversation(ctx, conv.ID)
	return responseFromStored(conv.SessionToken, reply), nil
}

// conversationHistory returns up to limit prior turns (oldest first),
// excluding the just-stored current user message.
func (s *Service) conversationHistory(ctx context.Context, conv Conversation, limit int) ([]ChatMessage, error) {
	msgs, err := s.store.ListMessages(ctx, conv.OrganizationID, conv.ID, limit+1)
	if err != nil {
		return nil, err
	}
	if n := len(msgs); n > 0 && msgs[n-1].Role == RoleUser {
		msgs = msgs[:n-1]
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

func responseFromStored(sessionToken string, reply ChatMessage) ChatResponse {
	status := StatusAnswered
	if reply.Status == MessageFallback {
		status = StatusInsufficientEvidence
	}
	sources := reply.Sources
	if sources == nil {
		sources = []Source{}
	}
	return ChatResponse{
		SessionID: sessionToken, Status: status,
		Answer: reply.Content, Sources: sources,
	}
}

// History returns persisted turns for a session, bound to widget+tenant.
func (s *Service) History(ctx context.Context, widgetID, sessionID, origin string) ([]HistoryMessage, error) {
	widgetID = strings.TrimSpace(widgetID)
	if widgetID == "" || !sessionTokenRe.MatchString(strings.TrimSpace(sessionID)) {
		return nil, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	access, err := s.tenants.ResolveWidget(ctx, widgetID)
	if err != nil || !access.Active {
		return nil, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	if in := normalizeOrigin(origin); in != "" && len(access.AllowedOrigins) > 0 && !originAllowed(access.AllowedOrigins, in) {
		return nil, &ServiceError{Code: CodeOriginNotAllowed, Message: "website origin is not allowed", Status: 403}
	}
	conv, err := s.store.GetConversationBySession(ctx, access.OrganizationID, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, coerceError(err)
	}
	if conv.WidgetID != access.WidgetID {
		return nil, &ServiceError{Code: CodeSessionNotFound, Message: "session not found", Status: 404}
	}
	msgs, err := s.store.ListMessages(ctx, conv.OrganizationID, conv.ID, 100)
	if err != nil {
		return nil, coerceError(err)
	}
	out := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		sources := m.Sources
		if sources == nil {
			sources = []Source{}
		}
		out = append(out, HistoryMessage{
			Role: m.Role, Content: m.Content, Status: m.Status,
			Sources: sources, CreatedAt: m.CreatedAt,
		})
	}
	return out, nil
}

func coerceError(err error) error {
	if se, ok := err.(*ServiceError); ok {
		return se
	}
	return &ServiceError{Code: CodeInternalError, Message: "chat is temporarily unavailable", Status: 500}
}
