package processing

import "time"

// Job states owned by Module 3.
const (
	JobQueued     = "queued"
	JobProcessing = "processing"
	JobCompleted  = "completed"
	JobFailed     = "failed"
	JobCancelled  = "cancelled"
)

// Operations mirror the Module 2 outbox contract.
const (
	OperationIndex   = "index"
	OperationReindex = "reindex"
	OperationDelete  = "delete"
)

// Controlled error codes reported on jobs.
const (
	ErrEmptyContent      = "EMPTY_CONTENT"
	ErrMalformedContent  = "MALFORMED_CONTENT"
	ErrUnsupportedContent = "UNSUPPORTED_CONTENT"
	ErrOCRRequired       = "OCR_REQUIRED"
	ErrDocumentInactive  = "DOCUMENT_INACTIVE"
	ErrDocumentNotFound  = "DOCUMENT_NOT_FOUND"
	ErrEmbeddingFailed   = "EMBEDDING_FAILED"
	ErrStorageFailed     = "STORAGE_FAILED"
	ErrDatabaseFailed    = "DATABASE_FAILED"
	ErrCancelled         = "CANCELLED"
)

// EmbeddingDim is the fixed all-MiniLM-L6-v2 dimension; must match the schema.
const EmbeddingDim = 384

// ProcessingRequest is the handoff contract from Module 2's outbox.
type ProcessingRequest struct {
	OrganizationID string
	DocumentID     string
	VersionID      string
	Operation      string
	IdempotencyKey string
}

// Job is a durable processing job for one immutable document version.
type Job struct {
	ID              string     `json:"id"`
	OrganizationID  string     `json:"organizationId"`
	DocumentID      string     `json:"documentId"`
	VersionID       string     `json:"versionId"`
	Operation       string     `json:"operation"`
	IdempotencyKey  string     `json:"idempotencyKey"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"maxAttempts"`
	ErrorCode       *string    `json:"errorCode,omitempty"`
	ErrorMessage    *string    `json:"errorMessage,omitempty"`
	RequestedBy     *string    `json:"requestedBy,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// Chunk is one searchable unit. Tenant + document + version ids are stamped on
// every chunk; content from different tenants/versions is never combined.
type Chunk struct {
	ID          string         `json:"id"`
	OrgID       string         `json:"organizationId"`
	DocumentID  string         `json:"documentId"`
	VersionID   string         `json:"versionId"`
	Index       int            `json:"chunkIndex"`
	Text        string         `json:"text"`
	Embedding   []float32      `json:"-"`
	TokenCount  int            `json:"tokenCount"`
	SourceMeta  map[string]any `json:"sourceMeta,omitempty"`
	IsActive    bool           `json:"isActive"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// ServiceError carries a stable API error code + HTTP status.
type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }

func strPtr(s string) *string { return &s }
