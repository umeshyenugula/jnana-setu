package knowledge

import "context"

// Store is the persistence boundary for Module 2. HTTP handlers and business
// logic program against this interface; Postgres and memory implementations
// live behind it. Module 3 data is never touched here.
type Store interface {
	CreateDocumentWithVersion(ctx context.Context, doc Document, ver Version, outbox ProcessingRequest) (Document, Version, ProcessingRequest, error)
	GetDocument(ctx context.Context, orgID, docID string) (Document, error)
	ListDocuments(ctx context.Context, orgID string, includeInactive bool, limit, offset int) ([]Document, error)
	UpdateDocument(ctx context.Context, orgID, docID string, title *string, isActive *bool) (Document, error)
	SoftDeleteDocument(ctx context.Context, orgID, docID string) (Document, error)
	GetVersion(ctx context.Context, orgID, docID, versionID string) (Version, error)
	ListVersions(ctx context.Context, orgID, docID string) ([]Version, error)
	AddVersion(ctx context.Context, ver Version, outbox ProcessingRequest) (Version, Document, ProcessingRequest, error)
	SetProcessingStatus(ctx context.Context, orgID, docID string, from []string, to string) (Document, error)
	FindOutboxByIdempotencyKey(ctx context.Context, key string) (ProcessingRequest, bool, error)
	CreateOutbox(ctx context.Context, req ProcessingRequest) (ProcessingRequest, error)
	GetOutbox(ctx context.Context, id string) (ProcessingRequest, error)
	ListPendingOutbox(ctx context.Context, limit int) ([]ProcessingRequest, error)
	MarkOutbox(ctx context.Context, id, status string, attempts int, lastErr *string) (ProcessingRequest, error)
}
