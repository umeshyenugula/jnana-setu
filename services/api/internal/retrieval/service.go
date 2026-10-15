package retrieval

import (
	"context"
	"strings"
	"time"
)

// Config bounds retrieval orchestration. All values are env-configurable.
type Config struct {
	Rank         RankConfig
	MaxTopK      int
	EmbedTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{Rank: DefaultRankConfig(), MaxTopK: 20, EmbedTimeout: DefaultEmbedTimeout}
}

// Service orchestrates validate -> embed (local) -> tenant-scoped vector
// search -> rank. It holds no index or document state.
type Service struct {
	cfg      Config
	repo     ChunkRepository
	embedder QueryEmbedder
}

func NewService(cfg Config, repo ChunkRepository, embedder QueryEmbedder) *Service {
	if cfg.MaxTopK <= 0 {
		cfg.MaxTopK = 20
	}
	return &Service{cfg: cfg, repo: repo, embedder: embedder}
}

func validUUID(s string) bool {
	// Canonical 8-4-4-4-12 hex form; avoids a new dependency.
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// Retrieve answers one trusted query. The tenant comes from Module 6's
// server-side resolution; arbitrary browser-supplied tenant IDs are rejected
// upstream by the internal-service authentication on the HTTP boundary.
func (s *Service) Retrieve(ctx context.Context, q Query) (Result, error) {
	orgID := strings.TrimSpace(q.OrganizationID)
	if orgID == "" || !validUUID(orgID) {
		return Result{}, &ServiceError{Code: "INVALID_REQUEST", Message: "valid organizationId is required", Status: 400}
	}
	topK := q.TopK
	if topK <= 0 {
		topK = s.cfg.Rank.TopK
	}
	if topK > s.cfg.MaxTopK {
		return Result{}, &ServiceError{Code: "INVALID_REQUEST", Message: "topK exceeds maximum", Status: 400}
	}
	for _, id := range q.Filters.DocumentIDs {
		if strings.TrimSpace(id) == "" || !validUUID(id) {
			return Result{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid document filter", Status: 400}
		}
	}
	if len(q.Filters.DocumentIDs) > 50 {
		return Result{}, &ServiceError{Code: "INVALID_REQUEST", Message: "too many document filters", Status: 400}
	}
	vec, err := EmbedQuery(ctx, s.embedder, q.Text, s.cfg.EmbedTimeout)
	if err != nil {
		return Result{}, err
	}
	// Over-fetch headroom for dedupe/threshold filtering, then rank to topK.
	fetch := topK*3 + 10
	if fetch > 100 {
		fetch = 100
	}
	candidates, err := s.repo.Search(ctx, orgID, vec, fetch, q.Filters.DocumentIDs)
	if err != nil {
		if se, ok := err.(*ServiceError); ok {
			return Result{}, se
		}
		return Result{}, &ServiceError{Code: "RETRIEVAL_UNAVAILABLE", Message: "search backend unavailable", Status: 503}
	}
	cfg := s.cfg.Rank
	cfg.TopK = topK
	evidence := Rank(candidates, cfg, s.cfg.MaxTopK)
	status := StatusResultsFound
	if len(evidence) == 0 {
		status = StatusInsufficientEvidence
	}
	return Result{
		Status: status, Evidence: evidence, TopK: topK, Returned: len(evidence),
		MinSimilarity: s.cfg.Rank.MinSimilarity, Model: QueryModelName, Dimensions: EmbeddingDimensions,
	}, nil
}
