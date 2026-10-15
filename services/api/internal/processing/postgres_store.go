package processing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStores implements JobStore + ChunkStore against Supabase PostgreSQL.
// All statements are parameterized and context-aware; chunk publish is a
// single transaction (staging-and-publish).
type PostgresStores struct {
	pool *pgxpool.Pool
}

func NewPostgresStores(pool *pgxpool.Pool) *PostgresStores { return &PostgresStores{pool: pool} }

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}

func (s *PostgresStores) CreateJob(ctx context.Context, job Job) (Job, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
		insert into public.ingestion_jobs
		(id, organization_id, document_id, document_version_id, operation, idempotency_key, status, max_attempts, requested_by)
		values ($1,$2,$3,$4,$5,$6,'queued',$7,$8)`,
		job.ID, job.OrganizationID, job.DocumentID, job.VersionID, job.Operation, job.IdempotencyKey, job.MaxAttempts, job.RequestedBy)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return Job{}, &ServiceError{Code: "CONFLICT", Message: "duplicate job for idempotency key", Status: 409}
		}
		return Job{}, err
	}
	return s.GetJob(ctx, job.ID)
}

func (s *PostgresStores) GetJob(ctx context.Context, id string) (Job, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var j Job
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, document_id, document_version_id, operation, idempotency_key,
		       status, attempts, max_attempts, error_code, error_message, requested_by,
		       started_at, finished_at, created_at, updated_at
		from public.ingestion_jobs where id=$1`, id).Scan(
		&j.ID, &j.OrganizationID, &j.DocumentID, &j.VersionID, &j.Operation, &j.IdempotencyKey,
		&j.Status, &j.Attempts, &j.MaxAttempts, &j.ErrorCode, &j.ErrorMessage, &j.RequestedBy,
		&j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return Job{}, &ServiceError{Code: "NOT_FOUND", Message: "job not found", Status: 404}
	}
	return j, nil
}

func (s *PostgresStores) FindJobByIdempotencyKey(ctx context.Context, key string) (Job, bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	if strings.TrimSpace(key) == "" {
		return Job{}, false, nil
	}
	var id string
	if err := s.pool.QueryRow(ctx, `select id from public.ingestion_jobs where idempotency_key=$1`, key).Scan(&id); err != nil {
		return Job{}, false, nil
	}
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return Job{}, false, nil
	}
	return j, true, nil
}

func (s *PostgresStores) ClaimJob(ctx context.Context, id string) (Job, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var j Job
	err := s.pool.QueryRow(ctx, `
		update public.ingestion_jobs
		set status='processing', attempts=attempts+1, started_at=coalesce(started_at, now()), updated_at=now()
		where id=$1 and status='queued'
		returning id, organization_id, document_id, document_version_id, operation, idempotency_key,
		          status, attempts, max_attempts, error_code, error_message, requested_by,
		          started_at, finished_at, created_at, updated_at`, id).Scan(
		&j.ID, &j.OrganizationID, &j.DocumentID, &j.VersionID, &j.Operation, &j.IdempotencyKey,
		&j.Status, &j.Attempts, &j.MaxAttempts, &j.ErrorCode, &j.ErrorMessage, &j.RequestedBy,
		&j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "job is not queued", Status: 409}
	}
	return j, nil
}

func (s *PostgresStores) FinishJob(ctx context.Context, id, status string, errCode, errMsg *string) (Job, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	finished := status == JobCompleted || status == JobFailed || status == JobCancelled
	_, err := s.pool.Exec(ctx, `
		update public.ingestion_jobs
		set status=$2, error_code=$3, error_message=$4, updated_at=now(),
		    finished_at = case when $5 then now() else finished_at end
		where id=$1`, id, status, errCode, errMsg, finished)
	if err != nil {
		return Job{}, err
	}
	return s.GetJob(ctx, id)
}

func (s *PostgresStores) CancelJob(ctx context.Context, id string) (Job, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var j Job
	err := s.pool.QueryRow(ctx, `
		update public.ingestion_jobs
		set status='cancelled', error_code='CANCELLED', finished_at=now(), updated_at=now()
		where id=$1 and status='queued'
		returning id, organization_id, document_id, document_version_id, operation, idempotency_key,
		          status, attempts, max_attempts, error_code, error_message, requested_by,
		          started_at, finished_at, created_at, updated_at`, id).Scan(
		&j.ID, &j.OrganizationID, &j.DocumentID, &j.VersionID, &j.Operation, &j.IdempotencyKey,
		&j.Status, &j.Attempts, &j.MaxAttempts, &j.ErrorCode, &j.ErrorMessage, &j.RequestedBy,
		&j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return Job{}, &ServiceError{Code: "CONFLICT", Message: "only queued jobs can be cancelled", Status: 409}
	}
	return j, nil
}

