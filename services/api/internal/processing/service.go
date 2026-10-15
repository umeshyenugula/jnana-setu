package processing

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
)

// Authorizer is the Module 1 membership context. identity.Store satisfies it.
// Processing never manages identity; it only checks it for job operations.
type Authorizer interface {
	GetMembership(ctx context.Context, organizationID, userID string) (identity.Membership, bool, error)
	GetOrganization(ctx context.Context, id string) (identity.Organization, error)
}

// ContentSource is the documented Module 2 content-access interface.
// Implementations are read-only: they fetch the exact immutable version bytes
// plus lifecycle flags and never mutate Module 2 state.
type ContentSource interface {
	FetchVersion(ctx context.Context, orgID, docID, versionID string) (VersionContent, error)
}

// VersionContent carries one immutable version's bytes and lifecycle flags.
type VersionContent struct {
	Data             []byte
	ContentType      string
	Filename         string
	IsActive         bool
	Deleted          bool
	CurrentVersionID string
}

// StatusReporter is the documented Module 2 status-update interface.
// Only these two transitions are used; Module 3 never writes document tables.
type StatusReporter interface {
	MarkProcessing(ctx context.Context, orgID, docID string) error
	ReportResult(ctx context.Context, orgID, docID, versionID string, ok bool, detail string) error
}

// OutboxEntry mirrors one Module 2 processing-outbox row.
type OutboxEntry struct {
	ID             string
	OrgID          string
	DocID          string
	VersionID      string
	Operation      string
	IdempotencyKey string
}

// OutboxRelay is the documented Module 2 handoff reader/writer.
type OutboxRelay interface {
	ListPending(ctx context.Context, limit int) ([]OutboxEntry, error)
	MarkDispatched(ctx context.Context, id string) error
	MarkDone(ctx context.Context, id string, ok bool, detail string) error
}

// Config tunes the pipeline. All knobs are env-configurable (see config).
type Config struct {
	Chunks       ChunkConfig
	MaxAttempts  int
	EmbedBatch   int
	ProcessTimeout time.Duration
	QueueSize    int
}

func DefaultConfig() Config {
	return Config{
		Chunks: DefaultChunkConfig(), MaxAttempts: 5, EmbedBatch: 32,
		ProcessTimeout: 5 * time.Minute, QueueSize: 128,
	}
}

// Service orchestrates extraction -> chunking -> embedding -> persistence.
// Stages are separated: extract, chunk, embed, persist, report.
type Service struct {
	cfg      Config
	jobs     JobStore
	chunks   ChunkStore
	embed    Embedder
	content  ContentSource
	reporter StatusReporter
	auth     Authorizer
	outbox   OutboxRelay

	queue  chan string
	mu     sync.Mutex
	worker bool
}

