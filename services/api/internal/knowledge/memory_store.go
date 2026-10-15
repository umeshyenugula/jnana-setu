package knowledge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an isolated in-process Store for unit tests and local dev
// without a database. Behavior (idempotency, version monotonicity, tenant
// isolation) mirrors the Postgres implementation.
type MemoryStore struct {
	mu        sync.Mutex
	docs      map[string]Document
	versions  map[string]Version
	docVers   map[string][]string
	outbox    map[string]ProcessingRequest
	byIdemKey map[string]string
	failNext  map[string]error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		docs:      map[string]Document{},
		versions:  map[string]Version{},
		docVers:   map[string][]string{},
		outbox:    map[string]ProcessingRequest{},
		byIdemKey: map[string]string{},
		failNext:  map[string]error{},
	}
}

// InjectFailure makes the next call to op return err. Test-only hook used to
// simulate database failures.
func (s *MemoryStore) InjectFailure(op string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[op] = err
}

func (s *MemoryStore) popFailure(op string) error {
	if err, ok := s.failNext[op]; ok {
		delete(s.failNext, op)
		return err
	}
	return nil
}

func (s *MemoryStore) CreateDocumentWithVersion(ctx context.Context, doc Document, ver Version, outbox ProcessingRequest) (Document, Version, ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("create"); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	if _, exists := s.byIdemKey[outbox.IdempotencyKey]; exists {
		return Document{}, Version{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "duplicate request: idempotency key already used", Status: 409}
	}
	now := time.Now().UTC()
	doc.CreatedAt, doc.UpdatedAt = now, now
	ver.CreatedAt = now
	outbox.CreatedAt, outbox.UpdatedAt = now, now
	s.docs[doc.ID] = doc
	s.versions[ver.ID] = ver
	s.docVers[doc.ID] = []string{ver.ID}
	doc.CurrentVersionID = &ver.ID
	s.docs[doc.ID] = doc
	s.outbox[outbox.ID] = outbox
	s.byIdemKey[outbox.IdempotencyKey] = outbox.ID
	return doc, ver, outbox, nil
}

func (s *MemoryStore) GetDocument(ctx context.Context, orgID, docID string) (Document, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("get"); err != nil {
		return Document{}, err
	}
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	return d, nil
}

func (s *MemoryStore) ListDocuments(ctx context.Context, orgID string, includeInactive bool, limit, offset int) ([]Document, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("list"); err != nil {
		return nil, err
	}
	var out []Document
	for _, d := range s.docs {
		if d.OrganizationID != orgID || d.DeletedAt != nil {
			continue
		}
		if !includeInactive && !d.IsActive {
			continue
		}
		out = append(out, d)
	}
	// Deterministic order for tests.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.Before(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(out) {
		return []Document{}, nil
	}
	out = out[offset:]
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	if out == nil {
		out = []Document{}
	}
	return out, nil
}

