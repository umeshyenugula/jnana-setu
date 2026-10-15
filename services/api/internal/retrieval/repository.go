package retrieval

import (
	"context"
	"math"
	"sort"
	"sync"
)

// ChunkRepository is the read-only search contract over indexed chunks.
// Implementations must enforce tenant scoping and the documented eligibility
// rule on every query. Nothing here writes chunk or document state.
type ChunkRepository interface {
	// Search returns up to limit candidates ordered by ascending distance for
	// the given tenant. Embedding vectors are not returned.
	Search(ctx context.Context, orgID string, queryVec []float32, limit int, docIDs []string) ([]Candidate, error)
}

// ---------------------------------------------------------------- memory

// StoredChunk is a test/dev chunk with explicit lifecycle flags mirroring the
// SQL eligibility join (chunk active + document active + not deleted +
// chunk version is the document's current version).
type StoredChunk struct {
	Candidate
	OrgID          string
	Embedding      []float32
	Active         bool
	DocActive      bool
	DocDeleted     bool
	IsCurrent      bool
}

// MemoryRepository is a brute-force cosine repository for unit tests and
// offline development. Eligibility filtering mirrors match_knowledge_chunks.
type MemoryRepository struct {
	mu     sync.RWMutex
	chunks []StoredChunk
	fail   error
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{} }

// Seed replaces the stored chunk set (tests only).
func (r *MemoryRepository) Seed(chunks []StoredChunk) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunks = append([]StoredChunk(nil), chunks...)
}

// InjectFailure makes the next Search fail (database-failure tests).
func (s *MemoryRepository) InjectFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

func (r *MemoryRepository) Search(ctx context.Context, orgID string, queryVec []float32, limit int, docIDs []string) ([]Candidate, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		err := r.fail
		r.fail = nil
		return nil, err
	}
	allowed := map[string]bool{}
	for _, id := range docIDs {
		allowed[id] = true
	}
	norm := norm2(queryVec)
	var out []Candidate
	for _, c := range r.chunks {
		if c.OrgID != orgID {
			continue
		}
		if !c.Active || !c.DocActive || c.DocDeleted || !c.IsCurrent {
			continue
		}
		if len(allowed) > 0 && !allowed[c.DocumentID] {
			continue
		}
		out = append(out, Candidate{
			ChunkID: c.ChunkID, DocumentID: c.DocumentID, VersionID: c.VersionID,
			Text: c.Text, TokenCount: c.TokenCount, SourceMeta: c.SourceMeta,
			Distance: cosineDistance(queryVec, norm, c.Embedding),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Distance == out[j].Distance {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].Distance < out[j].Distance
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func norm2(v []float32) float64 {
	var s float64
	for _, f := range v {
		s += float64(f) * float64(f)
	}
	if s == 0 {
		return 0
	}
	return s
}

func cosineDistance(a []float32, aNorm float64, b []float32) float64 {
	if aNorm == 0 {
		return 1
	}
	var dot, bNorm float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
		bNorm += float64(b[i]) * float64(b[i])
	}
	if bNorm == 0 {
		return 1
	}
	// 1 - dot/(|a||b|); inputs are unit-norm in practice, formula is general.
	denom := math.Sqrt(aNorm) * math.Sqrt(bNorm)
	if denom == 0 {
		return 1
	}
	return 1 - dot/denom
}
