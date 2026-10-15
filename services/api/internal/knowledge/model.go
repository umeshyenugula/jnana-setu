package knowledge

import "time"

// Lifecycle states owned by Module 2:
//   saved/uploaded  -> processing_status=pending, is_active=true
//   awaiting processing == pending (outbox row pending)
//   inactive        -> is_active=false (deactivated, excluded from retrieval)
//   deleted         -> deleted_at set (soft delete; excluded from retrieval,
//                      storage + index cleanup coordinated via outbox)
// Module 3 reports processing/ready/failed via ReportProcessingResult and MUST
// NOT write any other column. Module 2 never writes chunks/embeddings.
const (
	ProcessingPending    = "pending"
	ProcessingProcessing = "processing"
	ProcessingReady      = "ready"
	ProcessingFailed     = "failed"
)

const (
	SourceText = "text"
	SourceFile = "file"
)

const (
	OperationIndex   = "index"
	OperationReindex = "reindex"
	OperationDelete  = "delete"
)

type Document struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organizationId"`
	CreatorUserID    string     `json:"creatorUserId"`
	Title            string     `json:"title"`
	SourceType       string     `json:"sourceType"`
	CurrentVersionID *string    `json:"currentVersionId"`
	IsActive         bool       `json:"isActive"`
	ProcessingStatus string     `json:"processingStatus"`
	DeletedAt        *time.Time `json:"deletedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type Version struct {
	ID               string    `json:"id"`
	DocumentID       string    `json:"documentId"`
	VersionNumber    int       `json:"versionNumber"`
	ContentType      string    `json:"contentType"`
	OriginalFilename *string   `json:"originalFilename,omitempty"`
	StoragePath      string    `json:"storagePath"`
	ContentSize      int64     `json:"contentSize"`
	ChecksumSHA256   *string   `json:"checksumSha256,omitempty"`
	CreatorUserID    string    `json:"creatorUserId"`
	CreatedAt        time.Time `json:"createdAt"`
}

// ProcessingRequest is the Module 3 handoff contract. It always identifies an
// IMMUTABLE version: Module 3 must fetch that exact version, never "current".
type ProcessingRequest struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	DocumentID     string    `json:"documentId"`
	VersionID      string    `json:"versionId"`
	Operation      string    `json:"operation"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// ServiceError carries a stable API error code + HTTP status.
type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }
