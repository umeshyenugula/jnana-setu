package retrieval

import (
	"sort"
	"strings"
)

// RankConfig tunes evidence selection. MinSimilarity is intentionally not a
// universal constant: calibrate per deployment against real client questions
// (see docs/MODULE4_RETRIEVAL.md). Similarity is cosine similarity in
// [-1,1]; it is NOT a probability of correctness.
type RankConfig struct {
	// TopK bounds the returned evidence count (default 5, max enforced).
	TopK int
	// MinSimilarity drops candidates below this cosine similarity.
	MinSimilarity float64
	// MaxEvidenceTokens caps the total token budget across returned evidence.
	MaxEvidenceTokens int
}

func DefaultRankConfig() RankConfig {
	return RankConfig{TopK: 5, MinSimilarity: 0.1, MaxEvidenceTokens: 4000}
}

func (c RankConfig) withDefaults(maxTopK int) RankConfig {
	if c.TopK <= 0 {
		c.TopK = 5
	}
	if maxTopK > 0 && c.TopK > maxTopK {
		c.TopK = maxTopK
	}
	if c.MaxEvidenceTokens <= 0 {
		c.MaxEvidenceTokens = 4000
	}
	return c
}

// Rank orders candidates by ascending distance (similarity descending) with
// stable tie handling, applies minimum-relevance filtering, drops exact
// duplicate texts (keeping the best rank), enforces top-K and the total
// token budget, and validates source metadata.
func Rank(candidates []Candidate, cfg RankConfig, maxTopK int) []Evidence {
	cfg = cfg.withDefaults(maxTopK)
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Distance == sorted[j].Distance {
			if sorted[i].ChunkID == sorted[j].ChunkID {
				return sorted[i].DocumentID < sorted[j].DocumentID
			}
			return sorted[i].ChunkID < sorted[j].ChunkID
		}
		return sorted[i].Distance < sorted[j].Distance
	})
	var out []Evidence
	seen := map[string]bool{}
	budget := 0
	for _, c := range sorted {
		sim := CosineSimilarity(c.Distance)
		if sim != sim { // NaN guard: malformed distance never becomes evidence
			continue
		}
		if sim < cfg.MinSimilarity {
			continue
		}
		key := strings.Join(strings.Fields(strings.ToLower(c.Text)), " ")
		if key == "" || seen[key] {
			continue // empty or redundant duplicate
		}
		seen[key] = true
		tokens := c.TokenCount
		if tokens <= 0 {
			tokens = estimateTokens(c.Text)
		}
		if budget+tokens > cfg.MaxEvidenceTokens && len(out) > 0 {
			break // budget protection: keep higher-ranked evidence
		}
		budget += tokens
		out = append(out, Evidence{
			Rank: len(out) + 1, ChunkID: c.ChunkID, DocumentID: c.DocumentID,
			VersionID: c.VersionID, Text: c.Text, Similarity: sim,
			Distance: c.Distance, TokenCount: tokens,
			SourceMeta: validatedMeta(c.SourceMeta),
		})
		if len(out) >= cfg.TopK {
			break
		}
	}
	if out == nil {
		out = []Evidence{}
	}
	return out
}

func estimateTokens(text string) int {
	n := len(strings.Fields(text))
	if n == 0 {
		return 1
	}
	return n
}

// validatedMeta passes through well-formed metadata and drops malformed
// values without failing the query (partial/malformed metadata handling).
func validatedMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	clean := make(map[string]any, len(meta))
	for k, v := range meta {
		if k == "" {
			continue
		}
		switch v.(type) {
		case string, float64, int, int64, bool:
			clean[k] = v
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}