func vectorLiteral(v []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%g", f)
	}
	sb.WriteByte(']')
	return sb.String()
}

func (s *PostgresStores) StageChunks(ctx context.Context, chunks []Chunk) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, c := range chunks {
		if err := ValidateEmbeddings([][]float32{c.Embedding}); err != nil {
			return &ServiceError{Code: "INVALID_REQUEST", Message: err.Error(), Status: 400}
		}
		meta := c.SourceMeta
		if meta == nil {
			meta = map[string]any{}
		}
		_, err := tx.Exec(ctx, `
			insert into public.knowledge_chunks
			(id, organization_id, document_id, document_version_id, chunk_index, content_text, embedding, token_count, source_meta, is_active)
			values ($1,$2,$3,$4,$5,$6,$7::extensions.vector,$8,$9,false)
			on conflict (organization_id, document_id, document_version_id, chunk_index)
			do update set content_text=excluded.content_text, embedding=excluded.embedding,
			              token_count=excluded.token_count, source_meta=excluded.source_meta,
			              is_active=false`,
			c.ID, c.OrgID, c.DocumentID, c.VersionID, c.Index, c.Text, vectorLiteral(c.Embedding), c.TokenCount, meta)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStores) PublishVersion(ctx context.Context, orgID, docID, versionID string) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var staged int
	if err := tx.QueryRow(ctx, `
		select count(*) from public.knowledge_chunks
		where organization_id=$1 and document_id=$2 and document_version_id=$3`,
		orgID, docID, versionID).Scan(&staged); err != nil {
		return 0, err
	}
	if staged == 0 {
		return 0, &ServiceError{Code: "NOT_FOUND", Message: "no staged chunks for version", Status: 404}
	}
	if _, err := tx.Exec(ctx, `
		update public.knowledge_chunks set is_active=false
		where organization_id=$1 and document_id=$2 and document_version_id<>$3 and is_active=true`,
		orgID, docID, versionID); err != nil {
		return 0, err
	}
	cmd, err := tx.Exec(ctx, `
		update public.knowledge_chunks set is_active=true
		where organization_id=$1 and document_id=$2 and document_version_id=$3`,
		orgID, docID, versionID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(cmd.RowsAffected()), nil
}

func (s *PostgresStores) DeactivateDocument(ctx context.Context, orgID, docID string) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	cmd, err := s.pool.Exec(ctx, `
		update public.knowledge_chunks set is_active=false
		where organization_id=$1 and document_id=$2 and is_active=true`, orgID, docID)
	if err != nil {
		return 0, err
	}
	return int(cmd.RowsAffected()), nil
}

func (s *PostgresStores) ActiveChunks(ctx context.Context, orgID, docID string) ([]Chunk, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select id, organization_id, document_id, document_version_id, chunk_index, content_text,
		       embedding::text, token_count, source_meta, is_active, created_at
		from public.knowledge_chunks
		where organization_id=$1 and document_id=$2 and is_active=true order by chunk_index`, orgID, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChunks(rows)
}

func (s *PostgresStores) ChunksForVersion(ctx context.Context, orgID, docID, versionID string) ([]Chunk, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select id, organization_id, document_id, document_version_id, chunk_index, content_text,
		       embedding::text, token_count, source_meta, is_active, created_at
		from public.knowledge_chunks
		where organization_id=$1 and document_id=$2 and document_version_id=$3 order by chunk_index`, orgID, docID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChunks(rows)
}

func (s *PostgresStores) DeleteStaged(ctx context.Context, orgID, docID, versionID string, activeOnly bool) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	q := `delete from public.knowledge_chunks where organization_id=$1 and document_id=$2 and document_version_id=$3 and is_active=false`
	if activeOnly {
		q = `delete from public.knowledge_chunks where organization_id=$1 and document_id=$2 and document_version_id=$3 and is_active=true`
	}
	_, err := s.pool.Exec(ctx, q, orgID, docID, versionID)
	return err
}

type chunkRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

func scanChunks(rows chunkRows) ([]Chunk, error) {
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		var embText string
		var meta map[string]any
		var tokenCount *int
		if err := rows.Scan(&c.ID, &c.OrgID, &c.DocumentID, &c.VersionID, &c.Index, &c.Text,
			&embText, &tokenCount, &meta, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		vec, err := parseVector(embText)
		if err != nil {
			return nil, err
		}
		c.Embedding = vec
		if tokenCount != nil {
			c.TokenCount = *tokenCount
		}
		c.SourceMeta = meta
		out = append(out, c)
	}
	return out, rows.Err()
}

func parseVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.Split(s, ",")
	vec := make([]float32, 0, len(parts))
	for _, p := range parts {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%g", &f); err != nil {
			return nil, fmt.Errorf("invalid vector literal")
		}
		vec = append(vec, float32(f))
	}
	return vec, nil
}