func NewService(cfg Config, jobs JobStore, chunks ChunkStore, embed Embedder, content ContentSource, reporter StatusReporter, auth Authorizer, outbox OutboxRelay) *Service {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.EmbedBatch <= 0 || cfg.EmbedBatch > MaxEmbedBatch {
		cfg.EmbedBatch = 32
	}
	if cfg.ProcessTimeout <= 0 {
		cfg.ProcessTimeout = 5 * time.Minute
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	return &Service{cfg: cfg, jobs: jobs, chunks: chunks, embed: embed,
		content: content, reporter: reporter, auth: auth, outbox: outbox,
		queue: make(chan string, cfg.QueueSize)}
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---- authorization ----

func (s *Service) requireMember(ctx context.Context, orgID, userID string) error {
	m, ok, err := s.auth.GetMembership(ctx, orgID, userID)
	if err != nil {
		return &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify membership", Status: 500}
	}
	if !ok {
		return &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	_ = m
	org, err := s.auth.GetOrganization(ctx, orgID)
	if err != nil {
		return &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	if org.Status != identity.OrganizationActive {
		return &ServiceError{Code: "FORBIDDEN", Message: "organization is not active", Status: 403}
	}
	return nil
}

func (s *Service) requireWriter(ctx context.Context, orgID, userID string) error {
	m, ok, err := s.auth.GetMembership(ctx, orgID, userID)
	if err != nil {
		return &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify membership", Status: 500}
	}
	if !ok {
		return &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	if m.Role != identity.RoleOwner && m.Role != identity.RoleAdmin {
		return &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	org, err := s.auth.GetOrganization(ctx, orgID)
	if err != nil {
		return &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	if org.Status != identity.OrganizationActive {
		return &ServiceError{Code: "FORBIDDEN", Message: "organization is not active", Status: 403}
	}
	return nil
}

// ---- job operations (Task F) ----

type SubmitInput struct {
	OrganizationID string
	DocumentID     string
	VersionID      string
	Operation      string
	IdempotencyKey string
	RequestedBy    *string
}

func validOperation(op string) bool {
	return op == OperationIndex || op == OperationReindex || op == OperationDelete
}

// SubmitJob persists a job idempotently and enqueues it for the worker.
// A duplicate idempotency key returns the existing job (no duplicate work).
func (s *Service) SubmitJob(ctx context.Context, userID string, in SubmitInput) (Job, error) {
	if strings.TrimSpace(userID) == "" {
		return Job{}, &ServiceError{Code: "UNAUTHORIZED", Message: "authentication required", Status: 401}
	}
	if err := s.requireWriter(ctx, in.OrganizationID, userID); err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.VersionID) == "" {
		if in.Operation != OperationDelete || strings.TrimSpace(in.DocumentID) == "" {
			return Job{}, &ServiceError{Code: "INVALID_REQUEST", Message: "document and version are required", Status: 400}
		}
	}
	if !validOperation(in.Operation) {
		return Job{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid operation", Status: 400}
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		key = "job-" + newUUID()
	}
	if existing, ok, _ := s.jobs.FindJobByIdempotencyKey(ctx, key); ok {
		return existing, nil
	}
	now := time.Now().UTC()
	job := Job{ID: newUUID(), OrganizationID: in.OrganizationID, DocumentID: in.DocumentID,
		VersionID: in.VersionID, Operation: in.Operation, IdempotencyKey: key, Status: JobQueued,
		MaxAttempts: s.cfg.MaxAttempts, RequestedBy: in.RequestedBy, CreatedAt: now, UpdatedAt: now}
	created, err := s.jobs.CreateJob(ctx, job)
	if err != nil {
		if se, ok := err.(*ServiceError); ok && se.Code == "CONFLICT" {
			if existing, found, _ := s.jobs.FindJobByIdempotencyKey(ctx, key); found {
				return existing, nil
			}
		}
		return Job{}, err
	}
	select {
	case s.queue <- created.ID:
	default:
	}
	return created, nil
}

// SubmitRequest converts a Module 2 handoff into a job (system actor).
func (s *Service) SubmitRequest(ctx context.Context, req ProcessingRequest) (Job, error) {
	if !validOperation(req.Operation) {
		return Job{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid operation", Status: 400}
	}
	if existing, ok, _ := s.jobs.FindJobByIdempotencyKey(ctx, req.IdempotencyKey); ok {
		return existing, nil
	}
	now := time.Now().UTC()
	job := Job{ID: newUUID(), OrganizationID: req.OrganizationID, DocumentID: req.DocumentID,
		VersionID: req.VersionID, Operation: req.Operation, IdempotencyKey: req.IdempotencyKey,
		Status: JobQueued, MaxAttempts: s.cfg.MaxAttempts, CreatedAt: now, UpdatedAt: now}
	created, err := s.jobs.CreateJob(ctx, job)
	if err != nil {
		if existing, found, _ := s.jobs.FindJobByIdempotencyKey(ctx, req.IdempotencyKey); found {
			return existing, nil
		}
		return Job{}, err
	}
	select {
	case s.queue <- created.ID:
	default:
	}
	return created, nil
}

func (s *Service) GetJob(ctx context.Context, userID, jobID string) (Job, error) {
	j, err := s.jobs.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(userID) == "" {
		return Job{}, &ServiceError{Code: "UNAUTHORIZED", Message: "authentication required", Status: 401}
	}
	if err := s.requireMember(ctx, j.OrganizationID, userID); err != nil {
		return Job{}, err
	}
	return j, nil
}

// RetryJob re-queues a failed job until max attempts. Completed/cancelled jobs
// and exhausted retries are rejected.
func (s *Service) RetryJob(ctx context.Context, userID, jobID string) (Job, error) {
	j, err := s.jobs.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if err := s.requireWriter(ctx, j.OrganizationID, userID); err != nil {
		return Job{}, err
	}
	if j.Status != JobFailed {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "only failed jobs can be retried", Status: 409}
	}
	if j.Attempts >= j.MaxAttempts {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "job has exhausted its attempts", Status: 409}
	}
	requeued, err := s.jobs.FinishJob(ctx, jobID, JobQueued, nil, nil)
	if err != nil {
		return Job{}, err
	}
	select {
	case s.queue <- jobID:
	default:
	}
	return requeued, nil
}

func (s *Service) CancelJob(ctx context.Context, userID, jobID string) (Job, error) {
	j, err := s.jobs.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if err := s.requireWriter(ctx, j.OrganizationID, userID); err != nil {
		return Job{}, err
	}
	return s.jobs.CancelJob(ctx, jobID)
}

// RelayOnce pulls Module 2 outbox rows and submits one job per row, then marks
// them dispatched. Idempotent: re-running never creates duplicate jobs.
func (s *Service) RelayOnce(ctx context.Context, limit int) (int, error) {
	if s.outbox == nil {
		return 0, &ServiceError{Code: "INTERNAL_ERROR", Message: "outbox relay is not configured", Status: 500}
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	entries, err := s.outbox.ListPending(ctx, limit)
	if err != nil {
		return 0, err
	}
	relayed := 0
	for _, e := range entries {
		if _, err := s.SubmitRequest(ctx, ProcessingRequest{
			OrganizationID: e.OrgID, DocumentID: e.DocID, VersionID: e.VersionID,
			Operation: e.Operation, IdempotencyKey: e.IdempotencyKey,
		}); err != nil {
			continue
		}
		if err := s.outbox.MarkDispatched(ctx, e.ID); err != nil {
			continue
		}
		relayed++
	}
	return relayed, nil
}

// ---- pipeline ----

// Process executes one job synchronously: claim -> fetch -> extract -> chunk ->
// embed -> stage -> publish -> report. Safe to retry: staging is idempotent,
// publish is atomic, partial staged rows are cleaned on failure.
func (s *Service) Process(ctx context.Context, jobID string) (Job, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ProcessTimeout)
	defer cancel()
	job, err := s.jobs.ClaimJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if err := s.reporter.MarkProcessing(ctx, job.OrganizationID, job.DocumentID); err != nil {
		// Best effort; pipeline continues.
		_ = err
	}
	if job.Operation == OperationDelete {
		return s.processDelete(ctx, job)
	}
	return s.processIndex(ctx, job)
}

func (s *Service) processDelete(ctx context.Context, job Job) (Job, error) {
	if _, err := s.chunks.DeactivateDocument(ctx, job.OrganizationID, job.DocumentID); err != nil {
		return s.fail(ctx, job, ErrDatabaseFailed, "could not purge index: "+err.Error())
	}
	_ = s.reporter.ReportResult(ctx, job.OrganizationID, job.DocumentID, job.VersionID, true, "deleted")
	if s.outbox != nil {
		_ = s.outbox.MarkDone(ctx, job.IdempotencyKey, true, "deleted")
	}
	return s.jobs.FinishJob(ctx, job.ID, JobCompleted, nil, nil)
}

func (s *Service) processIndex(ctx context.Context, job Job) (Job, error) {
	vc, err := s.content.FetchVersion(ctx, job.OrganizationID, job.DocumentID, job.VersionID)
	if err != nil {
		if se, ok := err.(*ServiceError); ok && se.Status == 404 {
			return s.fail(ctx, job, ErrDocumentNotFound, se.Message)
		}
		return s.fail(ctx, job, ErrStorageFailed, "could not fetch version content")
	}
	if vc.Deleted || !vc.IsActive {
		// Inactive/deleted documents must not be searchable: purge any active
		// chunks and fail the job with a terminal, explicit code.
		_, _ = s.chunks.DeactivateDocument(ctx, job.OrganizationID, job.DocumentID)
		return s.fail(ctx, job, ErrDocumentInactive, "document is inactive or deleted; index purged")
	}
	extracted, err := Extract(vc.Data, vc.ContentType, vc.Filename)
	if err != nil {
		code := ErrMalformedContent
		if ee, ok := err.(*extractError); ok {
			code = ee.code
		}
		return s.fail(ctx, job, code, err.Error())
	}
	texts := ChunkText(extracted.Text, s.cfg.Chunks, SectionMapper(extracted.Sections))
	if len(texts) == 0 {
		return s.fail(ctx, job, ErrEmptyContent, "document produced no chunks")
	}
	// Embed in bounded batches (sequential: bounded memory, controlled load).
	type batchResult struct {
		vecs [][]float32
		err  error
	}
	allVecs := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += s.cfg.EmbedBatch {
		end := start + s.cfg.EmbedBatch
		if end > len(texts) {
			end = len(texts)
		}
		batch := make([]string, 0, end-start)
		for _, t := range texts[start:end] {
			batch = append(batch, t.Text)
		}
		var r batchResult
		r.vecs, r.err = s.embed.Embed(ctx, batch)
		if r.err != nil {
			_ = s.chunks.DeleteStaged(ctx, job.OrganizationID, job.DocumentID, job.VersionID, false)
			if ctx.Err() != nil {
				return s.fail(ctx, job, ErrEmbeddingFailed, "embedding timed out or cancelled")
			}
			return s.fail(ctx, job, ErrEmbeddingFailed, "embedding failed: "+r.err.Error())
		}
		allVecs = append(allVecs, r.vecs...)
	}
	if err := ValidateEmbeddings(allVecs); err != nil {
		_ = s.chunks.DeleteStaged(ctx, job.OrganizationID, job.DocumentID, job.VersionID, false)
		return s.fail(ctx, job, ErrEmbeddingFailed, "invalid embeddings: "+err.Error())
	}
	records := make([]Chunk, len(texts))
	for i, t := range texts {
		records[i] = Chunk{ID: newUUID(), OrgID: job.OrganizationID, DocumentID: job.DocumentID,
			VersionID: job.VersionID, Index: t.Index, Text: t.Text, Embedding: allVecs[i],
			TokenCount: t.TokenCount, SourceMeta: t.Meta}
	}
	if err := s.chunks.StageChunks(ctx, records); err != nil {
		_ = s.chunks.DeleteStaged(ctx, job.OrganizationID, job.DocumentID, job.VersionID, false)
		return s.fail(ctx, job, ErrDatabaseFailed, "could not stage chunks")
	}
	if _, err := s.chunks.PublishVersion(ctx, job.OrganizationID, job.DocumentID, job.VersionID); err != nil {
		// Previously active set is untouched (publish is atomic); remove the
		// staged rows so a retry starts clean, then fail.
		_ = s.chunks.DeleteStaged(ctx, job.OrganizationID, job.DocumentID, job.VersionID, false)
		return s.fail(ctx, job, ErrDatabaseFailed, "could not publish chunks")
	}
	_ = s.reporter.ReportResult(ctx, job.OrganizationID, job.DocumentID, job.VersionID, true, "indexed")
	if s.outbox != nil {
		_ = s.outbox.MarkDone(ctx, job.IdempotencyKey, true, "indexed")
	}
	return s.jobs.FinishJob(ctx, job.ID, JobCompleted, nil, nil)
}

func (s *Service) fail(ctx context.Context, job Job, code, msg string) (Job, error) {
	_ = s.reporter.ReportResult(ctx, job.OrganizationID, job.DocumentID, job.VersionID, false, code+": "+msg)
	if s.outbox != nil {
		_ = s.outbox.MarkDone(ctx, job.IdempotencyKey, false, code+": "+msg)
	}
	finished, ferr := s.jobs.FinishJob(ctx, job.ID, JobFailed, strPtr(code), strPtr(msg))
	if ferr != nil {
		return Job{}, ferr
	}
	return finished, &ServiceError{Code: code, Message: msg, Status: 500}
}

// ---- background worker ----

// Start launches n workers consuming the submit queue until ctx ends.
// Memory-bounded: the queue is fixed-size; submitters never block.
func (s *Service) Start(ctx context.Context, workers int) {
	s.mu.Lock()
	if s.worker {
		s.mu.Unlock()
		return
	}
	s.worker = true
	s.mu.Unlock()
	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-s.queue:
					_, _ = s.Process(context.Background(), id)
				}
			}
		}()
	}
}

// Health reports service liveness; Readiness additionally checks the embedder.
func (s *Service) Health() map[string]any {
	return map[string]any{"status": "ok", "service": "processing", "embedder": s.embed.Name()}
}

func (s *Service) Readiness(ctx context.Context) (map[string]any, error) {
	if err := s.embed.Health(ctx); err != nil {
		return map[string]any{"ready": false, "embedder": s.embed.Name(), "error": err.Error()}, err
	}
	return map[string]any{"ready": true, "embedder": s.embed.Name(), "model": EmbedModelName}, nil
}

// StartRelay polls the Module 2 outbox and submits jobs until ctx ends.
// This is the production handoff that turns uploads into ingestion jobs:
// without it, outbox rows sit pending forever and nothing gets indexed.
// Relay is idempotent and self-healing — failures are retried on the next
// tick and pending rows survive restarts.
func (s *Service) StartRelay(ctx context.Context, interval time.Duration, limit int) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	go func() {
		// Immediate first pass catches up rows written while we were down.
		_, _ = s.RelayOnce(context.Background(), limit)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = s.RelayOnce(context.Background(), limit)
			}
		}
	}()
}
