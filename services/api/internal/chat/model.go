package chat

import "time"

// Model for the Module 6 chat orchestration service. Module 6 owns the chat
// workflow, session persistence, prompt composition, and final response
// coordination. Retrieval (Module 4), generation (Module 5), and tenant
// identity (Module 1) are consumed through narrow documented interfaces.
// Retrieved text and generated text are untrusted data: never execute them
// as code and never render generated HTML directly.

// Answer statuses returned in the stable chat response.
const (
	StatusAnswered               = "answered"
	StatusInsufficientEvidence   = "insufficient_evidence"
	StatusTemporarilyUnavailable = "temporarily_unavailable"
)

// Message roles persisted for a conversation.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Persisted assistant-message statuses. "ok" carries a generated answer,
// "fallback" the configured insufficient-evidence text. Failed generations
// store nothing: no fabricated answers are ever persisted.
const (
	MessageOK       = "ok"
	MessageFallback = "fallback"
)

// Conversation binds an opaque server-issued session to one widget+tenant.
type Conversation struct {
	ID             string
	OrganizationID string
	WidgetID       string
	SessionToken   string
	Status         string
	LastMessageAt  *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ChatMessage is one persisted turn. Sources are validated Module 4
// references, never model-invented identifiers.
type ChatMessage struct {
	ID              string
	ConversationID  string
	OrganizationID  string
	Role            string
	Content         string
	Status          string
	Sources         []Source
	IdempotencyKey  *string
	CreatedAt       time.Time
}

// Source is one verified citation assembled from retrieval evidence.
type Source struct {
	DocumentID string         `json:"documentId"`
	VersionID  string         `json:"versionId"`
	ChunkID    string         `json:"chunkId"`
	Rank       int            `json:"rank"`
	Excerpt    string         `json:"excerpt"`
	SourceMeta map[string]any `json:"sourceMeta,omitempty"`
}

// ChatRequest is the body of POST /api/v1/chat. No tenant identifier is
// accepted: the tenant comes from server-side widget resolution.
type ChatRequest struct {
	WidgetID       string `json:"widgetId"`
	SessionID      string `json:"sessionId"`
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// ChatResponse is the stable Module 7 consumption contract.
type ChatResponse struct {
	SessionID string   `json:"sessionId"`
	Status    string   `json:"status"`
	Answer    string   `json:"answer"`
	Sources   []Source `json:"sources"`
}

// HistoryMessage is one turn returned by the session-history endpoint.
type HistoryMessage struct {
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	Status    string   `json:"status"`
	Sources   []Source `json:"sources"`
	CreatedAt time.Time `json:"createdAt"`
}

// ServiceError carries a stable API error code + HTTP status.
type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }

// Stable error codes for Module 7 consumption.
const (
	CodeInvalidRequest   = "INVALID_REQUEST"
	CodeWidgetNotFound   = "WIDGET_NOT_FOUND"
	CodeOriginNotAllowed = "ORIGIN_NOT_ALLOWED"
	CodeSessionNotFound  = "SESSION_NOT_FOUND"
	CodeRateLimited      = "RATE_LIMITED"
	CodeTemporarilyUnavailable = "TEMPORARILY_UNAVAILABLE"
	CodeInternalError    = "INTERNAL_ERROR"
)