func (s *MemoryStore) UpdateDocument(ctx context.Context, orgID, docID string, title *string, isActive *bool) (Document, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("update"); err != nil {
		return Document{}, err
	}
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID || d.DeletedAt != nil {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	if title != nil {
		d.Title = *title
	}
	if isActive != nil {
		d.IsActive = *isActive
		if !d.IsActive {
			d.ProcessingStatus = ProcessingPending
		}
	}
	d.UpdatedAt = time.Now().UTC()
	s.docs[docID] = d
	return d, nil
}

func (s *MemoryStore) SoftDeleteDocument(ctx context.Context, orgID, docID string) (Document, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("delete"); err != nil {
		return Document{}, err
	}
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID || d.DeletedAt != nil {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	now := time.Now().UTC()
	d.DeletedAt = &now
	d.IsActive = false
	d.UpdatedAt = now
	s.docs[docID] = d
	return d, nil
}

func (s *MemoryStore) GetVersion(ctx context.Context, orgID, docID, versionID string) (Version, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.versions[versionID]
	if !ok || v.DocumentID != docID {
		return Version{}, &ServiceError{Code: "NOT_FOUND", Message: "version not found", Status: 404}
	}
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID {
		return Version{}, &ServiceError{Code: "NOT_FOUND", Message: "version not found", Status: 404}
	}
	return v, nil
}

func (s *MemoryStore) ListVersions(ctx context.Context, orgID, docID string) ([]Version, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID {
		return nil, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	ids := s.docVers[docID]
	out := make([]Version, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.versions[id])
	}
	return out, nil
}

func (s *MemoryStore) AddVersion(ctx context.Context, ver Version, outbox ProcessingRequest) (Version, Document, ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("addVersion"); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	if _, exists := s.byIdemKey[outbox.IdempotencyKey]; exists {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "duplicate request: idempotency key already used", Status: 409}
	}
	d, ok := s.docs[ver.DocumentID]
	if !ok || d.DeletedAt != nil {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	// Enforce monotonic version number.
	max := 0
	for _, id := range s.docVers[ver.DocumentID] {
		if n := s.versions[id].VersionNumber; n > max {
			max = n
		}
	}
	if ver.VersionNumber != max+1 {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: fmt.Sprintf("expected version number %d", max+1), Status: 409}
	}
	now := time.Now().UTC()
	ver.CreatedAt = now
	outbox.CreatedAt, outbox.UpdatedAt = now, now
	s.versions[ver.ID] = ver
	s.docVers[ver.DocumentID] = append(s.docVers[ver.DocumentID], ver.ID)
	d.CurrentVersionID = &ver.ID
	d.ProcessingStatus = ProcessingPending
	d.UpdatedAt = now
	s.docs[ver.DocumentID] = d
	s.outbox[outbox.ID] = outbox
	s.byIdemKey[outbox.IdempotencyKey] = outbox.ID
	return ver, d, outbox, nil
}

func (s *MemoryStore) SetProcessingStatus(ctx context.Context, orgID, docID string, from []string, to string) (Document, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok || d.OrganizationID != orgID {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	if len(from) > 0 {
		allowed := false
		for _, f := range from {
			if d.ProcessingStatus == f {
				allowed = true
			}
		}
		if !allowed {
			return Document{}, &ServiceError{Code: "CONFLICT", Message: "invalid status transition from " + d.ProcessingStatus, Status: 409}
		}
	}
	d.ProcessingStatus = to
	d.UpdatedAt = time.Now().UTC()
	s.docs[docID] = d
	return d, nil
}

func (s *MemoryStore) FindOutboxByIdempotencyKey(ctx context.Context, key string) (ProcessingRequest, bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(key) == "" {
		return ProcessingRequest{}, false, nil
	}
	id, ok := s.byIdemKey[key]
	if !ok {
		return ProcessingRequest{}, false, nil
	}
	return s.outbox[id], true, nil
}

func (s *MemoryStore) CreateOutbox(ctx context.Context, req ProcessingRequest) (ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byIdemKey[req.IdempotencyKey]; exists {
		return ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "duplicate request: idempotency key already used", Status: 409}
	}
	now := time.Now().UTC()
	req.CreatedAt, req.UpdatedAt = now, now
	s.outbox[req.ID] = req
	s.byIdemKey[req.IdempotencyKey] = req.ID
	return req, nil
}

func (s *MemoryStore) GetOutbox(ctx context.Context, id string) (ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.outbox[id]
	if !ok {
		return ProcessingRequest{}, &ServiceError{Code: "NOT_FOUND", Message: "processing request not found", Status: 404}
	}
	return o, nil
}

func (s *MemoryStore) ListPendingOutbox(ctx context.Context, limit int) ([]ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ProcessingRequest
	for _, o := range s.outbox {
		if o.Status == "pending" {
			out = append(out, o)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []ProcessingRequest{}
	}
	return out, nil
}

func (s *MemoryStore) MarkOutbox(ctx context.Context, id, status string, attempts int, lastErr *string) (ProcessingRequest, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.outbox[id]
	if !ok {
		return ProcessingRequest{}, &ServiceError{Code: "NOT_FOUND", Message: "processing request not found", Status: 404}
	}
	o.Status = status
	o.Attempts = attempts
	_ = lastErr
	o.UpdatedAt = time.Now().UTC()
	s.outbox[id] = o
	return o, nil
}
