package processing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// JobStore + ChunkStore form the persistence boundary. Postgres and memory
// implementations live behind these interfaces.
type JobStore interface {
	CreateJob(ctx context.Context, job Job) (Job, error)
	GetJob(ctx context.Context, id string) (Job, error)
	FindJobByIdempotencyKey(ctx context.Context, key string) (Job, bool, error)
	ClaimJob(ctx context.Context, id string) (Job, error)
	FinishJob(ctx context.Context, id, status string, errCode, errMsg *string) (Job, error)
	CancelJob(ctx context.Context, id string) (Job, error)
}

type ChunkStore interface {
	// StageChunks writes inactive chunks idempotently (same version+index
	// overwrites). Never activates.
	StageChunks(ctx context.Context, chunks []Chunk) error
	// Publish activates exactly one version's chunks for a document and
	// deactivates all other versions' chunks, atomically. On failure the
	// previously active set is untouched.
	PublishVersion(ctx context.Context, orgID, docID, versionID string) (int, error)
	// DeactivateDocument removes all active chunks for a document (delete op).
	DeactivateDocument(ctx context.Context, orgID, docID string) (int, error)
	ActiveChunks(ctx context.Context, orgID, docID string) ([]Chunk, error)
	ChunksForVersion(ctx context.Context, orgID, docID, versionID string) ([]Chunk, error)
	DeleteStaged(ctx context.Context, orgID, docID, versionID string, activeOnly bool) error
}

// ------------------------------------------------------------------ memory

type MemoryStores struct {
	mu        sync.Mutex
	jobs      map[string]Job
	byIdem    map[string]string
	chunks    map[string]Chunk // key: org/doc/version/index
	failOn    map[string]error
	failStage bool
}

func NewMemoryStores() *MemoryStores {
	return &MemoryStores{jobs: map[string]Job{}, byIdem: map[string]string{}, chunks: map[string]Chunk{}, failOn: map[string]error{}}
}

// InjectFailure makes the next call to op ("claim","finish","stage","publish","chunks") fail.
func (s *MemoryStores) InjectFailure(op string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failOn[op] = err
}

func (s *MemoryStores) popFailure(op string) error {
	if err, ok := s.failOn[op]; ok {
		delete(s.failOn, op)
		return err
	}
	return nil
}

func chunkKey(org, doc, ver string, idx int) string {
	return fmt.Sprintf("%s/%s/%s/%d", org, doc, ver, idx)
}

func (s *MemoryStores) CreateJob(ctx context.Context, job Job) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("create"); err != nil {
		return Job{}, err
	}
	if id, ok := s.byIdem[job.IdempotencyKey]; ok {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "duplicate job for idempotency key (job " + id + ")", Status: 409}
	}
	now := time.Now().UTC()
	job.CreatedAt, job.UpdatedAt = now, now
	s.jobs[job.ID] = job
	s.byIdem[job.IdempotencyKey] = job.ID
	return job, nil
}

func (s *MemoryStores) GetJob(ctx context.Context, id string) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, &ServiceError{Code: "NOT_FOUND", Message: "job not found", Status: 404}
	}
	return j, nil
}

func (s *MemoryStores) FindJobByIdempotencyKey(ctx context.Context, key string) (Job, bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(key) == "" {
		return Job{}, false, nil
	}
	id, ok := s.byIdem[key]
	if !ok {
		return Job{}, false, nil
	}
	return s.jobs[id], true, nil
}

func (s *MemoryStores) ClaimJob(ctx context.Context, id string) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("claim"); err != nil {
		return Job{}, err
	}
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, &ServiceError{Code: "NOT_FOUND", Message: "job not found", Status: 404}
	}
	if j.Status != JobQueued {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "job is not queued (status " + j.Status + ")", Status: 409}
	}
	now := time.Now().UTC()
	j.Status = JobProcessing
	j.Attempts++
	j.StartedAt = &now
	j.UpdatedAt = now
	s.jobs[id] = j
	return j, nil
}

func (s *MemoryStores) FinishJob(ctx context.Context, id, status string, errCode, errMsg *string) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("finish"); err != nil {
		return Job{}, err
	}
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, &ServiceError{Code: "NOT_FOUND", Message: "job not found", Status: 404}
	}
	now := time.Now().UTC()
	j.Status = status
	j.ErrorCode, j.ErrorMessage = errCode, errMsg
	j.UpdatedAt = now
	if status == JobCompleted || status == JobFailed || status == JobCancelled {
		j.FinishedAt = &now
	}
	s.jobs[id] = j
	return j, nil
}

func (s *MemoryStores) CancelJob(ctx context.Context, id string) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, &ServiceError{Code: "NOT_FOUND", Message: "job not found", Status: 404}
	}
	if j.Status != JobQueued {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "only queued jobs can be cancelled", Status: 409}
	}
	now := time.Now().UTC()
	j.Status = JobCancelled
	j.ErrorCode = strPtr(ErrCancelled)
	j.FinishedAt = &now
	j.UpdatedAt = now
	s.jobs[id] = j
	return j, nil
}

func (s *MemoryStores) StageChunks(ctx context.Context, chunks []Chunk) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("stage"); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, c := range chunks {
		c.IsActive = false
		c.CreatedAt = now
		s.chunks[chunkKey(c.OrgID, c.DocumentID, c.VersionID, c.Index)] = c
	}
	return nil
}

func (s *MemoryStores) PublishVersion(ctx context.Context, orgID, docID, versionID string) (int, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("publish"); err != nil {
		return 0, err
	}
	count := 0
	for k, c := range s.chunks {
		if c.OrgID != orgID || c.DocumentID != docID {
			continue
		}
		if c.VersionID == versionID {
			c.IsActive = true
			s.chunks[k] = c
			count++
		} else if c.IsActive {
			c.IsActive = false
			s.chunks[k] = c
		}
	}
	if count == 0 {
		return 0, &ServiceError{Code: "NOT_FOUND", Message: "no staged chunks for version", Status: 404}
	}
	return count, nil
}

func (s *MemoryStores) DeactivateDocument(ctx context.Context, orgID, docID string) (int, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, c := range s.chunks {
		if c.OrgID == orgID && c.DocumentID == docID && c.IsActive {
			c.IsActive = false
			s.chunks[k] = c
			count++
		}
	}
	return count, nil
}

func (s *MemoryStores) ActiveChunks(ctx context.Context, orgID, docID string) ([]Chunk, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.popFailure("chunks"); err != nil {
		return nil, err
	}
	var out []Chunk
	for _, c := range s.chunks {
		if c.OrgID == orgID && c.DocumentID == docID && c.IsActive {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []Chunk{}
	}
	return out, nil
}

func (s *MemoryStores) ChunksForVersion(ctx context.Context, orgID, docID, versionID string) ([]Chunk, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Chunk
	for _, c := range s.chunks {
		if c.OrgID == orgID && c.DocumentID == docID && c.VersionID == versionID {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []Chunk{}
	}
	return out, nil
}

func (s *MemoryStores) DeleteStaged(ctx context.Context, orgID, docID, versionID string, activeOnly bool) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, c := range s.chunks {
		if c.OrgID == orgID && c.DocumentID == docID && c.VersionID == versionID {
			if activeOnly && c.IsActive {
				delete(s.chunks, k)
			} else if !activeOnly && !c.IsActive {
				delete(s.chunks, k)
			}
		}
	}
	return nil
}
