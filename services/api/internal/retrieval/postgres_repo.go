package retrieval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements ChunkRepository against Supabase PostgreSQL by
// calling the read-only match_knowledge_chunks function (migration 0004).
// It never writes chunk or document state. Even though the service_role
// connection bypasses RLS, tenant filtering is enforced inside the function
// body via p_organization_id on every call.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
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

func (r *PostgresRepository) Search(ctx context.Context, orgID string, queryVec []float32, limit int, docIDs []string) ([]Candidate, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var docArg any
	if len(docIDs) > 0 {
		docArg = docIDs
	}
	rows, err := r.pool.Query(ctx, `
		select chunk_id, document_id, document_version_id, content_text,
		       token_count, source_meta, distance
		from public.match_knowledge_chunks($1, $2::extensions.vector, $3, $4::uuid[])`,
		orgID, vectorLiteral(queryVec), limit, docArg)
	if err != nil {
		return nil, &ServiceError{Code: "RETRIEVAL_UNAVAILABLE", Message: "search backend unavailable", Status: 503}
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var meta map[string]any
		var tokens *int
		if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.VersionID, &c.Text, &tokens, &meta, &c.Distance); err != nil {
			return nil, &ServiceError{Code: "RETRIEVAL_UNAVAILABLE", Message: "could not decode search results", Status: 503}
		}
		if tokens != nil {
			c.TokenCount = *tokens
		}
		c.SourceMeta = meta
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, &ServiceError{Code: "RETRIEVAL_UNAVAILABLE", Message: "search backend unavailable", Status: 503}
	}
	if out == nil {
		out = []Candidate{}
	}
	return out, nil
}
